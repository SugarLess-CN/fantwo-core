// 发件池投递状态机（T-40，本项目最关键的一段）。
//
// # 为什么必须有池子
//
// QQ 官方明明支持主动消息，为什么还要自己排队？因为**用户可单方面关闭「允许主动发送」**
// （错误码 40054013），此时任何非被动消息都永久失败；而被动回复窗口只有 60 分钟。
// 管理员在后台点「同意」的那一刻，用户大概率已经半天没说话了 —— 直发必然失败，
// 还得让管理员自己看到原因再决定怎么办。池子的作用就是把这个「发不出去」变成
// 一个**有状态、可观测、可自动恢复**的积压项（docs/30 §二）。
//
// # 状态机（docs/29 §4.3）
//
//	PENDING ──成功──→ SENT（终态，写 sent_at + 回写业务表 + 追一条出站流水）
//	   │
//	   ├─窗口过期 / 次数用尽 → DEFERRED（**留在池里**，清空 reply_msg_id 等下次激活）
//	   ├─频控 / 去重 / 未知   → PENDING（attempts+1，next_retry_at 指数退避 1m/5m/15m/1h）
//	   └─永久失败 / 配置错误  → FAILED（终态，进死信，后台可人工重试拉回 PENDING）
//
// ★ 用户明确要求的语义：**DEFERRED 不是终态，绝不删行**。
// 这条消息会一直躺在池子里，直到用户下次给机器人发消息（activateWindow 把它拉回
// PENDING 并换用新的 source msg_id 重投）。只有 SENT / FAILED 才出池。
package service

import (
	"context"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/pkg/qqbot"
	"go.uber.org/zap"
)

// 发送模式标签（与 qqbot.SendMode 的取值一致，进日志用）。
const (
	qqbotModePassive   = "passive_reply"
	qqbotModeWakeup    = "wakeup"
	qqbotModeProactive = "proactive"
)

// qqbotPlan 一次投递失败后的去向（纯数据，便于单测直接断言）。
type qqbotPlan struct {
	// Status 目标状态：PENDING / DEFERRED / FAILED。
	Status string
	// NextRetryAt 下次可尝试时间（仅 PENDING 有意义）。
	NextRetryAt *time.Time
	// ClearReply 是否清空被动回复源。
	//
	// **只在窗口过期时清**：旧的 msg_id 已经不能用了，继续拿它发只会反复撞 40034005；
	// 清空后该任务在下一次投递时自动走召回/主动，等用户再说话时才重新挂上新 msg_id。
	ClearReply bool
	// Attempts 新的尝试次数。
	Attempts int
	// KindLabel / Code / Message 最后一次失败的归类、官方码、官方文案（仅供排查）。
	KindLabel string
	Code      int
	Message   string
}

// qqbotPassiveEligible 现在还能不能走被动回复。
//
// 两个条件缺一不可（docs/29 §4.1）：
//   - `reply_msg_at + 60min > now`：官方「收到消息后 60 分钟内」
//   - `msg_seq < 4`：同一条源消息最多回 4 次
//
// **不靠猜**：reply_msg_at 是入站消息的到达时间（激活窗口时写入），因此剩余窗口
// 可以直接算出来，不需要依赖任何本地状态。
func qqbotPassiveEligible(task *model.QqbotOutbox, now time.Time) bool {
	if task == nil || task.ReplyMsgID == "" || task.ReplyMsgAt == nil {
		return false
	}
	if task.MsgSeq >= QqbotMaxMsgSeq {
		return false
	}
	age := now.Sub(*task.ReplyMsgAt)
	return age >= 0 && age < QqbotPassiveWindow
}

// qqbotPlanFor 把「最后一次失败的归类」映射成池子动作。
//
// 这是一张**表**，不是一堆 if（docs/29 §4.2 与 docs/30 §二逐行对应）：
//
//	KindWindowExpired               → DEFERRED（清 reply_msg_id，等激活）
//	KindRetryable / KindUnknown     → PENDING + 退避；**到 6 次转 DEFERRED，绝不丢**
//	KindPermanent                   → FAILED（用户拒收 / 非好友 / 内容违规 / 权限不足）
//	KindConfig / KindInvalidArgument → FAILED（凭据或调用姿势错，重投必败，需人工修）
//
// 两处刻意偏离「kind 字面量」的判断，理由都写在这里：
//
//  1. **Retryable 到顶转 DEFERRED 而不是 FAILED**：频控/上游抖动不是消息本身的问题，
//     打进死信等于逼运维手动捞；留池等激活更省事，也不违反「只有 SENT/FAILED 才出池」。
//  2. **InvalidArgument 归 FAILED**：它是调用方参数错（如空内容、三种模式互斥被破坏），
//     与网络无关，重试一万次也一样，必须让人看见。
func qqbotPlanFor(e *qqbot.Error, attempts int) qqbotPlan {
	plan := qqbotPlan{Attempts: attempts}
	if e == nil {
		plan.Status = model.QqbotOutboxPending
		return plan
	}
	plan.KindLabel = e.Kind.String()
	plan.Code = e.Code
	plan.Message = e.Message

	switch e.Kind {
	case qqbot.KindWindowExpired:
		plan.Status = model.QqbotOutboxDeferred
		plan.ClearReply = true
	case qqbot.KindPermanent, qqbot.KindConfig, qqbot.KindInvalidArgument:
		plan.Status = model.QqbotOutboxFailed
	default: // KindRetryable / KindUnknown
		if attempts >= QqbotMaxAttempts {
			// 退避次数用尽：**不丢消息**，转为 DEFERRED 等用户开口（或人工重试）。
			plan.Status = model.QqbotOutboxDeferred
			return plan
		}
		plan.Status = model.QqbotOutboxPending
		next := time.Now().Add(qqbotBackoffFor(attempts))
		plan.NextRetryAt = &next
	}
	return plan
}

// DeliverOutbox 投递一条池任务（幂等 + 并发安全）。
//
// 幂等性来源：
//   - 终态（SENT / FAILED）直接返回当前状态，不发第二次
//   - 非终态先用 ClaimOutbox 做 **PENDING/DEFERRED → SENDING** 的原子状态迁移；
//     RowsAffected=0 表示别人已经抢到，本次直接退出
//
// 这样「webhook 激活」与「定时扫描」同时捞到同一条任务时，只会发一次
// （否则同一 msg_id + msg_seq 会撞 40054005 去重，甚至让用户收到两条一样的消息）。
func (s *QqbotService) DeliverOutbox(ctx context.Context, taskID string) (*dto.QqbotOutboxDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	acc := s.accessor()
	task, err := acc.FindOutbox(strings.TrimSpace(taskID))
	if err != nil {
		s.logger().Error("qqbot 读池任务失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if task == nil {
		return nil, errcode.ErrNotFound
	}

	// 终态：幂等返回（人工重试走 RetryOutbox，它会先把状态拉回 PENDING）。
	switch task.Status {
	case model.QqbotOutboxSent, model.QqbotOutboxFailed:
		return toQqbotOutboxDto(task), nil
	}

	client, cerr := s.clientOrErr()
	if cerr != nil {
		// 未启用 / 未配置：KindConfig → FAILED（死信，后台「重试」按钮可在修好配置后拉回）。
		s.logger().Warn("qqbot 客户端不可用，池任务转入死信",
			zap.String("outbox_id", task.ID), zap.String("kind", cerr.Kind.String()))
		s.applyPlan(task, qqbotPlanFor(cerr, task.Attempts+1), 0)
		return s.reloadOutbox(task.ID)
	}

	claimed, err := acc.ClaimOutbox(task.ID, []string{model.QqbotOutboxPending, model.QqbotOutboxDeferred})
	if err != nil {
		s.logger().Error("qqbot 抢占池任务失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if !claimed {
		// 已被别的投递者抢到（或正卡在 SENDING）：不重复发送，回读真实状态。
		return s.reloadOutbox(task.ID)
	}
	task.Status = model.QqbotOutboxSending

	resp, mode, seq, sendErr := s.attemptSend(client, task, s.now())
	if sendErr == nil {
		s.markSent(task, resp, mode, seq)
		s.logger().Info("qqbot 投递成功",
			zap.String("outbox_id", task.ID), zap.String("mode", mode), zap.Int("msg_seq", seq))
		return s.reloadOutbox(task.ID)
	}

	s.logger().Warn("qqbot 投递失败",
		zap.String("outbox_id", task.ID),
		zap.String("mode", mode),
		zap.String("kind", sendErr.Kind.String()),
		zap.Int("code", sendErr.Code))
	s.applyPlan(task, qqbotPlanFor(sendErr, task.Attempts+1), seq)
	return s.reloadOutbox(task.ID)
}

// attemptSend 按「被动 → 召回 → 主动」的顺序尝试一次发送。
//
// 返回 (响应, 实际使用的模式, 用掉的 msg_seq, 错误)。
//
// # 为什么被动失败后不接着试召回
//
// 被动回复失败（无论窗口过期还是内容违规）都说明**平台已经对这条消息给出结论**，
// 此时改走召回来发同一条内容：
//   - 内容违规 → 召回同样会被拒，白烧一次召回额度（30 天只有 4 个周期）；
//   - 窗口过期 → 说明 source msg_id 失效，交回上层判 DEFERRED，等用户开口重挂新 msg_id 更可靠。
//
// 召回只在「一开始就没有可用窗口」时使用 —— 那正是管理员审批的历史积压场景。
func (s *QqbotService) attemptSend(client qqbot.Client, task *model.QqbotOutbox, now time.Time) (*qqbot.SendResponse, string, int, *qqbot.Error) {
	// ① 窗口内：被动回复（送达率最高，不消耗任何主动消息额度）
	if qqbotPassiveEligible(task, now) {
		seq := task.MsgSeq + 1
		resp, err := client.SendC2C(task.OpenID, qqbot.SendRequest{
			MsgType: task.MsgType,
			Content: task.Content,
			MsgID:   task.ReplyMsgID,
			MsgSeq:  seq,
		})
		if err == nil {
			return resp, qqbotModePassive, seq, nil
		}
		return nil, qqbotModePassive, seq, asQqbotError(err, "send_c2c")
	}

	// ② 互动召回（用户对话后 30 天内，4 个周期各 1 条；周期用满返回 40034122）
	resp, err := client.SendC2C(task.OpenID, qqbot.SendRequest{
		MsgType:  task.MsgType,
		Content:  task.Content,
		IsWakeup: true,
	})
	if err == nil {
		return resp, qqbotModeWakeup, 0, nil
	}
	e := asQqbotError(err, "send_c2c")
	// 配置 / 参数类错误：换成主动模式也照样失败，直接交回上层（→ FAILED）。
	if e.Kind == qqbot.KindConfig || e.Kind == qqbot.KindInvalidArgument {
		return nil, qqbotModeWakeup, 0, e
	}
	// 召回额度用尽 / 用户拒收 → 降级到 ③，由上层按最终结果判终局。

	// ③ 普通主动（20/qpm、1000 条/天，受用户「允许主动发送」开关约束）
	resp, err = client.SendC2C(task.OpenID, qqbot.SendRequest{
		MsgType: task.MsgType,
		Content: task.Content,
	})
	if err == nil {
		return resp, qqbotModeProactive, 0, nil
	}
	return nil, qqbotModeProactive, 0, asQqbotError(err, "send_c2c")
}

// markSent 成功落地：SENT + sent_at + 追一条出站流水 + 回写业务表。
//
// 三件事都要做，少一件都会让后台看到自相矛盾的状态：
//   - 池任务状态（后台「发件池」列表）
//   - 会话流水（后台「聊天记录」—— 这条消息必须出现在对话里）
//   - 业务表（申请单的 invite_code —— 否则管理员看不到到底发出去了哪个码）
func (s *QqbotService) markSent(task *model.QqbotOutbox, resp *qqbot.SendResponse, mode string, seq int) {
	acc := s.accessor()
	now := s.now()

	sentMsgID := ""
	if resp != nil {
		sentMsgID = strings.TrimSpace(resp.ID)
	}

	fields := map[string]interface{}{
		"status":        model.QqbotOutboxSent,
		"sent_at":       now,
		"attempts":      task.Attempts + 1,
		"next_retry_at": nil,
		"last_kind":     "",
		"last_code":     0,
		"last_error":    "",
		"msg_seq":       seq,
	}
	if err := acc.UpdateOutbox(task.ID, fields); err != nil {
		s.logger().Error("qqbot 写 SENT 状态失败", zap.String("outbox_id", task.ID), zap.Error(err))
	}

	// 出站流水：让「后台聊天」看得到机器人发过什么（含召回与主动消息）。
	// 幂等性：同一条池任务重复投递不会发生（ClaimOutbox 已挡住），因此这里直接插。
	if _, err := acc.CreateMessage(&model.QqbotMessage{
		OpenID:     task.OpenID,
		Direction:  model.QqbotMsgOutbound,
		Content:    task.Content,
		MsgType:    task.MsgType,
		QqbotMsgID: sentMsgID,
		ReplyMsgID: task.ReplyMsgID,
		MsgSeq:     seq,
		SenderID:   task.SenderID,
		IsWakeup:   mode == qqbotModeWakeup,
	}); err != nil {
		s.logger().Error("qqbot 写出站流水失败", zap.String("outbox_id", task.ID), zap.Error(err))
	}

	// 业务回写：邀请码写进申请单（docs/29 §4.3 成功分支）。
	//
	// 申请单在审批那一刻就已经写入了同样的 invite_code/invite_id（管理员要立刻看到发的是哪个码），
	// 这里再写一次是**幂等兜底**：万一审批那次写库部分失败，投递成功这一条能把它补齐。
	if task.Kind == model.QqbotOutboxKindInviteCode && task.RefID != "" {
		writeBack := map[string]interface{}{
			"status":      model.QqbotAppApproved,
			"invite_code": task.InviteCode,
		}
		if err := acc.UpdateApplication(task.RefID, writeBack); err != nil {
			s.logger().Error("qqbot 回写申请单失败", zap.String("application_id", task.RefID), zap.Error(err))
		}
	}
}

// applyPlan 失败落地：按 plan 改状态，并把失败详情记在任务上（后台排查用）。
func (s *QqbotService) applyPlan(task *model.QqbotOutbox, plan qqbotPlan, usedSeq int) {
	fields := map[string]interface{}{
		"status":        plan.Status,
		"attempts":      plan.Attempts,
		"next_retry_at": plan.NextRetryAt,
		"last_kind":     plan.KindLabel,
		"last_code":     plan.Code,
		"last_error":    truncateRunes(plan.Message, 500),
	}
	if plan.ClearReply {
		fields["reply_msg_id"] = ""
		fields["reply_msg_at"] = nil
		fields["msg_seq"] = 0
	} else if usedSeq > 0 {
		// 记住了用掉的序号：下次重试自动 +1，避免撞 40054005「消息被去重」。
		fields["msg_seq"] = usedSeq
	}
	if err := s.accessor().UpdateOutbox(task.ID, fields); err != nil {
		s.logger().Error("qqbot 写失败状态失败", zap.String("outbox_id", task.ID), zap.Error(err))
	}
}

// RetryOutbox 人工重试：把 FAILED / DEFERRED 拉回 PENDING 并立即投递一次。
//
// 与定时扫描的区别：扫描只碰 PENDING；人工重试是唯一能让**死信（FAILED）**复活的入口
// （典型场景：用户之前关了主动消息，沟通后重新打开，管理员点重试就能把积压发出去）。
func (s *QqbotService) RetryOutbox(ctx context.Context, taskID string) (*dto.QqbotOutboxDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	acc := s.accessor()
	task, err := acc.FindOutbox(strings.TrimSpace(taskID))
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if task == nil {
		return nil, errcode.ErrNotFound
	}
	if task.Status == model.QqbotOutboxSent || task.Status == model.QqbotOutboxSending {
		// SENT 没有重试的意义；SENDING 正在发，交给并发保护（ClaimOutbox 会挡住）。
		return toQqbotOutboxDto(task), nil
	}

	fields := map[string]interface{}{
		"status":        model.QqbotOutboxPending,
		"attempts":      0,
		"next_retry_at": nil,
	}
	// 被动信息已被窗口过期清空时，尝试重新挂上用户最近一条消息（可能刚好在窗口内）。
	if task.ReplyMsgID == "" {
		latest := &model.QqbotOutbox{OpenID: task.OpenID}
		s.attachLatestWindow(latest)
		if latest.ReplyMsgID != "" {
			fields["reply_msg_id"] = latest.ReplyMsgID
			fields["reply_msg_at"] = latest.ReplyMsgAt
			fields["msg_seq"] = 0
		}
	}
	if err := acc.UpdateOutbox(task.ID, fields); err != nil {
		return nil, errcode.ErrInternal
	}
	return s.DeliverOutbox(ctx, task.ID)
}

// reloadOutbox 投递结束后回读真实状态（不凭内存对象编造返回值）。
func (s *QqbotService) reloadOutbox(id string) (*dto.QqbotOutboxDto, *errcode.Err) {
	task, err := s.accessor().FindOutbox(id)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if task == nil {
		return nil, errcode.ErrNotFound
	}
	return toQqbotOutboxDto(task), nil
}

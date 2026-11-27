// QQ 官方机器人业务服务（T-40）。
//
// # 文件分工
//
//	qqbot_service.go  服务装配、配置读取、客户端惰性构造、webhook 入站与窗口激活
//	qqbot_deliver.go  发件池状态机（DeliverOutbox）——**本项目最关键的一段**
//	qqbot_admin.go    后台侧：申请审批 / 会话 / 消息 / 发件池 / 健康检查
//	qqbot_worker.go   asynq 定时扫描（PENDING 到期重投）
//	qqbot_data.go     数据访问接口 + gorm 实现（测试注入内存桩）
//
// # 职责边界（与 SDK 的分工）
//
//	SDK（pkg/qqbot）  发消息的 HTTP、access_token 缓存、webhook 验签、错误码三分类
//	本文件            谁该收消息、发什么内容、发不出去怎么办（池子状态机）
//
// **业务侧不认识任何官方错误码**，只看 qqbot.ErrorKind（docs/29 §4.2 的核心结论）。
//
// # 三条硬约束（做错等于需求没实现）
//
//  1. `DEFERRED` 不是终态：消息留在池里等用户下次开口激活窗口，**绝不删行**
//  2. webhook 必须 3 秒内返回：handler 只验签 + 落库，窗口激活与重投走异步
//  3. 审批与后台发言都**只建池任务**，不直接调 SDK（docs/30 §一）
package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/pkg/qqbot"
	"go.uber.org/zap"
)

// 时效与额度常量（来源 docs/29 §4.1，改之前先看那节的能力矩阵）。
const (
	// QqbotPassiveWindow 被动回复窗口：收到消息后 60 分钟。
	QqbotPassiveWindow = 60 * time.Minute
	// QqbotMaxMsgSeq 同一条源消息的被动回复次数上限（msg_seq 1..4）。
	QqbotMaxMsgSeq = 4
	// QqbotMaxAttempts 频控重试次数上限。**到顶不是丢弃**，而是转 DEFERRED 留池。
	QqbotMaxAttempts = 6
	// QqbotActivateBatch 单次窗口激活最多捞多少条 DEFERRED。
	QqbotActivateBatch = 20
	// QqbotScanBatch 定时扫描单批条数。
	QqbotScanBatch = 50
	// QqbotSendingTimeout 卡在 SENDING 超过它视为进程崩溃残留，扫描时拉回 PENDING。
	QqbotSendingTimeout = 5 * time.Minute
	// QqbotChatContentMax 后台消息与自动回复的最大长度（官方单条上限远大于此，
	// 这里取保守值：拦在本地比让平台返回 40054007「消息长度超限」更好排查）。
	QqbotChatContentMax = 2000
)

// qqbotDefaultInviteCommand / qqbotDefaultAutoReply 配置缺失时的兜底。
const (
	qqbotDefaultInviteCommand = "邀请码"
	qqbotDefaultAutoReply     = "已收到你的邀请码申请，管理员审核后会把邀请码发给你。"
)

// qqbotBackoff 频控退避档位：1m / 5m / 15m / 1h，超过 4 次后恒 1h（docs/29 §4.3）。
var qqbotBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// qqbotBackoffFor 第 attempt 次（1 起）失败后应等多久再试。
func qqbotBackoffFor(attempt int) time.Duration {
	if attempt <= 0 {
		return qqbotBackoff[0]
	}
	if attempt > len(qqbotBackoff) {
		return qqbotBackoff[len(qqbotBackoff)-1]
	}
	return qqbotBackoff[attempt-1]
}

// QqbotService QQ 官方机器人业务服务。
//
// data / now / async / client 四个字段是**为可测性留的接缝**（生产路径不感知）：
//   - data：DB 访问（测试注入内存桩，见 qqbot_data.go 包注释）
//   - now：时间源（窗口判定是纯时间逻辑，必须能控制）
//   - async：异步执行器（默认起 goroutine；测试注入同步执行，避免 flaky）
//   - client：SDK 客户端（生产按配置惰性构造；测试注入脚本化桩）
type QqbotService struct {
	d     *Deps
	data  qqbotDataAccess
	now   func() time.Time
	async func(func())

	mu     sync.Mutex
	client qqbot.Client
	// clientSig 构造客户端所用的配置签名：配置改了（运维在后台改 appid/secret）就重建。
	clientSig string
	// clientInjected 直接注入的客户端（测试/联调），不参与签名比对。
	clientInjected bool
}

// NewQqbotService 构造。
//
// d.Cfg 是配置读取入口（进程级 ConfigService：一级缓存 → Redis → DB），
// d.Cfg 为 nil 时所有配置读默认值 → 等价「未启用即降级」。
func NewQqbotService(d *Deps) *QqbotService {
	return &QqbotService{
		d:     d,
		now:   time.Now,
		async: func(f func()) { go f() },
	}
}

// SetClient 直接注入 SDK 客户端（联调/测试用）。
//
// 生产不调用它：客户端由配置惰性构造（clientOrErr），
// 这样运维改了 DB 里的 appid/secret 不必重启进程。
func (s *QqbotService) SetClient(c qqbot.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = c
	s.clientSig = ""
	s.clientInjected = c != nil
}

// logger 日志出口（Deps.Logger 恒非 nil，见 NewDeps）。
func (s *QqbotService) logger() *zap.Logger {
	if s.d == nil || s.d.Logger == nil {
		return zap.NewNop()
	}
	return s.d.Logger
}

// ---------- 配置读取 ----------

// enabled 总开关。**默认 false**：填了凭据不等于想开，webhook 是公网暴露面。
func (s *QqbotService) enabled() bool {
	if s.d == nil || s.d.Cfg == nil {
		return false
	}
	return s.d.Cfg.GetBool(qqbot.CfgEnabled, false)
}

// cfgString 读一个字符串配置（seeds 已写入默认值，这里仍给 def 兜底）。
func (s *QqbotService) cfgString(key, def string) string {
	if s.d == nil || s.d.Cfg == nil {
		return def
	}
	if v := s.d.Cfg.GetString(key, def); v != "" {
		return v
	}
	return def
}

// sdkConfig 组装 SDK 所需配置（键名一律引用 qqbot.CfgXxx 常量，不裸写字符串）。
func (s *QqbotService) sdkConfig() qqbot.Config {
	return qqbot.Config{
		APIBase:   strings.TrimSpace(s.cfgString(qqbot.CfgAPIBase, qqbot.DefaultAPIBase)),
		TokenURL:  strings.TrimSpace(s.cfgString(qqbot.CfgTokenURL, qqbot.DefaultTokenURL)),
		AppID:     strings.TrimSpace(s.cfgString(qqbot.CfgAppID, "")),
		AppSecret: strings.TrimSpace(s.cfgString(qqbot.CfgAppSecret, "")),
		BotSecret: strings.TrimSpace(s.cfgString(qqbot.CfgBotSecret, "")),
	}
}

// inviteCommand 识别「邀请码申请」的关键字。
//
// 空关键字返回空串（= 永不命中）而不是兜底成「邀请码」：
// 运维把它清空往往是想临时关掉自动建单，此时把所有消息都当申请是更坏的结果。
func (s *QqbotService) inviteCommand() string {
	raw := s.cfgString(qqbot.CfgInviteCommand, qqbotDefaultInviteCommand)
	return strings.TrimSpace(raw)
}

// autoReplyText 收到申请时的自动回复文案（空串表示不回）。
func (s *QqbotService) autoReplyText() string {
	return strings.TrimSpace(s.cfgString(qqbot.CfgInviteAutoReply, qqbotDefaultAutoReply))
}

// isInviteCommand 内容是否命中申请关键字（包含即命中，大小写不敏感）。
func (s *QqbotService) isInviteCommand(content string) bool {
	cmd := s.inviteCommand()
	text := strings.TrimSpace(content)
	if cmd == "" || text == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(cmd))
}

// ---------- SDK 客户端 ----------

// asQqbotError 把任意 error 归一成 *qqbot.Error（取 Kind 用）。
//
// 非 SDK 错误按 KindUnknown 处理（保守：Unknown 归到「可重试」一侧，
// 宁可多试几次也不要把一次网络抖动误判成永久失败把消息打进死信）。
func asQqbotError(err error, op string) *qqbot.Error {
	if err == nil {
		return nil
	}
	var e *qqbot.Error
	if errors.As(err, &e) && e != nil {
		return e
	}
	if errors.Is(err, qqbot.ErrNotConfigured) {
		return &qqbot.Error{Kind: qqbot.KindConfig, Op: op, Err: err}
	}
	return &qqbot.Error{Kind: qqbot.KindUnknown, Op: op, Err: err}
}

// clientOrErr 返回可用的 SDK 客户端。
//
// 返回 *qqbot.Error（而不是 errcode）是刻意的：发件池要按 Kind 决定去向
// （未配置 → KindConfig → FAILED 进死信），走 errcode 反而要再翻译一次。
//
// 配置签名变化即重建客户端（运维在后台改 appid 不必重启进程），
// 与 Searcher 的「配置签名 → 惰性重建」同口径。
func (s *QqbotService) clientOrErr() (qqbot.Client, *qqbot.Error) {
	if !s.enabled() {
		return nil, &qqbot.Error{Kind: qqbot.KindConfig, Op: "qqbot.disabled", Err: qqbot.ErrNotConfigured}
	}
	cfg := s.sdkConfig()
	sig := cfg.APIBase + "|" + cfg.TokenURL + "|" + cfg.AppID + "|" + cfg.AppSecret + "|" + cfg.BotSecret

	s.mu.Lock()
	if s.client != nil && (s.clientInjected || s.clientSig == sig) {
		c := s.client
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	c, err := qqbot.New(cfg)
	if err != nil {
		// 不缓存失败结果：配置刚被补全时下一条消息应立即能发。
		s.mu.Lock()
		s.client, s.clientSig = nil, ""
		s.mu.Unlock()
		return nil, asQqbotError(err, "qqbot.new")
	}
	s.mu.Lock()
	s.client, s.clientSig = c, sig
	s.mu.Unlock()
	return c, nil
}

// ---------- webhook 入站 ----------

// mapWebhookErr webhook 错误 → 业务错误码。
//
// 故意对「签名不对」返回 401（而不是 200 吞掉）：伪造请求必须被明确拒绝，
// 而平台重推机制只在非 2xx 时生效 —— 真的漏收了平台会自动补，不会丢事件。
func mapWebhookErr(err error) *errcode.Err {
	if errors.Is(err, qqbot.ErrSignatureInvalid) {
		return errcode.ErrUnauthorized
	}
	e := asQqbotError(err, "parse_webhook")
	switch e.Kind {
	case qqbot.KindConfig:
		// bot_secret 缺失 = 无法验签。返回 503 而不是 200：让平台重推而不是静默丢事件。
		return errcode.ErrDependencyUnavailable
	default:
		return errcode.ErrBadRequest
	}
}

// HandleWebhook 处理平台回调（地址验证 + 业务事件）。
//
// 三态返回（与 SDK 的 ParseWebhook 对齐）：
//   - op=13 地址验证 → 返回 validation，handler **原样**回写（裸 JSON，不套统一响应）
//   - op=0  业务事件 → 返回 (nil, nil)，handler 回 200 空体
//   - 出错 → 返回 errcode，handler 按它回非 2xx
//
// # 为什么只做「验签 + 落库」
//
// 平台要求 3 秒内返回。落库是几百微秒的事，但「激活窗口 → 逐条重投 → 自动回复」
// 是 N 次上游 HTTP，必然超时。所以重活全部丢给 s.async，handler 拿到 200 就走。
// 幂等由 msg_id 唯一索引兜住：异步部分就算执行两次也不会重复发送
// （重复的那次会命中 ClaimOutbox 的 status 条件直接退出）。
func (s *QqbotService) HandleWebhook(ctx context.Context, body []byte, header http.Header) (*qqbot.ValidationResponse, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	client, cerr := s.clientOrErr()
	if cerr != nil {
		return nil, mapWebhookErr(cerr)
	}

	payload, validation, err := client.ParseWebhook(body, header)
	if err != nil {
		s.logger().Warn("qqbot webhook 解析/验签失败",
			zap.String("kind", asQqbotError(err, "parse_webhook").Kind.String()))
		return nil, mapWebhookErr(err)
	}
	if validation != nil {
		s.logger().Info("qqbot 回调地址验证")
		return validation, nil
	}

	switch payload.Type {
	case qqbot.EventC2CMessageCreate:
		var msg qqbot.C2CMessage
		if e := payload.DecodeData(&msg); e != nil {
			return nil, errcode.ErrBadRequest
		}
		if strings.TrimSpace(msg.ID) == "" || strings.TrimSpace(msg.Author.OpenID) == "" {
			return nil, errcode.ErrBadRequest
		}
		isNew, isApplication, e := s.ingestInbound(&msg)
		if e != nil {
			return nil, e
		}
		if !isNew {
			// 重复推送：什么都不用做（消息已在库、申请单已建、窗口早已被上一次激活过）。
			s.logger().Debug("qqbot 重复推送已忽略", zap.String("msg_id", msg.ID))
			return nil, nil
		}
		s.async(func() { s.afterInbound(context.Background(), &msg, isApplication) })
		return nil, nil

	default:
		// 群聊 / 加好友 / 按钮回调本轮不做（docs/29 §八 明确排除）。
		// 记录类型后直接 200：返回非 2xx 会让平台一直重推我们不打算处理的事件。
		s.logger().Info("qqbot 未处理的事件类型", zap.String("type", payload.Type))
		return nil, nil
	}
}

// ingestInbound 落库入站消息 + （若命中关键字）建申请单。
//
// 两步都幂等，且**申请单无论消息是否已存在都要尝试建**：
// 万一上一次在「建消息」与「建申请」之间崩溃，重推能让申请单补上；
// 而两条插入都是 ON CONFLICT DO NOTHING，多试不会产生第二条。
func (s *QqbotService) ingestInbound(msg *qqbot.C2CMessage) (isNew bool, isApplication bool, e *errcode.Err) {
	acc := s.accessor()
	content := strings.TrimSpace(msg.Content)
	openID := strings.TrimSpace(msg.Author.OpenID)

	inserted, err := acc.CreateMessage(&model.QqbotMessage{
		OpenID:     openID,
		Direction:  model.QqbotMsgInbound,
		Content:    content,
		MsgType:    qqbot.MsgTypeText,
		QqbotMsgID: strings.TrimSpace(msg.ID),
	})
	if err != nil {
		s.logger().Error("qqbot 落库入站消息失败", zap.Error(err))
		return false, false, errcode.ErrInternal
	}

	isApplication = s.isInviteCommand(content)
	if isApplication {
		if _, err := acc.CreateApplication(&model.QqbotInviteApplication{
			OpenID:      openID,
			UnionOpenID: strings.TrimSpace(msg.Author.UnionOpenID),
			QqbotMsgID:  strings.TrimSpace(msg.ID),
			Content:     content,
			Status:      model.QqbotAppPending,
		}); err != nil {
			s.logger().Error("qqbot 建申请单失败", zap.Error(err))
			return inserted, isApplication, errcode.ErrInternal
		}
	}
	return inserted, isApplication, nil
}

// afterInbound 异步侧：激活窗口 → 重投 DEFERRED → （首次申请）自动回复。
//
// 独立成函数并自带 recover：这里跑在 goroutine 里，panic 会带走整个进程。
func (s *QqbotService) afterInbound(ctx context.Context, msg *qqbot.C2CMessage, isApplication bool) {
	defer func() {
		if r := recover(); r != nil {
			s.logger().Error("qqbot 入站异步处理 panic", zap.Any("panic", r))
		}
	}()
	if e := s.activateWindow(ctx, msg, isApplication); e != nil {
		s.logger().Warn("qqbot 窗口激活失败", zap.Int("status_code", e.BizCode))
	}
}

// activateWindow 用户开口 → 激活该 openid 的全部 DEFERRED 并立即重投。
//
// ★ 这是「消息不丢」的兑现处（docs/29 §4.3 状态机左上那条回流箭头）：
// DEFERRED 行不会因为窗口过期被删除，只会在用户再次发消息时被拉回 PENDING，
// 并把被动回复源消息换成**这一条**新消息（旧 msg_id 已过期，只能用新的）。
//
// # msg_seq 怎么分配
//
// 同一条源消息最多被动回复 4 次（msg_seq 1→4），且 **相同 msg_id + msg_seq 重复发必失败**
// （40054005）。所以激活时按顺序给每个任务分一个「已用序号」槽位：
//
//	自动回复 slot=0（投递时用 msg_seq 1）
//	DEFERRED[0] slot=1（msg_seq 2）…
//
// 超过 4 条的任务清空被动信息，投递时改走召回/普通主动（不浪费被动位）。
func (s *QqbotService) activateWindow(ctx context.Context, msg *qqbot.C2CMessage, isApplication bool) *errcode.Err {
	acc := s.accessor()
	openID := strings.TrimSpace(msg.Author.OpenID)
	msgID := strings.TrimSpace(msg.ID)
	now := s.now()

	slot := 0
	var autoReplyID string
	if isApplication {
		if content := s.autoReplyText(); content != "" {
			task := &model.QqbotOutbox{
				OpenID:     openID,
				Kind:       model.QqbotOutboxKindAutoReply,
				Content:    content,
				MsgType:    qqbot.MsgTypeText,
				Status:     model.QqbotOutboxPending,
				ReplyMsgID: msgID,
				ReplyMsgAt: &now,
				MsgSeq:     0,
			}
			if err := acc.CreateOutbox(task); err != nil {
				s.logger().Error("qqbot 建自动回复任务失败", zap.Error(err))
			} else {
				autoReplyID = task.ID
				slot = 1
			}
		}
	}

	// 1) 激活 DEFERRED：换成新的被动回复源消息，状态回 PENDING，退避计数归零。
	deferred, err := acc.ListOutboxByStatuses(openID, []string{model.QqbotOutboxDeferred}, QqbotActivateBatch)
	if err != nil {
		s.logger().Error("qqbot 查询 DEFERRED 任务失败", zap.Error(err))
		return errcode.ErrInternal
	}
	activated := make([]string, 0, len(deferred))
	for _, t := range deferred {
		fields := map[string]interface{}{
			"status":        model.QqbotOutboxPending,
			"attempts":      0,
			"next_retry_at": nil,
			"last_kind":     "",
		}
		if slot < QqbotMaxMsgSeq {
			fields["reply_msg_id"] = msgID
			fields["reply_msg_at"] = now
			fields["msg_seq"] = slot
		} else {
			// 被动位用尽：清空被动信息 → 投递时走召回/主动（消息照样不丢）。
			fields["reply_msg_id"] = ""
			fields["reply_msg_at"] = nil
			fields["msg_seq"] = 0
		}
		slot++
		if err := acc.UpdateOutbox(t.ID, fields); err != nil {
			s.logger().Error("qqbot 激活 DEFERRED 失败", zap.String("outbox_id", t.ID), zap.Error(err))
			continue
		}
		activated = append(activated, t.ID)
	}

	// 2) 立即重投：自动回复优先（用户刚发完申请，先给他「已收到」的反馈），
	//    再投历史积压。逐个串行：同一 msg_seq 并发发会撞去重（40054005）。
	if autoReplyID != "" {
		if _, e := s.DeliverOutbox(ctx, autoReplyID); e != nil {
			s.logger().Warn("qqbot 自动回复投递未成功（已留池）", zap.String("outbox_id", autoReplyID))
		}
	}
	for _, id := range activated {
		if _, e := s.DeliverOutbox(ctx, id); e != nil {
			s.logger().Warn("qqbot 激活重投未成功（已留池）", zap.String("outbox_id", id))
		}
	}
	if len(activated) > 0 {
		s.logger().Info("qqbot 窗口激活完成",
			zap.String("open_id", openID), zap.Int("activated", len(activated)))
	}
	return nil
}

// attachLatestWindow 给新建的池任务挂上「最近一条入站消息」作为被动回复源。
//
// 用途：审批 / 后台发言时若用户 60 分钟内说过话，消息可以立刻以**被动回复**送达
// （送达率最高）；否则留空 → 投递时自动降级到召回/主动。
func (s *QqbotService) attachLatestWindow(task *model.QqbotOutbox) {
	if task == nil || task.OpenID == "" {
		return
	}
	last, err := s.accessor().LastMessageByDirection(task.OpenID, model.QqbotMsgInbound)
	if err != nil || last == nil {
		return
	}
	at := last.CreatedAt
	if at.IsZero() || s.now().Sub(at) >= QqbotPassiveWindow {
		return // 窗口已过：不挂 reply_msg_id，让投递走召回/主动（由池子决定）
	}
	task.ReplyMsgID = last.QqbotMsgID
	task.ReplyMsgAt = &at
	task.MsgSeq = 0
}

// newInviteCode 生成一个未被占用的邀请码（复用 invite_service 的生成器）。
//
// 为什么复用 generateInviteCode 而不是另写一份：邀请码格式变动时两处必须同步，
// 复用同一个生成器就不会出现「机器人发的码注册页不认」这种事故。
func (s *QqbotService) newInviteCode() (string, error) {
	acc := s.accessor()
	for i := 0; i < 5; i++ {
		code := generateInviteCode()
		exists, err := acc.InviteCodeExists(code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("qqbot: invite code collision after 5 attempts")
}

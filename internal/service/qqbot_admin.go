// QQ 机器人后台侧：申请审批 / 会话 / 消息 / 发件池 / 健康检查（T-40）。
//
// # 与发件池的分工（docs/30 §一）
//
// 审批与后台发言**只建池任务**，一条消息都不直接调 SDK：
//
//	管理员点「同意」→ 分配邀请码 + 建 outbox(INVITE_CODE) → 由池子投递
//
// 理由：管理员点按钮时用户的被动窗口大概率已过期，直发必然失败且管理员看不到原因；
// 走池子后要么 SENT，要么留在 DEFERRED 等用户开口，状态在 GET /qqbot/outbox 里可查、可人工重试。
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

// 申请审批默认邀请码参数（与 invite 域默认值保持一致：GENERAL + 5% 分成）。
const (
	qqbotDefaultCommissionRate = 0.05
	// qqbotMessagePreview 会话列表里 last_message 的截断长度。
	qqbotMessagePreview = 200
)

// qqbotInviteText 发码文案。
//
// 刻意写成「码独占一行」：用户常常直接复制整条消息，码前后夹字会复制出错。
func qqbotInviteText(code string) string {
	return "你的邀请码：" + code + "\n注册时填写即可。"
}

// qqbotRejectText 拒绝通知（note 为管理员填的原因，可空）。
func qqbotRejectText(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return "很抱歉，你的邀请码申请未通过审核。"
	}
	return "很抱歉，你的邀请码申请未通过审核。\n原因：" + note
}

// ---------- dto 组装（model → dto，绝不把 model 直接抛给 handler） ----------

// toQqbotApplicationDto 申请单视图。
func toQqbotApplicationDto(a model.QqbotInviteApplication) dto.QqbotApplicationDto {
	return dto.QqbotApplicationDto{
		ID:          a.ID,
		OpenID:      a.OpenID,
		UnionOpenID: a.UnionOpenID,
		Nickname:    a.Nickname,
		QqbotMsgID:  a.QqbotMsgID,
		Content:     a.Content,
		Status:      a.Status,
		InviteCode:  a.InviteCode,
		InviteID:    a.InviteID,
		ReviewerID:  a.ReviewerID,
		ReviewNote:  a.ReviewNote,
		ReviewedAt:  a.ReviewedAt, // nil 时 omitempty 让整个字段不传（docs/21）
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

// toQqbotMessageDto 会话流水视图。
func toQqbotMessageDto(m model.QqbotMessage) dto.QqbotMessageDto {
	return dto.QqbotMessageDto{
		ID:         m.ID,
		OpenID:     m.OpenID,
		Direction:  m.Direction,
		Content:    m.Content,
		MsgType:    m.MsgType,
		QqbotMsgID: m.QqbotMsgID,
		ReplyMsgID: m.ReplyMsgID,
		MsgSeq:     m.MsgSeq,
		SenderID:   m.SenderID,
		IsWakeup:   m.IsWakeup,
		CreatedAt:  m.CreatedAt,
	}
}

// toQqbotOutboxDto 池任务视图。
func toQqbotOutboxDto(t *model.QqbotOutbox) *dto.QqbotOutboxDto {
	if t == nil {
		return nil
	}
	return &dto.QqbotOutboxDto{
		ID:          t.ID,
		OpenID:      t.OpenID,
		Kind:        t.Kind,
		RefID:       t.RefID,
		Content:     t.Content,
		MsgType:     t.MsgType,
		Status:      t.Status,
		ReplyMsgID:  t.ReplyMsgID,
		ReplyMsgAt:  t.ReplyMsgAt,
		MsgSeq:      t.MsgSeq,
		Attempts:    t.Attempts,
		NextRetryAt: t.NextRetryAt,
		LastError:   t.LastError,
		LastCode:    t.LastCode,
		LastKind:    t.LastKind,
		SentAt:      t.SentAt,
		SenderID:    t.SenderID,
		InviteCode:  t.InviteCode,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// ---------- 申请单 ----------

// ListApplications 申请名单（分页 + 状态/关键字过滤）。
func (s *QqbotService) ListApplications(ctx context.Context, status, keyword string, page, limit int) ([]dto.QqbotApplicationDto, int64, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, 0, e
		}
	}
	page, limit = normPage(page, limit)
	list, total, err := s.accessor().ListApplications(qqbotAppFilter{
		Status:  strings.ToUpper(strings.TrimSpace(status)),
		Keyword: strings.TrimSpace(keyword),
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		s.logger().Error("qqbot 查申请单失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	out := make([]dto.QqbotApplicationDto, 0, len(list))
	for _, a := range list {
		out = append(out, toQqbotApplicationDto(a))
	}
	return out, total, nil
}

// GetApplication 申请详情 + 该用户的聊天流水 + 该用户的相关池任务。
//
// 三者一起返回是刻意的：管理员看一条申请时，真正想知道的是「这人还说过什么、
// 我批的那条码到底发出去没有」，分成三个请求会让人来回跳。
func (s *QqbotService) GetApplication(ctx context.Context, id string) (*dto.QqbotApplicationDetailDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	acc := s.accessor()
	app, err := acc.FindApplicationByID(strings.TrimSpace(id))
	if err != nil {
		s.logger().Error("qqbot 查申请单详情失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if app == nil {
		return nil, errcode.ErrNotFound
	}

	msgs, err := acc.ListMessages(app.OpenID, 0, 50)
	if err != nil {
		s.logger().Error("qqbot 查申请单相关消息失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	msgOut := make([]dto.QqbotMessageDto, 0, len(msgs))
	for _, m := range msgs {
		msgOut = append(msgOut, toQqbotMessageDto(m))
	}

	tasks, _, err := acc.ListOutbox(qqbotOutboxFilter{OpenID: app.OpenID, Page: 1, Limit: 20})
	if err != nil {
		s.logger().Error("qqbot 查申请单相关池任务失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	outboxOut := make([]dto.QqbotOutboxDto, 0, len(tasks))
	for _, t := range tasks {
		outboxOut = append(outboxOut, *toQqbotOutboxDto(&t))
	}

	return &dto.QqbotApplicationDetailDto{
		Application: toQqbotApplicationDto(*app),
		Messages:    msgOut,
		Outbox:      outboxOut,
	}, nil
}

// ApproveApplication 同意申请：分配邀请码 → 标记 APPROVED → 建 INVITE_CODE 池任务。
//
// 四步的顺序是设计：
//  1. 先取码（碰撞重试）并建 invites 记录 —— 邀请码是真实资源，注册页要认它
//  2. 再更新申请单（管理员立刻要看到「批的是哪个码」，所以不等投递成功）
//  3. 再建池任务（带 InviteCode 结构化副本，供投递成功后回写）
//  4. 最后异步投递：**不阻塞审批响应**（投递要打上游 HTTP，可能 10s 超时）
func (s *QqbotService) ApproveApplication(ctx context.Context, id, reviewerID, note string) (*dto.QqbotApproveResultDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	acc := s.accessor()
	app, err := acc.FindApplicationByID(strings.TrimSpace(id))
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if app == nil {
		return nil, errcode.ErrNotFound
	}
	// 幂等保护：重复点「同意」不会发第二个码（否则同一人收到两个码，且第一个作废较难处理）。
	if app.Status != model.QqbotAppPending {
		return nil, errcode.ErrConflict
	}

	code, cerr := s.newInviteCode()
	if cerr != nil {
		s.logger().Error("qqbot 生成邀请码失败", zap.Error(cerr))
		return nil, errcode.ErrInternal
	}
	inv := &model.Invite{
		InviteCode:     code,
		Type:           model.InviteTypeGeneral,
		Status:         model.InviteStatusPending,
		CommissionRate: qqbotDefaultCommissionRate,
		Remark:         "QQ机器人发放 open_id=" + app.OpenID,
	}
	if err := acc.CreateInvite(inv); err != nil {
		s.logger().Error("qqbot 建邀请码失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	note = strings.TrimSpace(note)
	reviewedAt := s.now()
	if err := acc.UpdateApplication(app.ID, map[string]interface{}{
		"status":      model.QqbotAppApproved,
		"invite_code": code,
		"invite_id":   inv.ID,
		"reviewer_id": reviewerID,
		"review_note": truncateRunes(note, 500),
		"reviewed_at": reviewedAt,
	}); err != nil {
		s.logger().Error("qqbot 更新申请单失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	task := &model.QqbotOutbox{
		OpenID:     app.OpenID,
		Kind:       model.QqbotOutboxKindInviteCode,
		RefID:      app.ID,
		Content:    qqbotInviteText(code),
		MsgType:    qqbot.MsgTypeText,
		Status:     model.QqbotOutboxPending,
		InviteCode: code,
	}
	s.attachLatestWindow(task) // 用户 60 分钟内说过话 → 立刻可被动回复送达
	if err := acc.CreateOutbox(task); err != nil {
		s.logger().Error("qqbot 建发码池任务失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	// 本地视图同步（不回读一次 DB：审批路径要快，字段值我们刚写进去的就是真值）
	app.Status = model.QqbotAppApproved
	app.InviteCode = code
	app.InviteID = inv.ID
	app.ReviewerID = reviewerID
	app.ReviewNote = truncateRunes(note, 500)
	app.ReviewedAt = &reviewedAt

	s.async(func() { s.deliverQuietly(context.Background(), task.ID) })
	return &dto.QqbotApproveResultDto{
		Application: toQqbotApplicationDto(*app),
		OutboxID:    task.ID,
	}, nil
}

// BatchApprove 批量同意：逐条走 ApproveApplication，**部分失败不影响其它条**。
//
// 返回 per-item 结果而不是整体失败：管理员的真实场景是「勾了 10 条，其中 1 条已经被别人批过」，
// 这时候整体回滚会让另外 9 条白批。
func (s *QqbotService) BatchApprove(ctx context.Context, ids []string, reviewerID, note string) (*dto.QqbotBatchResultDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	if len(ids) == 0 {
		return nil, errcode.ErrInvalidParam
	}
	out := &dto.QqbotBatchResultDto{
		Total:   len(ids),
		Results: make([]dto.QqbotBatchItemDto, 0, len(ids)),
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		item := dto.QqbotBatchItemDto{ID: id}
		if id == "" {
			item.StatusCode = errcode.BizInvalidParam
			item.Message = errcode.ErrInvalidParam.Msg()
			out.Failed++
			out.Results = append(out.Results, item)
			continue
		}
		res, e := s.ApproveApplication(ctx, id, reviewerID, note)
		if e != nil {
			item.StatusCode = e.BizCode
			item.Message = e.Msg()
			out.Failed++
			out.Results = append(out.Results, item)
			continue
		}
		item.Success = true
		item.Message = errcode.Success.Msg()
		item.InviteCode = res.Application.InviteCode
		item.OutboxID = res.OutboxID
		out.Succeeded++
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// RejectApplication 拒绝申请：标记 REJECTED + 建 REJECT 池任务（通知用户）。
func (s *QqbotService) RejectApplication(ctx context.Context, id, reviewerID, note string) (*dto.QqbotApproveResultDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	acc := s.accessor()
	app, err := acc.FindApplicationByID(strings.TrimSpace(id))
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if app == nil {
		return nil, errcode.ErrNotFound
	}
	if app.Status != model.QqbotAppPending {
		return nil, errcode.ErrConflict
	}

	note = strings.TrimSpace(note)
	reviewedAt := s.now()
	if err := acc.UpdateApplication(app.ID, map[string]interface{}{
		"status":      model.QqbotAppRejected,
		"reviewer_id": reviewerID,
		"review_note": truncateRunes(note, 500),
		"reviewed_at": reviewedAt,
	}); err != nil {
		return nil, errcode.ErrInternal
	}

	task := &model.QqbotOutbox{
		OpenID:  app.OpenID,
		Kind:    model.QqbotOutboxKindReject,
		RefID:   app.ID,
		Content: qqbotRejectText(note),
		MsgType: qqbot.MsgTypeText,
		Status:  model.QqbotOutboxPending,
	}
	s.attachLatestWindow(task)
	if err := acc.CreateOutbox(task); err != nil {
		return nil, errcode.ErrInternal
	}

	app.Status = model.QqbotAppRejected
	app.ReviewerID = reviewerID
	app.ReviewNote = truncateRunes(note, 500)
	app.ReviewedAt = &reviewedAt

	s.async(func() { s.deliverQuietly(context.Background(), task.ID) })
	return &dto.QqbotApproveResultDto{
		Application: toQqbotApplicationDto(*app),
		OutboxID:    task.ID,
	}, nil
}

// ---------- 会话与消息 ----------

// ListConversations 按 openid 聚合的会话列表（最后一条消息 + 未读 + 待发）。
func (s *QqbotService) ListConversations(ctx context.Context, page, limit int) ([]dto.QqbotConversationDto, int64, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, 0, e
		}
	}
	page, limit = normPage(page, limit)
	acc := s.accessor()
	rows, total, err := acc.ListConversationOpenIDs((page-1)*limit, limit)
	if err != nil {
		s.logger().Error("qqbot 查会话列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}

	out := make([]dto.QqbotConversationDto, 0, len(rows))
	for _, row := range rows {
		item := dto.QqbotConversationDto{
			OpenID:       row.OpenID,
			MessageCount: row.MessageCount,
		}
		at := row.LastMessageAt
		if !at.IsZero() {
			item.LastMessageAt = &at
		}
		// 昵称：机器人事件体不带昵称（无接口权限），只能从申请单里拿历史快照。
		if app, e := acc.LatestApplicationByOpenID(row.OpenID); e == nil && app != nil {
			item.Nickname = app.Nickname
		}
		if last, e := acc.LastMessage(row.OpenID); e == nil && last != nil {
			item.LastMessage = truncateRunes(last.Content, qqbotMessagePreview)
			item.LastDirection = last.Direction
		}
		// 未读 = 最后一条出站消息之后的入站条数（口径见 dto 注释）。
		var sinceTime = lastOutboundTime(acc, row.OpenID)
		if n, e := acc.CountInboundAfter(row.OpenID, sinceTime); e == nil {
			item.UnreadCount = n
		}
		if n, e := acc.CountOutboxByOpenID(row.OpenID, []string{
			model.QqbotOutboxPending, model.QqbotOutboxSending, model.QqbotOutboxDeferred,
		}); e == nil {
			item.PendingCount = n
		}
		if n, e := acc.CountOutboxByOpenID(row.OpenID, []string{model.QqbotOutboxDeferred}); e == nil {
			item.DeferredCount = n
		}
		out = append(out, item)
	}
	return out, total, nil
}

// lastOutboundTime 该 openid 最后一条出站消息的时间（无则零值 = 全部算未读）。
func lastOutboundTime(acc qqbotDataAccess, openID string) time.Time {
	last, err := acc.LastMessageByDirection(openID, model.QqbotMsgOutbound)
	if err != nil || last == nil {
		return time.Time{}
	}
	return last.CreatedAt
}

// ListMessages 某用户的对话流水（分页）。
func (s *QqbotService) ListMessages(ctx context.Context, openID string, page, limit int) ([]dto.QqbotMessageDto, int64, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, 0, e
		}
	}
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, 0, errcode.ErrInvalidParam
	}
	page, limit = normPage(page, limit)
	acc := s.accessor()
	total, err := acc.CountMessages(openID)
	if err != nil {
		s.logger().Error("qqbot 查消息数失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	list, err := acc.ListMessages(openID, (page-1)*limit, limit)
	if err != nil {
		s.logger().Error("qqbot 查消息流水失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	out := make([]dto.QqbotMessageDto, 0, len(list))
	for _, m := range list {
		out = append(out, toQqbotMessageDto(m))
	}
	return out, total, nil
}

// SendChat 后台发消息给用户（建 CHAT 池任务，走池子投递）。
func (s *QqbotService) SendChat(ctx context.Context, openID, adminID, content string) (*dto.QqbotOutboxDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	openID = strings.TrimSpace(openID)
	content = strings.TrimSpace(content)
	if openID == "" || content == "" || len([]rune(content)) > QqbotChatContentMax {
		return nil, errcode.ErrInvalidParam
	}

	task := &model.QqbotOutbox{
		OpenID:   openID,
		Kind:     model.QqbotOutboxKindChat,
		Content:  content,
		MsgType:  qqbot.MsgTypeText,
		Status:   model.QqbotOutboxPending,
		SenderID: adminID,
	}
	s.attachLatestWindow(task)
	if err := s.accessor().CreateOutbox(task); err != nil {
		s.logger().Error("qqbot 建后台消息池任务失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	s.async(func() { s.deliverQuietly(context.Background(), task.ID) })
	return toQqbotOutboxDto(task), nil
}

// ---------- 发件池 ----------

// ListOutbox 发件池列表（含 DEFERRED / FAILED 死信，供后台排查）。
func (s *QqbotService) ListOutbox(ctx context.Context, status, kind, openID string, page, limit int) ([]dto.QqbotOutboxDto, int64, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, 0, e
		}
	}
	page, limit = normPage(page, limit)
	list, total, err := s.accessor().ListOutbox(qqbotOutboxFilter{
		Status: strings.ToUpper(strings.TrimSpace(status)),
		Kind:   strings.ToUpper(strings.TrimSpace(kind)),
		OpenID: strings.TrimSpace(openID),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		s.logger().Error("qqbot 查发件池失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	out := make([]dto.QqbotOutboxDto, 0, len(list))
	for _, t := range list {
		out = append(out, *toQqbotOutboxDto(&t))
	}
	return out, total, nil
}

// ---------- 健康检查 ----------

// Status 机器人健康检查：配置齐否 + token 可用否 + 池子积压。
//
// token 检查只在「已启用且凭据齐全」时发生（否则每次探活都会打一次上游 token 接口，
// 白白消耗限频额度）。
func (s *QqbotService) Status(ctx context.Context) (*dto.QqbotStatusDto, *errcode.Err) {
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return nil, e
		}
	}
	cfg := s.sdkConfig()
	out := &dto.QqbotStatusDto{
		Enabled:      s.enabled(),
		AppID:        cfg.AppID,
		Configured:   cfg.AppID != "" && cfg.AppSecret != "",
		WebhookReady: cfg.BotSecret != "",
		Missing:      make([]string, 0, 3),
	}
	if cfg.AppID == "" {
		out.Missing = append(out.Missing, qqbot.CfgAppID)
	}
	if cfg.AppSecret == "" {
		out.Missing = append(out.Missing, qqbot.CfgAppSecret)
	}
	if cfg.BotSecret == "" {
		out.Missing = append(out.Missing, qqbot.CfgBotSecret)
	}

	if out.Enabled && out.Configured {
		if client, cerr := s.clientOrErr(); cerr != nil {
			out.TokenError = cerr.Kind.String()
		} else if err := client.Ping(); err != nil {
			out.TokenError = asQqbotError(err, "ping").Kind.String()
		} else {
			out.TokenOK = true
		}
	}

	acc := s.accessor()
	for _, item := range []struct {
		status string
		dst    *int64
	}{
		{model.QqbotOutboxPending, &out.OutboxPending},
		{model.QqbotOutboxDeferred, &out.OutboxDeferred},
		{model.QqbotOutboxFailed, &out.OutboxFailed},
		{model.QqbotOutboxSent, &out.OutboxSent},
	} {
		if n, err := acc.CountOutbox(item.status); err == nil {
			*item.dst = n
		}
	}
	if n, err := acc.CountApplications(model.QqbotAppPending); err == nil {
		out.PendingApplications = n
	}
	if recent, err := acc.LatestOutboxWithError(); err == nil && recent != nil {
		out.RecentError = recent.LastKind
		at := recent.UpdatedAt
		if !at.IsZero() {
			out.RecentErrorAt = &at
		}
	}
	return out, nil
}

// deliverQuietly 异步投递一次（失败只记日志 —— 状态已经在池子里了，不需要向上抛）。
func (s *QqbotService) deliverQuietly(ctx context.Context, taskID string) {
	defer func() {
		if r := recover(); r != nil {
			s.logger().Error("qqbot 异步投递 panic", zap.String("outbox_id", taskID), zap.Any("panic", r))
		}
	}()
	if _, e := s.DeliverOutbox(ctx, taskID); e != nil {
		s.logger().Warn("qqbot 异步投递未成功（已按状态机留池）",
			zap.String("outbox_id", taskID), zap.Int("status_code", e.BizCode))
	}
}

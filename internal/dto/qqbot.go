package dto

import "time"

// QQ 官方机器人域对外契约（T-40）。
//
// 覆盖四组结构：
//
//	申请单    QqbotApplicationDto / QqbotApplicationDetailDto / 审批请求与结果
//	会话流水  QqbotMessageDto / QqbotConversationDto
//	发件池    QqbotOutboxDto（含 DEFERRED / FAILED 死信，供后台排查）
//	运维      QqbotStatusDto / QqbotWebhookValidationDto
//
// 字段来源（逐条可追，不是想象）：
//
//	响应实体 ← internal/model/qqbot.go 三张表的 json tag
//	响应类型 ← internal/service/qqbot_service.go 的组装函数（toXxxDto）
//	请求体   ← 同文件的 XxxInput
//
// 空值规则（docs/21-JSON序列化规范.md，本文件全部遵守）：
//   - 字符串：值类型 string，空时 ""
//   - 数组：无 omitempty，组装层保证非 nil，空时 []
//   - 可选时间（reviewed_at / next_retry_at / sent_at / reply_msg_at）：*time.Time + omitempty，
//     空时整个字段不传（传 "" 会让前端 new Date("") 得到 Invalid Date）
//   - 必有时间（created_at / updated_at）：time.Time，随 BaseModel 恒有值
//
// 为什么**不**直接返回 model.QqbotOutbox：给表加一列就会泄漏到 API（AGENTS 硬规则）。

// ---------------------------------------------------------------------------
// 申请单
// ---------------------------------------------------------------------------

// QqbotApplicationDto 邀请码申请单（model.QqbotInviteApplication + BaseModel，15 字段）。
//
// 用于：GET /qqbot/applications 分页项、GET /qqbot/applications/{id}、
// POST /applications/{id}/approve、POST /applications/{id}/reject 的单条响应。
type QqbotApplicationDto struct {
	ID string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	// OpenID 机器人维度唯一标识。**不是本站用户 ID**，不能拿去查 user 表。
	OpenID      string `json:"open_id" example:"A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	UnionOpenID string `json:"union_open_id" example:"U1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	Nickname    string `json:"nickname" example:"QQ用户"`

	// QqbotMsgID 触发申请的原始消息 ID（唯一索引，重复推送命中它而不是插两条）。
	QqbotMsgID string `json:"qqbot_msg_id" example:"ROBOT1.0_aBcDeF1234567890"`
	Content    string `json:"content" example:"邀请码"`

	// Status 见 model.QqbotAppXxx：PENDING / APPROVED / REJECTED / CANCELLED。
	Status string `json:"status" example:"PENDING"`

	// InviteCode 批准后分配的邀请码（未批准与拒绝时为空串）。
	InviteCode string `json:"invite_code" example:"INVAB12CD34"`
	InviteID   string `json:"invite_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`

	ReviewerID string `json:"reviewer_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReviewNote string `json:"review_note" example:"老用户推荐，通过"`

	// ReviewedAt 审批时间：未审批时不传该字段。
	ReviewedAt *time.Time `json:"reviewed_at,omitempty" format:"date-time"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// QqbotApplicationDetailDto 申请详情（申请单 + 该用户聊天流水 + 相关池任务）。
//
// 用于：GET /qqbot/applications/{id}。
//
// 三块一起返回是刻意的：管理员看一条申请时真正想知道的是「这人还说过什么、我批的码
// 到底发出去没有」，拆成三个请求会让人来回跳页面。
type QqbotApplicationDetailDto struct {
	Application QqbotApplicationDto `json:"application"`
	Messages    []QqbotMessageDto   `json:"messages"`
	Outbox      []QqbotOutboxDto    `json:"outbox"`
}

// QqbotMessageDto 会话流水（model.QqbotMessage + BaseModel，11 字段）。
// 用于：GET /conversations/{openid}/messages 分页项、
// GET /applications/{id} 详情里的 `messages`（该用户与该申请相关的流水）。
type QqbotMessageDto struct {
	ID string `json:"id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	// OpenID 对话方。
	OpenID string `json:"open_id" example:"A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	// Direction INBOUND（用户发）/ OUTBOUND（机器人或后台发）。
	Direction string `json:"direction" example:"INBOUND"`
	Content   string `json:"content" example:"我要一个邀请码"`
	// MsgType 官方 msg_type：0 文本 / 2 markdown / 7 富媒体。
	MsgType int `json:"msg_type" example:"0"`
	// QqbotMsgID 平台消息 ID（入站即被动回复源；出站为发送响应 id）。
	QqbotMsgID string `json:"qqbot_msg_id" example:"ROBOT1.0_aBcDeF1234567890"`
	ReplyMsgID string `json:"reply_msg_id" example:"ROBOT1.0_aBcDeF1234567890"`
	MsgSeq     int    `json:"msg_seq" example:"1"`
	// SenderID 后台管理员 ID（仅「后台发出的」消息有值）。
	SenderID string `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	IsWakeup bool   `json:"is_wakeup" example:"false"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// QqbotOutboxDto 发件池任务（model.QqbotOutbox + BaseModel，20 字段）。
//
// 状态语义（docs/29 §4.3）：PENDING 待发 / SENDING 发送中 / **DEFERRED 等窗口激活（不是终态，
// 绝不删行）** / SENT 已发 / FAILED 死信（终态，可人工重试拉回 PENDING）。
type QqbotOutboxDto struct {
	ID     string `json:"id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	OpenID string `json:"open_id" example:"A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	// Kind INVITE_CODE / REJECT / CHAT / AUTO_REPLY。
	Kind  string `json:"kind" example:"INVITE_CODE"`
	RefID string `json:"ref_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	// Content 待发文本（**原样返回**：这是排查发件池问题的唯一线索）。
	Content string `json:"content" example:"您的邀请码是 INVAB12CD34"`
	MsgType int    `json:"msg_type" example:"0"`

	Status string `json:"status" example:"DEFERRED"`
	// ReplyMsgID / ReplyMsgAt 被动回复源消息与其到达时间（窗口判定依据）。
	ReplyMsgID string     `json:"reply_msg_id" example:"ROBOT1.0_aBcDeF1234567890"`
	ReplyMsgAt *time.Time `json:"reply_msg_at,omitempty" format:"date-time"`
	MsgSeq     int        `json:"msg_seq" example:"0"`

	Attempts    int        `json:"attempts" example:"2"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty" format:"date-time"`

	// LastError / LastCode / LastKind 最后失败详情（LastKind 取 SDK 的 ErrorKind：
	// retryable / window_expired / permanent / config / invalid_argument）。
	LastError string `json:"last_error" example:"reply msg expired"`
	LastCode  int    `json:"last_code" example:"40034005"`
	LastKind  string `json:"last_kind" example:"window_expired"`

	SentAt   *time.Time `json:"sent_at,omitempty" format:"date-time"`
	SenderID string     `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	// InviteCode 本任务要发放的邀请码（仅 INVITE_CODE 类型有值，其它为空串）。
	InviteCode string `json:"invite_code" example:"INVAB12CD34"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// QqbotConversationDto 会话列表项（按 openid 聚合，9 字段）。
//
// 未读数口径（服务层实现，写死在这里避免前后端理解分叉）：
// `unread_count` = 该 openid 下「最后一条后台/机器人出站消息之后」的入站消息条数。
// 没有出站消息时 = 全部入站条数。
type QqbotConversationDto struct {
	OpenID   string `json:"open_id" example:"A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	Nickname string `json:"nickname" example:"QQ用户"`
	// LastMessage 最后一条消息内容（空串表示尚无消息）。
	LastMessage string `json:"last_message" example:"什么时候能给我邀请码？"`
	// LastDirection 最后一条消息的方向：INBOUND / OUTBOUND（无消息时为空串）。
	LastDirection string `json:"last_direction" example:"INBOUND"`
	// LastMessageAt 最后一条消息时间（无消息时不传）。
	LastMessageAt *time.Time `json:"last_message_at,omitempty" format:"date-time"`

	MessageCount int64 `json:"message_count" example:"12"`
	UnreadCount  int64 `json:"unread_count" example:"3"`
	// PendingCount 该用户还在池子里没发出去的消息数（含 DEFERRED）——
	// 对应「用户没收到」的可观测面。
	PendingCount int64 `json:"pending_count" example:"1"`
	// DeferredCount 其中处于 DEFERRED（等用户开口激活窗口）的条数。
	DeferredCount int64 `json:"deferred_count" example:"1"`
}

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// QqbotApproveRequest 同意申请（note 可空）。
type QqbotApproveRequest struct {
	Note string `json:"note" binding:"omitempty,max=500" example:"老用户推荐，通过"`
}

// QqbotRejectRequest 拒绝申请（note 可空，会拼进给用户的拒绝通知里）。
type QqbotRejectRequest struct {
	Note string `json:"note" binding:"omitempty,max=500" example:"暂未开放申请"`
}

// QqbotBatchApproveRequest 批量同意（ids 必填，最多 100 条）。
type QqbotBatchApproveRequest struct {
	IDs  []string `json:"ids" binding:"required,min=1,max=100" example:"[\"64b59ed4-1a2b-4c3d-8e9f-0123456789ab\"]"`
	Note string   `json:"note" binding:"omitempty,max=500" example:"批次通过"`
}

// QqbotSendMessageRequest 后台发消息给某个 QQ 用户。
//
// 走发件池（**不是**直接调 SDK）：管理员点「发送」时被动窗口大概率已过期，
// 直发会失败且后台看不到原因；走池子则要么送达（SENT），要么进 DEFERRED 等用户开口。
type QqbotSendMessageRequest struct {
	OpenID  string `json:"open_id" binding:"required,max=128" example:"A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4"`
	Content string `json:"content" binding:"required,max=2000" example:"你的申请我们收到了，正在处理"`
}

// ---------------------------------------------------------------------------
// 结果与运维
// ---------------------------------------------------------------------------

// QqbotApproveResultDto 单条审批结果（同意与拒绝共用）。
type QqbotApproveResultDto struct {
	Application QqbotApplicationDto `json:"application"`
	// OutboxID 本次建出来的池任务 ID（审批只建任务不直接发，投递由池子完成）。
	OutboxID string `json:"outbox_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
}

// QqbotBatchItemDto 批量审批的单项结果。
//
// status_code / message 与统一响应体同语义：0 = 成功；非 0 时 message 是 **i18n key**
// （不是译文），前端按 status_code 判逻辑。
type QqbotBatchItemDto struct {
	ID         string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Success    bool   `json:"success" example:"true"`
	StatusCode int    `json:"status_code" example:"0"`
	Message    string `json:"message" example:"Success"`
	InviteCode string `json:"invite_code" example:"INVAB12CD34"`
	OutboxID   string `json:"outbox_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
}

// QqbotBatchResultDto 批量审批汇总（**部分失败也返回 200**，逐条看 results）。
type QqbotBatchResultDto struct {
	Total     int                 `json:"total" example:"5"`
	Succeeded int                 `json:"succeeded" example:"4"`
	Failed    int                 `json:"failed" example:"1"`
	Results   []QqbotBatchItemDto `json:"results"`
}

// QqbotStatusDto 机器人健康检查（配置齐否 + token 可用否 + 池子积压）。
//
// 刻意不返回 access_token / secret 的任何片段（连打码后的都不用）——
// 该端点只需要回答「现在能不能发消息」。
type QqbotStatusDto struct {
	// Enabled 总开关（qqbot_enabled）。
	Enabled bool `json:"enabled" example:"true"`
	// Configured 凭据是否齐全（app_id + app_secret；webhook 验签另需 bot_secret）。
	Configured bool `json:"configured" example:"true"`
	// WebhookReady bot_secret 是否已配置（缺它 webhook 一律验签失败）。
	WebhookReady bool `json:"webhook_ready" example:"true"`
	// AppID 便于运维核对填错（AppID 是公开标识，不敏感）。
	AppID string `json:"app_id" example:"102065886"`
	// TokenOK 拉一次 access_token 是否成功（Enabled=false 或未配置时恒 false）。
	TokenOK bool `json:"token_ok" example:"true"`
	// TokenError 取 token 失败时的归类标签（retryable / config / unknown…），成功时 ""。
	TokenError string `json:"token_error" example:"config"`
	// Missing 缺失的配置 key 列表（空时 []）。
	Missing []string `json:"missing"`

	// RecentError / RecentErrorAt 最近一次投递失败的归类与时间（无失败时不传时间）。
	RecentError   string     `json:"recent_error" example:"window_expired"`
	RecentErrorAt *time.Time `json:"recent_error_at,omitempty" format:"date-time"`

	// 池子积压（运维最关心的一组数字）。
	OutboxPending  int64 `json:"outbox_pending" example:"2"`
	OutboxDeferred int64 `json:"outbox_deferred" example:"7"`
	OutboxFailed   int64 `json:"outbox_failed" example:"0"`
	OutboxSent     int64 `json:"outbox_sent" example:"128"`

	// PendingApplications 待审批申请数。
	PendingApplications int64 `json:"pending_applications" example:"3"`
}

// QqbotWebhookValidationDto webhook 地址验证（op=13）的响应体。
//
// ⚠️ 这个端点**不走统一响应包装**（平台要裸 JSON，套 status_code 外壳会判定校验失败）：
// 成功时响应体就是这个结构本身，业务事件（op=0）时响应体是空对象 `{}` 或空。
type QqbotWebhookValidationDto struct {
	PlainToken string `json:"plain_token" example:"Arq0D5A61EgUu4OxUvOp"`
	Signature  string `json:"signature" example:"a1b2c3d4e5f60718293a4b5c6d7e8f90112233445566778899aabbccddeeff00"`
}

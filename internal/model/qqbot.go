// QQ 机器人域实体：邀请码申请单 + 会话消息 + 发件池。
//
// 三张表的职责边界：
//
//	· qqbot_invite_application —— 业务语义层（谁申请、批没批、发的什么码）
//	· qqbot_message            —— 会话流水（用户发的 + 后台发的，双向，供后台聊天视图）
//	· qqbot_outbox             —— 投递层（要发什么、发没发出去、失败了怎么办）
//
// **为什么不合并 message 与 outbox**：会话流水是「发生过什么」的事实记录，只增不改；
// 发件池是「还要做什么」的任务队列，状态会反复流转（PENDING→DEFERRED→SENT）。
// 混在一张表里会让「重试」被迫修改历史记录，池子一旦失败连聊天记录都被污染。
package model

import "time"

// ---------- 申请单 ----------

// 申请单状态。
const (
	// QqbotAppPending 待审批。
	QqbotAppPending = "PENDING"
	// QqbotAppApproved 已同意（已排入发件池或已发出）。
	QqbotAppApproved = "APPROVED"
	// QqbotAppRejected 已拒绝。
	QqbotAppRejected = "REJECTED"
	// QqbotAppCancelled 用户撤回/超时作废。
	QqbotAppCancelled = "CANCELLED"
)

// QqbotInviteApplication QQ 机器人收到的邀请码申请。
//
// 幂等：按 `qqbot_msg_id` 唯一 —— 官方明确「相同 msg_id 可能多次推送」，
// 重复事件必须命中唯一索引而不是插入两条申请单。
type QqbotInviteApplication struct {
	BaseModel
	// OpenID 申请人（机器人维度唯一，跨机器人不通用，**不能当全局用户 ID**）。
	OpenID string `gorm:"column:open_id;type:varchar(128);not null;index:idx_qqbot_app_open,priority:1;comment:申请人 openid" json:"open_id"`
	// UnionOpenID 跨机器人通用 ID（可选）。
	UnionOpenID string `gorm:"column:union_open_id;type:varchar(128);index;comment:跨机器人 openid" json:"union_open_id"`
	// Nickname 机器人侧昵称（无头像昵称接口权限时可能为空）。
	Nickname string `gorm:"column:nickname;type:varchar(64);comment:昵称" json:"nickname"`

	// QqbotMsgID 触发申请的原始消息 ID —— 唯一索引做幂等。
	QqbotMsgID string `gorm:"column:qqbot_msg_id;type:varchar(128);not null;uniqueIndex:uk_qqbot_app_msg;comment:原始消息ID（幂等键）" json:"qqbot_msg_id"`
	// Content 用户原始申请内容。
	Content string `gorm:"column:content;type:text;comment:申请内容" json:"content"`

	// Status 见 QqbotAppXxx。
	Status string `gorm:"column:status;type:varchar(20);not null;default:PENDING;index:idx_qqbot_app_status;comment:状态" json:"status" example:"PENDING"`

	// InviteCode 批准后分配的邀请码（拒绝时为空）。
	InviteCode string `gorm:"column:invite_code;type:varchar(64);index;comment:发放的邀请码" json:"invite_code"`
	// InviteID 关联 invites 表主键（可空：码可能由其它途径生成）。
	InviteID string `gorm:"column:invite_id;type:varchar(36);index;comment:邀请码记录ID" json:"invite_id"`

	// ReviewerID 审批人（后台管理员）。
	ReviewerID string `gorm:"column:reviewer_id;type:varchar(36);comment:审批人ID" json:"reviewer_id"`
	// ReviewNote 审批备注。
	ReviewNote string `gorm:"column:review_note;type:varchar(500);comment:审批备注" json:"review_note"`
	// ReviewedAt 审批时间。
	ReviewedAt *time.Time `gorm:"column:reviewed_at;comment:审批时间" json:"reviewed_at,omitempty"`
}

// TableName 表名。
func (QqbotInviteApplication) TableName() string { return "qqbot_invite_applications" }

// ---------- 会话消息 ----------

// 消息方向。
const (
	// QqbotMsgInbound 用户发给机器人。
	QqbotMsgInbound = "INBOUND"
	// QqbotMsgOutbound 机器人/后台发给用户。
	QqbotMsgOutbound = "OUTBOUND"
)

// QqbotMessage 会话流水（双向）。
type QqbotMessage struct {
	BaseModel
	// OpenID 对话方。
	OpenID string `gorm:"column:open_id;type:varchar(128);not null;index:idx_qqbot_msg_open,priority:1;comment:openid" json:"open_id"`
	// Direction 见 QqbotMsgXxx。
	Direction string `gorm:"column:direction;type:varchar(16);not null;index:idx_qqbot_msg_open,priority:2;comment:方向" json:"direction" example:"INBOUND"`
	// Content 文本内容。
	Content string `gorm:"column:content;type:text;comment:内容" json:"content"`
	// MsgType 官方 msg_type（0 文本 / 2 markdown / 7 富媒体）。
	MsgType int `gorm:"column:msg_type;type:int;default:0;comment:消息类型" json:"msg_type"`

	// QqbotMsgID 平台消息 ID。
	//
	// 入站：事件 d.id，**同时是回复时的 msg_id**；出站：发送响应里的消息 id。
	//
	// ⚠️ **不要给它加 gorm `uniqueIndex` tag**。
	//
	// 官方明确「相同 msg_id 可能多次推送」，去重必须靠数据库唯一约束（应用层
	// 「先查后插」在两个 webhook 并发到达时仍会插两条）。但这里**只能**用
	// **部分唯一索引**（`WHERE qqbot_msg_id <> ''`）—— 因为出站消息的
	// qqbot_msg_id 允许为空（发送响应可能不带 id），完整唯一索引会让所有空串
	// 互相冲突，第二条出站消息就插不进去。
	//
	// 约束由 `migrations/migrate_qqbot.go` 的 `ukQqbotMessageMsgID` 手写创建，
	// 不在 tag 上声明。改这里之前先读那段注释。
	QqbotMsgID string `gorm:"column:qqbot_msg_id;type:varchar(128);index;comment:平台消息ID" json:"qqbot_msg_id"`

	// ReplyMsgID 出站消息对应的被动回复 msg_id（为空表示主动消息）。
	ReplyMsgID string `gorm:"column:reply_msg_id;type:varchar(128);comment:被动回复的源消息ID" json:"reply_msg_id"`
	// MsgSeq 被动回复序号。
	MsgSeq int `gorm:"column:msg_seq;type:int;default:0;comment:回复序号" json:"msg_seq"`

	// SenderID 后台管理员 ID（仅出站且有后台操作人时填）。
	SenderID string `gorm:"column:sender_id;type:varchar(36);comment:后台发送人ID" json:"sender_id"`
	// IsWakeup 是否互动召回消息。
	IsWakeup bool `gorm:"column:is_wakeup;type:boolean;default:false;comment:是否召回消息" json:"is_wakeup"`
}

// TableName 表名。
func (QqbotMessage) TableName() string { return "qqbot_messages" }

// ---------- 发件池 ----------

// 池内任务状态。
//
// **DEFERRED 不是终态**（用户明确要求）：消息留在池里，等用户下次发消息
// 激活窗口后重投；只有 SENT / FAILED 才出池。
const (
	// QqbotOutboxPending 待发送（含退避重试等待中）。
	QqbotOutboxPending = "PENDING"
	// QqbotOutboxSending 正在发送（防并发重复投递）。
	QqbotOutboxSending = "SENDING"
	// QqbotOutboxDeferred 窗口过期/次数用尽 —— **留在池里等激活**。
	QqbotOutboxDeferred = "DEFERRED"
	// QqbotOutboxSent 已发送（终态）。
	QqbotOutboxSent = "SENT"
	// QqbotOutboxFailed 永久失败（终态，进死信供人工处理）。
	QqbotOutboxFailed = "FAILED"
)

// 池内任务的业务类型（决定「发成功后」要不要回写业务表）。
const (
	// QqbotOutboxKindInviteCode 发放邀请码（成功后回写申请单 InviteCode）。
	QqbotOutboxKindInviteCode = "INVITE_CODE"
	// QqbotOutboxKindReject 拒绝通知。
	QqbotOutboxKindReject = "REJECT"
	// QqbotOutboxKindChat 后台与用户聊天（纯文本，无业务回写）。
	QqbotOutboxKindChat = "CHAT"
	// QqbotOutboxKindAutoReply 自动回复（收到申请时的即时文案）。
	QqbotOutboxKindAutoReply = "AUTO_REPLY"
)

// QqbotOutbox 发件池。
//
// 状态机（详见 docs/29 §4.3）：
//
//	PENDING ──成功──→ SENT
//	   │                 ↑
//	   ├─窗口过期→ DEFERRED ──用户发新消息──┐
//	   │              └─有召回额度→is_wakeup─┘
//	   ├─频控/去重→ 留在 PENDING（next_retry_at 推后）
//	   └─永久失败→ FAILED
type QqbotOutbox struct {
	BaseModel
	// OpenID 收件人。
	OpenID string `gorm:"column:open_id;type:varchar(128);not null;index:idx_qqbot_outbox_open_status,priority:1;comment:openid" json:"open_id"`

	// Kind 见 QqbotOutboxKindXxx。
	Kind string `gorm:"column:kind;type:varchar(24);not null;index;comment:业务类型" json:"kind" example:"INVITE_CODE"`
	// RefID 关联业务主键（如申请单 ID）。
	RefID string `gorm:"column:ref_id;type:varchar(36);index;comment:关联业务ID" json:"ref_id"`

	// Content 待发送文本。
	Content string `gorm:"column:content;type:text;comment:待发内容" json:"content"`
	// MsgType 官方 msg_type。
	MsgType int `gorm:"column:msg_type;type:int;default:0;comment:消息类型" json:"msg_type"`

	// Status 见 QqbotOutboxXxx。
	Status string `gorm:"column:status;type:varchar(16);not null;default:PENDING;index:idx_qqbot_outbox_open_status,priority:2;index:idx_qqbot_outbox_status_retry,priority:1;comment:状态" json:"status" example:"PENDING"`

	// ReplyMsgID 可用于被动回复的源消息 ID（用户最近一条消息）。
	//
	// 窗口内直接用它做被动回复；为空或已过期则转主动/召回。
	ReplyMsgID string `gorm:"column:reply_msg_id;type:varchar(128);comment:可被动回复的源消息ID" json:"reply_msg_id"`
	// ReplyMsgAt 该 msg_id 的到达时间 —— 判断 60 分钟窗口是否还有效。
	//
	// **不靠猜**：官方窗口是「收到消息后 60 分钟」，存到达时间即可算剩余窗口。
	ReplyMsgAt *time.Time `gorm:"column:reply_msg_at;comment:源消息到达时间" json:"reply_msg_at,omitempty"`
	// MsgSeq 已用到的回复序号（0 表示还没用过；1..4 是单聊上限）。
	MsgSeq int `gorm:"column:msg_seq;type:int;default:0;comment:已用回复序号" json:"msg_seq"`

	// Attempts 已尝试次数。
	Attempts int `gorm:"column:attempts;type:int;default:0;comment:尝试次数" json:"attempts"`
	// NextRetryAt 下次可尝试时间（退避用）。
	NextRetryAt *time.Time `gorm:"column:next_retry_at;index:idx_qqbot_outbox_status_retry,priority:2;comment:下次重试时间" json:"next_retry_at,omitempty"`

	// LastError 最后一次失败原因（官方 message，仅供排查）。
	LastError string `gorm:"column:last_error;type:text;comment:最后错误" json:"last_error"`
	// LastCode 最后一次失败的业务码。
	LastCode int `gorm:"column:last_code;type:int;default:0;comment:最后错误码" json:"last_code"`
	// LastKind 最后一次失败的归类（retryable/window_expired/permanent）。
	LastKind string `gorm:"column:last_kind;type:varchar(24);comment:最后错误归类" json:"last_kind"`

	// SentAt 发送成功时间。
	SentAt *time.Time `gorm:"column:sent_at;comment:发送时间" json:"sent_at,omitempty"`
	// SenderID 后台操作人（Chat 类）。
	SenderID string `gorm:"column:sender_id;type:varchar(36);comment:后台发送人" json:"sender_id"`

	// InviteCode 本次要发放的邀请码（仅 INVITE_CODE 类型有值）。
	//
	// 为什么单独存一列而不是从 Content 里解析：发码文案是**配置**（后台可改模板），
	// 从文案里正则抠码必然随模板改动而失效；存一份结构化副本后，
	// 「投递成功 → 回写申请单 invite_code」（docs/29 §4.3 的成功分支）才有可靠数据源。
	InviteCode string `gorm:"column:invite_code;type:varchar(64);comment:关联邀请码（INVITE_CODE 类型）" json:"invite_code"`
}

// TableName 表名。
func (QqbotOutbox) TableName() string { return "qqbot_outbox" }

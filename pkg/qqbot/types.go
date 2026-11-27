// Package qqbot 是 QQ 官方机器人（BOT API v2）的独立第三方 SDK。
//
// 只做两件事：**发消息** 与 **收事件（webhook）**。不依赖本项目的任何内部包，
// 可被任意 Go 项目直接引用（与 pkg/oauthkit 同一定位）。
//
// # 为什么自实现而不引现成库
//
// 生态里 `tencent-connect/botgo` 以频道(Guild)为主、v1/v2 混排且依赖重（含 ws 全家桶）；
// 其余 fork 多为玩具级。本项目只需要 HTTP 发消息 + webhook 校验，不需要 WebSocket 长连接。
// 更关键的是：**官方错误码分类表是这个 SDK 的核心产出**，第三方库都没有做
// （详见 errors.go 的 ErrorKind）。
//
// # 三种发送模式（官方互斥语义）
//
//	① 被动回复  MsgID 非空          —— 收到消息后 60 分钟内，每条最多回 4 次（msg_seq 1→4）
//	② 互动召回  IsWakeup = true     —— 用户对话后 30 天内，4 个周期各 1 条
//	③ 普通主动  两者都为空          —— 单关系 20/qpm、每用户 1000 条/天，用户可关闭
//
// 用户可在 QQ 客户端关闭「允许主动发送」，此后 ②③ 一律失败（错误码 40054013）。
// 调用方应把这类失败视为**永久失败**并把消息留在自己的池子里，等用户下次发消息激活窗口。
package qqbot

import (
	"encoding/json"
	"net/http"
)

// ---------- 事件（webhook 入站） ----------

// OpCode webhook 的 op 字段。
type OpCode int

const (
	// OpDispatch 事件推送（业务事件，看 t 字段）。
	OpDispatch OpCode = 0
	// OpValidation 回调地址验证（平台首次配置时调用，必须回签名）。
	OpValidation OpCode = 13
)

// Payload webhook 通用信封（对齐官方 payload 结构）。
type Payload struct {
	// ID 事件 ID。可作为发送消息的 event_id 做被动回复。
	ID string `json:"id"`
	// Op 见 OpCode。
	Op OpCode `json:"op"`
	// Type 事件类型，如 C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE / FRIEND_ADD。
	Type string `json:"t"`
	// Data 事件体，按 Type 决定结构（用 DecodeData 解析）。
	Data json.RawMessage `json:"d"`
	// Seq 事件序号（ws 场景用，webhook 一般为 0）。
	Seq int64 `json:"s"`
}

// 事件类型常量。
const (
	EventC2CMessageCreate     = "C2C_MESSAGE_CREATE"      // 单聊消息
	EventGroupAtMessageCreate = "GROUP_AT_MESSAGE_CREATE" // 群内 @机器人
	EventGroupMessageCreate   = "GROUP_MESSAGE_CREATE"    // 群内全量消息
	EventFriendAdd            = "FRIEND_ADD"              // 添加好友
	EventInteractionCreate    = "INTERACTION_CREATE"      // 按钮回调
)

// ValidationRequest 回调地址验证请求体（OpValidation 时的 Data）。
type ValidationRequest struct {
	PlainToken string `json:"plain_token"`
	EventTs    string `json:"event_ts"`
}

// ValidationResponse 回调地址验证响应体 —— **必须**按此结构返回，否则平台判定校验失败。
type ValidationResponse struct {
	PlainToken string `json:"plain_token"`
	Signature  string `json:"signature"`
}

// C2CMessage 单聊消息事件体（C2C_MESSAGE_CREATE 的 Data）。
//
// ⚠️ `ID` 就是发送消息时用的 `msg_id`，位于**事件体的 d.id**（不是信封的 ID）。
// 拿错会得到 40034024「msg_id 无效或越权」。
type C2CMessage struct {
	ID        string      `json:"id"`
	Content   string      `json:"content"`
	Timestamp string      `json:"timestamp"`
	Author    MessageUser `json:"author"`
}

// MessageUser 单聊消息的发送者（C2C_MESSAGE_CREATE 的 d.author）。
type MessageUser struct {
	// OpenID 用户在该机器人下的唯一标识（跨机器人不通用，绝不能当全局用户 ID 存）。
	OpenID string `json:"user_openid"`
	// UnionOpenID 同一开发者主体下跨机器人通用（可选，部分场景才下发）。
	UnionOpenID string `json:"union_openid"`
	// Username 用户昵称（后台会话列表直接展示）。
	Username string `json:"username"`
	// Avatar 用户头像 URL。
	Avatar string `json:"avatar"`
	// Bot 发送者是否机器人。
	Bot bool `json:"bot"`
}

// GroupUser 群消息事件的发送者（GROUP_AT_MESSAGE_CREATE 的 d.author）。
//
// ⚠️ 与 MessageUser 的关键差异：群场景用 **member_openid**，不是 user_openid。
// 官方事件文档字段表原文：「author | GroupUser | 发送者（member_openid 有值）」。
// 复用 MessageUser 会让群内申请全部落成空 openid —— 单聊/群聊必须分开。
type GroupUser struct {
	// ID 用户唯一标识（OpenID 格式）。
	ID string `json:"id"`
	// Username 用户昵称，可直接落库。
	Username string `json:"username"`
	// Avatar 用户头像 URL。
	Avatar string `json:"avatar"`
	// Bot 发送者是否机器人。
	Bot bool `json:"bot"`
	// UnionOpenID 同一开发者主体下跨机器人通用。
	UnionOpenID string `json:"union_openid"`
	// MemberOpenID 群成员 OpenID —— **群场景的对方标识**（不是 user_openid）。
	MemberOpenID string `json:"member_openid"`
	// MemberRole 群内角色：member=普通成员 / admin=管理员 / owner=群主。
	MemberRole string `json:"member_role"`
	// MemberNickname 群内昵称。
	MemberNickname string `json:"member_nickname"`
}

// GroupAtMessage 群内 @机器人 事件体。
type GroupAtMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	GroupID string `json:"group_openid"`
	// Author 群场景发送者 —— 用 GroupUser（对方标识在 member_openid）。
	Author      GroupUser `json:"author"`
	Timestamp   string    `json:"timestamp"`
	MessageType int       `json:"message_type"`
}

// ---------- 发消息（出站） ----------

// 消息类型（msg_type）。
const (
	MsgTypeText     = 0 // content
	MsgTypeMarkdown = 2 // markdown
	MsgTypeInput    = 6 // 输入中状态
	MsgTypeMedia    = 7 // 富媒体
	MsgTypeCard     = 8 // 图文卡片
)

// SendRequest 发送消息请求体。
//
// 三种模式互斥（官方语义）：MsgID 非空=被动回复；IsWakeup=true=互动召回；都空=普通主动。
// 同时传会被平台拒绝或行为未定义，SDK 层做参数校验直接返回 ErrInvalidArgument。
type SendRequest struct {
	// MsgType 见 MsgTypeXxx，默认 0（文本）。
	MsgType int `json:"msg_type"`
	// Content 文本内容（MsgType=0 时是全文）。
	Content string `json:"content,omitempty"`
	// Markdown 富文本（MsgType=2 必填）；填了它 Content 必须为空。
	Markdown *Markdown `json:"markdown,omitempty"`

	// MsgID 被动回复的消息 ID（来自事件的 d.id），60 分钟内有效。
	MsgID string `json:"msg_id,omitempty"`
	// EventID 被动回复的事件 ID（与 MsgID 二选一）。
	EventID string `json:"event_id,omitempty"`
	// MsgSeq 回复序号，与 MsgID 联用。**相同 MsgID+MsgSeq 重复发送必失败**，递增可多次回复。
	MsgSeq int `json:"msg_seq,omitempty"`
	// IsWakeup 互动召回消息，与 MsgID/EventID 互斥。
	IsWakeup bool `json:"is_wakeup,omitempty"`
}

// Markdown Markdown 消息体。
type Markdown struct {
	Content string `json:"content,omitempty"`
}

// SendResponse 发送成功响应。
type SendResponse struct {
	// ID 消息 ID，可用于 2 分钟内撤回。
	ID string `json:"id"`
	// Timestamp 发送时间，RFC3339 东八区。
	Timestamp string `json:"timestamp"`
	// ExtInfo 扩展信息。
	ExtInfo *ExtInfo `json:"ext_info,omitempty"`
}

// ExtInfo 发送响应扩展信息。
type ExtInfo struct {
	// RefIdx 引用消息索引。
	RefIdx string `json:"ref_idx"`
}

// ---------- 客户端接口 ----------

// Client QQ 机器人客户端。
//
// 实现需保证：access_token 并发安全（多人同时发消息只刷一次）、
// 临近过期（60s 内）自动刷新、业务错误按 ErrorKind 分类返回 *Error。
type Client interface {
	// SendC2C 发送单聊消息给指定用户。
	//
	// 返回的 error 一定是 *Error（可直接 errors.As 取 Kind），便于调用方按
	// ErrorKindWindowExpired / Retryable / Permanent 决定池子状态。
	SendC2C(userOpenID string, req SendRequest) (*SendResponse, error)

	// SendGroup 发送群消息。
	SendGroup(groupOpenID string, req SendRequest) (*SendResponse, error)

	// ParseWebhook 校验并解析 webhook 请求体。
	//
	// 返回 (payload, validationResponse, error) 三态：
	//   · op=13 地址验证 → payload 非空且 validation 非空（调用方原样回写 validation）；
	//   · op=0  业务事件 → payload 非空、validation 为 nil；
	//   · 签名/结构非法 → error。
	//
	// 签名算法见 SignValidation（ed25519，seed 由 BotSecret 循环填充到 32 字节）。
	ParseWebhook(body []byte, header http.Header) (*Payload, *ValidationResponse, error)

	// Ping 健康检查（拉一次 access_token 验证凭据可用）。
	Ping() error
}

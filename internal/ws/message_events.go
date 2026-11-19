// Package ws message 域 WebSocket 事件表与 payload 定义。
//
// 事实来源（权威，逐事件核对）：
//   - tmp/picart/src/modules/message/message.gateway.ts（@SubscribeMessage + client.emit）
//   - tmp/picart/src/modules/message/message-realtime.service.ts（定向/广播推送）
//   - tmp/picart/test-websocket-client.js（客户端实际收发的事件与字段）
//
// 本文件只补齐 hub.go 尚未声明的事件常量与入参结构，不改动 hub.go 已有常量
// （骨架已定义的那批保持原样，此处仅追加）。namespace 对齐原 /ws-message。
//
// 传输差异说明：原前端用 socket.io，事件是 emit(event, payload) 的双向帧；
// 本仓库用 gorilla/websocket，帧结构统一为 {"event": "...", "data": {...}}
// （见 hub.go 的 Frame）。前端迁移需把 socket.emit(...)/socket.on(...) 换成该帧格式。
package ws

// 客户端 → 服务端事件（补齐 hub.go 未声明的部分）。
const (
	// EventSendMessage 发送消息（私信 / 广播 / 群发，源端 handleSendMessage）。
	EventSendMessage = "sendMessage"
	// EventGetHistory 拉取站内消息历史（源端 handleGetHistory，回 history）。
	EventGetHistory = "getHistory"
	// EventGetPrivateConversations 拉取私信会话列表（回 privateConversations）。
	EventGetPrivateConversations = "getPrivateConversations"
	// EventGetPrivateHistory 拉取与某人的私信历史（回 privateHistory）。
	EventGetPrivateHistory = "getPrivateHistory"
	// EventReadPrivateMessages 批量标记私信已读（回 privateMessagesRead）。
	EventReadPrivateMessages = "readPrivateMessages"
	// EventReadPrivateConversation 整会话标记已读（回 privateConversationRead）。
	EventReadPrivateConversation = "readPrivateConversation"
	// EventRecallPrivateMessage 撤回私信（回 privateMessageRecalled）。
	EventRecallPrivateMessage = "recallPrivateMessage"
	// EventGetUnreadCount 查询未读数（回 unreadCount）。
	EventGetUnreadCount = "getUnreadCount"
	// EventMarkAllAsRead 全部标记已读（回 allMarkedAsRead）。
	EventMarkAllAsRead = "markAllAsRead"
	// EventBatchOperation 批量操作消息（回 batchOperationResult）。
	EventBatchOperation = "batchOperation"
	// EventReadMessage 单条标记已读（回 read）。
	EventReadMessage = "readMessage"
	// EventGetProfile 取当前用户信息（回 profile）。
	EventGetProfile = "getProfile"
	// EventUnsubscribe 取消订阅某人在线状态（hub.go 只有 subscribeUserStatus）。
	EventUnsubscribe = "unsubscribeUserStatus"
	// EventGetUserStatus 查询某人是否在线（回 userStatus）。
	EventGetUserStatus = "getUserStatus"
)

// 服务端 → 客户端事件（补齐 hub.go 未声明的部分）。
const (
	// EventPrivateMessagesReadReceipt 发给对方的已读回执（源端 emitPrivateConversationRead）。
	EventPrivateMessagesReadReceipt = "privateMessagesReadReceipt"
	// EventBatchOperationResult 批量操作结果（源端 handleBatchOperation）。
	EventBatchOperationResult = "batchOperationResult"
)

// PresenceRoom 用户在线状态房间（对齐源端 getPresenceRoom：presence:user:{id}）。
func PresenceRoom(userID string) string { return "presence:user:" + userID }

// UserRoom 用户专属房间（连接时自动加入，等价 socket.io .join(String(uid))）。
func UserRoom(userID string) string { return userID }

// ---------- 客户端 → 服务端 入参 ----------

// SendMessageInput 发送消息入参（对齐源端 handleSendMessage 的 data）。
type SendMessageInput struct {
	Content     string                 `json:"content"`
	ToUserID    string                 `json:"toUserId"`
	ReceiverIDs []string               `json:"receiverIds"`
	IsBroadcast bool                   `json:"isBroadcast"`
	Type        string                 `json:"type"`        // private|system|notification
	MessageKind string                 `json:"messageKind"` // text|image|file|card
	Payload     map[string]interface{} `json:"payload"`
}

// GetHistoryInput 站内消息历史入参（对齐 handleGetHistory）。
type GetHistoryInput struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// CursorInput 游标分页入参（对齐 CursorPaginationDto）。
type CursorInput struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// GetPrivateHistoryInput 私信历史入参（对齐 handleGetPrivateHistory）。
type GetPrivateHistoryInput struct {
	UserID string `json:"userId"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// ReadPrivateMessagesInput 批量已读入参（对齐 handleReadPrivateMessages）。
type ReadPrivateMessagesInput struct {
	MessageIDs []string `json:"messageIds"`
}

// ReadPrivateConversationInput 整会话已读入参（对齐 handleReadPrivateConversation）。
type ReadPrivateConversationInput struct {
	UserID string `json:"userId"`
}

// RecallPrivateMessageInput 撤回私信入参（对齐 handleRecallPrivateMessage）。
type RecallPrivateMessageInput struct {
	MessageID string `json:"messageId"`
	Reason    string `json:"reason"`
}

// MarkAllAsReadInput 全部已读入参（对齐 handleMarkAllAsRead）。
type MarkAllAsReadInput struct {
	Type        string `json:"type"`
	IsBroadcast bool   `json:"isBroadcast"`
}

// BatchOperationInput 批量操作入参（对齐 handleBatchOperation）。
type BatchOperationInput struct {
	MessageIDs []string `json:"messageIds"`
	Action     string   `json:"action"` // read|delete
}

// ReadMessageInput 单条已读入参（对齐 handleReadMessage）。
type ReadMessageInput struct {
	MessageID string `json:"messageId"`
}

// UserStatusInput 在线状态入参（对齐 subscribeUserStatus / getUserStatus）。
type UserStatusInput struct {
	UserID string `json:"userId"`
}

// ---------- 服务端 → 客户端 payload ----------

// ConnectedPayload connected 事件载荷（对齐源端 client.emit("connected", {...})）。
type ConnectedPayload struct {
	Message string            `json:"message"`
	User    ConnectedUserInfo `json:"user"`
}

// ConnectedUserInfo connected 里的用户片段（源端只给四个字段）。
type ConnectedUserInfo struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
}

// ErrorPayload error 事件载荷（源端统一 {message, code}）。
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// 错误码常量（对齐源端 client.emit("error", {code}) 的字符串码）。
const (
	ErrCodeUserNotFound         = "USER_NOT_FOUND"
	ErrCodeAuthFailed           = "AUTH_FAILED"
	ErrCodeNoPermission         = "NO_PERMISSION_BROADCAST"
	ErrCodeMessageSendFail      = "MESSAGE_SEND_FAILED"
	ErrCodeHistoryFail          = "HISTORY_FETCH_FAILED"
	ErrCodeConversationsFail    = "PRIVATE_CONVERSATIONS_FETCH_FAILED"
	ErrCodePrivateHistoryFail   = "PRIVATE_HISTORY_FETCH_FAILED"
	ErrCodePrivateReadFail      = "PRIVATE_MESSAGES_READ_FAILED"
	ErrCodeConversationReadFail = "PRIVATE_CONVERSATION_READ_FAILED"
	ErrCodeRecallFail           = "PRIVATE_MESSAGE_RECALL_FAILED"
	ErrCodeUnreadFail           = "UNREAD_COUNT_FETCH_FAILED"
	ErrCodeMarkAllReadFail      = "MARK_ALL_READ_FAILED"
	ErrCodeBatchFail            = "BATCH_OPERATION_FAILED"
	ErrCodeMarkReadFail         = "MARK_READ_FAILED"
	ErrCodeStatusUserIDRequired = "USER_STATUS_USER_ID_REQUIRED"
)

// PongPayload pong 事件载荷（对齐 handlePing）。
type PongPayload struct {
	Message   string `json:"message"`
	UserID    string `json:"userId"`
	Timestamp string `json:"timestamp"`
}

// JoinedPayload joined / leaved 事件载荷（对齐 handleJoin / handleLeave）。
type JoinedPayload struct {
	UserID  string `json:"userId"`
	Message string `json:"message"`
	Room    string `json:"room"`
}

// UserStatusPayload 在线状态载荷（对齐 UserPresencePayload）。
type UserStatusPayload struct {
	UserID     string  `json:"userId"`
	IsOnline   bool    `json:"isOnline"`
	LastSeenAt *string `json:"lastSeenAt"`
}

// ProfilePayload getProfile 载荷（源端只给四个字段）。
type ProfilePayload struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
}

// 消息域（message）对外契约：站内消息 + 私信 + 拉黑。
//
// 空值口径（docs/21）：字符串一律值类型（空即 ""）、数组成员必须初始化（空即 []）、
// 对象 / 时间用指针 + omitempty（空即整个字段不传）。**任何字段都不输出 null。**
//
// 两处刻意保留原类型的字段（任务书「message 域特有注意点 2」）：
//   - MessageDto.Metadata / PrivateMessageDto.Payload 是 map[string]interface{}（运行时
//     model.MessageJSON），业务结构不定，**不改成结构体**；空时靠 omitempty 整个省略。
//   - MessageDto.ArticleID / CommentID / TargetID / TargetType / NotificationType 是
//     interface{}（从 metadata 平铺出来的透传值），同样不结构化，空时 omitempty 省略。
//
// WS 事件（socket.io 契约）不在本文件范围：只覆盖 REST 接口的 data。
package dto

import "time"

// ---------------------------------------------------------------------------
// 嵌套子结构
// ---------------------------------------------------------------------------

// MessageUserBriefDto 消息里嵌的收发双方简要（service.UserBriefVO，5 字段）。
//
// 与 article.AuthorBriefDto 不同：这里带 isBlocked（拉黑关系），文章侧带 isFollowed。
// 语义不同，不合并。
type MessageUserBriefDto struct {
	ID        string `json:"id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Username  string `json:"username" example:"xiaoming"`
	Nickname  string `json:"nickname" example:"小明"`
	Avatar    string `json:"avatar" example:"https://cdn.example.com/a.png"`
	IsBlocked bool   `json:"is_blocked" example:"false"`
}

// CursorMetaDto 游标分页元信息（service.CursorMeta）。
//
// 与 PageMeta 不同：私信会话 / 私信历史走游标分页（{limit,hasMore,nextCursor}），
// 不是总页数分页，因此不复用 dto.PageMeta。nextCursor 空时是 "" 不是 null
// （最后一页没有下一页，前端用 hasMore 判断，不需要 null 三态）。
type CursorMetaDto struct {
	Limit      int    `json:"limit" example:"20"`
	HasMore    bool   `json:"has_more" example:"true"`
	NextCursor string `json:"next_cursor" example:"eyJ0aW1lIjoiMjAyNi0xMS0yNVQxMDowMDowMFoiLCJpZCI6ImFiYyJ9"`
}

// ---------------------------------------------------------------------------
// 站内消息响应实体
// ---------------------------------------------------------------------------

// MessageDto 站内消息对外视图（service.MessageVO，含内嵌 model.Message 字段共 19 项）。
//
// 列表（GET /message、GET /message/search）与详情（GET /message/{id}）共用，
// 字段完全一致 —— 不拆 Brief 版本，避免两处漂移。
//
// isRead / isBroadcast 是 bool，**不加 omitempty**：false 是「未读」这一真实语义，
// 省略会让前端无法区分「字段不存在」与「未读」。
type MessageDto struct {
	ID         string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	SenderID   string `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReceiverID string `json:"receiver_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	Content    string `json:"content" example:"你关注的作者发布了新作品"`
	Title      string `json:"title" example:"系统通知"`
	Type       string `json:"type" example:"notification" enums:"private,system,notification"`

	// 布尔：false 有意义，不加 omitempty
	IsRead      bool `json:"is_read" example:"false"`
	IsBroadcast bool `json:"is_broadcast" example:"false"`

	// metadata 是运行时不定的业务结构（jsonb 列），保留 map 原类型，空时整个不传
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	Sender   *MessageUserBriefDto `json:"sender,omitempty"`
	Receiver *MessageUserBriefDto `json:"receiver,omitempty"`

	// 透传载荷：运行时不定的业务结构，保留 map / interface{} 原类型，空时整个不传。
	// 注意：interface{} 字段**不能带 example**，swag 会报
	// "any is unsupported type in example value"（实测踩到）。
	ArticleID        interface{} `json:"article_id,omitempty"`
	CommentID        interface{} `json:"comment_id,omitempty"`
	TargetID         interface{} `json:"target_id,omitempty"`
	TargetType       interface{} `json:"target_type,omitempty"`
	NotificationType interface{} `json:"notification_type,omitempty"`

	CreatedAt time.Time `json:"created_at" example:"2026-11-25T10:00:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2026-11-25T10:00:00Z"`
}

// MessageCreateResultDto 创建 / 全部已读 / 批量操作的 data（service.MessageCreateResult）。
//
// 对齐源端 {success,message,data} 包装；data 只在创建时带内容（单个 MessageDto 或
// MessageDto 数组），已读 / 批量操作不返回 data → 整个字段省略（omitempty）。
type MessageCreateResultDto struct {
	Success bool        `json:"success" example:"true"`
	Message string      `json:"message" example:"消息创建成功"`
	Data    interface{} `json:"data,omitempty"`
}

// UnreadCountDto 未读计数（service.UnreadCountVO，七字段，GET /message/unread/count）。
//
// personal 只算点对点私信，通知单列；total 是七项之和的口径由 service 保证。
type UnreadCountDto struct {
	Personal     int64 `json:"personal" example:"3"`
	Notification int64 `json:"notification" example:"1"`
	Direct       int64 `json:"direct" example:"2"`
	System       int64 `json:"system" example:"0"`
	Private      int64 `json:"private" example:"5"`
	Broadcast    int64 `json:"broadcast" example:"1"`
	Total        int64 `json:"total" example:"12"`
}

// ---------------------------------------------------------------------------
// 私信响应实体
// ---------------------------------------------------------------------------

// PrivateMessageDto 私信对外视图（service.PrivateMessageVO，含内嵌 model.PrivateMessage 共 17 项）。
//
// payload 保留 map[string]interface{} 原类型（运行时不定的卡片/图片结构，不结构化）。
// readAt / recalledAt / recallReason 有「未读 / 未撤回」语义 → 空时整个字段不传，
// 前端用 isRead / isRecalled 布尔判断，不靠 null。
type PrivateMessageDto struct {
	ID             string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ConversationID string `json:"conversation_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	SenderID       string `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReceiverID     string `json:"receiver_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	MessageKind    string `json:"message_kind" example:"text" enums:"text,image,file,card"`
	Content        string `json:"content" example:"在吗"`

	// 透传载荷：保留 map 原类型，空时整个不传
	Payload map[string]interface{} `json:"payload,omitempty"`

	// 时间 / 可选字符串：空时整个不传（未读、未撤回没有自然的空表示）
	ReadAt       *time.Time `json:"read_at,omitempty" example:"2026-11-25T10:00:00Z"`
	RecalledAt   *time.Time `json:"recalled_at,omitempty" example:"2026-11-25T10:05:00Z"`
	RecalledByID string     `json:"recalled_by_id,omitempty"`
	RecallReason string     `json:"recall_reason,omitempty"`

	// 派生字段：isRead / isRecalled 为 false 也是有效语义，不加 omitempty
	Type       string `json:"type" example:"private"`
	IsRead     bool   `json:"is_read" example:"false"`
	IsRecalled bool   `json:"is_recalled" example:"false"`

	Sender   *MessageUserBriefDto `json:"sender,omitempty"`
	Receiver *MessageUserBriefDto `json:"receiver,omitempty"`

	CreatedAt time.Time `json:"created_at" example:"2026-11-25T10:00:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2026-11-25T10:00:00Z"`
}

// ConversationDto 私信会话列表项（service.ConversationVO）。
//
// counterpart / latestMessage 为指针 + omitempty：对方注销或会话无消息时整个字段不传。
// lastMessageAt 是时间，空时不传。
type ConversationDto struct {
	ConversationID string `json:"conversation_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	// CounterpartID 对方用户 ID（扁平字符串）。
	//
	// 为什么与 Counterpart 并存：前端 lib/message-socket.ts 按 `counterpart_id` 解析，
	// 而我们原先只给嵌套对象 `counterpart` → 前端拿不到 id（docs/28 §六 E 段）。
	// 保留对象是为向后兼容（已有调用方读 counterpart.nickname 等）；两者恒一致。
	// 空值时是 ""（不是不传）—— 遵守 docs/21 字符串规则；Counterpart 为 nil 时同样 ""。
	CounterpartID string               `json:"counterpart_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	Counterpart   *MessageUserBriefDto `json:"counterpart,omitempty"`
	LatestMessage *PrivateMessageDto   `json:"latest_message,omitempty"`
	UnreadCount   int64                `json:"unread_count" example:"2"`
	LastMessageAt *time.Time           `json:"last_message_at,omitempty" example:"2026-11-25T10:00:00Z"`
}

// ConversationListDto 会话列表 data（service.ConversationListVO）。
//
// 注意：私信的两个列表接口 data 里是 {data,meta} 而不是 {list,meta}，
// 与站内消息的 PageBody 不同 —— 保持源端契约（对齐 getPrivateConversations）。
type ConversationListDto struct {
	Data []ConversationDto `json:"data"`
	Meta CursorMetaDto     `json:"meta"`
}

// PrivateHistoryDto 私信历史 data（service.PrivateHistoryVO），结构同会话列表。
type PrivateHistoryDto struct {
	Data []PrivateMessageDto `json:"data"`
	Meta CursorMetaDto       `json:"meta"`
}

// ReadResultDto 整会话已读结果（service.ReadResultVO，POST .../{userId}/read）。
//
// messageIds 空时必须是 []（service 用 make 初始化），readAt 空时不传。
type ReadResultDto struct {
	ConversationID string     `json:"conversation_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	CounterpartID  string     `json:"counterpart_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	MessageIDs     []string   `json:"message_ids"`
	ReadAt         *time.Time `json:"read_at,omitempty" example:"2026-11-25T10:00:00Z"`
}

// ReadReceiptDto 单条已读回执（service.ReadReceiptVO，WS 载荷与批量已读结果共用）。
//
// readAt 是 RFC3339 字符串（回执是瞬时事件，一定有值），因此用值类型。
type ReadReceiptDto struct {
	ConversationID string   `json:"conversation_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	SenderID       string   `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReceiverID     string   `json:"receiver_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	MessageIDs     []string `json:"message_ids"`
	ReadAt         string   `json:"read_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
}

// MarkMessagesReadResultDto 批量私信已读结果（POST /message/private/read-batch）。
//
// 取代原先手写的 map[string]interface{}（字段是 snake_case 的 message_ids/read_at，
// 与全站 camelCase 口径不符）；改为具名结构后同时进入 swagger。
type MarkMessagesReadResultDto struct {
	MessageIDs []string         `json:"message_ids"`
	ReadAt     *time.Time       `json:"read_at,omitempty" example:"2026-11-25T10:00:00Z"`
	Receipts   []ReadReceiptDto `json:"receipts"`
}

// ---------------------------------------------------------------------------
// 拉黑
// ---------------------------------------------------------------------------

// BlockedUserDto 拉黑列表项（service.BlockedUserVO）。
//
// reason 为空时是 ""（不是 null）；blockedUser 为指针（用户被硬删时整个字段不传）。
type BlockedUserDto struct {
	ID          string               `json:"id" example:"3c4d5e6f-0000-4a71-8cbd-000000000003"`
	Reason      string               `json:"reason" example:"骚扰"`
	CreatedAt   time.Time            `json:"created_at" example:"2026-11-25T10:00:00Z"`
	BlockedUser *MessageUserBriefDto `json:"blocked_user,omitempty"`
}

// BlockedUserListDto 拉黑列表 data（service 原返回 map{"data": [...]} 的具名形态）。
type BlockedUserListDto struct {
	Data []BlockedUserDto `json:"data"`
}

// BlockResultDto 拉黑 / 取消拉黑结果（POST|DELETE /message/private/block/{userId}）。
//
// 取代原先手写的 map（blocked_user_id 是 snake_case，与全站 camelCase 不符）。
type BlockResultDto struct {
	Success       bool   `json:"success" example:"true"`
	BlockedUserID string `json:"blocked_user_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
}

// ---------------------------------------------------------------------------
// 请求体
//
// 与 service 层同名结构（MessageService / PrivateMessageService 的 Input）字段一一对应：
// handler 实际绑定的是 service 那份，这里只为 swag 提供具名 schema。
// 改字段时**两处必须同步**。
// ---------------------------------------------------------------------------

// CreateMessageRequest 创建站内消息（service.CreateMessageInput，8 字段）。
//
// 三种投递方式按 isBroadcast → receiverIds → receiverId 的优先级判定：
// 广播时 receiver_id 置空；部分发送按 receiverIds 展开成多行。
type CreateMessageRequest struct {
	SenderID    string                 `json:"sender_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReceiverID  string                 `json:"receiver_id" example:"1a2b3c4d-5678-4abc-9def-0123456789ab"`
	ReceiverIDs []string               `json:"receiver_ids"`
	Content     string                 `json:"content" example:"你关注的作者发布了新作品"`
	Title       string                 `json:"title" example:"系统通知"`
	Type        string                 `json:"type" binding:"omitempty,oneof=private system notification" example:"notification"`
	IsBroadcast bool                   `json:"is_broadcast" example:"false"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// UpdateMessageRequest 更新站内消息（service.UpdateMessageInput，指针语义：nil 不改）。
type UpdateMessageRequest struct {
	Content     *string                 `json:"content" example:"改后的正文"`
	Title       *string                 `json:"title" example:"改后的标题"`
	Type        *string                 `json:"type" binding:"omitempty,oneof=private system notification" example:"image"`
	IsRead      *bool                   `json:"is_read" example:"true"`
	IsBroadcast *bool                   `json:"is_broadcast" example:"false"`
	Metadata    *map[string]interface{} `json:"metadata"`
}

// MarkAllReadRequest 全部已读（service.MarkAllReadInput；body 可空）。
type MarkAllReadRequest struct {
	Type        string `json:"type" example:"notification"`
	IsBroadcast bool   `json:"is_broadcast" example:"false"`
}

// BatchMessageRequest 批量已读 / 删除（service.BatchMessageInput）。
type BatchMessageRequest struct {
	MessageIDs []string `json:"message_ids" binding:"required"`
	Action     string   `json:"action" binding:"required" enums:"read,delete" example:"read"`
}

// SendPrivateRequest 发送私信（service.SendPrivateInput，3 字段）。
//
// payload 保留 map 原类型（卡片 / 图片消息的不定业务结构）。
type SendPrivateRequest struct {
	Content     string                 `json:"content" example:"在吗"`
	MessageKind string                 `json:"message_kind" binding:"omitempty,oneof=text image file card" example:"text"`
	Payload     map[string]interface{} `json:"payload"`
}

// PrivateReadBatchRequest 批量私信已读（handler 内匿名 struct 的具名形态）。
type PrivateReadBatchRequest struct {
	MessageIDs []string `json:"message_ids"`
}

// RecallPrivateRequest 撤回私信（body 可空，reason 可选）。
type RecallPrivateRequest struct {
	Reason string `json:"reason" example:"发错人了"`
}

// BlockUserRequest 拉黑（body 可空，reason 可选）。
type BlockUserRequest struct {
	Reason string `json:"reason" example:"骚扰"`
}

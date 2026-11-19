// 消息域服务：私信（会话 / 收发 / 已读回执 / 撤回 / 拉黑）。
//
// 对齐源端 tmp/picart/src/modules/message/private-message.service.ts：
//   - 会话按 (user_one_id, user_two_id) 字典序成对唯一，A→B 与 B→A 同一条；
//   - 游标分页：base64(JSON{time,id})，按 (created_at, id) 降序，多取一条判 hasMore；
//   - 已读：PrivateMessage.read_at，撤回的消息不计未读；
//   - 发送前双向拉黑校验（任一方向拉黑即禁止会话，源端 ensureUsersCanChat）；
//   - 内容校验：text 必须有正文；非 text 必须有 payload，且按 kind 校验必需字段。
//
// 与源端差异：字段命名统一 snake_case；推送时事件名与 payload 结构保持源端语义
// （详见 internal/ws/message_events.go）。
package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
	"gorm.io/gorm"
)

// Cursor 游标分页位置（对齐源端 encodeCursor / parseCursor）。
type Cursor struct {
	Time time.Time
	ID   string
}

// EncodeCursor 生成游标字符串。
func EncodeCursor(t time.Time, id string) string {
	raw, err := json.Marshal(map[string]interface{}{"time": t.UTC().Format(time.RFC3339Nano), "id": id})
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// ParseCursor 解析游标（非法游标返回 nil，按第一页处理，对齐源端 try/catch 语义）。
func ParseCursor(s string) *Cursor {
	if s == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var v struct {
		Time string `json:"time"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v.Time)
	if err != nil || v.ID == "" {
		return nil
	}
	return &Cursor{Time: t, ID: v.ID}
}

// CursorMeta 游标分页元信息（对齐源端 meta:{limit,hasMore,nextCursor}）。
type CursorMeta struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"hasMore"`
	NextCursor *string `json:"nextCursor"`
}

// PrivateMessageVO 私信出参（对齐源端 serializePrivateMessage）。
type PrivateMessageVO struct {
	model.PrivateMessage
	Type       string       `json:"type"` // 固定 "private"
	IsRead     bool         `json:"is_read"`
	IsRecalled bool         `json:"is_recalled"`
	Sender     *UserBriefVO `json:"sender"`
	Receiver   *UserBriefVO `json:"receiver"`
}

// ConversationVO 会话列表项（对齐源端 getPrivateConversations 的 data 元素）。
type ConversationVO struct {
	ConversationID string            `json:"conversation_id"`
	Counterpart    *UserBriefVO      `json:"counterpart"`
	LatestMessage  *PrivateMessageVO `json:"latest_message"`
	UnreadCount    int64             `json:"unread_count"`
	LastMessageAt  *time.Time        `json:"last_message_at"`
}

// ConversationListVO 会话列表结果体。
type ConversationListVO struct {
	Data []ConversationVO `json:"data"`
	Meta CursorMeta       `json:"meta"`
}

// PrivateHistoryVO 私信历史结果体。
type PrivateHistoryVO struct {
	Data []PrivateMessageVO `json:"data"`
	Meta CursorMeta         `json:"meta"`
}

// ReadResultVO 标记已读结果（对齐源端 markConversationAsRead 返回）。
type ReadResultVO struct {
	ConversationID string     `json:"conversation_id"`
	CounterpartID  string     `json:"counterpart_id"`
	MessageIDs     []string   `json:"message_ids"`
	ReadAt         *time.Time `json:"read_at"`
}

// ReadReceiptVO 已读回执（WS privateMessagesReadReceipt / privateConversationRead 载荷）。
type ReadReceiptVO struct {
	ConversationID string   `json:"conversationId"`
	SenderID       string   `json:"senderId"`
	ReceiverID     string   `json:"receiverId"`
	MessageIDs     []string `json:"messageIds"`
	ReadAt         string   `json:"readAt"`
}

// BlockedUserVO 拉黑列表项（对齐源端 getBlockedUsers 的 data 元素）。
type BlockedUserVO struct {
	ID          string       `json:"id"`
	Reason      *string      `json:"reason"`
	CreatedAt   time.Time    `json:"created_at"`
	BlockedUser *UserBriefVO `json:"blockedUser"`
}

// SendPrivateInput 发送私信入参（对齐 SendPrivateMessageDto）。
type SendPrivateInput struct {
	Content     string                 `json:"content"`
	MessageKind string                 `json:"message_kind"`
	Payload     map[string]interface{} `json:"payload"`
}

// PrivateMessageService 私信业务。
type PrivateMessageService struct {
	DB   *gorm.DB
	Msgs repository.MessageRepo
	Priv repository.PrivateMessageRepo
	Pub  MessagePublisher
}

// NewPrivateMessageService 构造。
func NewPrivateMessageService(db *gorm.DB, pub MessagePublisher) *PrivateMessageService {
	return &PrivateMessageService{
		DB:   db,
		Msgs: *repository.NewMessageRepo(),
		Priv: *repository.NewPrivateMessageRepo(),
		Pub:  pub,
	}
}

// ready 数据库可用校验。
func (s *PrivateMessageService) ready() *errcode.Err {
	if s == nil || s.DB == nil {
		return errcode.ErrDependencyUnavailable
	}
	return nil
}

// normalizeKind kind 缺省为 text。
func normalizeKind(k string) string {
	if k == "" {
		return PrivateKindText
	}
	return k
}

// validatePrivate 内容校验（对齐源端 validatePrivateMessage）。
func validatePrivate(in SendPrivateInput) error {
	kind := normalizeKind(in.MessageKind)
	content := strings.TrimSpace(in.Content)
	if kind == PrivateKindText && content == "" {
		return errcode.ErrBadRequest.With("文本消息内容不能为空")
	}
	if kind != PrivateKindText && len(in.Payload) == 0 {
		return errcode.ErrBadRequest.With("非文本消息必须提供 payload")
	}
	has := func(fields ...string) bool {
		for _, f := range fields {
			v, ok := in.Payload[f]
			if !ok || v == nil {
				return false
			}
			if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
				return false
			}
		}
		return true
	}
	switch kind {
	case PrivateKindText:
		return nil
	case PrivateKindImage:
		if !has("url") {
			if urls, ok := in.Payload["urls"].([]interface{}); !ok || len(urls) == 0 {
				return errcode.ErrBadRequest.With("图片消息 payload 需包含 url 或 urls")
			}
		}
	case PrivateKindFile:
		if !has("url", "name") {
			return errcode.ErrBadRequest.With("文件消息 payload 需包含 url 与 name")
		}
	case PrivateKindCard:
		if !has("title", "description") {
			return errcode.ErrBadRequest.With("卡片消息 payload 需包含 title 与 description")
		}
	default:
		return errcode.ErrBadRequest.With("message_kind 仅允许 text/image/file/card")
	}
	return nil
}

// Send 发送私信（对齐源端 sendPrivateMessage）。
//
// 顺序：自聊校验 → 用户存在 → 双向拉黑 → 内容校验 → 会话就绪 → 落库 → 更新会话指针 → 推送。
func (s *PrivateMessageService) Send(senderID, receiverID string, in SendPrivateInput) (*PrivateMessageVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	if receiverID == "" {
		return nil, errcode.ErrBadRequest.With("接收者不能为空")
	}
	if senderID == receiverID {
		return nil, errcode.ErrBadRequest.With("不能给自己发送私信")
	}
	// 接收者必须存在
	var cnt int64
	if err := s.DB.Model(&model.User{}).Where("id = ?", receiverID).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt == 0 {
		return nil, errcode.ErrNotFound.With("用户不存在")
	}
	blocked, err := s.Priv.HasAnyBlock(s.DB, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, errcode.ErrForbidden.With("对方已拉黑你或你已拉黑对方，无法发送私信")
	}
	if err := validatePrivate(in); err != nil {
		return nil, err
	}

	kind := normalizeKind(in.MessageKind)
	conv, err := s.Priv.FindOrCreateConversation(s.DB, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, errcode.ErrInternal.With("会话创建失败")
	}

	m := model.PrivateMessage{
		ConversationID: conv.ID,
		SenderID:       senderID,
		ReceiverID:     receiverID,
		MessageKind:    kind,
		Content:        strings.TrimSpace(in.Content),
		Payload:        model.MessageJSON(in.Payload),
	}
	if err := s.Priv.CreateMessage(s.DB, &m); err != nil {
		return nil, err
	}
	if err := s.Priv.TouchLastMessage(s.DB, conv.ID, m.ID, m.CreatedAt); err != nil {
		return nil, err
	}

	vo := s.toVO(&m, map[string]struct{}{})
	s.emitPrivateMessage(senderID, receiverID, vo)
	return vo, nil
}

// emitPrivateMessage 私信推送（对齐源端 emitPrivateMessage 的 4 条 emit + 会话更新）。
//
// 源端行为：给 receiver 推 newMessage + privateMessage；给 sender 推 newMessage + privateMessage；
// 然后双方各推一次 privateConversationUpdated。
func (s *PrivateMessageService) emitPrivateMessage(senderID, receiverID string, vo *PrivateMessageVO) {
	if s.Pub == nil {
		return
	}
	s.Pub.SendToUser(receiverID, ws.EventNewMessage, vo)
	s.Pub.SendToUser(receiverID, ws.EventPrivateMessage, vo)
	s.Pub.SendToUser(senderID, ws.EventNewMessage, vo)
	s.Pub.SendToUser(senderID, ws.EventPrivateMessage, vo)
	s.emitConversationUpdated(senderID, vo.ConversationID)
	s.emitConversationUpdated(receiverID, vo.ConversationID)
}

// emitConversationUpdated 推会话摘要更新（对齐源端 emitConversationUpdated）。
func (s *PrivateMessageService) emitConversationUpdated(userID, conversationID string) {
	if s.Pub == nil || conversationID == "" {
		return
	}
	summary, err := s.ConversationSummary(userID, conversationID)
	if err != nil || summary == nil {
		return
	}
	s.Pub.SendToUser(userID, ws.EventPrivateConversationUpdated, summary)
}

// emitUnreadCount 推未读数（对齐源端 emitUnreadCount；口径同 MessageService.UnreadCount）。
func (s *PrivateMessageService) emitUnreadCount(userID string) {
	if s.Pub == nil {
		return
	}
	vo, err := s.UnreadCount(userID)
	if err != nil {
		return
	}
	s.Pub.SendToUser(userID, ws.EventUnreadCount, vo)
}

// UnreadCount 未读计数（与 MessageService.UnreadCount 同口径，供私信侧推送复用）。
func (s *PrivateMessageService) UnreadCount(userID string) (*UnreadCountVO, error) {
	byType, err := s.Msgs.CountUnreadByType(s.DB, userID)
	if err != nil {
		return nil, err
	}
	privateUnread, err := s.Priv.CountUnreadPrivate(s.DB, userID)
	if err != nil {
		return nil, err
	}
	broadcastUnread, err := s.Msgs.CountUnreadBroadcast(s.DB, userID)
	if err != nil {
		return nil, err
	}
	direct := byType[MsgTypePrivate]
	system := byType[MsgTypeSystem]
	notification := byType[MsgTypeNotification] + system
	personal := direct + privateUnread
	return &UnreadCountVO{
		Personal:     personal,
		Notification: notification,
		Direct:       direct,
		System:       system,
		Private:      privateUnread,
		Broadcast:    broadcastUnread,
		Total:        personal + notification + broadcastUnread,
	}, nil
}

// Conversations 我的私信会话列表（游标分页，对齐 getPrivateConversations）。
func (s *PrivateMessageService) Conversations(userID, cursor string, limit int) (*ConversationListVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	limit = normCursorLimit(limit)
	cur := ParseCursor(cursor)

	var cursorTime *time.Time
	cursorID := ""
	if cur != nil {
		cursorTime = &cur.Time
		cursorID = cur.ID
	}
	rows, err := s.Priv.ListConversations(s.DB, userID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// 对方 id + 会话 id 批量收集
	counterpartIDs := make([]string, 0, len(rows))
	convIDs := make([]string, 0, len(rows))
	for _, c := range rows {
		if c.UserOneID == userID {
			counterpartIDs = append(counterpartIDs, c.UserTwoID)
		} else {
			counterpartIDs = append(counterpartIDs, c.UserOneID)
		}
		convIDs = append(convIDs, c.ID)
	}
	users := s.loadUsers(counterpartIDs)
	blocked, _ := s.Priv.BlockedIDs(s.DB, userID, counterpartIDs)
	unreadMap, err := s.Priv.CountUnreadByConversation(s.DB, convIDs, userID)
	if err != nil {
		return nil, err
	}

	out := make([]ConversationVO, 0, len(rows))
	for _, c := range rows {
		cpID := c.UserOneID
		if c.UserOneID == userID {
			cpID = c.UserTwoID
		}
		item := ConversationVO{
			ConversationID: c.ID,
			UnreadCount:    unreadMap[c.ID],
			LastMessageAt:  c.LastMessageAt,
		}
		if u, ok := users[cpID]; ok && u != nil {
			item.Counterpart = briefUser(u, blocked)
		}
		if c.LastMessageID != nil {
			if lm, err := s.Priv.FindMessageByID(s.DB, *c.LastMessageID); err == nil && lm != nil {
				item.LatestMessage = s.toVO(lm, blocked)
			}
		}
		out = append(out, item)
	}

	meta := CursorMeta{Limit: limit, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		if last.LastMessageAt != nil {
			nc := EncodeCursor(*last.LastMessageAt, last.ID)
			meta.NextCursor = &nc
		}
	}
	return &ConversationListVO{Data: out, Meta: meta}, nil
}

// ConversationMessages 与某人的私信历史（游标分页 + 自动标记已读，对齐源端 controller）。
func (s *PrivateMessageService) ConversationMessages(userID, counterpartID, cursor string, limit int) (*PrivateHistoryVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	limit = normCursorLimit(limit)
	conv, err := s.Priv.FindConversation(s.DB, userID, counterpartID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return &PrivateHistoryVO{Data: []PrivateMessageVO{}, Meta: CursorMeta{Limit: limit}}, nil
	}

	cur := ParseCursor(cursor)
	var cursorTime *time.Time
	cursorID := ""
	if cur != nil {
		cursorTime = &cur.Time
		cursorID = cur.ID
	}
	rows, err := s.Priv.ListConversationMessages(s.DB, conv.ID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// 拉取后把收到的未读置已读，并把结果回填到本次响应（对齐源端 controller 行为）
	readResult, err := s.markConversationRead(userID, counterpartID)
	if err != nil {
		return nil, err
	}
	readSet := make(map[string]struct{}, len(readResult.MessageIDs))
	for _, id := range readResult.MessageIDs {
		readSet[id] = struct{}{}
	}

	users := s.loadUsers([]string{userID, counterpartID})
	blocked, _ := s.Priv.BlockedIDs(s.DB, userID, []string{userID, counterpartID})
	out := make([]PrivateMessageVO, 0, len(rows))
	for i := range rows {
		vo := s.toVOWith(&rows[i], users, blocked)
		if _, ok := readSet[rows[i].ID]; ok {
			vo.IsRead = true
			if readResult.ReadAt != nil {
				vo.ReadAt = readResult.ReadAt
			}
		}
		out = append(out, *vo)
	}

	if len(readResult.MessageIDs) > 0 && readResult.ConversationID != "" {
		s.emitConversationRead(userID, counterpartID, readResult)
	}

	meta := CursorMeta{Limit: limit, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nc := EncodeCursor(last.CreatedAt, last.ID)
		meta.NextCursor = &nc
	}
	return &PrivateHistoryVO{Data: out, Meta: meta}, nil
}

// markConversationRead 内部实现：把该会话内我未读的私信置已读，返回结果。
func (s *PrivateMessageService) markConversationRead(userID, counterpartID string) (*ReadResultVO, error) {
	if counterpartID == "" || userID == counterpartID {
		return &ReadResultVO{CounterpartID: counterpartID, MessageIDs: []string{}}, nil
	}
	conv, err := s.Priv.FindConversation(s.DB, userID, counterpartID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return &ReadResultVO{CounterpartID: counterpartID, MessageIDs: []string{}}, nil
	}
	ids, err := s.Priv.UnreadInConversation(s.DB, conv.ID, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &ReadResultVO{ConversationID: conv.ID, CounterpartID: counterpartID, MessageIDs: []string{}}, nil
	}
	nowT := time.Now()
	if err := s.Priv.MarkMessagesRead(s.DB, ids, userID, nowT); err != nil {
		return nil, err
	}
	return &ReadResultVO{
		ConversationID: conv.ID,
		CounterpartID:  counterpartID,
		MessageIDs:     ids,
		ReadAt:         &nowT,
	}, nil
}

// MarkConversationRead 对外：整会话标记已读（POST /message/private/conversations/:userId/read）。
func (s *PrivateMessageService) MarkConversationRead(userID, counterpartID string) (*ReadResultVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	res, err := s.markConversationRead(userID, counterpartID)
	if err != nil {
		return nil, err
	}
	if len(res.MessageIDs) > 0 {
		s.emitConversationRead(userID, counterpartID, res)
	}
	return res, nil
}

// MarkMessagesRead 批量标记私信已读（POST /message/private/read-batch）。
//
// 对齐源端 markMessagesAsRead：只处理「我是 receiver 且尚未已读」的消息，
// 并对每条回执向发送者推送 privateMessagesReadReceipt。
func (s *PrivateMessageService) MarkMessagesRead(userID string, messageIDs []string) (map[string]interface{}, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	if len(messageIDs) == 0 {
		return map[string]interface{}{"message_ids": []string{}}, nil
	}
	rows, err := s.Priv.FindMessagesForUser(s.DB, messageIDs, userID)
	if err != nil {
		return nil, err
	}
	unread := make([]model.PrivateMessage, 0, len(rows))
	for i := range rows {
		if rows[i].ReceiverID == userID && rows[i].ReadAt == nil {
			unread = append(unread, rows[i])
		}
	}
	if len(unread) == 0 {
		return map[string]interface{}{"message_ids": []string{}}, nil
	}
	ids := make([]string, 0, len(unread))
	for i := range unread {
		ids = append(ids, unread[i].ID)
	}
	nowT := time.Now()
	if err := s.Priv.MarkMessagesRead(s.DB, ids, userID, nowT); err != nil {
		return nil, err
	}

	receipts := make([]ReadReceiptVO, 0, len(unread))
	seenConv := make(map[string]struct{}, len(unread))
	for i := range unread {
		r := ReadReceiptVO{
			ConversationID: unread[i].ConversationID,
			SenderID:       unread[i].SenderID,
			ReceiverID:     unread[i].ReceiverID,
			MessageIDs:     []string{unread[i].ID},
			ReadAt:         nowT.Format(time.RFC3339),
		}
		receipts = append(receipts, r)
		if s.Pub != nil {
			// 源端逐条向发送者推送回执并刷新双方会话摘要
			s.Pub.SendToUser(r.SenderID, ws.EventPrivateMessagesReadReceipt, r)
		}
		if _, ok := seenConv[r.ConversationID]; !ok {
			seenConv[r.ConversationID] = struct{}{}
			s.emitConversationUpdated(r.SenderID, r.ConversationID)
			s.emitConversationUpdated(r.ReceiverID, r.ConversationID)
		}
	}
	s.emitUnreadCount(userID)

	return map[string]interface{}{
		"message_ids": ids,
		"read_at":     nowT,
		"receipts":    receipts,
	}, nil
}

// emitConversationRead 推已读回执（对齐源端 emitPrivateConversationRead：推给对方 + 自己）。
func (s *PrivateMessageService) emitConversationRead(readerID, counterpartID string, res *ReadResultVO) {
	if s.Pub == nil || res == nil || res.ReadAt == nil {
		return
	}
	receipt := ReadReceiptVO{
		ConversationID: res.ConversationID,
		SenderID:       counterpartID,
		ReceiverID:     readerID,
		MessageIDs:     res.MessageIDs,
		ReadAt:         res.ReadAt.Format(time.RFC3339),
	}
	s.Pub.SendToUser(counterpartID, ws.EventPrivateMessagesReadReceipt, receipt)
	s.Pub.SendToUser(readerID, ws.EventPrivateConversationRead, receipt)
	s.emitConversationUpdated(readerID, res.ConversationID)
	s.emitConversationUpdated(counterpartID, res.ConversationID)
	s.emitUnreadCount(readerID)
}

// Recall 撤回私信（仅发送者；已撤回则幂等返回当前状态，对齐源端 recallMessage）。
func (s *PrivateMessageService) Recall(userID, messageID, reason string) (*PrivateMessageVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	m, err := s.Priv.FindMessageByID(s.DB, messageID)
	if err != nil {
		if repository.IsMessageNotFound(err) {
			return nil, errcode.ErrNotFound.With("私信不存在")
		}
		return nil, err
	}
	if m.SenderID != userID {
		return nil, errcode.ErrForbidden.With("无权撤回该私信")
	}

	blocked, _ := s.Priv.BlockedIDs(s.DB, userID, []string{m.SenderID, m.ReceiverID})
	if m.RecalledAt != nil {
		return s.toVO(m, blocked), nil // 幂等
	}

	// payload 打撤回标记（源端 {...payload, recalled:true}）
	pl := m.Payload
	if pl == nil {
		pl = model.MessageJSON{}
	}
	pl["recalled"] = true
	if err := s.Priv.Recall(s.DB, m.ID, userID, reason, time.Now(), pl); err != nil {
		return nil, err
	}
	updated, err := s.Priv.FindMessageByID(s.DB, m.ID)
	if err != nil {
		return nil, err
	}
	vo := s.toVO(updated, blocked)

	if s.Pub != nil {
		// 源端向发送者与接收者各推一次 privateMessageRecalled，并刷新双方会话
		s.Pub.SendToUser(updated.SenderID, ws.EventPrivateMessageRecalled, vo)
		s.Pub.SendToUser(updated.ReceiverID, ws.EventPrivateMessageRecalled, vo)
		s.emitConversationUpdated(updated.SenderID, updated.ConversationID)
		s.emitConversationUpdated(updated.ReceiverID, updated.ConversationID)
	}
	return vo, nil
}

// Block 拉黑（对齐源端 blockUser：不能拉黑自己、目标必须存在、重复拉黑幂等）。
func (s *PrivateMessageService) Block(userID, targetID string, reason *string) (map[string]interface{}, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	if userID == targetID {
		return nil, errcode.ErrBadRequest.With("不能拉黑自己")
	}
	var cnt int64
	if err := s.DB.Model(&model.User{}).Where("id = ?", targetID).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt == 0 {
		return nil, errcode.ErrNotFound.With("用户不存在")
	}
	existing, err := s.Priv.FindBlock(s.DB, userID, targetID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := s.Priv.CreateBlock(s.DB, userID, targetID, reason); err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{"success": true, "blocked_user_id": targetID}, nil
}

// Unblock 取消拉黑。
func (s *PrivateMessageService) Unblock(userID, targetID string) (map[string]interface{}, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	if err := s.Priv.DeleteBlock(s.DB, userID, targetID); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "blocked_user_id": targetID}, nil
}

// Blocks 我拉黑的用户列表。
func (s *PrivateMessageService) Blocks(userID string) (map[string]interface{}, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	rows, err := s.Priv.ListBlocks(s.DB, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, b := range rows {
		ids = append(ids, b.BlockedUserID)
	}
	users := s.loadUsers(ids)
	out := make([]BlockedUserVO, 0, len(rows))
	for _, b := range rows {
		item := BlockedUserVO{ID: b.ID, Reason: b.Reason, CreatedAt: b.CreatedAt}
		if u, ok := users[b.BlockedUserID]; ok && u != nil {
			item.BlockedUser = &UserBriefVO{
				ID: u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar, IsBlocked: true,
			}
		}
		out = append(out, item)
	}
	return map[string]interface{}{"data": out}, nil
}

// ConversationSummary 会话摘要（供 WS privateConversationUpdated 推送）。
func (s *PrivateMessageService) ConversationSummary(userID, conversationID string) (*ConversationVO, error) {
	conv, err := s.Priv.FindConversationByID(s.DB, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.UserOneID != userID && conv.UserTwoID != userID {
		return nil, nil // 非会话成员，不推送
	}
	cpID := conv.UserOneID
	if conv.UserOneID == userID {
		cpID = conv.UserTwoID
	}
	users := s.loadUsers([]string{cpID})
	blocked, _ := s.Priv.BlockedIDs(s.DB, userID, []string{cpID})
	unreadMap, err := s.Priv.CountUnreadByConversation(s.DB, []string{conv.ID}, userID)
	if err != nil {
		return nil, err
	}
	item := &ConversationVO{
		ConversationID: conv.ID,
		UnreadCount:    unreadMap[conv.ID],
		LastMessageAt:  conv.LastMessageAt,
	}
	if u, ok := users[cpID]; ok && u != nil {
		item.Counterpart = briefUser(u, blocked)
	}
	if conv.LastMessageID != nil {
		if lm, err := s.Priv.FindMessageByID(s.DB, *conv.LastMessageID); err == nil && lm != nil {
			item.LatestMessage = s.toVO(lm, blocked)
		}
	}
	return item, nil
}

// toVO 单条私信转向 VO（内部自查用户信息）。
func (s *PrivateMessageService) toVO(m *model.PrivateMessage, blocked map[string]struct{}) *PrivateMessageVO {
	users := s.loadUsers([]string{m.SenderID, m.ReceiverID})
	return s.toVOWith(m, users, blocked)
}

// toVOWith 用预加载的用户表组装 VO（对齐源端 serializePrivateMessage）。
func (s *PrivateMessageService) toVOWith(m *model.PrivateMessage, users map[string]*model.User, blocked map[string]struct{}) *PrivateMessageVO {
	vo := &PrivateMessageVO{
		PrivateMessage: *m,
		Type:           MsgTypePrivate,
		IsRead:         m.ReadAt != nil,
		IsRecalled:     m.RecalledAt != nil,
	}
	if u, ok := users[m.SenderID]; ok && u != nil {
		vo.Sender = briefUser(u, blocked)
	}
	if u, ok := users[m.ReceiverID]; ok && u != nil {
		vo.Receiver = briefUser(u, blocked)
	}
	// 撤回后正文已清空，前端靠 is_recalled 渲染占位
	return vo
}

// loadUsers 批量取用户（私信侧独立实现，避免与 MessageService 互相持有）。
func (s *PrivateMessageService) loadUsers(ids []string) map[string]*model.User {
	out := make(map[string]*model.User, len(ids))
	seen := make(map[string]struct{}, len(ids))
	uniq := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out
	}
	var users []model.User
	if err := s.DB.Where("id IN ?", uniq).Find(&users).Error; err != nil {
		return out
	}
	for i := range users {
		out[users[i].ID] = &users[i]
	}
	return out
}

// normCursorLimit 游标分页 limit 收敛（源端 clamp 到 [1,100]，默认 20）。
func normCursorLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

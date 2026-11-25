// 消息域服务：站内消息（通知 / 系统消息 / 部分发送）。
//
// 对齐源端 tmp/picart/src/modules/message/message.service.ts；
// 权限与状态机逐条平移：
//   - 全员广播（is_broadcast=true）需要 message:create 权限；
//   - 部分发送（receiver_ids）为每个接收者落一行；
//   - 已读两套语义：非广播改 message.is_read，广播写 message_read 幂等回执；
//   - 未读计数返回 personal/notification/direct/system/private/broadcast/total 七字段。
//
// 与源端的一处加固：源端 create 直接采信 body 里的 senderId（可伪造发送者），
// 这里以登录用户为准；仅当调用者持有 message:manage 时才允许代发（sender_id 覆盖）。
package service

import (
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
	"gorm.io/gorm"
)

// 消息类型枚举（对齐原 message.entity.ts type）。
const (
	MsgTypePrivate      = "private"
	MsgTypeSystem       = "system"
	MsgTypeNotification = "notification"
)

// 私信内容类型（对齐原 PrivateMessageKind）。
const (
	PrivateKindText  = "text"
	PrivateKindImage = "image"
	PrivateKindFile  = "file"
	PrivateKindCard  = "card"
)

// 批量操作动作（对齐源端 action: read|delete）。
const (
	BatchActionRead   = "read"
	BatchActionDelete = "delete"
)

// MessagePublisher WS 推送能力（由 internal/ws 实现；未接线时为 nil，静默跳过）。
//
// 这里用接口而非直接依赖 ws 包，避免 service → ws → service 的循环依赖。
type MessagePublisher interface {
	// SendToUser 定向推送某个用户（多端全部命中，等价 socket.io .to(String(uid)).emit）。
	SendToUser(userID, event string, data interface{})
	// Broadcast 全体推送（等价 socket.io server.emit）。
	Broadcast(event string, data interface{})
}

// UserBriefVO 用户简要信息（消息里嵌的 sender / receiver）。
//
// 结构定义收归 dto（对外契约唯一来源，docs/18），此处保留别名让调用方零改动。
// nickname / avatar 由 *string 改为值类型：空时 "" 而不是 null（docs/21 §一）。
type UserBriefVO = dto.MessageUserBriefDto

// MessageVO 站内消息出参（结构定义见 dto.MessageDto）。
//
// 不再内嵌 model.Message：内嵌会把 model 层的 *string / jsonb 列原样泄漏到对外
// JSON（title 为 null、metadata 为 null），违反 docs/21。改为逐字段组装，
// 转换见 buildMessageVO。
type MessageVO = dto.MessageDto

// MessageCreateResult 创建结果（对齐源端 {success, message, data} 包装）。
type MessageCreateResult = dto.MessageCreateResultDto

// UnreadCountVO 未读计数（七字段，对齐源端 getUnreadCount 返回值）。
type UnreadCountVO = dto.UnreadCountDto

// CreateMessageInput 创建消息入参（对齐 CreateMessageDto）。
type CreateMessageInput struct {
	SenderID    string                 `json:"senderId"`
	ReceiverID  string                 `json:"receiverId"`
	ReceiverIDs []string               `json:"receiverIds"`
	Content     string                 `json:"content"`
	Title       string                 `json:"title"`
	Type        string                 `json:"type"`
	IsBroadcast bool                   `json:"isBroadcast"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// UpdateMessageInput 更新消息入参（指针语义：nil = 不改，对齐源端部分更新）。
type UpdateMessageInput struct {
	Content     *string                 `json:"content"`
	Title       *string                 `json:"title"`
	Type        *string                 `json:"type"`
	IsRead      *bool                   `json:"isRead"`
	IsBroadcast *bool                   `json:"isBroadcast"`
	Metadata    *map[string]interface{} `json:"metadata"`
}

// MarkAllReadInput 全部已读入参（对齐 MarkAllReadDto）。
type MarkAllReadInput struct {
	Type        string `json:"type"`
	IsBroadcast bool   `json:"isBroadcast"`
}

// BatchMessageInput 批量操作入参（对齐 BatchMessageDto）。
type BatchMessageInput struct {
	MessageIDs []string `json:"messageIds" binding:"required"`
	Action     string   `json:"action" binding:"required"`
}

// MessageService 站内消息业务。
type MessageService struct {
	DB    *gorm.DB
	Users repository.UserRepo
	Msgs  repository.MessageRepo
	Priv  repository.PrivateMessageRepo
	Pub   MessagePublisher
}

// NewMessageService 构造（pub 可为 nil：WS 未接线时只落库不推送）。
func NewMessageService(db *gorm.DB, pub MessagePublisher) *MessageService {
	return &MessageService{
		DB:    db,
		Users: *repository.NewUserRepo(),
		Msgs:  *repository.NewMessageRepo(),
		Priv:  *repository.NewPrivateMessageRepo(),
		Pub:   pub,
	}
}

// ready 数据库可用校验（骨架懒连接：DB 为 nil 时返回 503）。
func (s *MessageService) ready() *errcode.Err {
	if s == nil || s.DB == nil {
		return errcode.ErrDependencyUnavailable
	}
	return nil
}

// UserProfileVO 连接鉴权用的用户简要信息（WS connected / getProfile 帧）。
type UserProfileVO struct {
	Username string  `json:"username"`
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
}

// Profile 取用户简要信息（WS 鉴权与 getProfile 用；用户不存在返回错误）。
func (s *MessageService) Profile(userID string) (*UserProfileVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	var u model.User
	if err := s.DB.Select("username", "nickname", "avatar").Where("id = ?", userID).First(&u).Error; err != nil {
		return nil, err
	}
	return &UserProfileVO{Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar}, nil
}

// Create 创建消息（全员 / 部分 / 个人）。
//
// 对齐源端 create：广播时 receiver_id 置空；部分发送按 receiver_ids 展开成多行。
func (s *MessageService) Create(operatorID string, canManage bool, in CreateMessageInput) (*MessageCreateResult, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	content := strings.TrimSpace(in.Content)
	if content == "" && !in.IsBroadcast {
		return nil, errcode.ErrBadRequest.WithKey(i18n.MessageContentEmpty)
	}
	msgType := in.Type
	if msgType == "" {
		msgType = MsgTypePrivate
	}
	switch msgType {
	case MsgTypePrivate, MsgTypeSystem, MsgTypeNotification:
	default:
		return nil, errcode.ErrBadRequest.WithKey(i18n.MessageKindInvalid)
	}

	// 源端要求 isBroadcast 时额外校验 message:create；此处以 message:manage 等价放行
	if in.IsBroadcast && !canManage {
		return nil, errcode.ErrForbidden.WithKey(i18n.MessageBroadcastForbidden)
	}

	// 发送者：默认登录用户；持 message:manage 时允许代发
	senderID := operatorID
	if canManage && in.SenderID != "" {
		senderID = in.SenderID
	}

	var title *string
	if t := strings.TrimSpace(in.Title); t != "" {
		title = &t
	}
	meta := model.MessageJSON(in.Metadata)

	// 1) 全员广播
	if in.IsBroadcast {
		m := model.Message{
			SenderID:    senderID,
			ReceiverID:  nil,
			Content:     content,
			Type:        msgType,
			IsBroadcast: true,
			Title:       title,
			Metadata:    meta,
		}
		if err := s.Msgs.Create(s.DB, &m); err != nil {
			return nil, err
		}
		vo, _ := s.toVO(&m, operatorID, map[string]struct{}{})
		// 全体推送（源端 this.server.emit("newMessage")）
		if s.Pub != nil {
			s.Pub.Broadcast(ws.EventNewMessage, vo)
		}
		return &MessageCreateResult{Success: true, Message: "消息创建成功", Data: vo}, nil
	}

	// 2) 部分发送
	if len(in.ReceiverIDs) > 0 {
		rows := make([]model.Message, 0, len(in.ReceiverIDs))
		for _, rid := range in.ReceiverIDs {
			rid := rid
			if rid == "" || rid == senderID {
				continue
			}
			rows = append(rows, model.Message{
				SenderID:   senderID,
				ReceiverID: &rid,
				Content:    content,
				Type:       msgType,
				Title:      title,
				Metadata:   meta,
			})
		}
		if len(rows) == 0 {
			return nil, errcode.ErrBadRequest.WithKey(i18n.MessageReceiverRequired)
		}
		if err := s.Msgs.CreateBatch(s.DB, rows); err != nil {
			return nil, err
		}
		out := make([]MessageVO, 0, len(rows))
		for i := range rows {
			vo, _ := s.toVO(&rows[i], operatorID, map[string]struct{}{})
			if vo != nil {
				out = append(out, *vo)
				if s.Pub != nil {
					// 源端 notifyUser 逐个推送 newMessage
					s.Pub.SendToUser(*rows[i].ReceiverID, ws.EventNewMessage, vo)
				}
			}
		}
		return &MessageCreateResult{Success: true, Message: "消息创建成功", Data: out}, nil
	}

	// 3) 单发
	if in.ReceiverID == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.MessageReceiverRequired)
	}
	rid := in.ReceiverID
	m := model.Message{
		SenderID:   senderID,
		ReceiverID: &rid,
		Content:    content,
		Type:       msgType,
		Title:      title,
		Metadata:   meta,
	}
	if err := s.Msgs.Create(s.DB, &m); err != nil {
		return nil, err
	}
	vo, _ := s.toVO(&m, operatorID, map[string]struct{}{})
	if s.Pub != nil {
		s.Pub.SendToUser(rid, ws.EventNewMessage, vo)
	}
	return &MessageCreateResult{Success: true, Message: "消息创建成功", Data: vo}, nil
}

// ListByUser 当前用户全部可见消息（含全员通知），对齐 findAllByUser。
func (s *MessageService) ListByUser(userID string, page, limit int) ([]MessageVO, int64, error) {
	return s.list(userID, repository.MessageFilter{}, page, limit)
}

// Search 高级查询（类型 / 已读 / 广播 / 关键词 / 收发人），对齐 findAll。
func (s *MessageService) Search(userID, keyword, msgType, senderID, receiverID string, isRead, isBroadcast *bool, page, limit int) ([]MessageVO, int64, error) {
	return s.list(userID, repository.MessageFilter{
		Type:        msgType,
		IsRead:      isRead,
		IsBroadcast: isBroadcast,
		Keyword:     keyword,
		SenderID:    senderID,
		ReceiverID:  receiverID,
	}, page, limit)
}

// list 列表公共实现：查询 → 广播已读合并 → 用户信息 → 派生字段。
func (s *MessageService) list(userID string, f repository.MessageFilter, page, limit int) ([]MessageVO, int64, error) {
	if e := s.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	rows, total, err := s.Msgs.ListByUser(s.DB, userID, f, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []MessageVO{}, total, nil
	}

	ids := make([]string, 0, len(rows))
	userIDs := make([]string, 0, len(rows)*2)
	for _, m := range rows {
		ids = append(ids, m.ID)
		if m.SenderID != "" {
			userIDs = append(userIDs, m.SenderID)
		}
		if m.ReceiverID != nil && *m.ReceiverID != "" {
			userIDs = append(userIDs, *m.ReceiverID)
		}
	}
	// 广播已读回执（源端 broadcastReadSet）
	readSet, err := s.Msgs.ReadMessageIDs(s.DB, userID, ids)
	if err != nil {
		return nil, 0, err
	}
	blocked, err := s.Priv.BlockedIDs(s.DB, userID, userIDs)
	if err != nil {
		return nil, 0, err
	}
	users := s.loadUsers(userIDs)

	out := make([]MessageVO, 0, len(rows))
	for i := range rows {
		m := rows[i]
		if m.IsBroadcast {
			_, ok := readSet[m.ID]
			m.IsRead = ok // 广播已读以回执为准，覆盖行内值
		}
		vo := buildMessageVO(&m, users, blocked)
		out = append(out, *vo)
	}
	return out, total, nil
}

// FindOne 单条详情（对齐 findOne：非广播仅收发双方可见，广播人人可见）。
func (s *MessageService) FindOne(userID, id string) (*MessageVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	m, err := s.Msgs.FindByID(s.DB, id)
	if err != nil {
		if repository.IsMessageNotFound(err) {
			return nil, errcode.ErrNotFound.WithKey(i18n.MessageNotFound)
		}
		return nil, err
	}
	if !m.IsBroadcast && m.ReceiverID != nil && *m.ReceiverID != userID && m.SenderID != userID {
		return nil, errcode.ErrForbidden.WithKey(i18n.MessageViewForbidden)
	}
	readSet, err := s.Msgs.ReadMessageIDs(s.DB, userID, []string{m.ID})
	if err != nil {
		return nil, err
	}
	if m.IsBroadcast {
		_, ok := readSet[m.ID]
		m.IsRead = ok
	}
	targets := []string{}
	if m.SenderID != "" {
		targets = append(targets, m.SenderID)
	}
	if m.ReceiverID != nil {
		targets = append(targets, *m.ReceiverID)
	}
	blocked, _ := s.Priv.BlockedIDs(s.DB, userID, targets)
	return buildMessageVO(m, s.loadUsers(targets), blocked), nil
}

// Update 更新消息（仅发送者或 message:manage 可改，对齐源端 update 校验）。
func (s *MessageService) Update(userID, id string, canManage bool, in UpdateMessageInput) (*MessageVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	m, err := s.Msgs.FindByID(s.DB, id)
	if err != nil {
		if repository.IsMessageNotFound(err) {
			return nil, errcode.ErrNotFound.WithKey(i18n.MessageNotFound)
		}
		return nil, err
	}
	if m.SenderID != userID && !canManage {
		return nil, errcode.ErrForbidden.WithKey(i18n.MessageUpdateForbidden)
	}

	fields := map[string]interface{}{}
	if in.Content != nil {
		fields["content"] = *in.Content
	}
	if in.Title != nil {
		fields["title"] = *in.Title
	}
	if in.Type != nil {
		switch *in.Type {
		case MsgTypePrivate, MsgTypeSystem, MsgTypeNotification:
			fields["type"] = *in.Type
		default:
			return nil, errcode.ErrBadRequest.WithKey(i18n.MessageKindInvalid)
		}
	}
	if in.IsRead != nil {
		fields["is_read"] = *in.IsRead
	}
	if in.IsBroadcast != nil {
		fields["is_broadcast"] = *in.IsBroadcast
	}
	if in.Metadata != nil {
		fields["metadata"] = model.MessageJSON(*in.Metadata)
	}
	if err := s.Msgs.UpdateFields(s.DB, id, fields); err != nil {
		return nil, err
	}
	return s.FindOne(userID, id)
}

// Remove 删除消息（仅发送者或 message:manage，对齐源端 remove 校验）。
func (s *MessageService) Remove(userID, id string, canManage bool) error {
	if e := s.ready(); e != nil {
		return e
	}
	m, err := s.Msgs.FindByID(s.DB, id)
	if err != nil {
		if repository.IsMessageNotFound(err) {
			return errcode.ErrNotFound.WithKey(i18n.MessageNotFound)
		}
		return err
	}
	if m.SenderID != userID && !canManage {
		return errcode.ErrForbidden.WithKey(i18n.MessageDeleteForbidden)
	}
	return s.Msgs.Delete(s.DB, id)
}

// MarkAsRead 单条标记已读（广播写回执、非广播改行内 is_read）。
func (s *MessageService) MarkAsRead(userID, id string) error {
	if e := s.ready(); e != nil {
		return e
	}
	m, err := s.Msgs.FindByID(s.DB, id)
	if err != nil {
		if repository.IsMessageNotFound(err) {
			return errcode.ErrNotFound.WithKey(i18n.MessageNotFound)
		}
		return err
	}
	if !m.IsBroadcast && m.ReceiverID != nil && *m.ReceiverID != userID {
		return errcode.ErrForbidden.WithKey(i18n.MessageDeleteForbidden)
	}
	if m.IsBroadcast {
		return s.Msgs.InsertReadRecords(s.DB, userID, []string{id})
	}
	return s.Msgs.MarkReadPersonal(s.DB, id)
}

// MarkAllAsRead 全部已读。
//
// 对齐源端 markAllAsRead 的三分支：
//   - type=private：只清私信（PrivateMessage.read_at），不动站内消息；
//   - is_broadcast=true：为全部广播消息补写已读回执；
//   - 其它：把本人收件箱（可按 type 过滤）的非广播消息置已读。
func (s *MessageService) MarkAllAsRead(userID string, in MarkAllReadInput) (*MessageCreateResult, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	nowT := time.Now()
	if in.Type == MsgTypePrivate {
		if err := s.Priv.MarkAllPrivateRead(s.DB, userID, nowT); err != nil {
			return nil, err
		}
		s.publishUnreadCount(userID)
		return &MessageCreateResult{Success: true, Message: "全部消息已标记为已读"}, nil
	}
	if in.IsBroadcast {
		ids, err := s.Msgs.BroadcastIDs(s.DB, in.Type)
		if err != nil {
			return nil, err
		}
		if err := s.Msgs.InsertReadRecords(s.DB, userID, ids); err != nil {
			return nil, err
		}
	} else if err := s.Msgs.MarkReadAllPersonal(s.DB, userID, in.Type); err != nil {
		return nil, err
	}
	s.publishUnreadCount(userID)
	return &MessageCreateResult{Success: true, Message: "全部消息已标记为已读"}, nil
}

// BatchOperation 批量标记已读 / 删除（对齐源端 batchOperation）。
func (s *MessageService) BatchOperation(userID string, canManage bool, in BatchMessageInput) (*MessageCreateResult, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	if len(in.MessageIDs) == 0 {
		return nil, errcode.ErrBadRequest.WithKey(i18n.MessageIDsRequired)
	}
	// 归属校验：非广播消息必须是我发或我收
	rows := make([]model.Message, 0, len(in.MessageIDs))
	for _, id := range in.MessageIDs {
		m, err := s.Msgs.FindByID(s.DB, id)
		if err != nil {
			continue // 不存在的跳过（源端 In() 查不到即忽略）
		}
		if !m.IsBroadcast && m.SenderID != userID && (m.ReceiverID == nil || *m.ReceiverID != userID) {
			return nil, errcode.ErrForbidden.WithKey(i18n.MessageBatchForbidden)
		}
		rows = append(rows, *m)
	}

	switch in.Action {
	case BatchActionRead:
		broadcastIDs := []string{}
		personalIDs := []string{}
		for i := range rows {
			if rows[i].IsBroadcast {
				broadcastIDs = append(broadcastIDs, rows[i].ID)
			} else {
				personalIDs = append(personalIDs, rows[i].ID)
			}
		}
		if err := s.Msgs.InsertReadRecords(s.DB, userID, broadcastIDs); err != nil {
			return nil, err
		}
		if err := s.Msgs.MarkReadPersonalBatch(s.DB, personalIDs, userID); err != nil {
			return nil, err
		}
	case BatchActionDelete:
		for i := range rows {
			if rows[i].SenderID != userID && !canManage {
				return nil, errcode.ErrForbidden.WithKey(i18n.MessageBatchForbidden)
			}
		}
		if err := s.Msgs.DeleteBatch(s.DB, in.MessageIDs); err != nil {
			return nil, err
		}
	default:
		return nil, errcode.ErrBadRequest.WithKey(i18n.MessageActionInvalid)
	}
	s.publishUnreadCount(userID)
	label := "批量标记已读"
	if in.Action == BatchActionDelete {
		label = "批量删除"
	}
	return &MessageCreateResult{Success: true, Message: label + "成功"}, nil
}

// UnreadCount 未读数（七字段，口径对齐源端注释：personal 仅点对点私信，通知单列）。
func (s *MessageService) UnreadCount(userID string) (*UnreadCountVO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
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
	// personal = 站内 private 类 + 私信表未读（源端 personalUnreadCount 口径）
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

// publishUnreadCount 变更后推送未读数（源端 emitUnreadCount 语义）。
func (s *MessageService) publishUnreadCount(userID string) {
	if s.Pub == nil {
		return
	}
	vo, err := s.UnreadCount(userID)
	if err != nil {
		return
	}
	s.Pub.SendToUser(userID, ws.EventUnreadCount, vo)
}

// loadUsers 批量取用户简要信息（不预加载角色，性能友好）。
func (s *MessageService) loadUsers(ids []string) map[string]*model.User {
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

// toVO 单条消息转向 VO（内部复用）。
func (s *MessageService) toVO(m *model.Message, userID string, blocked map[string]struct{}) (*MessageVO, error) {
	targets := []string{}
	if m.SenderID != "" {
		targets = append(targets, m.SenderID)
	}
	if m.ReceiverID != nil {
		targets = append(targets, *m.ReceiverID)
	}
	return buildMessageVO(m, s.loadUsers(targets), blocked), nil
}

// buildMessageVO 组装出参：用户信息 + metadata 派生字段（对齐源端 transformMessage）。
//
// 逐字段赋值而不内嵌 model.Message：title 是 model 层 *string（空值语义）、
// receiverId 也是 *string —— 直接内嵌会输出 null。这里统一走 strOrEmpty，
// 保证对外 JSON 无 null（docs/21）。
func buildMessageVO(m *model.Message, users map[string]*model.User, blocked map[string]struct{}) *MessageVO {
	vo := &MessageVO{
		ID:          m.ID,
		SenderID:    m.SenderID,
		ReceiverID:  strOrEmpty(m.ReceiverID),
		Content:     m.Content,
		Title:       strOrEmpty(m.Title),
		Type:        m.Type,
		IsRead:      m.IsRead,
		IsBroadcast: m.IsBroadcast,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	// metadata 是运行时不定的业务结构：保留 map 原类型，空时整个字段不传（omitempty）
	if len(m.Metadata) > 0 {
		vo.Metadata = m.Metadata
	}
	if u, ok := users[m.SenderID]; ok && u != nil {
		vo.Sender = briefUser(u, blocked)
	}
	if m.ReceiverID != nil {
		if u, ok := users[*m.ReceiverID]; ok && u != nil {
			vo.Receiver = briefUser(u, blocked)
		}
	}
	// 源端把 metadata 里的业务键平铺到响应（articleId/commentId/targetId/targetType/notificationType）。
	// 取不到就保持 nil → omitempty 让整个字段不传（不输出 null）。
	vo.ArticleID = metaValue(m.Metadata, "article_id", "articleId")
	vo.CommentID = metaValue(m.Metadata, "comment_id", "commentId")
	vo.TargetID = metaValue(m.Metadata, "target_id", "targetId")
	vo.TargetType = metaValue(m.Metadata, "target_type", "targetType")
	vo.NotificationType = metaValue(m.Metadata, "notification_type", "notificationType")
	return vo
}

// metaValue 取 metadata 里的业务键（兼容 snake_case 与 camelCase 两种历史写法）。
// 都取不到返回 nil（配合 omitempty 让字段整个不传）。
func metaValue(meta model.MessageJSON, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := meta[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// briefUser 用户简要信息 + 拉黑标记。
//
// nickname / avatar 是 model 层 *string，走 strOrEmpty 转值类型（空时 ""）。
func briefUser(u *model.User, blocked map[string]struct{}) *UserBriefVO {
	_, isBlocked := blocked[u.ID]
	return &UserBriefVO{
		ID:        u.ID,
		Username:  u.Username,
		Nickname:  strOrEmpty(u.Nickname),
		Avatar:    strOrEmpty(u.Avatar),
		IsBlocked: isBlocked,
	}
}

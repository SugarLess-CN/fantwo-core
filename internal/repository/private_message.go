// Package repository message 域数据访问：私信会话 / 私信 / 拉黑。
//
// 对齐源端 private-message.service.ts 的查询面：会话按 (user_one_id, user_two_id)
// 字典序成对唯一；私信游标分页 (created_at, id) 降序；拉黑单向（我拉黑对方）。
package repository

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PrivateMessageRepo 私信域读写。
type PrivateMessageRepo struct{}

// NewPrivateMessageRepo 构造。
func NewPrivateMessageRepo() *PrivateMessageRepo { return &PrivateMessageRepo{} }

// pair 会话双方按字典序归一（源端 [a,b].sort() 语义，A→B 与 B→A 落到同一行）。
func pair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}

// FindConversation 查找两人会话（不存在返回 nil, nil）。
func (PrivateMessageRepo) FindConversation(db *gorm.DB, a, b string) (*model.PrivateConversation, error) {
	one, two := pair(a, b)
	var c model.PrivateConversation
	err := db.Where("user_one_id = ? AND user_two_id = ?", one, two).First(&c).Error
	if err != nil {
		if IsMessageNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// FindOrCreateConversation 查找或创建会话（并发安全：唯一索引 + ON CONFLICT DO NOTHING 后重读）。
func (PrivateMessageRepo) FindOrCreateConversation(db *gorm.DB, a, b string) (*model.PrivateConversation, error) {
	if c, err := (PrivateMessageRepo{}).FindConversation(db, a, b); err != nil || c != nil {
		return c, err
	}
	one, two := pair(a, b)
	created := model.PrivateConversation{UserOneID: one, UserTwoID: two}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&created).Error; err != nil {
		return nil, err
	}
	// DO NOTHING 命中时 created.ID 为空，回读真实行
	return (PrivateMessageRepo{}).FindConversation(db, a, b)
}

// FindConversationByID 按会话主键取（含成员校验由 service 做）。
func (PrivateMessageRepo) FindConversationByID(db *gorm.DB, id string) (*model.PrivateConversation, error) {
	var c model.PrivateConversation
	if err := db.Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListConversations 游标分页列出用户参与的会话（last_message_at desc, id desc）。
// cursorTime/cursorID 为 nil 表示第一页。
func (PrivateMessageRepo) ListConversations(db *gorm.DB, userID string, cursorTime *time.Time, cursorID string, limit int) ([]model.PrivateConversation, error) {
	q := db.Model(&model.PrivateConversation{}).
		Where("user_one_id = ? OR user_two_id = ?", userID, userID).
		Order("last_message_at DESC").Order("id DESC").
		Limit(limit + 1) // 多取一条判 hasMore
	if cursorTime != nil {
		q = q.Where("(last_message_at < ? OR (last_message_at = ? AND id < ?))", *cursorTime, *cursorTime, cursorID)
	}
	var rows []model.PrivateConversation
	err := q.Find(&rows).Error
	return rows, err
}

// TouchLastMessage 更新会话最后一条消息指针。
func (PrivateMessageRepo) TouchLastMessage(db *gorm.DB, conversationID, messageID string, at time.Time) error {
	return db.Model(&model.PrivateConversation{}).
		Where("id = ?", conversationID).
		Updates(map[string]interface{}{"last_message_id": messageID, "last_message_at": at}).Error
}

// CreateMessage 落库私信。
func (PrivateMessageRepo) CreateMessage(db *gorm.DB, m *model.PrivateMessage) error {
	return db.Create(m).Error
}

// FindMessageByID 按主键取私信。
func (PrivateMessageRepo) FindMessageByID(db *gorm.DB, id string) (*model.PrivateMessage, error) {
	var m model.PrivateMessage
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListConversationMessages 会话内消息游标分页（created_at desc, id desc）。
func (PrivateMessageRepo) ListConversationMessages(db *gorm.DB, conversationID string, cursorTime *time.Time, cursorID string, limit int) ([]model.PrivateMessage, error) {
	q := db.Model(&model.PrivateMessage{}).
		Where("conversation_id = ?", conversationID).
		Order("created_at DESC").Order("id DESC").
		Limit(limit + 1)
	if cursorTime != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))", *cursorTime, *cursorTime, cursorID)
	}
	var rows []model.PrivateMessage
	err := q.Find(&rows).Error
	return rows, err
}

// UnreadInConversation 会话内我未读的私信 id 列表（read_at IS NULL 且未撤回）。
func (PrivateMessageRepo) UnreadInConversation(db *gorm.DB, conversationID, userID string) ([]string, error) {
	var ids []string
	err := db.Model(&model.PrivateMessage{}).
		Where("conversation_id = ? AND receiver_id = ? AND read_at IS NULL AND recalled_at IS NULL", conversationID, userID).
		Pluck("id", &ids).Error
	return ids, err
}

// MarkMessagesRead 批量置已读（仅 receiver 本人且未撤回）。
func (PrivateMessageRepo) MarkMessagesRead(db *gorm.DB, ids []string, userID string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&model.PrivateMessage{}).
		Where("id IN ? AND receiver_id = ? AND read_at IS NULL", ids, userID).
		Update("read_at", at).Error
}

// MarkAllPrivateRead 全部私信置已读（对齐源端 markAllAsRead type=private）。
func (PrivateMessageRepo) MarkAllPrivateRead(db *gorm.DB, userID string, at time.Time) error {
	return db.Model(&model.PrivateMessage{}).
		Where("receiver_id = ? AND read_at IS NULL AND recalled_at IS NULL", userID).
		Update("read_at", at).Error
}

// FindMessagesForUser 取一批私信并校验归属（id 命中且 sender/receiver 为当前用户）。
func (PrivateMessageRepo) FindMessagesForUser(db *gorm.DB, ids []string, userID string) ([]model.PrivateMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.PrivateMessage
	err := db.Where("id IN ? AND (sender_id = ? OR receiver_id = ?)", ids, userID, userID).Find(&rows).Error
	return rows, err
}

// Recall 撤回私信：置撤回字段并清空正文（源端 recallMessage 语义）。
func (PrivateMessageRepo) Recall(db *gorm.DB, id, byUserID, reason string, at time.Time, payload model.MessageJSON) error {
	fields := map[string]interface{}{
		"recalled_at":    at,
		"recalled_by_id": byUserID,
		"content":        "",
	}
	if reason != "" {
		fields["recall_reason"] = reason
	}
	if payload != nil {
		fields["payload"] = payload
	}
	return db.Model(&model.PrivateMessage{}).Where("id = ?", id).Updates(fields).Error
}

// CountUnreadPrivate 我未读私信总数（对齐源端 getUnreadCount 的 privateUnreadCount）。
func (PrivateMessageRepo) CountUnreadPrivate(db *gorm.DB, userID string) (int64, error) {
	var count int64
	err := db.Model(&model.PrivateMessage{}).
		Where("receiver_id = ? AND read_at IS NULL AND recalled_at IS NULL", userID).
		Count(&count).Error
	return count, err
}

// CountUnreadByConversation 批量统计多个会话的未读数。
func (PrivateMessageRepo) CountUnreadByConversation(db *gorm.DB, conversationIDs []string, userID string) (map[string]int64, error) {
	out := make(map[string]int64, len(conversationIDs))
	if len(conversationIDs) == 0 {
		return out, nil
	}
	type row struct {
		ConversationID string
		Count          int64
	}
	var rows []row
	err := db.Model(&model.PrivateMessage{}).
		Select("conversation_id, COUNT(*) AS count").
		Where("conversation_id IN ? AND receiver_id = ? AND read_at IS NULL AND recalled_at IS NULL", conversationIDs, userID).
		Group("conversation_id").Scan(&rows).Error
	for _, r0 := range rows {
		out[r0.ConversationID] = r0.Count
	}
	return out, err
}

// ---------- 拉黑 ----------

// ListBlocks 我拉黑的全部关系（created_at desc）。
func (PrivateMessageRepo) ListBlocks(db *gorm.DB, userID string) ([]model.UserBlock, error) {
	var rows []model.UserBlock
	err := db.Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// FindBlock 单条拉黑关系（不存在返回 nil, nil）。
func (PrivateMessageRepo) FindBlock(db *gorm.DB, userID, blockedID string) (*model.UserBlock, error) {
	var b model.UserBlock
	err := db.Where("user_id = ? AND blocked_user_id = ?", userID, blockedID).First(&b).Error
	if err != nil {
		if IsMessageNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// CreateBlock 创建拉黑（复用 user 域 model.UserBlock / 表 user_block）。
// 该表只有普通复合索引、无唯一约束，故幂等由 service 侧先 FindBlock 保证；
// 这里仍带 ON CONFLICT DO NOTHING 兜住并发下的唯一约束（若后续补上）。
func (PrivateMessageRepo) CreateBlock(db *gorm.DB, userID, blockedID string, reason *string) error {
	b := model.UserBlock{UserID: userID, BlockedUserID: blockedID, Reason: reason}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&b).Error
}

// DeleteBlock 取消拉黑。
func (PrivateMessageRepo) DeleteBlock(db *gorm.DB, userID, blockedID string) error {
	return db.Where("user_id = ? AND blocked_user_id = ?", userID, blockedID).
		Delete(&model.UserBlock{}).Error
}

// BlockedIDs 取给定用户中被我拉黑的集合（批量判定 is_blocked）。
func (PrivateMessageRepo) BlockedIDs(db *gorm.DB, userID string, targets []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(targets))
	if len(targets) == 0 {
		return out, nil
	}
	var rows []model.UserBlock
	if err := db.Select("blocked_user_id").
		Where("user_id = ? AND blocked_user_id IN ?", userID, targets).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, b := range rows {
		out[b.BlockedUserID] = struct{}{}
	}
	return out, nil
}

// HasAnyBlock 双向拉黑判定（发送前校验，对齐源端 ensureUsersCanChat）。
func (PrivateMessageRepo) HasAnyBlock(db *gorm.DB, a, b string) (bool, error) {
	var count int64
	err := db.Model(&model.UserBlock{}).
		Where("(user_id = ? AND blocked_user_id = ?) OR (user_id = ? AND blocked_user_id = ?)", a, b, b, a).
		Count(&count).Error
	return count > 0, err
}

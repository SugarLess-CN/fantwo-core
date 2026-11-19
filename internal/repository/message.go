// Package repository message 域数据访问：站内消息（对齐 message.service.ts）。
//
// 约定（docs/04 §2）：repository 只做 GORM 读写封装，不做业务判断；
// 方法一律接受 *gorm.DB（可为事务 tx），返回 error 由上层 service 决策。
// 分页统一返回 (list, total, err)。
//
// 查询语义严格对齐源端：
//   - 可见范围：receiver_id = 当前用户 OR is_broadcast = true（全员通知人人可见）；
//   - 已读状态两套：非广播读 message.is_read，广播读 message_read 是否有当前用户行；
//   - 列表先取 id 再取实体（源端 getPagedMessages 两段式，避免 1:N join 去重）。
package repository

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MessageFilter 站内消息查询过滤（对应源端 QueryMessageDto）。
type MessageFilter struct {
	Type        string // private|system|notification
	IsRead      *bool
	IsBroadcast *bool
	Keyword     string
	SenderID    string
	ReceiverID  string
}

// MessageRepo 站内消息读写。
type MessageRepo struct{}

// NewMessageRepo 构造。
func NewMessageRepo() *MessageRepo { return &MessageRepo{} }

// accessible 「当前用户可见」基础查询（对齐源端 buildAccessibleMessageQuery）。
//
// 注意 soft_delete 插件的删除列是整数时间戳：未经 ORM 的裸 join 必须显式带
// `mr.deleted_at = 0`，否则已软删的已读回执会被误判为存在。
func (MessageRepo) accessible(db *gorm.DB, userID string) *gorm.DB {
	return db.Model(&model.Message{}).
		Where(db.Where("receiver_id = ?", userID).Or("is_broadcast = ?", true))
}

// applyFilter 追加过滤条件（对齐源端 applyMessageFilters）。
func (r MessageRepo) applyFilter(db *gorm.DB, userID string, f MessageFilter) *gorm.DB {
	if f.Type != "" {
		db = db.Where("type = ?", f.Type)
	}
	if f.SenderID != "" {
		db = db.Where("sender_id = ?", f.SenderID)
	}
	if f.ReceiverID != "" {
		db = db.Where("receiver_id = ?", f.ReceiverID)
	}
	if f.Keyword != "" {
		db = db.Where("content LIKE ?", "%"+f.Keyword+"%")
	}
	if f.IsBroadcast != nil {
		db = db.Where("is_broadcast = ?", *f.IsBroadcast)
	}
	if f.IsRead != nil {
		// 广播看逐用户回执，非广播看行内 is_read（源端同一分支语义）
		sub := "EXISTS (SELECT 1 FROM message_read mr WHERE mr.message_id = message.id AND mr.user_id = ? AND mr.deleted_at = 0)"
		if !*f.IsRead {
			sub = "NOT EXISTS (SELECT 1 FROM message_read mr WHERE mr.message_id = message.id AND mr.user_id = ? AND mr.deleted_at = 0)"
		}
		db = db.Where(
			db.Where("is_broadcast = ? AND "+sub, true, userID).
				Or("is_broadcast = ? AND is_read = ?", false, *f.IsRead),
		)
	}
	return db
}

// ListByUser 分页查询当前用户可见消息（对齐 findAllByUser / findAll）。
func (r MessageRepo) ListByUser(db *gorm.DB, userID string, f MessageFilter, page, limit int) ([]model.Message, int64, error) {
	base := r.applyFilter(r.accessible(db, userID), userID, f)

	var total int64
	if err := base.Session(&gorm.Session{}).Distinct("id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []model.Message{}, 0, nil
	}

	var ids []string
	if err := base.Session(&gorm.Session{}).
		Order("created_at DESC").Order("id DESC").
		Offset((page-1)*limit).Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return []model.Message{}, total, nil
	}

	var rows []model.Message
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	// 恢复 id 顺序（IN 查询不保证顺序）
	byID := make(map[string]model.Message, len(rows))
	for _, m := range rows {
		byID[m.ID] = m
	}
	out := make([]model.Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, total, nil
}

// FindByID 按主键取单条。
func (MessageRepo) FindByID(db *gorm.DB, id string) (*model.Message, error) {
	var m model.Message
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// Create 单条创建。
func (MessageRepo) Create(db *gorm.DB, m *model.Message) error { return db.Create(m).Error }

// CreateBatch 批量创建（部分发送：每个接收者一行）。
func (MessageRepo) CreateBatch(db *gorm.DB, rows []model.Message) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Create(&rows).Error
}

// UpdateFields 部分更新（不传即不改，由 service 组装 fields）。
func (MessageRepo) UpdateFields(db *gorm.DB, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return db.Model(&model.Message{}).Where("id = ?", id).Updates(fields).Error
}

// Delete 软删单条。
func (MessageRepo) Delete(db *gorm.DB, id string) error {
	return db.Where("id = ?", id).Delete(&model.Message{}).Error
}

// DeleteBatch 软删多条。
func (MessageRepo) DeleteBatch(db *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Where("id IN ?", ids).Delete(&model.Message{}).Error
}

// MarkReadPersonal 非广播消息置已读。
func (MessageRepo) MarkReadPersonal(db *gorm.DB, id string) error {
	return db.Model(&model.Message{}).Where("id = ?", id).Update("is_read", true).Error
}

// MarkReadPersonalBatch 批量置已读（限当前用户收件箱）。
func (MessageRepo) MarkReadPersonalBatch(db *gorm.DB, ids []string, userID string) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&model.Message{}).
		Where("id IN ? AND receiver_id = ?", ids, userID).
		Update("is_read", true).Error
}

// MarkReadAllPersonal 全部非广播消息置已读，可按 type 过滤。
func (MessageRepo) MarkReadAllPersonal(db *gorm.DB, userID, msgType string) error {
	q := db.Model(&model.Message{}).
		Where("receiver_id = ? AND is_read = ? AND is_broadcast = ?", userID, false, false)
	if msgType != "" {
		q = q.Where("type = ?", msgType)
	}
	return q.Update("is_read", true).Error
}

// BroadcastIDs 取全部广播消息 id（可按 type 过滤），供批量写已读回执。
func (MessageRepo) BroadcastIDs(db *gorm.DB, msgType string) ([]string, error) {
	q := db.Model(&model.Message{}).Where("is_broadcast = ?", true)
	if msgType != "" {
		q = q.Where("type = ?", msgType)
	}
	var ids []string
	err := q.Pluck("id", &ids).Error
	return ids, err
}

// InsertReadRecords 幂等写全员通知已读回执（对齐源端 insert().orIgnore()）。
func (MessageRepo) InsertReadRecords(db *gorm.DB, userID string, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	rows := make([]model.MessageRead, 0, len(messageIDs))
	for _, mid := range messageIDs {
		rows = append(rows, model.MessageRead{UserID: userID, MessageID: mid})
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// ReadMessageIDs 查询给定消息中当前用户已读的集合（广播已读判定）。
func (MessageRepo) ReadMessageIDs(db *gorm.DB, userID string, messageIDs []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}
	var rows []model.MessageRead
	if err := db.Where("user_id = ? AND message_id IN ?", userID, messageIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.MessageID] = struct{}{}
	}
	return out, nil
}

// CountUnreadByType 未读计数（收件箱按 type 分组，仅非广播）。
func (MessageRepo) CountUnreadByType(db *gorm.DB, userID string) (map[string]int64, error) {
	type row struct {
		Type  string
		Count int64
	}
	var rows []row
	err := db.Model(&model.Message{}).
		Select("type, COUNT(*) AS count").
		Where("receiver_id = ? AND is_read = ? AND is_broadcast = ?", userID, false, false).
		Group("type").Scan(&rows).Error
	out := make(map[string]int64, 3)
	for _, r0 := range rows {
		out[r0.Type] = r0.Count
	}
	return out, err
}

// CountUnreadBroadcast 未读广播数（对齐源端 leftJoin message_read IS NULL）。
func (MessageRepo) CountUnreadBroadcast(db *gorm.DB, userID string) (int64, error) {
	var count int64
	err := db.Model(&model.Message{}).
		Where("is_broadcast = ?", true).
		Where("NOT EXISTS (SELECT 1 FROM message_read mr WHERE mr.message_id = message.id AND mr.user_id = ? AND mr.deleted_at = 0)", userID).
		Count(&count).Error
	return count, err
}

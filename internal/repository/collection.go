package repository

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// CollectionRepository 合集域数据访问。
type CollectionRepository struct{ db *gorm.DB }

// NewCollectionRepository 构造合集仓储。
func NewCollectionRepository(db *gorm.DB) *CollectionRepository {
	return &CollectionRepository{db: db}
}

// DB 底层连接。
func (r *CollectionRepository) DB() *gorm.DB { return r.db }

// WithTx 绑定事务。
func (r *CollectionRepository) WithTx(tx *gorm.DB) *CollectionRepository {
	return &CollectionRepository{db: tx}
}

// CollectionListQuery 合集列表条件。
type CollectionListQuery struct {
	UserID  string
	Keyword string
	SortBy  string // sort / latest / itemCount / views
	SortOrd string // ASC / DESC
	Page    int
	Limit   int
}

// List 分页查询合集。
func (r *CollectionRepository) List(ctx context.Context, q CollectionListQuery) ([]model.Collection, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.Collection{})
	if q.UserID != "" {
		base = base.Where("user_id = ?", q.UserID)
	}
	if q.Keyword != "" {
		base = base.Where("name ILIKE ?", "%"+q.Keyword+"%")
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	ord := "DESC"
	if q.OrdIsASC() {
		ord = "ASC"
	}
	column := "sort"
	switch q.SortBy {
	case "latest", "createdAt":
		column = "created_at"
	case "itemCount":
		column = "item_count"
	case "views":
		column = "views"
	}

	var rows []model.Collection
	err := base.Order(column + " " + ord + ", created_at DESC").
		Offset(offsetOf(q.Page, q.Limit)).Limit(limitOf(q.Limit)).Find(&rows).Error
	return rows, total, err
}

// OrdIsASC 排序方向是否升序。
func (q CollectionListQuery) OrdIsASC() bool { return q.SortOrd == "ASC" }

// FindByID 按主键取合集。
func (r *CollectionRepository) FindByID(ctx context.Context, id string) (*model.Collection, error) {
	var c model.Collection
	if err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// Create 落库合集。
func (r *CollectionRepository) Create(ctx context.Context, c *model.Collection) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// Save 保存合集。
func (r *CollectionRepository) Save(ctx context.Context, c *model.Collection) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// UpdateFields 局部更新合集。
func (r *CollectionRepository) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Collection{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除合集。
func (r *CollectionRepository) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Collection{}).Error
}

// IncrCounter 原子增减合集计数（item_count/views）。
func (r *CollectionRepository) IncrCounter(ctx context.Context, id, column string, delta int) error {
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Collection{}).Where("id = ?", id).
		UpdateColumn(column, expr).Error
}

// ---------- 合集项 ----------

// FindItem 查合集内某文章项。
func (r *CollectionRepository) FindItem(ctx context.Context, collectionID, articleID string) (*model.CollectionItem, error) {
	var it model.CollectionItem
	if err := r.db.WithContext(ctx).
		Where("collection_id = ? AND article_id = ?", collectionID, articleID).
		First(&it).Error; err != nil {
		return nil, err
	}
	return &it, nil
}

// CreateItem 新增合集项。
func (r *CollectionRepository) CreateItem(ctx context.Context, it *model.CollectionItem) error {
	return r.db.WithContext(ctx).Create(it).Error
}

// UpdateItemFields 更新合集项（排序/备注）。
func (r *CollectionRepository) UpdateItemFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CollectionItem{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteItem 删除合集项。
func (r *CollectionRepository) DeleteItem(ctx context.Context, collectionID, articleID string) error {
	return r.db.WithContext(ctx).
		Where("collection_id = ? AND article_id = ?", collectionID, articleID).
		Delete(&model.CollectionItem{}).Error
}

// DeleteItemsByCollection 清空合集内全部项（合集删除级联）。
func (r *CollectionRepository) DeleteItemsByCollection(ctx context.Context, collectionID string) error {
	return r.db.WithContext(ctx).Where("collection_id = ?", collectionID).
		Delete(&model.CollectionItem{}).Error
}

// ListItems 分页取合集项（合集中排序优先）。
func (r *CollectionRepository) ListItems(ctx context.Context, collectionID string, page, limit int) ([]model.CollectionItem, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.CollectionItem{}).Where("collection_id = ?", collectionID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.CollectionItem
	err := base.Order("sort DESC, created_at DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// AllItems 取合集全部项（无分页，用于详情聚合）。
func (r *CollectionRepository) AllItems(ctx context.Context, collectionID string) ([]model.CollectionItem, error) {
	var rows []model.CollectionItem
	err := r.db.WithContext(ctx).Where("collection_id = ?", collectionID).
		Order("sort DESC, created_at DESC").Find(&rows).Error
	return rows, err
}

// UserCollectionsContaining 查用户哪些合集包含指定文章（对齐 /collection/check/:articleId）。
func (r *CollectionRepository) UserCollectionsContaining(ctx context.Context, userID, articleID string) ([]model.Collection, error) {
	var collections []model.Collection
	err := r.db.WithContext(ctx).Model(&model.Collection{}).
		Where("user_id = ? AND id IN (SELECT collection_id FROM collection_items WHERE article_id = ?)",
			userID, articleID).
		Order("sort DESC, created_at DESC").Find(&collections).Error
	return collections, err
}

// NextItemSort 合集内下一个排序值（追加到最前）。
func (r *CollectionRepository) NextItemSort(ctx context.Context, collectionID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&model.CollectionItem{}).
		Where("collection_id = ?", collectionID).
		Select("COALESCE(MAX(sort),0)").Scan(&max).Error
	return max + 1, err
}

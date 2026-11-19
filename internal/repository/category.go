package repository

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// CategoryRepository 分类域数据访问。
type CategoryRepository struct{ db *gorm.DB }

// NewCategoryRepository 构造分类仓储。
func NewCategoryRepository(db *gorm.DB) *CategoryRepository { return &CategoryRepository{db: db} }

// DB 底层连接。
func (r *CategoryRepository) DB() *gorm.DB { return r.db }

// WithTx 绑定事务。
func (r *CategoryRepository) WithTx(tx *gorm.DB) *CategoryRepository {
	return &CategoryRepository{db: tx}
}

// CategoryListQuery 分类列表条件。
type CategoryListQuery struct {
	Name     string
	Status   string
	ParentID string // 指定父分类；"root" 语义由 service 用 OnlyRoot 表达
	OnlyRoot bool
	SortBy   string
	SortOrd  string
	Page     int
	Limit    int
}

// List 分页查询分类。
func (r *CategoryRepository) List(ctx context.Context, q CategoryListQuery) ([]model.Category, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.Category{})
	if q.Name != "" {
		base = base.Where("name ILIKE ?", "%"+q.Name+"%")
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	if q.ParentID != "" {
		base = base.Where("parent_id = ?", q.ParentID)
	}
	if q.OnlyRoot {
		base = base.Where("parent_id IS NULL")
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	ord := "DESC"
	if q.SortOrd == "ASC" {
		ord = "ASC"
	}
	column := "sort"
	switch q.SortBy {
	case "articleCount":
		column = "article_count"
	case "followCount":
		column = "follow_count"
	case "latest", "createdAt":
		column = "created_at"
	}

	var rows []model.Category
	err := base.Order(column + " " + ord + ", created_at DESC").
		Offset((q.Page - 1) * q.Limit).Limit(q.Limit).Find(&rows).Error
	return rows, total, err
}

// All 取全部分类（构建分类树用，无分页）。
func (r *CategoryRepository) All(ctx context.Context) ([]model.Category, error) {
	var rows []model.Category
	err := r.db.WithContext(ctx).Order("sort DESC, created_at DESC").Find(&rows).Error
	return rows, err
}

// FindByID 按主键取分类。
func (r *CategoryRepository) FindByID(ctx context.Context, id string) (*model.Category, error) {
	var c model.Category
	if err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// FindByName 按名称取分类（名称唯一）。
func (r *CategoryRepository) FindByName(ctx context.Context, name string) (*model.Category, error) {
	var c model.Category
	if err := r.db.WithContext(ctx).First(&c, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// FindByIDs 批量按主键取分类。
func (r *CategoryRepository) FindByIDs(ctx context.Context, ids []string) ([]model.Category, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.Category
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error
	return rows, err
}

// Children 取直接子分类。
func (r *CategoryRepository) Children(ctx context.Context, parentID string) ([]model.Category, error) {
	var rows []model.Category
	err := r.db.WithContext(ctx).Where("parent_id = ?", parentID).
		Order("sort DESC, created_at DESC").Find(&rows).Error
	return rows, err
}

// HasChildren 是否存在子分类（删除前校验）。
func (r *CategoryRepository) HasChildren(ctx context.Context, id string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Category{}).Where("parent_id = ?", id).Count(&n).Error
	return n > 0, err
}

// Create 落库分类。
func (r *CategoryRepository) Create(ctx context.Context, c *model.Category) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// Save 保存分类。
func (r *CategoryRepository) Save(ctx context.Context, c *model.Category) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// UpdateFields 局部更新分类。
func (r *CategoryRepository) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Category{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除分类。
func (r *CategoryRepository) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Category{}).Error
}

// IncrCounter 原子增减分类计数（article_count/follow_count）。
func (r *CategoryRepository) IncrCounter(ctx context.Context, id, column string, delta int) error {
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Category{}).Where("id = ?", id).
		UpdateColumn(column, expr).Error
}

// CountArticles 统计某分类下文章数（重建冗余计数用）。
func (r *CategoryRepository) CountArticles(ctx context.Context, categoryID, status string) (int64, error) {
	var n int64
	db := r.db.WithContext(ctx).Model(&model.Article{}).Where("category_id = ?", categoryID)
	if status != "" {
		db = db.Where("status = ?", status)
	}
	err := db.Count(&n).Error
	return n, err
}

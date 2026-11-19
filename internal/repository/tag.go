package repository

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// TagRepository 标签域数据访问。
type TagRepository struct{ db *gorm.DB }

// NewTagRepository 构造标签仓储。
func NewTagRepository(db *gorm.DB) *TagRepository { return &TagRepository{db: db} }

// DB 底层连接。
func (r *TagRepository) DB() *gorm.DB { return r.db }

// WithTx 绑定事务。
func (r *TagRepository) WithTx(tx *gorm.DB) *TagRepository { return &TagRepository{db: tx} }

// TagListQuery 标签列表条件。
type TagListQuery struct {
	Name    string // 名称模糊
	SortBy  string // sort / articleCount / followCount / latest
	SortOrd string // ASC / DESC
	Page    int
	Limit   int
}

func tagOrder(q TagListQuery) string {
	ord := "DESC"
	if q.SortOrd == "ASC" {
		ord = "ASC"
	}
	switch q.SortBy {
	case "articleCount":
		return "article_count " + ord + ", sort DESC"
	case "followCount":
		return "follow_count " + ord + ", sort DESC"
	case "latest", "createdAt":
		return "created_at " + ord
	default:
		return "sort " + ord + ", created_at DESC"
	}
}

// List 分页查询标签。
func (r *TagRepository) List(ctx context.Context, q TagListQuery) ([]model.Tag, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.Tag{})
	if q.Name != "" {
		base = base.Where("name ILIKE ?", "%"+q.Name+"%")
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Tag
	err := base.Order(tagOrder(q)).
		Offset((q.Page - 1) * q.Limit).Limit(q.Limit).Find(&rows).Error
	return rows, total, err
}

// Recommended 推荐标签：按文章数降序取前 N（对齐原 /tag/recommended）。
func (r *TagRepository) Recommended(ctx context.Context, page, limit int) ([]model.Tag, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.Tag{})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Tag
	err := base.Order("article_count DESC, follow_count DESC, sort DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// Hot 热门标签统计：按「文章数 × 权重 + 关注数」综合排序，只取有文章或有关注的标签。
func (r *TagRepository) Hot(ctx context.Context, limit int) ([]model.Tag, error) {
	var rows []model.Tag
	err := r.db.WithContext(ctx).
		Where("article_count > 0 OR follow_count > 0").
		Order("(article_count * 2 + follow_count) DESC, sort DESC").
		Limit(limit).Find(&rows).Error
	return rows, err
}

// FindByID 按主键取标签。
func (r *TagRepository) FindByID(ctx context.Context, id string) (*model.Tag, error) {
	var t model.Tag
	if err := r.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// FindByName 按名称取标签（名称唯一）。
func (r *TagRepository) FindByName(ctx context.Context, name string) (*model.Tag, error) {
	var t model.Tag
	if err := r.db.WithContext(ctx).First(&t, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// FindByNames 批量按名称取标签。
func (r *TagRepository) FindByNames(ctx context.Context, names []string) ([]model.Tag, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var rows []model.Tag
	err := r.db.WithContext(ctx).Where("name IN ?", names).Find(&rows).Error
	return rows, err
}

// FindByIDs 批量按主键取标签。
func (r *TagRepository) FindByIDs(ctx context.Context, ids []string) ([]model.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.Tag
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error
	return rows, err
}

// Create 落库标签。
func (r *TagRepository) Create(ctx context.Context, t *model.Tag) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// Save 保存标签。
func (r *TagRepository) Save(ctx context.Context, t *model.Tag) error {
	return r.db.WithContext(ctx).Save(t).Error
}

// UpdateFields 局部更新标签。
func (r *TagRepository) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Tag{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除标签。
func (r *TagRepository) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Tag{}).Error
}

// IncrCounter 原子增减标签计数（article_count/follow_count）。
func (r *TagRepository) IncrCounter(ctx context.Context, id, column string, delta int) error {
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Tag{}).Where("id = ?", id).
		UpdateColumn(column, expr).Error
}

// IncrCounters 批量增减多个标签的同一计数列。
func (r *TagRepository) IncrCounters(ctx context.Context, ids []string, column string, delta int) error {
	if len(ids) == 0 {
		return nil
	}
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Tag{}).Where("id IN ?", ids).
		UpdateColumn(column, expr).Error
}

// ---------- 关注 ----------

// FindFollow 查关注记录。
func (r *TagRepository) FindFollow(ctx context.Context, userID, tagID string) (*model.TagFollow, error) {
	var f model.TagFollow
	if err := r.db.WithContext(ctx).Where("user_id = ? AND tag_id = ?", userID, tagID).
		First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateFollow 新增关注。
func (r *TagRepository) CreateFollow(ctx context.Context, f *model.TagFollow) error {
	return r.db.WithContext(ctx).Create(f).Error
}

// DeleteFollow 取消关注。
func (r *TagRepository) DeleteFollow(ctx context.Context, userID, tagID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND tag_id = ?", userID, tagID).
		Delete(&model.TagFollow{}).Error
}

// FollowedTagIDs 分页取用户关注的标签 ID。
func (r *TagRepository) FollowedTagIDs(ctx context.Context, userID string, page, limit int) ([]string, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.TagFollow{}).Where("user_id = ?", userID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	err := base.Order("created_at DESC").Offset((page-1)*limit).Limit(limit).
		Pluck("tag_id", &ids).Error
	return ids, total, err
}

// FollowedTagIDsByName 按标签名过滤用户关注的标签 ID 列表。
func (r *TagRepository) FollowedTagIDsByName(ctx context.Context, userID, name string) ([]string, error) {
	db := r.db.WithContext(ctx).Model(&model.TagFollow{}).Where("user_id = ?", userID)
	if name != "" {
		db = db.Where("tag_id IN (SELECT id FROM tags WHERE name ILIKE ?)", "%"+name+"%")
	}
	var ids []string
	err := db.Pluck("tag_id", &ids).Error
	return ids, err
}

// UserFollowedSet 取用户已关注标签集合（列表页标记 followed 状态用）。
func (r *TagRepository) UserFollowedSet(ctx context.Context, userID string, tagIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(tagIDs))
	if userID == "" || len(tagIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.TagFollow{}).
		Where("user_id = ? AND tag_id IN ?", userID, tagIDs).Pluck("tag_id", &ids).Error
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

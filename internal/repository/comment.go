package repository

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// CommentRepository 评论域数据访问。
type CommentRepository struct{ db *gorm.DB }

// NewCommentRepository 构造评论仓储。
func NewCommentRepository(db *gorm.DB) *CommentRepository { return &CommentRepository{db: db} }

// DB 底层连接。
func (r *CommentRepository) DB() *gorm.DB { return r.db }

// WithTx 绑定事务。
func (r *CommentRepository) WithTx(tx *gorm.DB) *CommentRepository {
	return &CommentRepository{db: tx}
}

// CommentListQuery 评论列表条件。
type CommentListQuery struct {
	ArticleID  string // 文章内评论
	UserID     string // 指定用户
	ParentID   string // 指定父评论的回复
	RootOnly   bool   // 只看一级评论（root_id 为空）
	Keyword    string // 内容模糊（管理端）
	Status     []string
	OnlyAuthor bool   // 只看帖主（文章作者的评论）
	AuthorID   string // 帖主 ID（配合 OnlyAuthor）
	OrderBy    string // latest / oldest / hot
	Page       int
	Limit      int
}

func applyCommentFilters(db *gorm.DB, q CommentListQuery) *gorm.DB {
	if q.ArticleID != "" {
		db = db.Where("article_id = ?", q.ArticleID)
	}
	if q.UserID != "" {
		db = db.Where("author_id = ?", q.UserID)
	}
	if q.ParentID != "" {
		db = db.Where("parent_id = ?", q.ParentID)
	}
	if q.RootOnly {
		db = db.Where("root_id IS NULL")
	}
	if q.Keyword != "" {
		db = db.Where("content ILIKE ?", "%"+q.Keyword+"%")
	}
	if len(q.Status) > 0 {
		db = db.Where("status IN ?", q.Status)
	}
	if q.OnlyAuthor && q.AuthorID != "" {
		db = db.Where("author_id = ?", q.AuthorID)
	}
	return db
}

func commentOrder(orderBy string) string {
	switch orderBy {
	case "oldest":
		return "is_pinned DESC, created_at ASC"
	case "hot":
		return "is_pinned DESC, likes DESC, created_at DESC"
	default:
		return "is_pinned DESC, created_at DESC"
	}
}

// List 分页查询评论。
func (r *CommentRepository) List(ctx context.Context, q CommentListQuery) ([]model.Comment, int64, error) {
	base := applyCommentFilters(r.db.WithContext(ctx).Model(&model.Comment{}), q)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Comment
	err := base.Order(commentOrder(q.OrderBy)).
		Offset(offsetOf(q.Page, q.Limit)).Limit(limitOf(q.Limit)).Find(&rows).Error
	return rows, total, err
}

// FindByID 按主键取评论。
func (r *CommentRepository) FindByID(ctx context.Context, id string) (*model.Comment, error) {
	var c model.Comment
	if err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// FindReplies 取一组根评论下的全部回复（一次查询，service 侧组树）。
func (r *CommentRepository) FindReplies(ctx context.Context, rootIDs []string) ([]model.Comment, error) {
	if len(rootIDs) == 0 {
		return nil, nil
	}
	var rows []model.Comment
	err := r.db.WithContext(ctx).Where("root_id IN ?", rootIDs).
		Order("created_at ASC").Find(&rows).Error
	return rows, err
}

// ListRepliesPaged 某楼层根下的回复分页（GET /comment/:id 的 replies 分页）。
//
// rootID 是该楼层根的评论 ID（一级评论传自身 ID，二级回复传它的 root_id ——
// 与原 JS comment.service.findCommentDetail 的 `rootID = comment.rootID || comment.id` 同口径）。
// 排序按发表顺序 ASC，与 buildTree 组树顺序一致，避免同一楼层在列表与详情里顺序相反。
// 分页走 pageClause 统一兜底：limit <= 0 → 默认 10，绝不出 `LIMIT 0`。
func (r *CommentRepository) ListRepliesPaged(ctx context.Context, rootID string, page, limit int) ([]model.Comment, int64, error) {
	if rootID == "" {
		return []model.Comment{}, 0, nil
	}
	base := r.db.WithContext(ctx).Model(&model.Comment{}).
		Where("root_id = ? AND status = ?", rootID, model.CommentStatusPublished)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset, size := pageClause(page, limit)
	var rows []model.Comment
	err := base.Order("created_at ASC").Offset(offset).Limit(size).Find(&rows).Error
	return rows, total, err
}

// Create 落库评论。
func (r *CommentRepository) Create(ctx context.Context, c *model.Comment) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// Save 保存评论。
func (r *CommentRepository) Save(ctx context.Context, c *model.Comment) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// UpdateFields 局部更新。
func (r *CommentRepository) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Comment{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除评论。
func (r *CommentRepository) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Comment{}).Error
}

// SoftDeleteByArticle 软删除某文章下全部评论（文章删除级联）。
func (r *CommentRepository) SoftDeleteByArticle(ctx context.Context, articleID string) error {
	return r.db.WithContext(ctx).Where("article_id = ?", articleID).Delete(&model.Comment{}).Error
}

// IncrCounter 原子增减评论计数（likes/reply_count）。
func (r *CommentRepository) IncrCounter(ctx context.Context, id, column string, delta int) error {
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Comment{}).Where("id = ?", id).
		UpdateColumn(column, expr).Error
}

// CountByArticle 某文章已发布评论数（楼层计算与 /comment/article/:id/count 共用）。
func (r *CommentRepository) CountByArticle(ctx context.Context, articleID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Comment{}).
		Where("article_id = ? AND status = ?", articleID, model.CommentStatusPublished).
		Count(&n).Error
	return n, err
}

// MaxFloor 某文章当前最大楼层号。
func (r *CommentRepository) MaxFloor(ctx context.Context, articleID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&model.Comment{}).
		Where("article_id = ? AND root_id IS NULL", articleID).
		Select("COALESCE(MAX(floor),0)").Scan(&max).Error
	return max, err
}

// ClearPinned 清除某文章内的评论置顶（保证同时只有一个置顶）。
func (r *CommentRepository) ClearPinned(ctx context.Context, articleID string) error {
	return r.db.WithContext(ctx).Model(&model.Comment{}).
		Where("article_id = ? AND is_pinned = ?", articleID, true).
		Updates(map[string]interface{}{"is_pinned": false, "pinned_at": nil}).Error
}

// ---------- 评论点赞 ----------

// FindLike 查评论点赞记录。
func (r *CommentRepository) FindLike(ctx context.Context, commentID, userID string) (*model.CommentLike, error) {
	var l model.CommentLike
	if err := r.db.WithContext(ctx).Where("comment_id = ? AND user_id = ?", commentID, userID).
		First(&l).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateLike 新增评论点赞。
func (r *CommentRepository) CreateLike(ctx context.Context, l *model.CommentLike) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// DeleteLike 取消评论点赞。
func (r *CommentRepository) DeleteLike(ctx context.Context, commentID, userID string) error {
	return r.db.WithContext(ctx).Where("comment_id = ? AND user_id = ?", commentID, userID).
		Delete(&model.CommentLike{}).Error
}

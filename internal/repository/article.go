// Package repository 数据访问层：封装 GORM 查询。
//
// 本文件为内容域-文章部分：文章本体、点赞、收藏、不喜欢、浏览历史的读写。
package repository

import (
	"context"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// ArticleRepository 文章域数据访问。
type ArticleRepository struct{ db *gorm.DB }

// NewArticleRepository 构造文章仓储。
func NewArticleRepository(db *gorm.DB) *ArticleRepository { return &ArticleRepository{db: db} }

// DB 暴露底层连接（供 service 开事务）。
func (r *ArticleRepository) DB() *gorm.DB { return r.db }

// WithTx 返回绑定事务的新仓储实例。
func (r *ArticleRepository) WithTx(tx *gorm.DB) *ArticleRepository {
	return &ArticleRepository{db: tx}
}

// ArticleListQuery 文章列表查询条件（全部字段可选，零值即不过滤）。
type ArticleListQuery struct {
	Title      string   // 标题模糊
	Keyword    string   // 关键词（标题/摘要）
	CategoryID string   // 分类
	TagID      string   // 标签（经 article_tags 关联）
	AuthorID   string   // 作者（单值）
	AuthorIDs  []string // 作者集合（关注流：IN 查询）
	Status     []string // 状态白名单；空表示按调用方给定的默认可见性
	ExcludeIDs []string // 排除（不喜欢过滤、相关推荐排除自身）
	OnlyPinned bool     // 仅看个人主页置顶
	OrderBy    string   // latest / popular / sort / relevance
	Page       int
	Limit      int
}

// applyArticleFilters 把查询条件套到 *gorm.DB 上（列表与计数复用，保证两者一致）。
func applyArticleFilters(db *gorm.DB, q ArticleListQuery) *gorm.DB {
	if q.Title != "" {
		db = db.Where("title ILIKE ?", "%"+q.Title+"%")
	}
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		db = db.Where("(title ILIKE ? OR summary ILIKE ?)", like, like)
	}
	if q.CategoryID != "" {
		db = db.Where("category_id = ?", q.CategoryID)
	}
	if q.AuthorID != "" {
		db = db.Where("author_id = ?", q.AuthorID)
	}
	if len(q.AuthorIDs) > 0 {
		db = db.Where("author_id IN ?", q.AuthorIDs)
	}
	if len(q.Status) > 0 {
		db = db.Where("status IN ?", q.Status)
	}
	if len(q.ExcludeIDs) > 0 {
		db = db.Where("id NOT IN ?", q.ExcludeIDs)
	}
	if q.OnlyPinned {
		db = db.Where("is_pinned_on_profile = ?", true)
	}
	if q.TagID != "" {
		db = db.Where("id IN (SELECT article_id FROM article_tags WHERE tag_id = ?)", q.TagID)
	}
	return db
}

// articleOrder 返回排序子句（对齐原 type 语义）。
func articleOrder(orderBy string) string {
	switch strings.ToLower(orderBy) {
	case "popular":
		return "views DESC, created_at DESC"
	case "latest":
		return "created_at DESC"
	case "relevance":
		return "likes DESC, views DESC, created_at DESC"
	default:
		return "sort DESC, created_at DESC"
	}
}

// List 分页查询文章列表。
func (r *ArticleRepository) List(ctx context.Context, q ArticleListQuery) ([]model.Article, int64, error) {
	base := applyArticleFilters(r.db.WithContext(ctx).Model(&model.Article{}), q)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []model.Article
	err := base.Order(articleOrder(q.OrderBy)).
		Offset((q.Page - 1) * q.Limit).
		Limit(q.Limit).
		Find(&rows).Error
	return rows, total, err
}

// Count 按条件统计。
func (r *ArticleRepository) Count(ctx context.Context, q ArticleListQuery) (int64, error) {
	var total int64
	err := applyArticleFilters(r.db.WithContext(ctx).Model(&model.Article{}), q).Count(&total).Error
	return total, err
}

// FindByID 按主键取文章（不存在返回 gorm.ErrRecordNotFound）。
func (r *ArticleRepository) FindByID(ctx context.Context, id string) (*model.Article, error) {
	var a model.Article
	if err := r.db.WithContext(ctx).First(&a, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// FindByIDs 批量取文章，返回 id→文章 映射。
// FindBriefByIDs 批量取文章**精简**字段（评论内嵌 article 用，禁止在循环里查）。
//
// 只 select 卡片需要的列（id/title/cover/summary/author_id/category_id），
// 不读 content / images / 互动计数 —— 评论列表里每条评论都带一份文章摘要，
// 查全列会把正文一起拉回来，20 条评论就是 20 份正文。
func (r *ArticleRepository) FindBriefByIDs(ctx context.Context, ids []string) (map[string]*model.Article, error) {
	out := make(map[string]*model.Article, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	uniq := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []model.Article
	err := r.db.WithContext(ctx).
		Select("id", "title", "cover", "summary", "author_id", "category_id").
		Where("id IN ?", uniq).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}

func (r *ArticleRepository) FindByIDs(ctx context.Context, ids []string) (map[string]*model.Article, error) {
	out := make(map[string]*model.Article, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.Article
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}

// Create 落库新文章。
func (r *ArticleRepository) Create(ctx context.Context, a *model.Article) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// Save 全量保存文章（含计数与状态变更）。
func (r *ArticleRepository) Save(ctx context.Context, a *model.Article) error {
	return r.db.WithContext(ctx).Save(a).Error
}

// UpdateFields 局部更新文章字段（map 形式，含零值时用）。
func (r *ArticleRepository) UpdateFields(ctx context.Context, id string, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Article{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDelete 软删除文章（写软删列，保留数据可追溯）。
func (r *ArticleRepository) SoftDelete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Article{}).Error
}

// IncrCounter 原子增减文章计数列（views/likes/favorite_count/comment_count/shares）。
// delta 为负时不会把计数减到负数（GREATEST 兜底）。
func (r *ArticleRepository) IncrCounter(ctx context.Context, id, column string, delta int) error {
	expr := gorm.Expr(column+" + ?", delta)
	if delta < 0 {
		expr = gorm.Expr("GREATEST("+column+" + ?, 0)", delta)
	}
	return r.db.WithContext(ctx).Model(&model.Article{}).Where("id = ?", id).
		UpdateColumn(column, expr).Error
}

// ---------- 标签关联 ----------

// ReplaceArticleTags 覆盖式写入文章标签关联（先删后插），并可返回被移除的标签 ID。
func (r *ArticleRepository) ReplaceArticleTags(ctx context.Context, articleID string, tagIDs []string) (removed []string, added []string, err error) {
	var old []string
	if err = r.db.WithContext(ctx).Model(&model.ArticleTag{}).
		Where("article_id = ?", articleID).Pluck("tag_id", &old).Error; err != nil {
		return nil, nil, err
	}
	oldSet := make(map[string]struct{}, len(old))
	for _, id := range old {
		oldSet[id] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(tagIDs))
	for _, id := range tagIDs {
		newSet[id] = struct{}{}
		if _, ok := oldSet[id]; !ok {
			added = append(added, id)
		}
	}
	for _, id := range old {
		if _, ok := newSet[id]; !ok {
			removed = append(removed, id)
		}
	}

	if err = r.db.WithContext(ctx).Where("article_id = ?", articleID).
		Delete(&model.ArticleTag{}).Error; err != nil {
		return nil, nil, err
	}
	if len(tagIDs) == 0 {
		return removed, added, nil
	}
	rows := make([]model.ArticleTag, 0, len(tagIDs))
	for _, id := range tagIDs {
		rows = append(rows, model.ArticleTag{ArticleID: articleID, TagID: id})
	}
	if err = r.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, nil, err
	}
	return removed, added, nil
}

// ArticleTagIDs 取单篇文章的标签 ID 列表。
func (r *ArticleRepository) ArticleTagIDs(ctx context.Context, articleID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ArticleTag{}).
		Where("article_id = ?", articleID).Pluck("tag_id", &ids).Error
	return ids, err
}

// ArticleTagMap 批量取文章→标签 ID 列表映射。
func (r *ArticleRepository) ArticleTagMap(ctx context.Context, articleIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(articleIDs))
	if len(articleIDs) == 0 {
		return out, nil
	}
	var rows []model.ArticleTag
	if err := r.db.WithContext(ctx).Where("article_id IN ?", articleIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ArticleID] = append(out[row.ArticleID], row.TagID)
	}
	return out, nil
}

// DeleteArticleTags 清空某文章的标签关联（文章删除/标签移除时调用）。
func (r *ArticleRepository) DeleteArticleTags(ctx context.Context, articleID string) error {
	return r.db.WithContext(ctx).Where("article_id = ?", articleID).Delete(&model.ArticleTag{}).Error
}

// ---------- 点赞 ----------

// FindLike 查某用户对某文章的点赞记录。
func (r *ArticleRepository) FindLike(ctx context.Context, articleID, userID string) (*model.ArticleLike, error) {
	var l model.ArticleLike
	if err := r.db.WithContext(ctx).Where("article_id = ? AND user_id = ?", articleID, userID).
		First(&l).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateLike 新增点赞记录。
func (r *ArticleRepository) CreateLike(ctx context.Context, l *model.ArticleLike) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// UpdateLikeReaction 更新表情类型。
func (r *ArticleRepository) UpdateLikeReaction(ctx context.Context, id, reaction string) error {
	return r.db.WithContext(ctx).Model(&model.ArticleLike{}).Where("id = ?", id).
		Update("reaction_type", reaction).Error
}

// DeleteLike 删除点赞记录（软删）。
func (r *ArticleRepository) DeleteLike(ctx context.Context, articleID, userID string) error {
	return r.db.WithContext(ctx).Where("article_id = ? AND user_id = ?", articleID, userID).
		Delete(&model.ArticleLike{}).Error
}

// LikeCount 按表情类型统计（reaction 为空则统计全部）。
func (r *ArticleRepository) LikeCount(ctx context.Context, articleID, reaction string) (int64, error) {
	db := r.db.WithContext(ctx).Model(&model.ArticleLike{}).Where("article_id = ?", articleID)
	if reaction != "" {
		db = db.Where("reaction_type = ?", reaction)
	}
	var n int64
	err := db.Count(&n).Error
	return n, err
}

// LikedArticleIDs 分页取用户点赞过的文章 ID（按点赞时间倒序）。
func (r *ArticleRepository) LikedArticleIDs(ctx context.Context, userID string, page, limit int) ([]string, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.ArticleLike{}).Where("user_id = ?", userID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	err := base.Order("created_at DESC").Offset((page-1)*limit).Limit(limit).
		Pluck("article_id", &ids).Error
	return ids, total, err
}

// ---------- 收藏 ----------

// FindFavorite 查收藏记录。
func (r *ArticleRepository) FindFavorite(ctx context.Context, userID, articleID string) (*model.ArticleFavorite, error) {
	var f model.ArticleFavorite
	if err := r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateFavorite 新增收藏。
func (r *ArticleRepository) CreateFavorite(ctx context.Context, f *model.ArticleFavorite) error {
	return r.db.WithContext(ctx).Create(f).Error
}

// DeleteFavorite 取消收藏（软删）。
func (r *ArticleRepository) DeleteFavorite(ctx context.Context, userID, articleID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		Delete(&model.ArticleFavorite{}).Error
}

// FavoritedArticleIDs 分页取用户收藏的文章 ID（收藏时间倒序）。
func (r *ArticleRepository) FavoritedArticleIDs(ctx context.Context, userID string, page, limit int) ([]string, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.ArticleFavorite{}).Where("user_id = ?", userID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	err := base.Order("created_at DESC").Offset((page-1)*limit).Limit(limit).
		Pluck("article_id", &ids).Error
	return ids, total, err
}

// ---------- 不喜欢 ----------

// FindDislike 查不喜欢记录。
func (r *ArticleRepository) FindDislike(ctx context.Context, userID, articleID string) (*model.ArticleDislike, error) {
	var d model.ArticleDislike
	if err := r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDislike 新增不喜欢记录。
func (r *ArticleRepository) CreateDislike(ctx context.Context, d *model.ArticleDislike) error {
	return r.db.WithContext(ctx).Create(d).Error
}

// DeleteDislike 取消不喜欢（软删）。
func (r *ArticleRepository) DeleteDislike(ctx context.Context, userID, articleID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		Delete(&model.ArticleDislike{}).Error
}

// DislikedArticleIDs 分页取用户不喜欢的文章 ID。
func (r *ArticleRepository) DislikedArticleIDs(ctx context.Context, userID string, page, limit int) ([]string, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.ArticleDislike{}).Where("user_id = ?", userID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	err := base.Order("created_at DESC").Offset((page-1)*limit).Limit(limit).
		Pluck("article_id", &ids).Error
	return ids, total, err
}

// AllDislikedArticleIDs 取用户全部不喜欢文章 ID（推荐流排除用）。
func (r *ArticleRepository) AllDislikedArticleIDs(ctx context.Context, userID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ArticleDislike{}).
		Where("user_id = ?", userID).Pluck("article_id", &ids).Error
	return ids, err
}

// ---------- 浏览历史 ----------

// FindBrowse 查浏览记录。
func (r *ArticleRepository) FindBrowse(ctx context.Context, userID, articleID string) (*model.BrowseHistory, error) {
	var b model.BrowseHistory
	if err := r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateBrowse 新增浏览记录。
func (r *ArticleRepository) CreateBrowse(ctx context.Context, b *model.BrowseHistory) error {
	return r.db.WithContext(ctx).Create(b).Error
}

// SaveBrowse 保存浏览记录（累加次数、更新进度）。
func (r *ArticleRepository) SaveBrowse(ctx context.Context, b *model.BrowseHistory) error {
	return r.db.WithContext(ctx).Save(b).Error
}

// ListBrowse 分页取浏览历史（最后浏览时间倒序）。
func (r *ArticleRepository) ListBrowse(ctx context.Context, userID string, page, limit int, order string) ([]model.BrowseHistory, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.BrowseHistory{}).Where("user_id = ?", userID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 排序方向：newest（默认，最近浏览在前）/ oldest（最早浏览在前）。
	// 取值对齐原 JS QueryBrowseHistoryDto.order 的 enum（newest / oldest）。
	// 其它值一律按 newest —— 与 browser history 的默认行为一致，不让非法参数变成空列表。
	sqlOrder := "updated_at DESC"
	if strings.EqualFold(strings.TrimSpace(order), "oldest") {
		sqlOrder = "updated_at ASC"
	}
	offset, size := pageClause(page, limit) // limit<=0 → 默认 10，绝不出 LIMIT 0
	var rows []model.BrowseHistory
	err := base.Order(sqlOrder).Offset(offset).Limit(size).Find(&rows).Error
	return rows, total, err
}

// RecentBrowse 取最近浏览记录。
func (r *ArticleRepository) RecentBrowse(ctx context.Context, userID string, limit int) ([]model.BrowseHistory, error) {
	var rows []model.BrowseHistory
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("updated_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// BrowseStats 浏览统计：记录数、总浏览次数、平均进度、平均停留时长。
func (r *ArticleRepository) BrowseStats(ctx context.Context, userID string) (totalRecords int64, totalViews int64, avgProgress float64, avgDuration float64, err error) {
	row := struct {
		Cnt      int64
		Views    int64
		Progress float64
		Duration float64
	}{}
	err = r.db.WithContext(ctx).Model(&model.BrowseHistory{}).
		Select("COUNT(*) AS cnt, COALESCE(SUM(view_count),0) AS views, COALESCE(AVG(progress),0) AS progress, COALESCE(AVG(duration),0) AS duration").
		Where("user_id = ?", userID).Scan(&row).Error
	return row.Cnt, row.Views, row.Progress, row.Duration, err
}

// DeleteBrowse 删除单条浏览记录。
func (r *ArticleRepository) DeleteBrowse(ctx context.Context, userID, articleID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND article_id = ?", userID, articleID).
		Delete(&model.BrowseHistory{}).Error
}

// DeleteBrowseBatch 批量删除浏览记录。
func (r *ArticleRepository) DeleteBrowseBatch(ctx context.Context, userID string, articleIDs []string) error {
	if len(articleIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("user_id = ? AND article_id IN ?", userID, articleIDs).
		Delete(&model.BrowseHistory{}).Error
}

// ClearBrowse 清空用户浏览历史。
func (r *ArticleRepository) ClearBrowse(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.BrowseHistory{}).Error
}

// PublishedArticleIDs 取全部已发布文章 ID（对齐原 /article/published/ids，供 sitemap 等离线消费）。
func (r *ArticleRepository) PublishedArticleIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("status = ?", model.ArticleStatusPublished).
		Order("created_at DESC").Pluck("id", &ids).Error
	return ids, err
}

// ---------- 批量互动状态（列表页避免 N+1） ----------

// LikedSet 批量取用户对一组文章的点赞状态集合。
func (r *ArticleRepository) LikedSet(ctx context.Context, userID string, articleIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(articleIDs))
	if userID == "" || len(articleIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ArticleLike{}).
		Where("user_id = ? AND article_id IN ?", userID, articleIDs).
		Pluck("article_id", &ids).Error
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// FavoritedSet 批量取用户对一组文章的收藏状态集合。
func (r *ArticleRepository) FavoritedSet(ctx context.Context, userID string, articleIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(articleIDs))
	if userID == "" || len(articleIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ArticleFavorite{}).
		Where("user_id = ? AND article_id IN ?", userID, articleIDs).
		Pluck("article_id", &ids).Error
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// DislikedSet 批量取用户对一组文章的「不喜欢」状态集合。
func (r *ArticleRepository) DislikedSet(ctx context.Context, userID string, articleIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(articleIDs))
	if userID == "" || len(articleIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.ArticleDislike{}).
		Where("user_id = ? AND article_id IN ?", userID, articleIDs).
		Pluck("article_id", &ids).Error
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// ---------------------------------------------------------------------------
// 下载资源（downloads 表，对齐原 NestJS article.downloads 一对多）
// ---------------------------------------------------------------------------

// ReplaceDownloads 全量替换某文章的下载资源（先删后插，事务内调用）。
//
// 为什么全量替换而不是增量 diff：原版的语义就是「提交什么就是什么」，
// 增量 diff 需要客户端带 ID 才能区分新增/修改/删除，而前端表单通常不维护 ID。
func (r *ArticleRepository) ReplaceDownloads(ctx context.Context, articleID string, items []model.Download) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("article_id = ?", articleID).Delete(&model.Download{}).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	for i := range items {
		items[i].ArticleID = articleID
		items[i].Sort = i
	}
	return db.Create(&items).Error
}

// FindDownloadsByArticle 查单篇文章的下载资源（按 sort 升序）。
func (r *ArticleRepository) FindDownloadsByArticle(ctx context.Context, articleID string) ([]model.Download, error) {
	var out []model.Download
	err := r.db.WithContext(ctx).
		Where("article_id = ?", articleID).
		Order("sort ASC").
		Find(&out).Error
	return out, err
}

// FindDownloadsByArticles 批量查多篇文章的下载资源（避免列表页 N+1）。
//
// 返回 map[articleID][]Download，未命中的文章不在 map 里（调用方按空切片处理）。
func (r *ArticleRepository) FindDownloadsByArticles(ctx context.Context, articleIDs []string) (map[string][]model.Download, error) {
	out := make(map[string][]model.Download, len(articleIDs))
	if len(articleIDs) == 0 {
		return out, nil
	}
	var rows []model.Download
	if err := r.db.WithContext(ctx).
		Where("article_id IN ?", articleIDs).
		Order("sort ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, d := range rows {
		out[d.ArticleID] = append(out[d.ArticleID], d)
	}
	return out, nil
}

// ---------- 表情统计（批量，避免列表页 N+1） ----------

// ReactionStatsRow 单篇文章的单个表情计数（GROUP BY 结果行）。
type ReactionStatsRow struct {
	ArticleID    string `gorm:"column:article_id"`
	ReactionType string `gorm:"column:reaction_type"`
	Count        int    `gorm:"column:cnt"`
}

// ReactionStatsMap 批量统计多篇文章的各表情计数。
//
// 返回 map[articleID]map[reactionType]count。未命中的文章不在 map 里 ——
// 调用方用 emptyReactionStats() 兜底成「全 0 对象」而不是 null（对齐原
// buildEmptyReactionStats() 与 docs/21）。
func (r *ArticleRepository) ReactionStatsMap(ctx context.Context, articleIDs []string) (map[string]map[string]int, error) {
	out := make(map[string]map[string]int, len(articleIDs))
	if len(articleIDs) == 0 {
		return out, nil
	}
	var rows []ReactionStatsRow
	err := r.db.WithContext(ctx).Model(&model.ArticleLike{}).
		Select("article_id, reaction_type, COUNT(*) AS cnt").
		Where("article_id IN ?", articleIDs).
		Group("article_id, reaction_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		m, ok := out[row.ArticleID]
		if !ok {
			m = make(map[string]int, 7)
			out[row.ArticleID] = m
		}
		m[row.ReactionType] = row.Count
	}
	return out, nil
}

// ReactionSet 批量取「当前用户对多篇文章的表情类型」。
//
// 返回 map[articleID]reactionType。未命中表示没表态（调用方给 ""，不是 null）。
func (r *ArticleRepository) ReactionSet(ctx context.Context, userID string, articleIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(articleIDs))
	if userID == "" || len(articleIDs) == 0 {
		return out, nil
	}
	type row struct {
		ArticleID    string `gorm:"column:article_id"`
		ReactionType string `gorm:"column:reaction_type"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.ArticleLike{}).
		Select("article_id, reaction_type").
		Where("user_id = ? AND article_id IN ?", userID, articleIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ArticleID] = row.ReactionType
	}
	return out, nil
}

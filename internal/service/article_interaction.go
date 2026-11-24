package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ---------- 点赞 / 表情回复 ----------

// LikeArticle 点赞/表情回复文章（对齐 POST /article/:id/like）。
//
// 语义：同一用户同一文章仅一条记录；已存在则更新表情类型（不重复计数），
// 不存在则插入并给文章 likes +1。reactionType 缺省 like。
func (s *ArticleService) Like(ctx context.Context, articleID string, actor Actor, req ArticleReactionRequest) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	article, e0 := s.getArticle(ctx, articleID)
	if e0 != nil {
		return nil, e0
	}

	reaction := req.ReactionType
	if reaction == "" {
		reaction = model.ReactionLike
	}

	existing, err := s.repo.FindLike(ctx, articleID, actor.UserID)
	switch {
	case err == nil:
		if existing.ReactionType != reaction {
			if err := s.repo.UpdateLikeReaction(ctx, existing.ID, reaction); err != nil {
				s.log.Error("更新表情失败", zap.Error(err))
				return nil, errcode.ErrInternal
			}
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := s.repo.CreateLike(ctx, &model.ArticleLike{
			ArticleID:    articleID,
			UserID:       actor.UserID,
			ReactionType: reaction,
		}); err != nil {
			s.log.Error("写入点赞失败", zap.Error(err))
			return nil, errcode.ErrInternal
		}
		if err := s.repo.IncrCounter(ctx, articleID, "likes", 1); err != nil {
			s.log.Warn("点赞计数自增失败", zap.Error(err))
		}
		// 仅「首次点赞」发布事件（改表情/重复点赞不算），推进的是文章作者成就。
		publishArticleReceivedLike(article, actor.UserID)
	default:
		s.log.Error("查询点赞失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	count, _ := s.repo.LikeCount(ctx, articleID, "")
	out := map[string]interface{}{"isLiked": true, "reactionType": reaction, "likes": count}
	return &out, nil
}

// GetLikeStatus 点赞状态（对齐 GET /article/:id/like/status）。
func (s *ArticleService) GetLikeStatus(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	out := map[string]interface{}{"isLiked": false, "reactionType": nil}
	if !actor.Anonymous() {
		if l, err := s.repo.FindLike(ctx, articleID, actor.UserID); err == nil {
			out["isLiked"] = true
			out["reactionType"] = l.ReactionType
		}
	}
	return &out, nil
}

// GetLikeCount 点赞数量（对齐 GET /article/:id/like/count）。
func (s *ArticleService) GetLikeCount(ctx context.Context, articleID string) (*map[string]interface{}, *errcode.Err) {
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	count, err := s.repo.LikeCount(ctx, articleID, "")
	if err != nil {
		s.log.Error("统计点赞失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"count": count}
	return &out, nil
}

// LikedArticles 用户点赞的文章列表（对齐 GET /article/liked）。
func (s *ArticleService) LikedArticles(ctx context.Context, actor Actor, page, limit int) ([]ArticleView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	ids, total, err := s.repo.LikedArticleIDs(ctx, actor.UserID, page, limit)
	if err != nil {
		s.log.Error("查询点赞列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.viewsByIDs(ctx, ids, actor, total)
}

// ---------- 收藏 ----------

// Favorite 收藏文章（加入轻量收藏，对齐 POST /article/:id/favorite）。
// 与合集（Collection）互补；profile 域的「我的收藏」读取本表。
func (s *ArticleService) Favorite(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	if _, err := s.repo.FindFavorite(ctx, actor.UserID, articleID); err == nil {
		count, _ := s.CountFavoriteByArticle(ctx, articleID)
		out := map[string]interface{}{"isFavorited": true, "favoriteCount": count}
		return &out, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询收藏失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if err := s.repo.CreateFavorite(ctx, &model.ArticleFavorite{UserID: actor.UserID, ArticleID: articleID}); err != nil {
		s.log.Error("写入收藏失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if err := s.repo.IncrCounter(ctx, articleID, "favorite_count", 1); err != nil {
		s.log.Warn("收藏计数自增失败", zap.Error(err))
	}
	count, _ := s.CountFavoriteByArticle(ctx, articleID)
	out := map[string]interface{}{"isFavorited": true, "favoriteCount": count}
	return &out, nil
}

// Unfavorite 取消收藏（对齐 DELETE /article/:id/favorite）。
func (s *ArticleService) Unfavorite(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	if err := s.repo.DeleteFavorite(ctx, actor.UserID, articleID); err != nil {
		s.log.Error("取消收藏失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if err := s.repo.IncrCounter(ctx, articleID, "favorite_count", -1); err != nil {
		s.log.Warn("收藏计数自减失败", zap.Error(err))
	}
	count, _ := s.CountFavoriteByArticle(ctx, articleID)
	out := map[string]interface{}{"isFavorited": false, "favoriteCount": count}
	return &out, nil
}

// FavoriteStatus 收藏状态（对齐 GET /article/:id/favorite/status）。
func (s *ArticleService) FavoriteStatus(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	isFav := false
	if _, err := s.repo.FindFavorite(ctx, actor.UserID, articleID); err == nil {
		isFav = true
	}
	out := map[string]interface{}{"isFavorited": isFav}
	return &out, nil
}

// FavoritedArticles 收藏文章列表（对齐 GET /article/favorited/list）。
//
// targetUserID 为空时看自己；跨用户查看依赖 profile 域隐私设置，
// 本域未接入前不做隐私拦截（见遗留问题清单）。
func (s *ArticleService) FavoritedArticles(ctx context.Context, actor Actor, targetUserID string, page, limit int) ([]ArticleView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	userID := targetUserID
	if userID == "" {
		userID = actor.UserID
	}
	ids, total, err := s.repo.FavoritedArticleIDs(ctx, userID, page, limit)
	if err != nil {
		s.log.Error("查询收藏列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.viewsByIDs(ctx, ids, actor, total)
}

// ---------- 不喜欢 ----------

// Dislike 标记不喜欢（对齐 POST /article/:id/dislike）。
// 快照作者/分类/标签，供推荐流按维度过滤。
func (s *ArticleService) Dislike(ctx context.Context, articleID string, actor Actor, req ArticleDislikeRequest) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	a, e := s.getArticle(ctx, articleID)
	if e != nil {
		return nil, e
	}
	if _, err := s.repo.FindDislike(ctx, actor.UserID, articleID); err == nil {
		out := map[string]interface{}{"isDisliked": true}
		return &out, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询不喜欢失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	tagIDs, _ := s.repo.ArticleTagIDs(ctx, articleID)
	var tagText *string
	if len(tagIDs) > 0 {
		tagText = marshalJSONText(tagIDs)
	}

	d := &model.ArticleDislike{
		UserID:     actor.UserID,
		ArticleID:  articleID,
		AuthorID:   a.AuthorID,
		CategoryID: a.CategoryID,
		TagIDs:     tagText,
		Reason:     req.Reason,
	}
	if err := s.repo.CreateDislike(ctx, d); err != nil {
		s.log.Error("写入不喜欢失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"isDisliked": true}
	return &out, nil
}

// CancelDislike 取消不喜欢（对齐 DELETE /article/:id/dislike）。
func (s *ArticleService) CancelDislike(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if err := s.repo.DeleteDislike(ctx, actor.UserID, articleID); err != nil {
		s.log.Error("取消不喜欢失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"isDisliked": false}
	return &out, nil
}

// DislikeStatus 不喜欢状态（对齐 GET /article/:id/dislike/status）。
func (s *ArticleService) DislikeStatus(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	is := false
	if _, err := s.repo.FindDislike(ctx, actor.UserID, articleID); err == nil {
		is = true
	}
	out := map[string]interface{}{"isDisliked": is}
	return &out, nil
}

// DislikedArticles 不喜欢的文章列表（对齐 GET /article/disliked/list）。
func (s *ArticleService) DislikedArticles(ctx context.Context, actor Actor, page, limit int) ([]ArticleView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	ids, total, err := s.repo.DislikedArticleIDs(ctx, actor.UserID, page, limit)
	if err != nil {
		s.log.Error("查询不喜欢列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.viewsByIDs(ctx, ids, actor, total)
}

// ---------- 分享 ----------

// Share 上报分享（对齐 POST /article/:id/share），文章 shares +1。
func (s *ArticleService) Share(ctx context.Context, articleID string, actor Actor, req ArticleShareRequest) (*map[string]interface{}, *errcode.Err) {
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	channel := req.Channel
	if channel == "" {
		channel = "link"
	}
	if err := s.repo.IncrCounter(ctx, articleID, "shares", 1); err != nil {
		s.log.Error("分享计数自增失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"channel": channel, "shared": true}
	return &out, nil
}

// ---------- 精华 / 个人主页置顶 ----------

// SetFeatured 设置精华状态（对齐 PATCH /article/:id/featured）。
// 仅管理员（对齐原实现：作者/管理员判断，实际上原实现要求 featured 权限才可达此分支）。
func (s *ArticleService) SetFeatured(ctx context.Context, articleID string, actor Actor, isFeatured bool) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}
	if !actor.IsManager() {
		return nil, errcode.ErrForbidden.WithKey(i18n.ArticleFeaturedAdminOnly)
	}
	fields := map[string]interface{}{"is_featured": isFeatured}
	if isFeatured {
		fields["featured_at"] = time.Now()
	} else {
		fields["featured_at"] = nil
	}
	if err := s.repo.UpdateFields(ctx, articleID, fields); err != nil {
		s.log.Error("设置精华失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"id": articleID, "isFeatured": isFeatured}
	return &out, nil
}

// SetProfilePin 设置个人主页置顶（对齐 PATCH /article/:id/profile-pin）。
// 作者本人或管理员可操作；置顶时会顺带校验文章归该作者。
func (s *ArticleService) SetProfilePin(ctx context.Context, articleID string, actor Actor, isPinned bool) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	a, e := s.getArticle(ctx, articleID)
	if e != nil {
		return nil, e
	}
	if !s.canModify(a, actor) {
		return nil, errcode.ErrForbidden.WithKey(i18n.ArticleNoPermission)
	}
	fields := map[string]interface{}{"is_pinned_on_profile": isPinned}
	if isPinned {
		fields["pinned_at"] = time.Now()
	} else {
		fields["pinned_at"] = nil
	}
	if err := s.repo.UpdateFields(ctx, articleID, fields); err != nil {
		s.log.Error("设置主页置顶失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"id": articleID, "isPinnedOnProfile": isPinned}
	return &out, nil
}

// ---------- 浏览历史 ----------

// UpdateBrowseProgress 更新浏览进度（对齐 POST /article/:id/browse/progress）。
// 首次浏览建记录并给文章 views +1；重复浏览累加次数与时长、更新进度最大值。
func (s *ArticleService) UpdateBrowseProgress(ctx context.Context, articleID string, actor Actor, req BrowseProgressRequest) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getArticle(ctx, articleID); e != nil {
		return nil, e
	}

	progress := intVal(req.Progress)
	duration := intVal(req.Duration)

	existing, err := s.repo.FindBrowse(ctx, actor.UserID, articleID)
	switch {
	case err == nil:
		if progress > existing.Progress {
			existing.Progress = progress
		}
		existing.Duration += duration
		existing.ViewCount++
		if err := s.repo.SaveBrowse(ctx, existing); err != nil {
			s.log.Error("更新浏览记录失败", zap.Error(err))
			return nil, errcode.ErrInternal
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := s.repo.CreateBrowse(ctx, &model.BrowseHistory{
			UserID:    actor.UserID,
			ArticleID: articleID,
			ViewCount: 1,
			Progress:  progress,
			Duration:  duration,
		}); err != nil {
			s.log.Error("写入浏览记录失败", zap.Error(err))
			return nil, errcode.ErrInternal
		}
		if err := s.repo.IncrCounter(ctx, articleID, "views", 1); err != nil {
			s.log.Warn("阅读量自增失败", zap.Error(err))
		}
	default:
		s.log.Error("查询浏览记录失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	out := map[string]interface{}{"articleId": articleID, "progress": progress, "duration": duration}
	return &out, nil
}

// BrowseHistoryView 浏览历史条目视图（附带文章概要）。
type BrowseHistoryView struct {
	ArticleID string       `json:"articleId"`
	ViewCount int          `json:"viewCount"`
	Progress  int          `json:"progress"`
	Duration  int          `json:"duration"`
	Article   *ArticleView `json:"article,omitempty"`
	ViewedAt  time.Time    `json:"viewedAt"`
}

// UserBrowseHistory 浏览历史列表（对齐 GET /article/browse/history）。
func (s *ArticleService) UserBrowseHistory(ctx context.Context, actor Actor, page, limit int) ([]BrowseHistoryView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	rows, total, err := s.repo.ListBrowse(ctx, actor.UserID, page, limit)
	if err != nil {
		s.log.Error("查询浏览历史失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ArticleID)
	}
	articles := map[string]*model.Article{}
	if m, err2 := s.repo.FindByIDs(ctx, ids); err2 == nil {
		articles = m
	}
	views := s.BuildViews(ctx, articleListFromMap(articles, ids), actor, false)
	viewByID := map[string]*ArticleView{}
	for i := range views {
		viewByID[views[i].ID] = &views[i]
	}

	out := make([]BrowseHistoryView, 0, len(rows))
	for _, r := range rows {
		item := BrowseHistoryView{
			ArticleID: r.ArticleID,
			ViewCount: r.ViewCount,
			Progress:  r.Progress,
			Duration:  r.Duration,
			ViewedAt:  r.UpdatedAt,
		}
		if v, ok := viewByID[r.ArticleID]; ok {
			item.Article = v
		}
		out = append(out, item)
	}
	return out, total, nil
}

// RecentBrowsed 最近浏览的文章（对齐 GET /article/browse/recent）。
func (s *ArticleService) RecentBrowsed(ctx context.Context, actor Actor, limit int) ([]ArticleView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.repo.RecentBrowse(ctx, actor.UserID, limit)
	if err != nil {
		s.log.Error("查询最近浏览失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ArticleID)
	}
	views, _, e := s.viewsByIDs(ctx, ids, actor, int64(len(ids)))
	if e != nil {
		return nil, e
	}
	return views, nil
}

// GetBrowseStats 浏览统计（对齐 GET /article/browse/stats）。
func (s *ArticleService) GetBrowseStats(ctx context.Context, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	records, views, avgProgress, avgDuration, err := s.repo.BrowseStats(ctx, actor.UserID)
	if err != nil {
		s.log.Error("浏览统计失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{
		"totalArticles": records,
		"totalViews":    views,
		"avgProgress":   avgProgress,
		"avgDuration":   avgDuration,
	}
	return &out, nil
}

// GetBrowseHistory 单条浏览记录（对齐 GET /article/browse/:articleId）。
func (s *ArticleService) GetBrowseHistory(ctx context.Context, articleID string, actor Actor) (*BrowseHistoryView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	b, err := s.repo.FindBrowse(ctx, actor.UserID, articleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.InteractionViewNotFound)
		}
		s.log.Error("查询浏览记录失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := &BrowseHistoryView{
		ArticleID: b.ArticleID,
		ViewCount: b.ViewCount,
		Progress:  b.Progress,
		Duration:  b.Duration,
		ViewedAt:  b.UpdatedAt,
	}
	return out, nil
}

// DeleteBrowseHistory 删除单条浏览记录（对齐 DELETE /article/browse/:articleId）。
func (s *ArticleService) DeleteBrowseHistory(ctx context.Context, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if err := s.repo.DeleteBrowse(ctx, actor.UserID, articleID); err != nil {
		s.log.Error("删除浏览记录失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"deleted": true}
	return &out, nil
}

// BatchDeleteBrowseHistory 批量删除浏览记录（对齐 POST /article/browse/batch-delete）。
func (s *ArticleService) BatchDeleteBrowseHistory(ctx context.Context, actor Actor, articleIDs []string) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if err := s.repo.DeleteBrowseBatch(ctx, actor.UserID, articleIDs); err != nil {
		s.log.Error("批量删除浏览记录失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"deleted": len(articleIDs)}
	return &out, nil
}

// ClearBrowseHistory 清空浏览历史（对齐 DELETE /article/browse）。
func (s *ArticleService) ClearBrowseHistory(ctx context.Context, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if err := s.repo.ClearBrowse(ctx, actor.UserID); err != nil {
		s.log.Error("清空浏览历史失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"cleared": true}
	return &out, nil
}

// ---------- 内部工具 ----------

// viewsByIDs 按 ID 顺序组装视图（保持调用方传入的排序语义）。
func (s *ArticleService) viewsByIDs(ctx context.Context, ids []string, actor Actor, total int64) ([]ArticleView, int64, *errcode.Err) {
	if len(ids) == 0 {
		return []ArticleView{}, total, nil
	}
	m, err := s.repo.FindByIDs(ctx, ids)
	if err != nil {
		s.log.Error("批量查询文章失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	rows := articleListFromMap(m, ids)
	views := s.BuildViews(ctx, rows, actor, false)
	return views, total, nil
}

// articleListFromMap 按 ids 顺序还原文章切片。
func articleListFromMap(m map[string]*model.Article, ids []string) []model.Article {
	out := make([]model.Article, 0, len(ids))
	for _, id := range ids {
		if a, ok := m[id]; ok && a != nil {
			out = append(out, *a)
		}
	}
	return out
}

// marshalJSONText 序列化为 JSON 文本（失败返回 nil）。
func marshalJSONText(v interface{}) *string {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

// CountFavoriteByArticle 统计某文章收藏数（冗余列兜底校准用）。
func (s *ArticleService) CountFavoriteByArticle(ctx context.Context, articleID string) (int64, error) {
	var n int64
	err := s.repo.DB().WithContext(ctx).Model(&model.ArticleFavorite{}).
		Where("article_id = ?", articleID).Count(&n).Error
	return n, err
}

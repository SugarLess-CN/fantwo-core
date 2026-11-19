package service

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"go.uber.org/zap"
)

// BuildViews 把实体批量组装为视图（避免逐条查询造成的 N+1）。
//
// withContent=false 时省略 content 字段（列表场景减包体）；
// 跨域信息（作者、分类）在解析器/仓储缺失时静默降级，不影响主流程。
func (s *ArticleService) BuildViews(ctx context.Context, rows []model.Article, actor Actor, withContent bool) []ArticleView {
	out := make([]ArticleView, 0, len(rows))
	if len(rows) == 0 {
		return out
	}

	ids := make([]string, 0, len(rows))
	catIDs := make([]string, 0, len(rows))
	authorIDs := make([]string, 0, len(rows))
	seenCat := map[string]bool{}
	seenAuthor := map[string]bool{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		if r.CategoryID != nil && !seenCat[*r.CategoryID] {
			seenCat[*r.CategoryID] = true
			catIDs = append(catIDs, *r.CategoryID)
		}
		if r.AuthorID != nil && !seenAuthor[*r.AuthorID] {
			seenAuthor[*r.AuthorID] = true
			authorIDs = append(authorIDs, *r.AuthorID)
		}
	}

	// 标签：文章→标签 ID→标签实体
	tagMap := map[string][]string{}
	if m, err := s.repo.ArticleTagMap(ctx, ids); err == nil {
		tagMap = m
	} else {
		s.log.Warn("批量查询文章标签失败", zap.Error(err))
	}
	allTagIDs := make([]string, 0)
	seenTag := map[string]bool{}
	for _, list := range tagMap {
		for _, tid := range list {
			if !seenTag[tid] {
				seenTag[tid] = true
				allTagIDs = append(allTagIDs, tid)
			}
		}
	}
	tagEntity := map[string]model.Tag{}
	if len(allTagIDs) > 0 {
		if tags, err := s.tags.FindByIDs(ctx, allTagIDs); err == nil {
			for _, t := range tags {
				tagEntity[t.ID] = t
			}
		}
	}

	// 分类
	catEntity := map[string]model.Category{}
	if len(catIDs) > 0 {
		if cats, err := s.cats.FindByIDs(ctx, catIDs); err == nil {
			for _, c := range cats {
				catEntity[c.ID] = c
			}
		}
	}

	// 作者（跨域，可缺省）
	authorMap := map[string]AuthorBrief{}
	if s.authors != nil && len(authorIDs) > 0 {
		if m, err := s.authors.ResolveAuthors(ctx, authorIDs); err == nil {
			authorMap = m
		} else {
			s.log.Warn("批量解析作者信息失败", zap.Error(err))
		}
	}

	// 互动状态
	liked, _ := s.repo.LikedSet(ctx, actor.UserID, ids)
	favorited, _ := s.repo.FavoritedSet(ctx, actor.UserID, ids)
	disliked, _ := s.repo.DislikedSet(ctx, actor.UserID, ids)

	for _, r := range rows {
		v := ArticleView{
			ID:                r.ID,
			Title:             r.Title,
			RequireLogin:      r.RequireLogin,
			RequireFollow:     r.RequireFollow,
			RequirePayment:    r.RequirePayment,
			RequireMembership: r.RequireMembership,
			ListRequireLogin:  r.ListRequireLogin,
			ViewPrice:         r.ViewPrice,
			Type:              r.Type,
			Images:            ParseImages(r.Images),
			VideoURL:          r.VideoURL,
			Cover:             r.Cover,
			Summary:           r.Summary,
			Sort:              r.Sort,
			Views:             r.Views,
			Likes:             r.Likes,
			FavoriteCount:     r.FavoriteCount,
			CommentCount:      r.CommentCount,
			Shares:            r.Shares,
			IsFeatured:        r.IsFeatured,
			FeaturedAt:        r.FeaturedAt,
			IsPinnedOnProfile: r.IsPinnedOnProfile,
			PinnedAt:          r.PinnedAt,
			Status:            r.Status,
			AuthorID:          r.AuthorID,
			CategoryID:        r.CategoryID,
			Tags:              []TagBrief{},
			ActivityID:        r.ActivityID,
			AllowReprint:      r.AllowReprint,
			IsLiked:           liked[r.ID],
			IsFavorited:       favorited[r.ID],
			IsDisliked:        disliked[r.ID],
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
		}
		if withContent {
			v.Content = r.Content
		}

		// 标签
		for _, tid := range tagMap[r.ID] {
			if t, ok := tagEntity[tid]; ok {
				v.Tags = append(v.Tags, TagBrief{ID: t.ID, Name: t.Name, Cover: t.Cover})
			}
		}
		// 分类
		if r.CategoryID != nil {
			if c, ok := catEntity[*r.CategoryID]; ok {
				v.Category = &CategoryBrief{ID: c.ID, Name: c.Name, Link: c.Link, Avatar: c.Avatar, Status: c.Status}
			}
		}
		// 作者
		if r.AuthorID != nil {
			if a, ok := authorMap[*r.AuthorID]; ok {
				brief := a
				v.Author = &brief
			}
		}

		out = append(out, v)
	}
	return out
}

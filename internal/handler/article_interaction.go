package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// ---------- 点赞 / 表情回复 ----------

// Like 点赞/表情回复 POST /article/:id/like。
func (h *ArticleHandler) Like(c *gin.Context) {
	var req service.ArticleReactionRequest
	// body 可空：未传时按 like 处理
	_ = c.ShouldBindJSON(&req)
	out, e := h.svc.Like(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// LikeStatus 点赞状态 GET /article/:id/like/status。
func (h *ArticleHandler) LikeStatus(c *gin.Context) {
	out, e := h.svc.GetLikeStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// LikeCount 点赞数量 GET /article/:id/like/count（免登录）。
func (h *ArticleHandler) LikeCount(c *gin.Context) {
	out, e := h.svc.GetLikeCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Liked 用户点赞文章列表 GET /article/liked。
func (h *ArticleHandler) Liked(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.LikedArticles(c.Request.Context(), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// ---------- 收藏 ----------

// Favorite 收藏文章 POST /article/:id/favorite。
func (h *ArticleHandler) Favorite(c *gin.Context) {
	out, e := h.svc.Favorite(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Unfavorite 取消收藏 DELETE /article/:id/favorite。
func (h *ArticleHandler) Unfavorite(c *gin.Context) {
	out, e := h.svc.Unfavorite(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// FavoriteStatus 收藏状态 GET /article/:id/favorite/status。
func (h *ArticleHandler) FavoriteStatus(c *gin.Context) {
	out, e := h.svc.FavoriteStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// FavoritedList 收藏文章列表 GET /article/favorited/list。
func (h *ArticleHandler) FavoritedList(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.FavoritedArticles(c.Request.Context(), actorOf(c), c.Query("userId"), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// ---------- 不喜欢 ----------

// Dislike 标记不喜欢 POST /article/:id/dislike。
func (h *ArticleHandler) Dislike(c *gin.Context) {
	var req service.ArticleDislikeRequest
	_ = c.ShouldBindJSON(&req)
	out, e := h.svc.Dislike(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// CancelDislike 取消不喜欢 DELETE /article/:id/dislike。
func (h *ArticleHandler) CancelDislike(c *gin.Context) {
	out, e := h.svc.CancelDislike(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// DislikeStatus 不喜欢状态 GET /article/:id/dislike/status。
func (h *ArticleHandler) DislikeStatus(c *gin.Context) {
	out, e := h.svc.DislikeStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// DislikedList 不喜欢的文章列表 GET /article/disliked/list。
func (h *ArticleHandler) DislikedList(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.DislikedArticles(c.Request.Context(), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// ---------- 分享 ----------

// Share 上报分享 POST /article/:id/share。
func (h *ArticleHandler) Share(c *gin.Context) {
	var req service.ArticleShareRequest
	_ = c.ShouldBindJSON(&req)
	out, e := h.svc.Share(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// ---------- 浏览历史 ----------

// BrowseProgress 更新浏览进度 POST /article/:id/browse/progress。
func (h *ArticleHandler) BrowseProgress(c *gin.Context) {
	var req service.BrowseProgressRequest
	_ = c.ShouldBindJSON(&req)
	out, e := h.svc.UpdateBrowseProgress(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseHistory 浏览历史列表 GET /article/browse/history。
func (h *ArticleHandler) BrowseHistory(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.UserBrowseHistory(c.Request.Context(), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// BrowseStats 浏览统计 GET /article/browse/stats。
func (h *ArticleHandler) BrowseStats(c *gin.Context) {
	out, e := h.svc.GetBrowseStats(c.Request.Context(), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseRecent 最近浏览 GET /article/browse/recent。
func (h *ArticleHandler) BrowseRecent(c *gin.Context) {
	list, e := h.svc.RecentBrowsed(c.Request.Context(), actorOf(c), intQuery(c, "limit", 10))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// BrowseBatchDelete 批量删除浏览记录 POST /article/browse/batch-delete。
func (h *ArticleHandler) BrowseBatchDelete(c *gin.Context) {
	var req service.BatchDeleteBrowseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
		return
	}
	out, e := h.svc.BatchDeleteBrowseHistory(c.Request.Context(), actorOf(c), req.ArticleIDs)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseOne 单条浏览记录 GET /article/browse/:articleId。
func (h *ArticleHandler) BrowseOne(c *gin.Context) {
	out, e := h.svc.GetBrowseHistory(c.Request.Context(), c.Param("articleId"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseDelete 删除单条浏览记录 DELETE /article/browse/:articleId。
func (h *ArticleHandler) BrowseDelete(c *gin.Context) {
	out, e := h.svc.DeleteBrowseHistory(c.Request.Context(), c.Param("articleId"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseClear 清空浏览历史 DELETE /article/browse。
func (h *ArticleHandler) BrowseClear(c *gin.Context) {
	out, e := h.svc.ClearBrowseHistory(c.Request.Context(), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

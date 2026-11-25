package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// ---------- 点赞 / 表情回复 ----------

// Like 点赞/表情回复 POST /article/:id/like。
//
//	@Summary	点赞文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_like
//	@Router		/article/{id}/like [post]
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
//
//	@Summary	点赞状态
//	@Tags		文章
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getLikeStatus
//	@Router		/article/{id}/like/status [get]
func (h *ArticleHandler) LikeStatus(c *gin.Context) {
	out, e := h.svc.GetLikeStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// LikeCount 点赞数量 GET /article/:id/like/count（免登录）。
//
//	@Summary	点赞数
//	@Tags		文章
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getLikeCount
//	@Router		/article/{id}/like/count [get]
func (h *ArticleHandler) LikeCount(c *gin.Context) {
	out, e := h.svc.GetLikeCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Liked 用户点赞文章列表 GET /article/liked。
//
//	@Summary	我赞过的文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				ArticleController_getLikedArticles
//	@Router		/article/liked [get]
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
//
//	@Summary	收藏文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_favoriteArticle
//	@Router		/article/{id}/favorite [post]
func (h *ArticleHandler) Favorite(c *gin.Context) {
	out, e := h.svc.Favorite(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Unfavorite 取消收藏 DELETE /article/:id/favorite。
//
//	@Summary	取消收藏
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_unfavoriteArticle
//	@Router		/article/{id}/favorite [delete]
func (h *ArticleHandler) Unfavorite(c *gin.Context) {
	out, e := h.svc.Unfavorite(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// FavoriteStatus 收藏状态 GET /article/:id/favorite/status。
//
//	@Summary	收藏状态
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_checkFavoriteStatus
//	@Router		/article/{id}/favorite/status [get]
func (h *ArticleHandler) FavoriteStatus(c *gin.Context) {
	out, e := h.svc.FavoriteStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// FavoritedList 收藏文章列表 GET /article/favorited/list。
//
//	@Summary	我收藏的文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				ArticleController_getFavoritedArticles
//	@Router		/article/favorited/list [get]
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
//
//	@Summary	点踩文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_dislikeArticle
//	@Router		/article/{id}/dislike [post]
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
//
//	@Summary	取消点踩
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_cancelDislikeArticle
//	@Router		/article/{id}/dislike [delete]
func (h *ArticleHandler) CancelDislike(c *gin.Context) {
	out, e := h.svc.CancelDislike(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// DislikeStatus 不喜欢状态 GET /article/:id/dislike/status。
//
//	@Summary	点踩状态
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getDislikeStatus
//	@Router		/article/{id}/dislike/status [get]
func (h *ArticleHandler) DislikeStatus(c *gin.Context) {
	out, e := h.svc.DislikeStatus(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// DislikedList 不喜欢的文章列表 GET /article/disliked/list。
//
//	@Summary	我点踩的文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				ArticleController_getDislikedArticles
//	@Router		/article/disliked/list [get]
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
//
//	@Summary	分享文章
//	@Tags		文章
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleHandler_Share
//	@Router		/article/{id}/share [post]
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
//
//	@Summary	上报阅读进度
//	@Tags		文章
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_updateBrowseProgress
//	@Router		/article/{id}/browse/progress [post]
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
//
//	@Summary	浏览历史
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				ArticleController_getUserBrowseHistory
//	@Router		/article/browse/history [get]
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
//
//	@Summary	浏览统计
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getBrowseStats
//	@Router		/article/browse/stats [get]
func (h *ArticleHandler) BrowseStats(c *gin.Context) {
	out, e := h.svc.GetBrowseStats(c.Request.Context(), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseRecent 最近浏览 GET /article/browse/recent。
//
//	@Summary	最近浏览
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getRecentBrowsedArticles
//	@Router		/article/browse/recent [get]
func (h *ArticleHandler) BrowseRecent(c *gin.Context) {
	list, e := h.svc.RecentBrowsed(c.Request.Context(), actorOf(c), intQuery(c, "limit", 10))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// BrowseBatchDelete 批量删除浏览记录 POST /article/browse/batch-delete。
//
//	@Summary	批量删除浏览记录
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_batchDeleteBrowseHistory
//	@Router		/article/browse/batch-delete [post]
func (h *ArticleHandler) BrowseBatchDelete(c *gin.Context) {
	var req service.BatchDeleteBrowseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
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
//
//	@Summary	单条浏览记录
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		articleId path string true "articleId"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_getBrowseHistory
//	@Router		/article/browse/{articleId} [get]
func (h *ArticleHandler) BrowseOne(c *gin.Context) {
	out, e := h.svc.GetBrowseHistory(c.Request.Context(), c.Param("articleId"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseDelete 删除单条浏览记录 DELETE /article/browse/:articleId。
//
//	@Summary	删除浏览记录
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		articleId path string true "articleId"
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_deleteBrowseHistory
//	@Router		/article/browse/{articleId} [delete]
func (h *ArticleHandler) BrowseDelete(c *gin.Context) {
	out, e := h.svc.DeleteBrowseHistory(c.Request.Context(), c.Param("articleId"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// BrowseClear 清空浏览历史 DELETE /article/browse。
//
//	@Summary	清空浏览历史
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				ArticleController_clearBrowseHistory
//	@Router		/article/browse [delete]
func (h *ArticleHandler) BrowseClear(c *gin.Context) {
	out, e := h.svc.ClearBrowseHistory(c.Request.Context(), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

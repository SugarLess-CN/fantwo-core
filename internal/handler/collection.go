package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CollectionHandler 合集管理 HTTP 处理（对齐原 CollectionController）。
type CollectionHandler struct {
	svc *service.CollectionService
	log *zap.Logger
}

// Create 创建合集 POST /collection。
//
//	@Summary	创建合集
//	@Tags		合集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "collectionRequest{name,description,isPublic}"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection [post]
func (h *CollectionHandler) Create(c *gin.Context) {
	var req service.CreateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
		return
	}
	view, e := h.svc.Create(c.Request.Context(), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// List 合集列表 GET /collection。
//
//	@Summary	合集列表
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponsePageBody
//	@Router		/collection [get]
func (h *CollectionHandler) List(c *gin.Context) {
	page, limit := pageOf(c)
	q := repository.CollectionListQuery{
		UserID:  c.Query("userId"),
		Keyword: c.Query("keyword"),
		SortBy:  c.Query("sortBy"),
		SortOrd: c.DefaultQuery("sortOrder", "DESC"),
		Page:    page,
		Limit:   limit,
	}
	list, total, e := h.svc.List(c.Request.Context(), actorOf(c), q)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// FindOne 合集详情 GET /collection/:id。
//
//	@Summary	合集详情
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection/{id} [get]
func (h *CollectionHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindOne(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新合集 PATCH /collection/:id。
//
//	@Summary	更新合集
//	@Tags		合集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Param		body body object true "collectionRequest"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection/{id} [patch]
func (h *CollectionHandler) Update(c *gin.Context) {
	var req service.UpdateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
		return
	}
	view, e := h.svc.Update(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Remove 删除合集 DELETE /collection/:id。
//
//	@Summary	删除合集
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection/{id} [delete]
func (h *CollectionHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// AddArticle 添加文章到合集 POST /collection/:id/article/:articleId。
//
//	@Summary	添加文章到合集
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Param		articleId path string true "文章 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection/{id}/article/{articleId} [post]
func (h *CollectionHandler) AddArticle(c *gin.Context) {
	out, e := h.svc.AddArticle(c.Request.Context(), c.Param("id"), c.Param("articleId"), actorOf(c), nil)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// RemoveArticle 从合集移除文章 DELETE /collection/:id/article/:articleId。
//
//	@Summary	从合集移除文章
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Param		articleId path string true "文章 ID"
//	@Success	200	{object}	ResponseBody
//	@Router		/collection/{id}/article/{articleId} [delete]
func (h *CollectionHandler) RemoveArticle(c *gin.Context) {
	out, e := h.svc.RemoveArticle(c.Request.Context(), c.Param("id"), c.Param("articleId"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// ListItems 合集内文章列表 GET /collection/:id/items。
//
//	@Summary	合集内文章列表
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@Router		/collection/{id}/items [get]
func (h *CollectionHandler) ListItems(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.ListItems(c.Request.Context(), c.Param("id"), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// ReorderItems 合集内文章重排 PUT /collection/:id/items/order。
func (h *CollectionHandler) ReorderItems(c *gin.Context) {
	var req service.ReorderCollectionItemsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
		return
	}
	out, e := h.svc.ReorderItems(c.Request.Context(), c.Param("id"), actorOf(c), req.ArticleIDs)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// CheckArticle 检查文章是否在合集中 GET /collection/check/:articleId。
func (h *CollectionHandler) CheckArticle(c *gin.Context) {
	out, e := h.svc.CheckArticle(c.Request.Context(), actorOf(c), c.Param("articleId"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// ArticleCollectionInfo 文章所在合集信息 GET /collection/article/:articleId/info。
func (h *CollectionHandler) ArticleCollectionInfo(c *gin.Context) {
	out, e := h.svc.ArticleCollectionInfo(c.Request.Context(), actorOf(c), c.Param("articleId"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

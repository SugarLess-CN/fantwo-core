package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
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
//	@Param		body body dto.CreateCollectionRequest true "合集信息"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionDto}
//	@Param		body body dto.CreateCollectionRequest true "请求体"
//	@Router		/collection [post]
//	@ID			CollectionController_create
func (h *CollectionHandler) Create(c *gin.Context) {
	var req service.CreateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
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
//	@Param		user_id		query	string	false	"按作者过滤"
//	@Param		keyword		query	string	false	"按名称搜索"
//	@Param		sort_by		query	string	false	"排序字段"
//	@Param		sort_order	query	string	false	"排序方向 ASC / DESC（默认 DESC）"
//	@Param		page		query	int		false	"页码"
//	@Param		limit		query	int		false	"每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.CollectionDto}}
//	@Router		/collection [get]
//	@ID			CollectionController_findAll
func (h *CollectionHandler) List(c *gin.Context) {
	page, limit := pageOf(c)
	q := repository.CollectionListQuery{
		UserID:  c.Query("user_id"),
		Keyword: c.Query("keyword"),
		SortBy:  c.Query("sort_by"),
		SortOrd: c.DefaultQuery("sort_order", "DESC"),
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionDetailDto}
//	@Router		/collection/{id} [get]
//	@ID			CollectionController_findOne
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
//	@Param		body body dto.UpdateCollectionRequest true "要改的字段（不传 = 不改）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionDto}
//	@Router		/collection/{id} [patch]
//	@ID			CollectionController_update
func (h *CollectionHandler) Update(c *gin.Context) {
	var req service.UpdateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@Router		/collection/{id} [delete]
//	@ID			CollectionController_remove
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionArticleAddResultDto}
//	@Router		/collection/{id}/article/{articleId} [post]
//	@ID			CollectionController_addArticle
func (h *CollectionHandler) AddArticle(c *gin.Context) {
	out, e := h.svc.AddArticle(c.Request.Context(), c.Param("id"), c.Param("articleId"), actorOf(c), nil)
	if e != nil {
		ctFail(c, e)
		return
	}
	// message 提到顶层（原实现在 data 里带 message）；data 只留业务字段
	msg := i18n.CollectionArticleAdded
	if out != nil && !out.Added {
		msg = i18n.CollectionArticleExists
	}
	response.SuccessMsg(c, msg, out)
}

// RemoveArticle 从合集移除文章 DELETE /collection/:id/article/:articleId。
//
//	@Summary	从合集移除文章
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "合集 ID"
//	@Param		articleId path string true "文章 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionArticleRemoveResultDto}
//	@Router		/collection/{id}/article/{articleId} [delete]
//	@ID			CollectionController_removeArticle
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
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.CollectionItemDto}}
//	@Router		/collection/{id}/items [get]
//	@ID			CollectionController_findItems
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
//
//	@Summary	调整合集内文章顺序
//	@Tags		合集
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body dto.ReorderCollectionItemsRequest true "articleIds 的顺序即展示顺序"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionReorderResultDto}
//	@Router		/collection/{id}/items/order [put]
//	@ID			CollectionController_reorderItems
func (h *CollectionHandler) ReorderItems(c *gin.Context) {
	var req service.ReorderCollectionItemsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
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
//
//	@Summary	文章所在合集检查
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		articleId path string true "articleId"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CollectionCheckDto}
//	@Router		/collection/check/{articleId} [get]
//	@ID			CollectionController_checkArticle
func (h *CollectionHandler) CheckArticle(c *gin.Context) {
	out, e := h.svc.CheckArticle(c.Request.Context(), actorOf(c), c.Param("articleId"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// ArticleCollectionInfo 文章所在合集信息 GET /collection/article/:articleId/info。
//
//	@Summary	文章合集信息
//	@Tags		合集
//	@Produce	json
//	@Security	BearerAuth
//	@Param		articleId path string true "articleId"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ArticleCollectionInfoDto}
//	@Router		/collection/article/{articleId}/info [get]
//	@ID			CollectionController_articleCollectionInfo
func (h *CollectionHandler) ArticleCollectionInfo(c *gin.Context) {
	out, e := h.svc.ArticleCollectionInfo(c.Request.Context(), actorOf(c), c.Param("articleId"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// 让 dto 包成为本文件的真实 import：swag 按「注解所在文件的 import」解析跨包类型
// （docs/18 §三之一 约束 B），本文件的注解写了 dto.Xxx，这里必须引用一次。
// 不参与运行逻辑。
var _ = dto.ResponseBody{}

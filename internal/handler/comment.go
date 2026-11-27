package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CommentHandler 评论管理 HTTP 处理（对齐原 CommentController）。
type CommentHandler struct {
	svc *service.CommentService
	log *zap.Logger
}

// commentListQuery 解析评论列表 query。
func commentListQuery(c *gin.Context) service.CommentListQuery {
	page, limit := pageOf(c)
	return service.CommentListQuery{
		Page:       page,
		Limit:      limit,
		SortBy:     c.DefaultQuery("sort_by", "latest"),
		OnlyAuthor: boolQuery(c, "only_author"), // 旧名 onlyAuthor 已废弃（snake_case 规范）
		Keyword:    c.Query("keyword"),
		UserID:     c.Query("user_id"),
	}
}

// Create 创建评论 POST /comment（权限 comment:create）。
//
//	@Summary		创建评论
//	@Description	对文章发表评论，入 text-audit 队列异步审核
//	@Tags			评论
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param		body body dto.CreateCommentRequest true "请求体"
//	@Success		200		{object}	dto.ResponseBody{data=dto.CommentDto}
//	@Failure		400		{object}	ResponseBody
//	@ID			CommentController_create
//	@Router			/comment [post]
func (h *CommentHandler) Create(c *gin.Context) {
	var req service.CreateCommentRequest
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

// FindAll 全部评论 GET /comment（权限 comment:manage，管理端）。
//
//	@Summary	全部评论（管理端）
//	@Tags		评论
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Param		only_author query bool false "只看帖主（文章作者）的评论" example(false)
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.CommentDto}}
//	@ID			CommentController_findAllComments
//	@Router		/comment [get]
func (h *CommentHandler) FindAll(c *gin.Context) {
	q := commentListQuery(c)
	list, total, e := h.svc.FindAllComments(c.Request.Context(), actorOf(c), q)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, q.Page, q.Limit)
}

// FindByArticle 文章评论列表 GET /comment/article/:id（可选登录）。
//
//	@Summary		文章评论列表
//	@Tags			评论
//	@Produce		json
//	@Param		id path string true "文章 ID" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Param		only_author query bool false "只看帖主（文章作者）的评论" example(false)
//	@Success		200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.CommentDto}}
//	@ID			CommentController_findAll
//	@Router			/comment/article/{id} [get]
func (h *CommentHandler) FindByArticle(c *gin.Context) {
	q := commentListQuery(c)
	list, total, e := h.svc.FindByArticle(c.Request.Context(), c.Param("id"), q, actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, q.Page, q.Limit)
}

// FindOne 评论详情 GET /comment/:id（可选登录）。
//
//	@Summary	评论详情
//	@Tags		评论
//	@Produce	json
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Param		page query int false "回复页码" example(1)
//	@Param		limit query int false "回复每页条数" example(10)
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommentDto}
//	@ID			CommentController_findOne
//	@Router		/comment/{id} [get]
func (h *CommentHandler) FindOne(c *gin.Context) {
	page, limit := pageOf(c)
	view, e := h.svc.FindCommentDetail(c.Request.Context(), c.Param("id"), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新评论 PATCH /comment/:id（权限 comment:update）。
//
//	@Summary	更新评论
//	@Tags		评论
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Param		body body dto.UpdateCommentRequest true "待更新字段（部分更新）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommentDto}
//	@ID			CommentController_update
//	@Router		/comment/{id} [patch]
func (h *CommentHandler) Update(c *gin.Context) {
	var req service.UpdateCommentRequest
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

// SetPin 设置评论置顶 PATCH /comment/:id/pin。
//
//	@Summary	设置/取消评论置顶
//	@Tags		评论
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Param		body body dto.SetCommentPinRequest true "是否置顶"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommentPinResultDto}
//	@ID			CommentController_setPin
//	@Router		/comment/{id}/pin [patch]
func (h *CommentHandler) SetPin(c *gin.Context) {
	var req service.SetCommentPinRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsPinned == nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, "isPinned must be a boolean"))
		return
	}
	out, e := h.svc.SetPin(c.Request.Context(), c.Param("id"), actorOf(c), *req.IsPinned)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Remove 删除评论 DELETE /comment/:id（权限 comment:delete）。
//
//	@Summary	删除评论
//	@Tags		评论
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "评论 ID" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@Failure	403	{object}	ResponseBody
//	@ID			CommentController_remove
//	@Router		/comment/{id} [delete]
func (h *CommentHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Like 点赞评论 POST /comment/:id/like。
//
//	@Summary	点赞评论
//	@Tags		评论
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommentLikeResultDto}
//	@ID			CommentController_like
//	@Router		/comment/{id}/like [post]
func (h *CommentHandler) Like(c *gin.Context) {
	out, e := h.svc.Like(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// UserComments 用户评论列表 GET /comment/user/:userId。
//
//	@Summary	用户评论列表
//	@Tags		评论
//	@Produce	json
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.CommentDto}}
//	@ID			CommentController_getUserComments
//	@Router		/comment/user/{user_id} [get]
func (h *CommentHandler) UserComments(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.UserComments(c.Request.Context(), c.Param("user_id"), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// Count 文章评论数 GET /comment/article/:id/count。
//
//	@Summary	文章评论数
//	@Tags		评论
//	@Produce	json
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.CountDto}
//	@ID			CommentController_getCommentCount
//	@Router		/comment/article/{id}/count [get]
func (h *CommentHandler) Count(c *gin.Context) {
	out, e := h.svc.CommentCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// var _ 保证 dto 的 import 不被判为未使用（swag 只看注解所在文件的 import，docs/18 §三之一 B）。
var _ = dto.ResponseBody{}

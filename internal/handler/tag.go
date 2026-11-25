package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TagHandler 标签管理 HTTP 处理（对齐原 TagController）。
type TagHandler struct {
	svc *service.TagService
	log *zap.Logger
}

// Create 创建标签 POST /tag（权限 tag:create）。
//
//	@Summary	创建标签
//	@Tags		标签
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body service.CreateTagRequest true "tagRequest{name,description}"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_create
//	@Router		/tag [post]
func (h *TagHandler) Create(c *gin.Context) {
	var req service.CreateTagRequest
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

// List 标签列表 GET /tag（可选登录）。
//
//	@Summary	标签列表
//	@Tags		标签
//	@Produce	json
//	@Success	200	{object}	ResponsePageBody
//	@ID				TagController_findAll
//	@Router		/tag [get]
func (h *TagHandler) List(c *gin.Context) {
	q := parseTagQuery(c)
	list, total, e := h.svc.List(c.Request.Context(), actorOf(c), q)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, q.Page, q.Limit)
}

// Recommended 推荐标签 GET /tag/recommended（可选登录）。
//
//	@Summary	推荐标签
//	@Tags		标签
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_getRecommendedTags
//	@Router		/tag/recommended [get]
func (h *TagHandler) Recommended(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.Recommended(c.Request.Context(), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// Hot 热门标签统计 GET /tag/hot（可选登录）。
//
//	@Summary	热门标签
//	@Tags		标签
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				TagHandler_Hot
//	@Router		/tag/hot [get]
func (h *TagHandler) Hot(c *gin.Context) {
	list, e := h.svc.Hot(c.Request.Context(), actorOf(c), intQuery(c, "limit", 10))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// FollowedList 用户关注标签 GET /tag/followed/list（可选登录）。
//
//	@Summary	我关注的标签
//	@Tags		标签
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponsePageBody
//	@ID				tagController_followedList
//	@Router		/tag/followed/list [get]
func (h *TagHandler) FollowedList(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.FollowedList(c.Request.Context(), actorOf(c), c.Query("userId"), c.Query("name"), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// FindOne 标签详情 GET /tag/:id（可选登录）。
//
//	@Summary	标签详情
//	@Tags		标签
//	@Produce	json
//	@Param		id path string true "标签 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_findOne
//	@Router		/tag/{id} [get]
func (h *TagHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindOne(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新标签 PATCH /tag/:id（权限 tag:update）。
//
//	@Summary	更新标签
//	@Tags		标签
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "标签 ID"
//	@Param		body body service.UpdateTagRequest true "tagRequest"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_update
//	@Router		/tag/{id} [patch]
func (h *TagHandler) Update(c *gin.Context) {
	var req service.UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
		return
	}
	view, e := h.svc.Update(c.Request.Context(), c.Param("id"), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Remove 删除标签 DELETE /tag/:id（权限 tag:delete）。
//
//	@Summary	删除标签
//	@Tags		标签
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "标签 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_remove
//	@Router		/tag/{id} [delete]
func (h *TagHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Follow 关注标签 POST /tag/:id/follow。
//
//	@Summary	关注标签
//	@Tags		标签
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "标签 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_follow
//	@Router		/tag/{id}/follow [post]
func (h *TagHandler) Follow(c *gin.Context) {
	out, e := h.svc.Follow(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// Unfollow 取消关注标签 DELETE /tag/:id/follow。
//
//	@Summary	取消关注标签
//	@Tags		标签
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				TagController_unfollow
//	@Router		/tag/{id}/follow [delete]
func (h *TagHandler) Unfollow(c *gin.Context) {
	out, e := h.svc.Unfollow(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CategoryHandler 分类管理 HTTP 处理（对齐原 CategoryController）。
type CategoryHandler struct {
	svc *service.CategoryService
	log *zap.Logger
}

// parseCategoryQuery 解析分类列表 query。
func parseCategoryQuery(c *gin.Context) repository.CategoryListQuery {
	page, limit := pageOf(c)
	q := repository.CategoryListQuery{
		Name:     c.Query("name"),
		Status:   c.Query("status"),
		ParentID: c.Query("parentId"),
		SortBy:   c.Query("sortBy"),
		SortOrd:  c.DefaultQuery("sortOrder", "DESC"),
		Page:     page,
		Limit:    limit,
	}
	// parentId=0 视为「只取根分类」（对齐原 DTO 把 0 转 null 的语义）
	if c.Query("parentId") == "0" {
		q.ParentID = ""
		q.OnlyRoot = true
	}
	return q
}

// Create 创建分类 POST /category（权限 category:create）。
//
//	@Summary	创建分类
//	@Tags		分类
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body service.CreateCategoryRequest true "categoryRequest{name,parentId}"
//	@Success	200	{object}	ResponseBody
//	@ID				CategoryController_create
//	@Router		/category [post]
func (h *CategoryHandler) Create(c *gin.Context) {
	var req service.CreateCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
		return
	}
	view, e := h.svc.Create(c.Request.Context(), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// List 分类列表 GET /category（可选登录；tree=true 返回两级树）。
//
//	@Summary	分类列表
//	@Tags		分类
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				CategoryController_findAll
//	@Router		/category [get]
func (h *CategoryHandler) List(c *gin.Context) {
	q := parseCategoryQuery(c)
	list, total, e := h.svc.List(c.Request.Context(), q, boolQuery(c, "tree"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, q.Page, q.Limit)
}

// FindOne 分类详情 GET /category/:id。
//
//	@Summary	分类详情
//	@Tags		分类
//	@Produce	json
//	@Param		id path string true "分类 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				CategoryController_findOne
//	@Router		/category/{id} [get]
func (h *CategoryHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindOne(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新分类 PATCH /category/:id（权限 category:update）。
//
//	@Summary	更新分类
//	@Tags		分类
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "分类 ID"
//	@Param		body body service.UpdateCategoryRequest true "categoryRequest"
//	@Success	200	{object}	ResponseBody
//	@ID				CategoryController_update
//	@Router		/category/{id} [patch]
func (h *CategoryHandler) Update(c *gin.Context) {
	var req service.UpdateCategoryRequest
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

// Remove 删除分类 DELETE /category/:id（权限 category:delete）。
//
//	@Summary	删除分类
//	@Tags		分类
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "分类 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				CategoryController_remove
//	@Router		/category/{id} [delete]
func (h *CategoryHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

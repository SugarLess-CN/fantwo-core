// Package handler RBAC 端点：角色管理 10 操作 + 权限管理 5 操作。
//
// 路径对齐 docs/02：/role**、/permission**；
// 任务书要求的 /admin/roles/** 以别名形式一并挂载（同一 handler）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// RoleHandler 角色端点。
type RoleHandler struct {
	svc *service.RoleService
}

// NewRoleHandler 构造。
func NewRoleHandler(svc *service.RoleService) *RoleHandler { return &RoleHandler{svc: svc} }

// List GET /role：全部角色。
//
//	@Summary	角色列表（全量）
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.RoleDto}
//	@ID			RoleController_findAll
//	@Router		/role [get]
func (h *RoleHandler) List(c *gin.Context) {
	roles, e := h.svc.ListRoles(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, roles)
}

// ListPaged GET /role/list：分页角色。
//
//	@Summary	角色分页列表
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.RoleDto}}
//	@ID			RoleController_findWithPagination
//	@Router		/role/list [get]
func (h *RoleHandler) ListPaged(c *gin.Context) {
	page, limit := queryPage(c)
	roles, total, e := h.svc.ListRolesPaged(c.Request.Context(), page, limit,
		c.Query("name"), queryBool(c, "isActive"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, roles, total, page, limit)
}

// Active GET /role/active：活跃角色。
//
//	@Summary	启用中的角色
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.RoleDto}
//	@ID			RoleController_getActiveRoles
//	@Router		/role/active [get]
func (h *RoleHandler) Active(c *gin.Context) {
	roles, e := h.svc.ActiveRoles(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, roles)
}

// FindOne GET /role/{id}。
//
//	@Summary	角色详情
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleDetailDto}
//	@ID			RoleController_findOne
//	@Router		/role/{id} [get]
func (h *RoleHandler) FindOne(c *gin.Context) {
	out, e := h.svc.FindRole(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Create POST /role。
//
//	@Summary	创建角色
//	@Tags		角色
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.CreateRoleRequest true "创建角色入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleDto}
//	@ID			RoleController_create
//	@Router		/role [post]
func (h *RoleHandler) Create(c *gin.Context) {
	var in service.CreateRoleInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.CreateRole(c.Request.Context(), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Update PATCH /role/{id}。
//
//	@Summary	更新角色
//	@Tags		角色
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Param		body body dto.UpdateRoleRequest true "更新角色入参（全部指针语义，nil=不改）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleDto}
//	@ID			RoleController_update
//	@Router		/role/{id} [patch]
func (h *RoleHandler) Update(c *gin.Context) {
	var in service.UpdateRoleInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.UpdateRole(c.Request.Context(), c.Param("id"), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Remove DELETE /role/{id}。
//
//	@Summary	删除角色
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"（系统角色不可删）
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			RoleController_remove
//	@Router		/role/{id} [delete]
func (h *RoleHandler) Remove(c *gin.Context) {
	if e := h.svc.DeleteRole(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// Copy POST /role/{id}/copy。
//
//	@Summary	复制角色
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "源角色 ID"
//	@Param		body body dto.CopyRoleRequest true "复制角色入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleDto}
//	@ID			RoleController_copyRole
//	@Router		/role/{id}/copy [post]
func (h *RoleHandler) Copy(c *gin.Context) {
	var in dto.CopyRoleRequest
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.CopyRole(c.Request.Context(), c.Param("id"), in.Name, in.DisplayName)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// AssignPermissions POST /role/{id}/permissions（任务书别名 PUT /admin/roles/{id}/permissions）。
//
//	@Summary	绑定角色权限
//	@Tags		角色
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Param		body body dto.AssignPermissionsRequest true "权限 ID 全量重写"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RolePermissionsAssignedDto}
//	@ID			RoleController_assignPermissions
//	@Router		/role/{id}/permissions [post]
func (h *RoleHandler) AssignPermissions(c *gin.Context) {
	var in struct {
		PermissionIDs []string `json:"permissionIds"`
	}
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.AssignPermissions(c.Request.Context(), c.Param("id"), in.PermissionIDs)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// ToggleStatus PATCH /role/{id}/status。
//
//	@Summary	启用/禁用角色
//	@Tags		角色
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Param		body body dto.ToggleRoleStatusRequest true "启用/禁用（nil 按 true）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleStatusDto}
//	@ID			RoleController_toggleStatus
//	@Router		/role/{id}/status [patch]
func (h *RoleHandler) ToggleStatus(c *gin.Context) {
	var in struct {
		IsActive *bool `json:"isActive"`
	}
	if !bindJSON(c, &in) {
		return
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	out, e := h.svc.ToggleStatus(c.Request.Context(), c.Param("id"), active)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// AssignMenus PUT /admin/roles/{id}/menus（另挂 PUT /role/{id}/menus）。
//
//	@Summary	绑定角色菜单
//	@Tags		角色
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Param		body body dto.AssignMenusRequest true "菜单键全量重写"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleMenusAssignedDto}
//	@ID			RoleController_bindMenus
//	@Router		/role/{id}/menus [put]
func (h *RoleHandler) AssignMenus(c *gin.Context) {
	var in struct {
		Menus []string `json:"menus"`
	}
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.AssignMenus(c.Request.Context(), c.Param("id"), in.Menus)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Menus GET /role/{id}/menus（读取角色菜单 + 全量菜单清单）。
//
//	@Summary	角色菜单
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "角色 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.RoleMenusDto}
//	@ID			RoleController_getMenus
//	@Router		/role/{id}/menus [get]
func (h *RoleHandler) Menus(c *gin.Context) {
	out, e := h.svc.RoleMenus(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// MyMenus GET /profile/menus：当前登录用户的菜单树。
//
// data 是 {menus:[{key,title}], total}（dto.MyMenusDto）。
// 注意：当前实现是**平铺列表**，没有嵌套 children —— 详见 dto/profile.go 的说明。
//
//	@Summary	我的菜单树
//	@Tags		角色
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.MyMenusDto}
//	@ID			UserController_getMenus
//	@Router		/profile/menus [get]
func (h *RoleHandler) MyMenus(c *gin.Context) {
	uid := middleware.CurrentUserID(c)
	var perms []string
	if claims := middleware.ClaimsFrom(c); claims != nil {
		perms = claims.Permissions
	}
	out, e := h.svc.MyMenus(c.Request.Context(), uid, perms)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// PermissionHandler 权限端点。
type PermissionHandler struct {
	svc *service.RoleService
}

// NewPermissionHandler 构造。
func NewPermissionHandler(svc *service.RoleService) *PermissionHandler {
	return &PermissionHandler{svc: svc}
}

// List GET /permission（任务书别名 GET /admin/permissions/all）。
//
//	@Summary	权限点列表
//	@Tags		权限
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.PermissionDto}
//	@ID			PermissionController_findAll
//	@Router		/permission [get]
func (h *PermissionHandler) List(c *gin.Context) {
	list, e := h.svc.ListPermissions(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, list)
}

// Tree GET /permission/tree：按模块分组（前端勾选树）。
//
//	@Summary	权限树
//	@Tags		权限
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.PermissionGroupDto}
//	@ID			PermissionController_getTree
//	@Router		/permission/tree [get]
func (h *PermissionHandler) Tree(c *gin.Context) {
	list, e := h.svc.PermissionTree(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, list)
}

// FindOne GET /permission/{id}。
//
//	@Summary	权限点详情
//	@Tags		权限
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "权限 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.PermissionDto}
//	@ID			PermissionController_findOne
//	@Router		/permission/{id} [get]
func (h *PermissionHandler) FindOne(c *gin.Context) {
	out, e := h.svc.FindPermission(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Create POST /permission。
//
//	@Summary	创建权限点
//	@Tags		权限
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.PermissionRequest true "权限点入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.PermissionDto}
//	@ID			PermissionController_create
//	@Router		/permission [post]
func (h *PermissionHandler) Create(c *gin.Context) {
	var in service.PermissionInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.CreatePermission(c.Request.Context(), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Update PATCH /permission/{id}。
//
//	@Summary	更新权限点
//	@Tags		权限
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "权限 ID"
//	@Param		body body dto.PermissionRequest true "权限点入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.PermissionDto}
//	@ID			PermissionController_update
//	@Router		/permission/{id} [patch]
func (h *PermissionHandler) Update(c *gin.Context) {
	var in service.PermissionInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.UpdatePermission(c.Request.Context(), c.Param("id"), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Remove DELETE /permission/{id}。
//
//	@Summary	删除权限点
//	@Tags		权限
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "权限 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			PermissionController_remove
//	@Router		/permission/{id} [delete]
func (h *PermissionHandler) Remove(c *gin.Context) {
	if e := h.svc.DeletePermission(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// 让 dto 包成为本文件的真实 import：swag 只按「注解所在文件的 import 列表」
// 解析跨包类型（docs/18 §三之一 约束 B）。本文件的注解写了 dto.Xxx，这里引用一次。
// 不参与运行逻辑。
var _ = dto.ResponseBody{}

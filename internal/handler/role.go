// Package handler RBAC 端点：角色管理 10 操作 + 权限管理 5 操作。
//
// 路径对齐 docs/02：/role**、/permission**；
// 任务书要求的 /admin/roles/** 以别名形式一并挂载（同一 handler）。
package handler

import (
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
func (h *RoleHandler) List(c *gin.Context) {
	roles, e := h.svc.ListRoles(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, roles)
}

// ListPaged GET /role/list：分页角色。
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
func (h *RoleHandler) Active(c *gin.Context) {
	roles, e := h.svc.ActiveRoles(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, roles)
}

// FindOne GET /role/{id}。
func (h *RoleHandler) FindOne(c *gin.Context) {
	out, e := h.svc.FindRole(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Create POST /role。
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
func (h *RoleHandler) Remove(c *gin.Context) {
	if e := h.svc.DeleteRole(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// Copy POST /role/{id}/copy。
func (h *RoleHandler) Copy(c *gin.Context) {
	var in struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}
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
func (h *RoleHandler) Menus(c *gin.Context) {
	out, e := h.svc.RoleMenus(c.Request.Context(), c.Param("id"))
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
func (h *PermissionHandler) List(c *gin.Context) {
	list, e := h.svc.ListPermissions(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, list)
}

// Tree GET /permission/tree：按模块分组（前端勾选树）。
func (h *PermissionHandler) Tree(c *gin.Context) {
	list, e := h.svc.PermissionTree(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, list)
}

// FindOne GET /permission/{id}。
func (h *PermissionHandler) FindOne(c *gin.Context) {
	out, e := h.svc.FindPermission(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Create POST /permission。
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
func (h *PermissionHandler) Remove(c *gin.Context) {
	if e := h.svc.DeletePermission(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

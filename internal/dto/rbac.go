package dto

import "time"

// 本文件覆盖 role / permission 两个 RBAC 域的对外请求与响应结构
// （11 + 6 = 17 个操作，见 docs/swagger 的 /role** 与 /permission**）。
//
// 字段来源（逐条可追，不是想象）：
//   - RoleDto / RoleDetailDto ← internal/service/role_service.go 的
//     ListRoles/ActiveRoles/ListRolesPaged（返回 *model.Role）与
//     FindRole（返回 map，键 id/name/displayName/description/isActive/isSystem/
//     permissions/menus/createdAt/updatedAt）
//   - RoleMenusDto            ← RoleService.RoleMenus（roleId/menus/allMenus）
//   - RoleMenusAssignedDto    ← RoleService.AssignMenus（roleId/menus）
//   - RolePermissionsAssignedDto ← RoleService.AssignPermissions
//     （roleId/permissionCount/affectedUsers/permVersionBumped）
//   - RoleStatusDto           ← RoleService.ToggleStatus（id/name/isActive）
//   - PermissionDto           ← model.Permission 的 json tag
//   - PermissionGroupDto      ← RoleService.PermissionTree（module/permissions）
//
// 复用说明（任务书要求核对 dto/user.go）：
// dto/user.go 的 RoleBriefDto 是**三个字段的摘要**（id/name/displayName），
// 用于嵌在用户对象里的 roles；本域需要的是完整角色（含 isActive/isSystem/
// permissions/时间戳），字段集不同，因此**没有复用** —— 用 RoleBriefDto 会丢字段。
// 反过来：删除成功结果直接复用 dto.SuccessDto（见 handler），不重复造。
//
// 空值规则（docs/21-JSON序列化规范.md）：
//   - displayName / description 在 model 层是 *string（DB 要区分 NULL），
//     但对外必须是 ""（值类型）—— 组装层用 strOrEmpty 转换。
//     这是本域 null 的主要来源：GET /role 曾返回 "displayName": null。
//   - permissions / menus / allMenus 空时 []（make 显式初始化）。
//   - id/createdAt/updatedAt 由 DB 必然给出，用值类型（docs/21 §四 例外）。

// PermissionDto 权限点（GET /permission、/permission/{id}、角色内嵌的 permissions 元素）。
//
// description 在 model 层是 *string，对外为 ""（docs/21 规则一）。
type PermissionDto struct {
	ID          string    `json:"id" example:"0b9c8d7e-6f5a-4b3c-2d1e-0f9a8b7c6d5e"`
	Name        string    `json:"name" example:"article:create"`
	Description string    `json:"description" example:"创建文章"`
	CreatedAt   time.Time `json:"created_at" example:"2026-11-19T10:00:00Z"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-11-19T10:00:00Z"`
}

// RoleDto 角色（GET /role、GET /role/active、GET /role/list、
// POST /role、PATCH /role/{id}、POST /role/{id}/copy 的 data）。
//
// permissions 无 omitempty：空时必须 `[]`（model.Role 原本有 omitempty，
// 会整个省略该字段；docs/21 要求数组空时是 []，不是缺失也不是 null）。
type RoleDto struct {
	ID string `json:"id" example:"3f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`

	Name        string `json:"name" example:"editor"`
	DisplayName string `json:"display_name" example:"编辑"`
	Description string `json:"description" example:"内容编辑角色"`

	IsActive bool `json:"is_active" example:"true"`
	// IsSystem 系统内置角色不可删除（DeleteRole 会拒绝）。
	IsSystem bool `json:"is_system" example:"false"`

	Permissions []PermissionDto `json:"permissions"`

	CreatedAt time.Time `json:"created_at" example:"2026-11-19T10:00:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2026-11-19T10:00:00Z"`
}

// RoleDetailDto 角色详情（GET /role/{id}）。
//
// 比 RoleDto 多一个 menus（该角色已授权的菜单键集合）—— FindRole 除角色本体外
// 还查 role_menus 并一并返回。menus 空时 []。
type RoleDetailDto struct {
	RoleDto
	// Menus 该角色已分配的菜单键（对齐 role_menus.menu_key；全集见 DefaultMenuKeys）。
	Menus []string `json:"menus"`
}

// RoleMenusDto 角色菜单（GET /role/{id}/menus）。
//
// AllMenus 是全量合法菜单键清单（service.DefaultMenuKeys），供前端渲染勾选框。
type RoleMenusDto struct {
	RoleID   string   `json:"role_id" example:"3f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`
	Menus    []string `json:"menus"`
	AllMenus []string `json:"all_menus"`
}

// RoleMenusAssignedDto 绑定角色菜单的结果（PUT /role/{id}/menus）。
type RoleMenusAssignedDto struct {
	RoleID string   `json:"role_id" example:"3f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`
	Menus  []string `json:"menus"`
}

// RolePermissionsAssignedDto 绑定角色权限的结果（POST /role/{id}/permissions）。
//
// AffectedUsers / PermVersionBumped 是权限版本 bump 的副产物：
// 权限变更后受影响用户必须重新登录才拿到新权限集。
type RolePermissionsAssignedDto struct {
	RoleID            string `json:"role_id" example:"3f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`
	PermissionCount   int    `json:"permission_count" example:"12"`
	AffectedUsers     int    `json:"affected_users" example:"37"`
	PermVersionBumped bool   `json:"perm_version_bumped" example:"true"`
}

// RoleStatusDto 启用/禁用角色的结果（PATCH /role/{id}/status）。
type RoleStatusDto struct {
	ID       string `json:"id" example:"3f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`
	Name     string `json:"name" example:"editor"`
	IsActive bool   `json:"is_active" example:"false"`
}

// CreateRoleRequest 创建角色请求体（POST /role，service.CreateRoleInput）。
//
// IsActive / IsSystem 保持 *bool：请求体三态是真实需求（不传 = 用默认值）。
type CreateRoleRequest struct {
	Name          string   `json:"name" binding:"required" example:"editor"`
	DisplayName   string   `json:"display_name" example:"编辑"`
	Description   string   `json:"description" example:"内容编辑角色"`
	PermissionIDs []string `json:"permission_ids"`
	IsActive      *bool    `json:"is_active" example:"true"`
	IsSystem      *bool    `json:"is_system" example:"false"`
}

// UpdateRoleRequest 更新角色请求体（PATCH /role/{id}，service.UpdateRoleInput）。
//
// 全部指针语义：nil = 该字段不改（PATCH 的部分更新契约）。
type UpdateRoleRequest struct {
	Name          *string  `json:"name" example:"editor"`
	DisplayName   *string  `json:"display_name" example:"编辑"`
	Description   *string  `json:"description" example:"内容编辑角色"`
	PermissionIDs []string `json:"permission_ids"`
	IsActive      *bool    `json:"is_active" example:"true"`
	IsSystem      *bool    `json:"is_system" example:"false"`
}

// CopyRoleRequest 复制角色请求体（POST /role/{id}/copy）。
type CopyRoleRequest struct {
	Name        string `json:"name" binding:"required" example:"editor-copy"`
	DisplayName string `json:"display_name" example:"编辑（副本）"`
}

// AssignPermissionsRequest 绑定角色权限请求体（POST /role/{id}/permissions）。
//
// 语义是**全量重写**（ReplacePermissions），不是增量追加；传空数组即清空该角色全部权限。
type AssignPermissionsRequest struct {
	PermissionIDs []string `json:"permission_ids" example:"[\"article:create\",\"article:update\"]"`
}

// AssignMenusRequest 绑定角色菜单请求体（PUT /role/{id}/menus）。
//
// 同样是全量重写（ReplaceMenus）；合法键集合见 DefaultMenuKeys。
type AssignMenusRequest struct {
	Menus []string `json:"menus" example:"[\"dashboard\",\"articles\"]"`
}

// ToggleRoleStatusRequest 启用/禁用角色请求体（PATCH /role/{id}/status）。
//
// IsActive 为 nil 时 service 按 true 处理（见 RoleHandler.ToggleStatus）。
type ToggleRoleStatusRequest struct {
	IsActive *bool `json:"is_active" example:"false"`
}

// PermissionRequest 权限点创建 / 更新请求体（POST /permission、PATCH /permission/{id}）。
type PermissionRequest struct {
	Name        string `json:"name" binding:"required" example:"article:pin"`
	Description string `json:"description" example:"置顶文章"`
}

// PermissionGroupDto 权限树的一个模块分组（GET /permission/tree 的数组元素）。
//
// Module 取自权限名的冒号前缀（article:create → article）；无冒号时整串作模块名。
type PermissionGroupDto struct {
	Module      string          `json:"module" example:"article"`
	Permissions []PermissionDto `json:"permissions"`
}

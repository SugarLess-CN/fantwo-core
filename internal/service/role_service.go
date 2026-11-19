// Package service RBAC 服务：角色 / 权限 / 菜单分配 + 权限版本 bump。
//
// 对齐原 role.service.ts + permission.service.ts：
//   - 系统内置角色 super-admin（全权限）/ user（基础权限），启动时幂等初始化；
//   - 角色 CRUD、分页、启用禁用、复制、分配权限；
//   - 权限 CRUD；
//   - 每次权限/角色变更 → bump 受影响用户的 perm_ver（Redis），令其旧 token 失效
//     （对齐 lead 定下的「JWT 携带 roles/perms + perm_ver 版本号」方案）。
package service

import (
	"context"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"gorm.io/gorm"
)

// RoleService 角色/权限业务。
type RoleService struct {
	d *Deps
}

// NewRoleService 构造。
func NewRoleService(d *Deps) *RoleService { return &RoleService{d: d} }

// DefaultPermissionSeeds 系统默认权限清单（对齐原 permission.service.ts 初始化清单）。
var DefaultPermissionSeeds = []struct{ Name, Desc string }{
	{"user:read", "查看用户"}, {"user:create", "创建用户"}, {"user:update", "更新用户"},
	{"user:delete", "删除用户"}, {"user:manage", "用户管理"},
	{"role:read", "查看角色"}, {"role:create", "创建角色"}, {"role:update", "更新角色"}, {"role:delete", "删除角色"},
	{"permission:read", "查看权限"}, {"permission:create", "创建权限"}, {"permission:update", "更新权限"},
	{"permission:delete", "删除权限"}, {"permission:manage", "权限管理"},
	{"article:read", "查看文章"}, {"article:create", "创建文章"}, {"article:update", "更新文章"},
	{"article:delete", "删除文章"}, {"article:manage", "文章管理"},
	{"comment:read", "查看评论"}, {"comment:create", "创建评论"}, {"comment:update", "更新评论"},
	{"comment:delete", "删除评论"}, {"comment:manage", "评论管理"},
	{"category:read", "查看分类"}, {"category:create", "创建分类"}, {"category:update", "更新分类"},
	{"category:delete", "删除分类"}, {"category:manage", "分类管理"},
	{"tag:read", "查看标签"}, {"tag:create", "创建标签"}, {"tag:update", "更新标签"},
	{"tag:delete", "删除标签"}, {"tag:manage", "标签管理"},
	{"collection:read", "查看合集"}, {"collection:create", "创建合集"}, {"collection:update", "更新合集"},
	{"collection:delete", "删除合集"}, {"collection:manage", "合集管理"},
	{"upload:create", "上传文件"}, {"upload:info", "查看上传信息"}, {"upload:delete", "删除上传"},
	{"message:read", "查看消息"}, {"message:create", "发送消息"}, {"message:manage", "消息管理"},
	{"order:read", "查看订单"}, {"order:create", "创建订单"}, {"order:update", "更新订单"},
	{"order:cancel", "取消订单"}, {"order:refund", "订单退款"}, {"order:delete", "删除订单"}, {"order:manage", "订单管理"},
	{"payment:read", "查看支付"}, {"payment:manage", "支付管理"},
	{"report:read", "查看举报"}, {"report:create", "提交举报"}, {"report:manage", "举报管理"},
	{"points:view", "查看积分"}, {"points:claim", "领取积分"}, {"points:manage", "积分管理"},
	{"achievement:read", "查看成就"}, {"achievement:create", "创建成就"}, {"achievement:update", "更新成就"},
	{"achievement:delete", "删除成就"}, {"achievement:manage", "成就管理"},
	{"decoration:read", "查看装饰品"}, {"decoration:create", "创建装饰品"}, {"decoration:update", "更新装饰品"},
	{"decoration:delete", "删除装饰品"}, {"decoration:purchase", "购买装饰品"}, {"decoration:equip", "佩戴装饰品"},
	{"decoration:gift", "赠送装饰品"}, {"decoration:use", "使用装饰品"}, {"decoration:manage", "装饰品管理"},
	{"emoji:read", "查看表情包"}, {"emoji:manage", "表情包管理"},
	{"banner:read", "查看轮播"}, {"banner:list", "轮播列表"}, {"banner:create", "创建轮播"},
	{"banner:update", "更新轮播"}, {"banner:delete", "删除轮播"},
	{"audit:read", "查看审核"}, {"audit:update", "审核操作"},
	{"config:read", "查看配置"}, {"config:update", "更新配置"},
	{"statistics:read", "查看统计"},
	{"certification:read", "查看认证"}, {"certification:audit", "认证审核"},
}

// DefaultUserRolePermissions 普通用户基础权限（对齐原 initializeUserRole）。
var DefaultUserRolePermissions = []string{
	"article:read", "article:create", "article:update", "article:delete",
	"comment:read", "comment:create", "comment:update", "comment:delete",
	"category:read", "tag:read", "user:read", "user:update",
	"upload:info", "upload:create",
	"order:read", "order:create", "order:cancel", "order:refund",
	"report:read", "report:create",
	"decoration:read", "decoration:purchase", "decoration:equip", "decoration:gift",
	"points:view", "points:claim",
	"collection:read", "collection:create", "collection:update", "collection:delete",
	"emoji:read", "banner:read", "banner:list",
}

// DefaultMenuKeys 默认菜单清单（角色菜单分配的合法键集合）。
var DefaultMenuKeys = []string{
	"dashboard", "users", "roles", "permissions", "articles", "comments",
	"categories", "tags", "collections", "uploads", "orders", "payments",
	"reports", "points", "achievements", "decorations", "emojis", "banners",
	"audit", "config", "statistics", "certifications",
}

// InitRBAC 幂等初始化：权限清单 + super-admin / user 角色（对齐原 onModuleInit）。
func (s *RoleService) InitRBAC(ctx context.Context) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	// 1) 权限补齐
	for _, seed := range DefaultPermissionSeeds {
		if _, err := s.d.Perms.FindByName(s.d.DB, seed.Name); err == nil {
			continue
		} else if !IsNotFound(err) {
			return errcode.ErrInternal
		}
		desc := seed.Desc
		if err := s.d.Perms.Create(s.d.DB, &model.Permission{Name: seed.Name, Description: &desc}); err != nil {
			return errcode.ErrInternal
		}
	}
	all, err := s.d.Perms.FindAll(s.d.DB)
	if err != nil {
		return errcode.ErrInternal
	}
	// 2) 超级管理员：全权限
	allIDs := make([]string, 0, len(all))
	for _, p := range all {
		allIDs = append(allIDs, p.ID)
	}
	if e := s.ensureRole(model.RoleNameSuperAdmin, model.RoleDisplayNameSuperAdmin,
		"超级管理员，拥有所有权限", true, allIDs); e != nil {
		return e
	}
	// 3) 普通用户：基础权限
	basic, err := s.d.Perms.FindByNames(s.d.DB, DefaultUserRolePermissions)
	if err != nil {
		return errcode.ErrInternal
	}
	basicIDs := make([]string, 0, len(basic))
	for _, p := range basic {
		basicIDs = append(basicIDs, p.ID)
	}
	return s.ensureRole(model.RoleNameUser, model.RoleDisplayNameUser,
		"普通用户，拥有基础权限", true, basicIDs)
}

// ensureRole 角色不存在则创建，存在则补齐缺失权限（可增不减，对齐原实现）。
func (s *RoleService) ensureRole(name, display, desc string, isSystem bool, permissionIDs []string) *errcode.Err {
	role, err := s.d.Roles.FindByName(s.d.DB, name)
	if IsNotFound(err) {
		d := display
		dd := desc
		r := &model.Role{Name: name, DisplayName: &d, Description: &dd, IsActive: true, IsSystem: isSystem}
		if err := s.d.Roles.Create(s.d.DB, r); err != nil {
			s.d.Logger.Error("ensureRole create: " + err.Error())
			return errcode.ErrInternal
		}
		if err := s.d.Roles.ReplacePermissions(s.d.DB, r.ID, permissionIDs); err != nil {
			return errcode.ErrInternal
		}
		return nil
	}
	if err != nil {
		return errcode.ErrInternal
	}
	existing := map[string]struct{}{}
	for _, p := range role.Permissions {
		existing[p.ID] = struct{}{}
	}
	missing := make([]string, 0)
	for _, id := range permissionIDs {
		if _, ok := existing[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	merged := make([]string, 0, len(existing)+len(missing))
	for id := range existing {
		merged = append(merged, id)
	}
	merged = append(merged, missing...)
	if err := s.d.Roles.ReplacePermissions(s.d.DB, role.ID, merged); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// ListRoles 全部角色。
func (s *RoleService) ListRoles(ctx context.Context) ([]model.Role, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	roles, err := s.d.Roles.FindAll(s.d.DB)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return roles, nil
}

// ActiveRoles 活跃角色。
func (s *RoleService) ActiveRoles(ctx context.Context) ([]model.Role, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	roles, err := s.d.Roles.FindActive(s.d.DB)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return roles, nil
}

// ListRolesPaged 分页角色。
func (s *RoleService) ListRolesPaged(ctx context.Context, page, limit int, name string, isActive *bool) ([]model.Role, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	roles, total, err := s.d.Roles.List(s.d.DB, page, limit, name, isActive)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	return roles, total, nil
}

// FindRole 角色详情（含权限与菜单）。
func (s *RoleService) FindRole(ctx context.Context, id string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	role, err := s.d.Roles.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	menus, _ := s.d.Roles.MenuKeys(s.d.DB, id)
	return map[string]interface{}{
		"id": role.ID, "name": role.Name, "displayName": role.DisplayName,
		"description": role.Description, "isActive": role.IsActive,
		"isSystem": role.IsSystem, "permissions": role.Permissions,
		"menus": menus, "createdAt": role.CreatedAt, "updatedAt": role.UpdatedAt,
	}, nil
}

// CreateRoleInput 创建角色入参。
type CreateRoleInput struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName"`
	Description   string   `json:"description"`
	PermissionIDs []string `json:"permissionIds"`
	IsActive      *bool    `json:"isActive"`
	IsSystem      *bool    `json:"isSystem"`
}

// CreateRole 创建角色。
func (s *RoleService) CreateRole(ctx context.Context, in CreateRoleInput) (*model.Role, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Name == "" {
		return nil, errcode.ErrInvalidParam
	}
	if _, err := s.d.Roles.FindByName(s.d.DB, in.Name); err == nil {
		return nil, errcode.ErrRoleNameExists
	} else if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	system := false
	if in.IsSystem != nil {
		system = *in.IsSystem
	}
	role := &model.Role{
		Name: in.Name, DisplayName: nilIfEmpty(in.DisplayName),
		Description: nilIfEmpty(in.Description), IsActive: active, IsSystem: system,
	}
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.d.Roles.Create(tx, role); err != nil {
			return err
		}
		return s.d.Roles.ReplacePermissions(tx, role.ID, in.PermissionIDs)
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	return s.findRoleOrErr(role.ID)
}

// UpdateRoleInput 更新角色入参。
type UpdateRoleInput struct {
	Name          *string  `json:"name"`
	DisplayName   *string  `json:"displayName"`
	Description   *string  `json:"description"`
	PermissionIDs []string `json:"permissionIds"`
	IsActive      *bool    `json:"isActive"`
	IsSystem      *bool    `json:"isSystem"`
}

// UpdateRole 更新角色（含权限重写 + perm_ver bump）。
func (s *RoleService) UpdateRole(ctx context.Context, id string, in UpdateRoleInput) (*model.Role, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if _, err := s.d.Roles.FindByID(s.d.DB, id); IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	} else if err != nil {
		return nil, errcode.ErrInternal
	}
	fields := map[string]interface{}{}
	if in.Name != nil && *in.Name != "" {
		if exist, err := s.d.Roles.FindByName(s.d.DB, *in.Name); err == nil && exist.ID != id {
			return nil, errcode.ErrRoleNameExists
		}
		fields["name"] = *in.Name
	}
	if in.DisplayName != nil {
		fields["display_name"] = *in.DisplayName
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.IsActive != nil {
		fields["is_active"] = *in.IsActive
	}
	if in.IsSystem != nil {
		fields["is_system"] = *in.IsSystem
	}
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		if len(fields) > 0 {
			if err := s.d.Roles.UpdateFields(tx, id, fields); err != nil {
				return err
			}
		}
		if in.PermissionIDs != nil {
			return s.d.Roles.ReplacePermissions(tx, id, in.PermissionIDs)
		}
		return nil
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	if in.PermissionIDs != nil {
		s.bumpRoleUsers(ctx, id)
	}
	return s.findRoleOrErr(id)
}

// DeleteRole 删除角色（系统角色禁删、使用中禁删）。
func (s *RoleService) DeleteRole(ctx context.Context, id string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	role, err := s.d.Roles.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return errcode.ErrRoleNotExist
	}
	if err != nil {
		return errcode.ErrInternal
	}
	if role.IsSystem {
		return errcode.ErrRoleIsSystem
	}
	n, err := s.d.Roles.CountUsers(s.d.DB, id)
	if err != nil {
		return errcode.ErrInternal
	}
	if n > 0 {
		return errcode.ErrRoleInUse
	}
	if err := s.d.Roles.Delete(s.d.DB, id); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// CopyRole 复制角色（权限/菜单一并复制）。
func (s *RoleService) CopyRole(ctx context.Context, id, name, displayName string) (*model.Role, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if name == "" {
		return nil, errcode.ErrInvalidParam
	}
	src, err := s.d.Roles.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if _, err := s.d.Roles.FindByName(s.d.DB, name); err == nil {
		return nil, errcode.ErrRoleNameExists
	} else if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	permIDs := make([]string, 0, len(src.Permissions))
	for _, p := range src.Permissions {
		permIDs = append(permIDs, p.ID)
	}
	menus, _ := s.d.Roles.MenuKeys(s.d.DB, id)
	dn := displayName
	if dn == "" && src.DisplayName != nil {
		dn = *src.DisplayName + "（副本）"
	}
	desc := "复制自角色 " + src.Name
	copied := &model.Role{
		Name: name, DisplayName: nilIfEmpty(dn), Description: &desc,
		IsActive: src.IsActive, IsSystem: false,
	}
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.d.Roles.Create(tx, copied); err != nil {
			return err
		}
		if err := s.d.Roles.ReplacePermissions(tx, copied.ID, permIDs); err != nil {
			return err
		}
		return s.d.Roles.ReplaceMenus(tx, copied.ID, menus)
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	return s.findRoleOrErr(copied.ID)
}

// ToggleStatus 启用/禁用角色。
func (s *RoleService) ToggleStatus(ctx context.Context, id string, isActive bool) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	role, err := s.d.Roles.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if err := s.d.Roles.UpdateFields(s.d.DB, id, map[string]interface{}{"is_active": isActive}); err != nil {
		return nil, errcode.ErrInternal
	}
	// 角色启用状态变化 → 该角色用户权限集可能变化，bump 让其重新登录。
	s.bumpRoleUsers(ctx, id)
	return map[string]interface{}{"id": role.ID, "name": role.Name, "isActive": isActive}, nil
}

// AssignPermissions 为角色分配权限（重写 + bump）。
func (s *RoleService) AssignPermissions(ctx context.Context, roleID string, permissionIDs []string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if _, err := s.d.Roles.FindByID(s.d.DB, roleID); IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	} else if err != nil {
		return nil, errcode.ErrInternal
	}
	if permissionIDs != nil {
		perms, err := s.d.Perms.FindByIDs(s.d.DB, permissionIDs)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if len(perms) != len(permissionIDs) {
			return nil, errcode.ErrPermissionNotExist
		}
	}
	if err := s.d.Roles.ReplacePermissions(s.d.DB, roleID, permissionIDs); err != nil {
		return nil, errcode.ErrInternal
	}
	bumped := s.bumpRoleUsers(ctx, roleID)
	return map[string]interface{}{
		"roleId": roleID, "permissionCount": len(permissionIDs),
		"affectedUsers": bumped, "permVersionBumped": bumped > 0,
	}, nil
}

// AssignMenus 为角色分配菜单。
func (s *RoleService) AssignMenus(ctx context.Context, roleID string, menuKeys []string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if _, err := s.d.Roles.FindByID(s.d.DB, roleID); IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	} else if err != nil {
		return nil, errcode.ErrInternal
	}
	if err := s.d.Roles.ReplaceMenus(s.d.DB, roleID, menuKeys); err != nil {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{"roleId": roleID, "menus": menuKeys}, nil
}

// RoleMenus 角色菜单。
func (s *RoleService) RoleMenus(ctx context.Context, roleID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	menus, err := s.d.Roles.MenuKeys(s.d.DB, roleID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{"roleId": roleID, "menus": menus, "allMenus": DefaultMenuKeys}, nil
}

// bumpRoleUsers 递增角色下全部用户的权限版本，返回影响人数。
func (s *RoleService) bumpRoleUsers(ctx context.Context, roleID string) int {
	var ids []string
	if err := s.d.DB.Table("user_roles").Where("role_id = ?", roleID).
		Pluck("user_id", &ids).Error; err != nil {
		s.d.Logger.Warn("bump permver query users failed: " + err.Error())
		return 0
	}
	if len(ids) == 0 {
		return 0
	}
	authtoken.BumpPermVerMany(ctx, s.d.Redis, ids)
	return len(ids)
}

// ---------- 权限 CRUD ----------

// ListPermissions 全部权限。
func (s *RoleService) ListPermissions(ctx context.Context) ([]model.Permission, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	list, err := s.d.Perms.FindAll(s.d.DB)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return list, nil
}

// FindPermission 权限详情。
func (s *RoleService) FindPermission(ctx context.Context, id string) (*model.Permission, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	p, err := s.d.Perms.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrPermissionNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return p, nil
}

// PermissionInput 权限创建/更新入参。
type PermissionInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreatePermission 创建权限。
func (s *RoleService) CreatePermission(ctx context.Context, in PermissionInput) (*model.Permission, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Name == "" {
		return nil, errcode.ErrInvalidParam
	}
	if _, err := s.d.Perms.FindByName(s.d.DB, in.Name); err == nil {
		return nil, errcode.ErrPermissionNameExists
	} else if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	p := &model.Permission{Name: in.Name, Description: nilIfEmpty(in.Description)}
	if err := s.d.Perms.Create(s.d.DB, p); err != nil {
		return nil, errcode.ErrInternal
	}
	return p, nil
}

// UpdatePermission 更新权限。
func (s *RoleService) UpdatePermission(ctx context.Context, id string, in PermissionInput) (*model.Permission, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if _, err := s.d.Perms.FindByID(s.d.DB, id); IsNotFound(err) {
		return nil, errcode.ErrPermissionNotExist
	} else if err != nil {
		return nil, errcode.ErrInternal
	}
	fields := map[string]interface{}{}
	if in.Name != "" {
		if exist, err := s.d.Perms.FindByName(s.d.DB, in.Name); err == nil && exist.ID != id {
			return nil, errcode.ErrPermissionNameExists
		}
		fields["name"] = in.Name
	}
	if in.Description != "" {
		fields["description"] = in.Description
	}
	if len(fields) > 0 {
		if err := s.d.Perms.UpdateFields(s.d.DB, id, fields); err != nil {
			return nil, errcode.ErrInternal
		}
	}
	return s.findPermOrErr(id)
}

// DeletePermission 删除权限（引用中禁删）。
func (s *RoleService) DeletePermission(ctx context.Context, id string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if _, err := s.d.Perms.FindByID(s.d.DB, id); IsNotFound(err) {
		return errcode.ErrPermissionNotExist
	} else if err != nil {
		return errcode.ErrInternal
	}
	n, err := s.d.Perms.CountRoles(s.d.DB, id)
	if err != nil {
		return errcode.ErrInternal
	}
	if n > 0 {
		return errcode.ErrPermissionInUse
	}
	if err := s.d.Perms.Delete(s.d.DB, id); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// PermissionTree 权限按模块分组（前端勾选树）。
func (s *RoleService) PermissionTree(ctx context.Context) ([]map[string]interface{}, *errcode.Err) {
	list, e := s.ListPermissions(ctx)
	if e != nil {
		return nil, e
	}
	order := []string{}
	groups := map[string][]model.Permission{}
	for _, p := range list {
		mod := p.Name
		for i, ch := range p.Name {
			if ch == ':' {
				mod = p.Name[:i]
				break
			}
		}
		if _, ok := groups[mod]; !ok {
			order = append(order, mod)
		}
		groups[mod] = append(groups[mod], p)
	}
	out := make([]map[string]interface{}, 0, len(order))
	for _, mod := range order {
		out = append(out, map[string]interface{}{
			"module": mod, "permissions": groups[mod],
		})
	}
	return out, nil
}

// findRoleOrErr 取角色并转成统一错误。
func (s *RoleService) findRoleOrErr(id string) (*model.Role, *errcode.Err) {
	role, err := s.d.Roles.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrRoleNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return role, nil
}

// findPermOrErr 取权限并转成统一错误。
func (s *RoleService) findPermOrErr(id string) (*model.Permission, *errcode.Err) {
	p, err := s.d.Perms.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrPermissionNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return p, nil
}

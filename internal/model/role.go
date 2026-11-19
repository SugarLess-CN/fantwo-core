// Package model RBAC 实体：Role / Permission / RolePermission / RoleMenu。
//
// 对齐原 TypeORM：role.entity.ts（@JoinTable role_permissions）、
// permission.entity.ts。菜单授权（原项目无独立表，任务书 §3 要求
// PUT /admin/roles/{id}/menus）以 role_menus 落地，menu_key 存菜单标识。
package model

import "time"

// 系统内置角色名（对齐原 role.service.ts 的初始化常量）。
const (
	RoleNameSuperAdmin        = "super-admin"
	RoleDisplayNameSuperAdmin = "超级管理员"
	RoleNameUser              = "user"
	RoleDisplayNameUser       = "普通用户"
)

// Role 角色表。
type Role struct {
	BaseModel
	Name        string  `gorm:"column:name;type:varchar(64);uniqueIndex;not null" json:"name"`
	DisplayName *string `gorm:"column:display_name;type:varchar(64)" json:"display_name"`
	Description *string `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive    bool    `gorm:"column:is_active;default:true" json:"is_active"`
	IsSystem    bool    `gorm:"column:is_system;default:false" json:"is_system"`

	// Permissions 角色权限（join 表 role_permissions，eager 语义由 repository 预加载）。
	Permissions []Permission `gorm:"many2many:role_permissions;" json:"permissions,omitempty"`
}

// TableName 表名。
func (Role) TableName() string { return "role" }

// Permission 权限表。
type Permission struct {
	BaseModel
	Name        string  `gorm:"column:name;type:varchar(64);uniqueIndex;not null" json:"name"`
	Description *string `gorm:"column:description;type:varchar(255)" json:"description"`
}

// TableName 表名。
func (Permission) TableName() string { return "permission" }

// RolePermission 角色-权限关联表（GORM many2many 的显式模型，
// 与 Role.Permissions 的 join 表同名同列；提供精确的 AutoMigrate 列定义）。
type RolePermission struct {
	RoleID       string    `gorm:"column:role_id;type:char(36);primaryKey" json:"role_id"`
	PermissionID string    `gorm:"column:permission_id;type:char(36);primaryKey" json:"permission_id"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名。
func (RolePermission) TableName() string { return "role_permissions" }

// RoleMenu 角色-菜单关联表（PUT /admin/roles/{id}/menus 的落点）。
type RoleMenu struct {
	RoleID    string    `gorm:"column:role_id;type:char(36);primaryKey" json:"role_id"`
	MenuKey   string    `gorm:"column:menu_key;type:varchar(128);primaryKey" json:"menu_key"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名。
func (RoleMenu) TableName() string { return "role_menus" }

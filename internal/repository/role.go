// Package repository RBAC 仓储：角色 / 权限 / 角色-权限 / 角色-菜单。
package repository

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// RoleRepo 角色仓储。
type RoleRepo struct{}

// NewRoleRepo 构造。
func NewRoleRepo() *RoleRepo { return &RoleRepo{} }

// Create 创建角色（含权限关联）。
func (r *RoleRepo) Create(db *gorm.DB, role *model.Role) error {
	return db.Create(role).Error
}

// Save 保存角色（含权限关联重写）。
func (r *RoleRepo) Save(db *gorm.DB, role *model.Role) error { return db.Save(role).Error }

// FindByID 按 ID 取角色（预加载权限）。
func (r *RoleRepo) FindByID(db *gorm.DB, id string) (*model.Role, error) {
	var role model.Role
	if err := db.Preload("Permissions").Where("id = ?", id).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// FindByName 按名称取角色。
func (r *RoleRepo) FindByName(db *gorm.DB, name string) (*model.Role, error) {
	var role model.Role
	if err := db.Preload("Permissions").Where("name = ?", name).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

// FindByIDs 批量取角色。
func (r *RoleRepo) FindByIDs(db *gorm.DB, ids []string) ([]model.Role, error) {
	var roles []model.Role
	if len(ids) == 0 {
		return roles, nil
	}
	err := db.Preload("Permissions").Where("id IN ?", ids).Find(&roles).Error
	return roles, err
}

// FindAll 全部角色（预加载权限，对齐原 eager: true）。
func (r *RoleRepo) FindAll(db *gorm.DB) ([]model.Role, error) {
	var roles []model.Role
	err := db.Preload("Permissions").Order("created_at ASC").Find(&roles).Error
	return roles, err
}

// FindActive 活跃角色。
func (r *RoleRepo) FindActive(db *gorm.DB) ([]model.Role, error) {
	var roles []model.Role
	err := db.Preload("Permissions").Where("is_active = ?", true).
		Order("created_at ASC").Find(&roles).Error
	return roles, err
}

// List 角色分页（name 模糊 + isActive 过滤）。
func (r *RoleRepo) List(db *gorm.DB, page, limit int, name string, isActive *bool) ([]model.Role, int64, error) {
	var (
		roles []model.Role
		total int64
	)
	q := db.Model(&model.Role{})
	if name != "" {
		like := "%" + name + "%"
		q = q.Where("name LIKE ? OR display_name LIKE ?", like, like)
	}
	if isActive != nil {
		q = q.Where("is_active = ?", *isActive)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Preload("Permissions").Where(q).
		Order("created_at ASC").Offset((page - 1) * limit).Limit(limit).Find(&roles).Error
	return roles, total, err
}

// UpdateFields 局部更新角色。
func (r *RoleRepo) UpdateFields(db *gorm.DB, id string, fields map[string]interface{}) error {
	return db.Model(&model.Role{}).Where("id = ?", id).Updates(fields).Error
}

// Delete 删除角色（清关联）。
func (r *RoleRepo) Delete(db *gorm.DB, id string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_roles WHERE role_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Role{}).Error
	})
}

// ReplacePermissions 重写角色权限关联（PUT/POST 分配权限）。
func (r *RoleRepo) ReplacePermissions(db *gorm.DB, roleID string, permissionIDs []string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		if len(permissionIDs) == 0 {
			return nil
		}
		rows := make([]model.RolePermission, 0, len(permissionIDs))
		for _, pid := range permissionIDs {
			rows = append(rows, model.RolePermission{RoleID: roleID, PermissionID: pid})
		}
		return tx.Create(&rows).Error
	})
}

// ReplaceMenus 重写角色菜单关联（PUT /admin/roles/{id}/menus）。
func (r *RoleRepo) ReplaceMenus(db *gorm.DB, roleID string, menuKeys []string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		if len(menuKeys) == 0 {
			return nil
		}
		rows := make([]model.RoleMenu, 0, len(menuKeys))
		for _, k := range menuKeys {
			rows = append(rows, model.RoleMenu{RoleID: roleID, MenuKey: k})
		}
		return tx.Create(&rows).Error
	})
}

// MenuKeys 角色菜单键列表。
func (r *RoleRepo) MenuKeys(db *gorm.DB, roleID string) ([]string, error) {
	var keys []string
	err := db.Model(&model.RoleMenu{}).Where("role_id = ?", roleID).
		Pluck("menu_key", &keys).Error
	return keys, err
}

// CountUsers 角色被多少用户使用（删除前校验）。
func (r *RoleRepo) CountUsers(db *gorm.DB, roleID string) (int64, error) {
	var n int64
	err := db.Table("user_roles").Where("role_id = ?", roleID).Count(&n).Error
	return n, err
}

// AssignRoleToUser 给用户挂角色（user_roles join 表，幂等）。
func (r *RoleRepo) AssignRoleToUser(db *gorm.DB, userID, roleID string) error {
	var n int64
	if err := db.Table("user_roles").Where("user_id = ? AND role_id = ?", userID, roleID).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, roleID).Error
}

// PermissionRepo 权限仓储。
type PermissionRepo struct{}

// NewPermissionRepo 构造。
func NewPermissionRepo() *PermissionRepo { return &PermissionRepo{} }

// FindAll 全部权限。
func (r *PermissionRepo) FindAll(db *gorm.DB) ([]model.Permission, error) {
	var list []model.Permission
	err := db.Order("name ASC").Find(&list).Error
	return list, err
}

// FindByID 按 ID 取权限。
func (r *PermissionRepo) FindByID(db *gorm.DB, id string) (*model.Permission, error) {
	var p model.Permission
	if err := db.Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByName 按名称取权限。
func (r *PermissionRepo) FindByName(db *gorm.DB, name string) (*model.Permission, error) {
	var p model.Permission
	if err := db.Where("name = ?", name).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByIDs 批量取权限。
func (r *PermissionRepo) FindByIDs(db *gorm.DB, ids []string) ([]model.Permission, error) {
	var list []model.Permission
	if len(ids) == 0 {
		return list, nil
	}
	err := db.Where("id IN ?", ids).Order("name ASC").Find(&list).Error
	return list, err
}

// FindByNames 按名称批量取。
func (r *PermissionRepo) FindByNames(db *gorm.DB, names []string) ([]model.Permission, error) {
	var list []model.Permission
	if len(names) == 0 {
		return list, nil
	}
	err := db.Where("name IN ?", names).Find(&list).Error
	return list, err
}

// Create 创建权限。
func (r *PermissionRepo) Create(db *gorm.DB, p *model.Permission) error { return db.Create(p).Error }

// Save 保存权限。
func (r *PermissionRepo) Save(db *gorm.DB, p *model.Permission) error { return db.Save(p).Error }

// UpdateFields 局部更新。
func (r *PermissionRepo) UpdateFields(db *gorm.DB, id string, fields map[string]interface{}) error {
	return db.Model(&model.Permission{}).Where("id = ?", id).Updates(fields).Error
}

// Delete 删除权限（先清角色关联）。
func (r *PermissionRepo) Delete(db *gorm.DB, id string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("permission_id = ?", id).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Permission{}).Error
	})
}

// CountRoles 权限被多少角色引用。
func (r *PermissionRepo) CountRoles(db *gorm.DB, permissionID string) (int64, error) {
	var n int64
	err := db.Model(&model.RolePermission{}).Where("permission_id = ?", permissionID).Count(&n).Error
	return n, err
}

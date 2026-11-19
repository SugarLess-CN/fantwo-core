// 用户域实体迁移清单（T-05b）。
//
// 约定见 migrate.go：每域一个 MigrateXxx 文件，只在本文件维护本域实体列表，
// 由 Run 统一依序调用，避免并行成员同时改同一个实体切片。
package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// MigrateUser 用户域 / RBAC / 钱包流水建表。
//
// 顺序说明：先 Role/Permission（user_roles、role_permissions 的 join 表由
// many2many 自动创建，需两端表先存在），再 User 及其附属表。
func MigrateUser(db *gorm.DB) error {
	return db.AutoMigrate(
		// RBAC
		&model.Permission{},
		&model.Role{},
		&model.RolePermission{},
		&model.RoleMenu{},
		// 用户主体与附属
		&model.User{},
		&model.UserProfile{},
		&model.UserDevice{},
		&model.UserSignIn{},
		&model.UserBlock{},
		&model.UserFollowing{},
		&model.UserCertification{},
		&model.AccountDeletion{},
		// 钱包流水
		&model.WalletTransaction{},
	)
}

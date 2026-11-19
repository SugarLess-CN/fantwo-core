// Package migrations 数据库迁移。
//
// 决策：采用 GORM AutoMigrate（对齐原仓库 DB_SYNC=synchronize 的开发语义；
// 用户已确认不需兼容存量数据、全新建库）。生产环境如后续需要受控变更，
// 再引入 golang-migrate 管理 SQL 版本（届时 SQL 放本目录，命名 V{n}__desc.sql）。
package migrations

import (
	"log"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// Run 对注册的全部实体执行 AutoMigrate。
// 新增实体时在此追加到切片即可。
func Run(db *gorm.DB) error {
	if db == nil {
		log.Println("[WARN] 数据库不可用，跳过 AutoMigrate")
		return nil
	}
	return db.AutoMigrate(
		// 骨架示例实体（验证迁移链路；业务实体落地后可移除）
		&model.Sample{},
		// 业务实体按批次在此注册：&model.User{}, &model.Article{}, ...
	)
}

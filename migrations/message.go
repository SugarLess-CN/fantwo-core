// 消息域迁移：站内消息 / 已读回执 / 私信会话 / 私信。
//
// 与 ops 域同样的分工方式：本域实体清单在 MessageModels() 里维护，
// migrate.go 只保留一行 MigrateMessage(db) 调用，避免多成员并行时同改一个实体列表。
//
// 拉黑关系复用 user 域的 model.UserBlock（表 user_block），由 user 域迁移负责建表，
// 本域不重复注册。
package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// MigrateMessage 建 message 域四张表。
func MigrateMessage(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(model.MessageModels()...)
}

// 基础设施域实体迁移（上传文件表 + 系统配置表）。
//
// 独立成文件，避免多成员并行时同时修改 migrate.go 的 AutoMigrate 列表
// （与 ops.go 同一模式，由 migrate.go 的 Run 依次调用各域 Migrate*）。
package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// InfraModels 基础设施域全部实体（顺序按依赖：无外键，配置表先建）。
func InfraModels() []interface{} {
	return []interface{}{
		&model.Config{},    // config 表：系统配置 + 敏感词
		&model.Upload{},    // uploads 表：上传文件
		&model.SearchLog{}, // search_logs 表：检索日志（热门搜索词聚合，keyword+created_at 联合索引）
	}
}

// MigrateInfra 执行基础设施域 AutoMigrate。
func MigrateInfra(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(InfraModels()...)
}

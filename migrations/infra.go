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
	if err := db.AutoMigrate(InfraModels()...); err != nil {
		return err
	}
	return fixUploadHashIndex(db)
}

// fixUploadHashIndex 把 uploads 的单列唯一索引 hash 换成复合唯一 (hash, scene)。
//
// # 为什么必须单独做
//
// AutoMigrate 只会**新增**索引，不会删旧的。模型里把 Hash 的 uniqueIndex 去掉后，
// 老库里那条 `idx_uploads_hash UNIQUE (hash)` 依然存在 —— 于是：
//
//	· 同一张图先传 /upload/file（水印）再传 /upload/avatar（无水印）
//	· 去重查询按 (hash, scene) 查不到 → 走新建流程
//	· INSERT 撞上老的单列唯一约束 → 500 duplicate key
//
// # 为什么改成复合
//
// 水印图与无水印图是**不同产物**，内容 hash 相同但必须各存一行。
// 唯一性应当表达为「同一内容在每个场景下只有一行」= UNIQUE (hash, scene)。
//
// 幂等：DROP IF EXISTS + CREATE IF NOT EXISTS，重复启动安全。
func fixUploadHashIndex(db *gorm.DB) error {
	// 仅 PostgreSQL：其它方言（测试用内存库）没有这套语法，直接跳过。
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	// ① 删掉历史的单列唯一索引
	if err := db.Exec(`DROP INDEX IF EXISTS idx_uploads_hash`).Error; err != nil {
		return err
	}

	// ② 建复合唯一索引（gorm 的 tag 已声明，这里兜底确保老库也有）
	//
	// 注意：若老库里已存在「同 hash 同 scene」的重复行，这条会失败 ——
	// 那种情况说明历史上已经出过 bug，需要人工合并而不是静默跳过。
	return db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_upload_hash_scene
		ON uploads (hash, scene)
	`).Error
}

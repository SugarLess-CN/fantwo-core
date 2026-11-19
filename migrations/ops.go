// 运营域实体迁移（积分/成就/表情包/装饰品/轮播/举报/邀请）。
//
// 独立成文件，避免多成员并行时同时修改 migrate.go 的 AutoMigrate 列表；
// migrate.go 的 Run 只需调用一次 MigrateOps。
package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// OpsModels 运营域全部实体（顺序按依赖：字典表 → 关联表 → 流水表）。
func OpsModels() []interface{} {
	return []interface{}{
		// 积分域
		&model.PointsAccount{},
		&model.PointsActivity{},
		&model.PointsTaskRecord{},
		&model.PointsTransaction{},
		// 成就域
		&model.Achievement{},
		&model.UserAchievement{},
		// 表情包域
		&model.Emoji{},
		&model.EmojiFavorite{},
		&model.EmojiPack{},
		&model.EmojiPackItem{},
		// 装饰品域
		&model.Decoration{},
		&model.UserDecoration{},
		&model.DecorationActivity{},
		&model.UserActivityProgress{},
		// 轮播
		&model.Banner{},
		// 举报
		&model.Report{},
		// 邀请
		&model.Invite{},
		&model.InviteCommission{},
	}
}

// MigrateOps 执行运营域 AutoMigrate。
func MigrateOps(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(OpsModels()...)
}

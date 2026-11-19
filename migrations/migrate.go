// Package migrations 数据库迁移。
//
// 决策：采用 GORM AutoMigrate（对齐原仓库 DB_SYNC=synchronize 的开发语义；
// 用户已确认不需兼容存量数据、全新建库）。生产环境如后续需要受控变更，
// 再引入 golang-migrate 管理 SQL 版本（届时 SQL 放本目录，命名 V{n}__desc.sql）。
//
// 多成员并行约定：本文件只做「汇总调用」，各域实体清单由各自的 MigrateXxx 函数维护，
// 避免同时修改同一个实体列表造成冲突。
package migrations

import (
	"log"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// Run 对注册的全部实体执行 AutoMigrate。
func Run(db *gorm.DB) error {
	if db == nil {
		log.Println("[WARN] 数据库不可用，跳过 AutoMigrate")
		return nil
	}
	if err := db.AutoMigrate(
		// 骨架示例实体（验证迁移链路；业务实体落地后可移除）
		&model.Sample{},
	); err != nil {
		return err
	}
	// 各域实体清单分别维护（每域一个文件），此处依序调用。
	// 注意：跨域只读视图（ConfigKV / UserPaymentView / ArticleOrderView 等）不参与迁移，
	// 其底层表归各自域。
	for _, m := range []func(*gorm.DB) error{
		MigrateUser,    // 用户域：用户/RBAC/设备/签到/关注/认证/注销/钱包流水
		MigrateContent, // 内容域：文章/评论/合集/标签/分类
		MigrateOps,     // 运营域：积分/成就/表情包/装饰品/轮播/举报/邀请
		MigrateTrade,   // 交易域：订单/支付/佣金流水/审核记录/队列去重
		MigrateInfra,   // 基础设施域：系统配置 + 上传文件
		MigrateMessage, // 消息域：站内消息/已读回执/私信会话/私信
	} {
		if err := m(db); err != nil {
			return err
		}
	}
	// 种子数据（幂等：按 code 先查再建，已存在则跳过）。
	if err := SeedOps(db); err != nil {
		return err
	}
	return nil
}

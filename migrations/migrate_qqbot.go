// QQ 机器人域实体迁移清单（T-40）。
//
// 约定见 migrate.go：每域一个 MigrateXxx 文件，只在本文件维护本域实体列表，
// 由 Run 统一依序调用，避免并行成员同时改同一个实体切片。
package migrations

import (
	"fmt"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// MigrateQqbot QQ 机器人三张表建表 + 一条「事件幂等」唯一索引。
//
// 三张表的关系（详见 internal/model/qqbot.go 包注释）：
//
//	qqbot_invite_applications  业务语义（谁申请、批没批、发什么码）
//	qqbot_messages             会话流水（双向，只增不改）
//	qqbot_outbox               发件池（状态反复流转，DEFERRED 不是终态）
//
// # 为什么这里要手写一条 CREATE UNIQUE INDEX
//
// 官方明确「相同 msg_id 可能多次推送」，去重的正确位置是**数据库唯一约束**——
// 应用层「先查后插」在两个 webhook 并发到达时仍会插两条。
//
// 但 model.QqbotMessage.QqbotMsgID 目前只有普通 index（模型文件由 lead 定稿，
// 本次不改它的 tag），所以在这里补一个**部分唯一索引**：
//
//	WHERE qqbot_msg_id <> ''   —— 出站消息允许 qqbot_msg_id 为空（发送响应可能没带 id），
//	                              不带 WHERE 会让所有空串行互相冲突，第二条就插不进去。
//
// 用 IF NOT EXISTS 保证 AutoMigrate 反复跑幂等（重启多次不会报错）。
//
// 副作用边界：这是 PostgreSQL 语法（partial index）。本项目目标库已定 PostgreSQL
// （AGENTS「项目概览」），因此不引入跨库兼容层。
const ukQqbotMessageMsgID = `
CREATE UNIQUE INDEX IF NOT EXISTS uk_qqbot_message_msg_id
    ON qqbot_messages (qqbot_msg_id)
    WHERE qqbot_msg_id <> ''`

// MigrateQqbot 建表 + 幂等唯一索引。
//
// 顺序：先 AutoMigrate 三张表（表必须存在才能建索引），再建索引。
func MigrateQqbot(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.QqbotInviteApplication{},
		&model.QqbotMessage{},
		&model.QqbotOutbox{},
	); err != nil {
		return err
	}
	if err := db.Exec(ukQqbotMessageMsgID).Error; err != nil {
		return fmt.Errorf("创建 qqbot_messages 唯一索引失败: %w", err)
	}
	return nil
}

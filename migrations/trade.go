package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// TradeModels 交易域实体清单（订单 / 支付 / 佣金流水 / 审核记录 / 队列去重）。
// 跨域只读视图（ConfigKV / UserPaymentView / ArticleOrderView）不参与迁移。
func TradeModels() []interface{} {
	out := make([]interface{}, 0, 8)
	out = append(out, model.OrderModels()...)
	out = append(out, model.PaymentModels()...)
	out = append(out, model.AuditModels()...)
	return out
}

// MigrateTrade 迁移交易域实体。
func MigrateTrade(db *gorm.DB) error {
	return db.AutoMigrate(TradeModels()...)
}

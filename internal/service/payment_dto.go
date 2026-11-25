// 支付域 model → dto 转换（对外视图的唯一组装点）。
//
// 三条规则与其余域一致（docs/21）：
//   - 字符串：空即 ""（Payment 的字符串列 NOT NULL 默认空串）
//   - 时间：*time.Time → *string（RFC3339），nil 保持 nil → omitempty 不传字段
//   - 数组：由 toPaymentDtos 统一 make 初始化，空即 []
//
// 金额：Amount 直接透传 float64，不做舍入/换算 —— 精度语义保持原样
// （model 是 decimal(10,2)，落库时已由 PG 舍入）。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// toPaymentDto 支付记录 → dto（12 字段）。
//
// Details 是 jsonb 透传：nil 时不赋值，`json:"details,omitempty"` 让字段整个不传。
func toPaymentDto(p *model.Payment) *dto.PaymentDto {
	if p == nil {
		return nil
	}
	return &dto.PaymentDto{
		ID:                p.ID,
		CreatedAt:         p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         p.UpdatedAt.Format(time.RFC3339),
		OrderID:           p.OrderID,
		UserID:            p.UserID,
		PaymentMethod:     p.PaymentMethod,
		Amount:            p.Amount,
		ThirdPartyOrderNo: p.ThirdPartyOrderNo,
		Status:            p.Status,
		PaidAt:            timePtrRFC3339(p.PaidAt),
		Details:           p.Details,
		ErrorMessage:      p.ErrorMessage,
	}
}

// toPaymentDtos 批量转换并保证非 nil 切片（nil 切片会序列化成 null）。
func toPaymentDtos(rows []model.Payment) []dto.PaymentDto {
	out := make([]dto.PaymentDto, 0, len(rows))
	for i := range rows {
		out = append(out, *toPaymentDto(&rows[i]))
	}
	return out
}

// 订单域 model → dto 转换（对外视图的唯一组装点）。
//
// 三条规则与积分域一致（docs/21）：
//   - 字符串：空即 ""（Order 表的字符串列都是 NOT NULL 默认空串，无需 strOrEmpty，
//     但 PaidAt 等指针必须单独处理）
//   - 时间：*time.Time → *string（RFC3339），nil 保持 nil → omitempty 不传字段
//   - 数组：一律 make 初始化，空即 []
//
// 金额：Amount 直接透传 float64，**不做任何舍入/换算** —— 精度语义保持原样
// （model 是 decimal(10,2)，落库时已由 PG 舍入，这里再动会与库内值不一致）。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// toOrderDto 订单 → dto（17 字段）。
//
// Details 是 jsonb 透传：nil 时不赋值，靠 `json:"details,omitempty"` 让字段整个不传
// （不是 null）。空 map 同样被 omitempty 省略，语义上等价于「没有详情」。
func toOrderDto(o *model.Order) *dto.OrderDto {
	if o == nil {
		return nil
	}
	return &dto.OrderDto{
		ID:             o.ID,
		CreatedAt:      o.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      o.UpdatedAt.Format(time.RFC3339),
		UserID:         o.UserID,
		AuthorID:       o.AuthorID,
		ArticleID:      o.ArticleID,
		OrderNo:        o.OrderNo,
		Type:           o.Type,
		Title:          o.Title,
		Amount:         o.Amount,
		PaymentMethod:  o.PaymentMethod,
		PaymentOrderNo: o.PaymentOrderNo,
		Status:         o.Status,
		PaidAt:         timePtrRFC3339(o.PaidAt),
		Details:        o.Details,
		Remark:         o.Remark,
	}
}

// toOrderDtos 批量转换并保证**非 nil 切片**。
//
// 为什么单独一个函数：三处列表接口（ListOrders / ListPendingOrders 以及后续可能的
// 导出）都要同一保证。nil 切片经 json.Marshal 会输出 null，而对外契约要求 []。
func toOrderDtos(rows []model.Order) []dto.OrderDto {
	out := make([]dto.OrderDto, 0, len(rows))
	for i := range rows {
		out = append(out, *toOrderDto(&rows[i]))
	}
	return out
}

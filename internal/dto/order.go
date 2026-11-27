// 订单域（交易域 order 子域）对外契约：订单实体 + 请求体 + 钱包余额。
//
// 字段来源严格取自 internal/model/order.go（Order 表）与
// internal/service/order_service.go（创建/详情/分页/取消/退款）。
//
// 空值口径（docs/21，硬规则）：
//   - 字符串 → 值类型，空时 ""
//   - 数组   → 非 nil 切片，空时 []
//   - 时间 / 嵌套对象 → *T + omitempty，空时整个字段不传
//
// 金额精度（本批特有）：model.Order.Amount 是 `decimal(10,2)` + Go `float64`，
// dto 保持 float64 不变 —— 改成整数分会让前端已有的数值计算（比较/累加/展示）
// 全部失真，属于不该做的精度语义变更。DTO 注释里标注单位「元」。
package dto

// OrderDto 订单对外视图（model.Order 的全部对外字段，17 个）。
//
// 关于任务书提到的 items / canceledAt：**本项目契约里没有这两个字段**，因此不声明：
//   - 订单没有独立的 items 表（原 NestJS order.entity 也没有）：文章单的商品信息是
//     details 里的 articleId/articleTitle，会员单是 details 里的 plan/duration；
//   - 取消订单只把 status 置为 CANCELLED，不写 canceledAt 列（原仓库同样没有）。
//
// 为了文档不撒谎，这两个字段**不写进 DTO** —— 声明一个前端永远拿不到的数组比缺字段更糟
// （SDK 生成器会为它生成必填类型，前端运行时判空逻辑全落空）。
type OrderDto struct {
	ID        string `json:"id" example:"3c4d5e6f-7a8b-4c9d-8e0f-112233445566"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	// UserID 买家用户 ID。
	UserID string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	// AuthorID 卖家用户 ID。
	//
	// ⚠️ 会员订单（type=MEMBERSHIP）这里是**平台单哨兵值字面量 "1"**，
	// 不是真实用户 ID（对齐原仓库 authorId=1 的约定，见 order_service.go 的
	// CreateMembershipOrder）。前端若用它反查用户会 404，需按 type 分支处理。
	// 已知问题 P3，本轮保持不动。
	AuthorID string `json:"author_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	// ArticleID 文章 ID（仅 type=ARTICLE 的订单有值）。
	ArticleID string `json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`

	// OrderNo 订单号：ORDER + 毫秒时间戳 + 3 位随机（共 21 字符）。
	OrderNo string `json:"order_no" example:"ORDER1764050000000123"`

	Type string `json:"type" enums:"MEMBERSHIP,PRODUCT,SERVICE,ARTICLE" example:"MEMBERSHIP"`

	// Title 订单标题。**中文业务数据（data 内容不是 message），不要翻译**
	// （AGENTS「响应契约与 i18n」第 8 条）。
	Title string `json:"title" example:"充值VIP会员 1个月"`

	// Amount 订单金额，**单位：元**。模型侧是 decimal(10,2)，Go 侧 float64，
	// dto 保持 float64 不改精度语义（改整数分会让前端所有金额计算失真）。
	Amount float64 `json:"amount" example:"19.9"`

	// PaymentMethod 支付方式；未支付时为空串（不是 null）。
	PaymentMethod string `json:"payment_method" enums:"ALIPAY,WECHAT,EPAY,BALANCE" example:"BALANCE"`
	// PaymentOrderNo 第三方支付订单号；未支付时为空串。
	PaymentOrderNo string `json:"payment_order_no" example:"2026112522001445361234567890"`

	Status string `json:"status" enums:"PENDING,PAID,CANCELLED,REFUNDED" example:"PENDING"`

	// PaidAt 支付时间；未支付时不传该字段（不是 null）。
	PaidAt *string `json:"paid_at,omitempty" format:"date-time" example:"2026-11-25T10:05:00Z"`

	// Details 订单详情（jsonb 透传）。内容随 type 变化：
	// 文章单含 articleId/articleTitle/remark，会员单含 plan/duration/basePrice/
	// membershipLevel/membershipName/isLifetime/totalAmount，退款后多 refund 子对象。
	// 空时不传该字段（不是 null）；因为是动态结构，不具名成固定 DTO。
	Details map[string]interface{} `json:"details,omitempty"`

	// Remark 备注（中文业务数据，不翻译）；无备注时为空串。
	Remark string `json:"remark" example:"请尽快开通"`
}

// CreateArticleOrderRequest 创建文章购买订单请求体（POST /order/article）。
//
// 与 service.CreateArticleOrder 的入参逐字对应（handler 就绑这个类型）。
type CreateArticleOrderRequest struct {
	ArticleID string `json:"article_id" binding:"required" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Remark    string `json:"remark" example:"请尽快开通"`
}

// CreateMembershipOrderRequest 创建会员订单请求体（POST /order/membership）。
//
// plan 与 duration 二选一：传 plan 用套餐价（配置表 membership_price_<plan>），
// 传 duration 按月单价 × 月数计算。两者都传时 plan 优先。
//
// plan 的合法取值是小写月份缩写 —— 这是历史遗留的**文档缺口**（P2）：
// 之前 swagger 只声明 object 没枚举，前端传 MONTHLY 会被拒（400）。
// 现在用 enums tag 显式列进文档。
type CreateMembershipOrderRequest struct {
	Plan     string `json:"plan" enums:"1m,3m,6m,12m,lifetime" example:"1m"`
	Duration int    `json:"duration" example:"1"`
	Remark   string `json:"remark" example:"请尽快开通"`
}

// OrderRefundRequest 申请退款请求体（POST /order/{id}/refund）。
//
// 原实现是 handler 内联匿名结构，抽成具名 DTO 后 swagger 才有可读的请求体定义。
// reason 可为空（退款原因不强制填写）。
type OrderRefundRequest struct {
	Reason string `json:"reason" example:"重复购买"`
}

// OrderWalletDto 订单域的钱包余额视图（GET /order/wallet/balance）。
//
// 不复用 dto.WalletBalanceDto：那个只有 balance 一个字段
// （/user/wallet/balance 的契约），而本端点返回 userId + wallet 两个字段。
// 字段名是 wallet 不是 balance —— 既有契约，改名会破前端。
type OrderWalletDto struct {
	UserID string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	// Wallet 钱包余额，**单位：元**（model.UserPaymentView.Wallet 是 decimal(10,2)）。
	Wallet float64 `json:"wallet" example:"100.5"`
}

// 支付域（交易域 payment 子域）对外契约：支付记录 + 发起支付请求/结果 + 易支付签名自测。
//
// 字段来源严格取自 internal/model/payment.go（Payment 表）与
// internal/service/payment_service.go（CreatePayment / GetPaymentRecord /
// GetPaymentByOrder / ListUserPayments / TestEpaySignature）。
//
// 三个回调端点（/payment/notify/{alipay,wechat,epay}）**不走统一契约**，因此没有对应 DTO：
//   - 支付宝：纯文本 "success" / "failure: 原因"
//   - 微信 v3：{"code":"SUCCESS|FAIL","message":"…"}（微信服务器契约）
//   - 易支付：纯文本 "success" / "fail: 原因"
//
// 这是 AGENTS「响应契约与 i18n」第 7 条明列的例外，强行套 ResponseBody 会让支付回调失败。
//
// 空值口径（docs/21）：字符串值类型（空即 ""）、数组非 nil（空即 []）、
// 时间与嵌套对象 *T + omitempty（空即整个字段不传）。任何字段都不输出 null。
//
// 金额精度（本批特有）：model.Payment.Amount 是 `decimal(10,2)` + Go `float64`，
// dto 保持 float64 不变，不擅自改成整数分（会破前端全部金额计算）。
package dto

// PaymentDto 支付记录对外视图（model.Payment 的全部对外字段，12 个）。
//
// 详情（GET /payment/record/{id}、GET /payment/order/{orderId}）与列表
// （GET /payment/user）**共用本结构**：列表不省略任何字段，单独建一个 Brief 会让
// 两处结构各改一遍、迟早漂移。
//
// 中文业务数据说明：本结构本身没有标题/文案类字段。订单标题在 OrderDto.Title
// （Payment 表不冗余存标题），那边已按「data 内容不翻译」处理。
type PaymentDto struct {
	ID        string `json:"id" example:"4d5e6f70-8a9b-4c0d-9e1f-223344556677"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	OrderID string `json:"order_id" example:"3c4d5e6f-7a8b-4c9d-8e0f-112233445566"`
	UserID  string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`

	// PaymentMethod 支付渠道。
	PaymentMethod string `json:"payment_method" enums:"ALIPAY,WECHAT,EPAY,BALANCE" example:"ALIPAY"`

	// Amount 支付金额，**单位：元**（decimal(10,2) / float64，精度语义保持原样）。
	Amount float64 `json:"amount" example:"19.9"`

	// ThirdPartyOrderNo 第三方支付订单号；未回调时为空串（不是 null）。
	ThirdPartyOrderNo string `json:"third_party_order_no" example:"2026112522001445361234567890"`

	Status string `json:"status" enums:"PENDING,SUCCESS,FAILED,CANCELLED" example:"PENDING"`

	// PaidAt 支付成功时间；未成功时不传该字段（不是 null）。
	PaidAt *string `json:"paid_at,omitempty" format:"date-time" example:"2026-11-25T10:05:00Z"`

	// Details 支付详情（jsonb 透传）。按渠道不同含 alipayUrl / wechatCodeUrl /
	// epayUrl / prepayId / orderNo / amount / previousBalance / currentBalance 等键；
	// 余额支付含 balancePayment 标记。空时不传该字段（不是 null）。
	Details map[string]interface{} `json:"details,omitempty"`

	// ErrorMessage 落账失败原因（英文，仅内部诊断用）；正常时为空串。
	ErrorMessage string `json:"error_message" example:""`
}

// PaymentRecordDto 支付记录详情（GET /payment/record/{id} 的语义名）。
//
// 与 PaymentDto 是同一个结构 —— 用别名而不是复制字段：详情与列表项没有差异，
// 复制一份只会产生「改一处忘另一处」的漂移。swag 注解统一写 dto.PaymentDto，
// 保证 schema 名稳定。
type PaymentRecordDto = PaymentDto

// CreatePaymentRequest 发起支付请求体（POST /payment/create）。
//
// 与 service.CreatePaymentInput 一一对应（后者是该类型的别名）。
// paymentMethod 的枚举校验在 binding tag 上：小写 alipay / 未知渠道 / 空值都会被拒（400）。
type CreatePaymentRequest struct {
	// OrderID 待支付的订单 ID（必须是本人的、非 PAID 的订单）。
	OrderID string `json:"order_id" binding:"required" example:"3c4d5e6f-7a8b-4c9d-8e0f-112233445566"`
	// PaymentMethod 支付渠道；渠道开关由系统配置表控制，未开启返回「Payment channel disabled」。
	PaymentMethod string `json:"payment_method" binding:"required,oneof=ALIPAY WECHAT EPAY BALANCE" enums:"ALIPAY,WECHAT,EPAY,BALANCE" example:"ALIPAY"`
	// ReturnURL 支付完成后的前端跳转地址（可选；缺省用配置 default_return_url）。
	ReturnURL string `json:"return_url" example:"https://example.com/pay/return"`
	// Type 仅 EPAY 生效的子渠道：alipay / wxpay / usdt（缺省 alipay）。
	Type string `json:"type" enums:"alipay,wxpay,usdt" example:"alipay"`
}

// CreatePaymentResultDto 发起支付的返回（POST /payment/create 的 data）。
//
// 字段集合随渠道变化，**全部为可选**（不同渠道只出现其中几个键）：
//
//	ALIPAY  → paymentId + paymentUrl + paymentMethod
//	WECHAT  → paymentId + codeUrl   + paymentMethod
//	EPAY    → paymentId + paymentUrl + paymentMethod
//	BALANCE → paymentId + status    + paymentMethod + balance
//
// 运行时由 service 返回 map（按键存在性表达「有无」），因此省略字段**不会**产生
// null —— 这与 map 的序列化行为一致。Balance 用 omitempty：余额支付后余额为 0
// 时该键仍由 map 明确给出，本 DTO 只用于文档，不影响线上输出。
type CreatePaymentResultDto struct {
	PaymentID     string `json:"payment_id" example:"4d5e6f70-8a9b-4c0d-9e1f-223344556677"`
	PaymentMethod string `json:"payment_method" enums:"ALIPAY,WECHAT,EPAY,BALANCE" example:"ALIPAY"`
	// PaymentURL 跳转支付地址（支付宝 / 易支付）。
	PaymentURL string `json:"payment_url,omitempty" example:"https://openapi.alipay.com/gateway.do?..."`
	// CodeURL 二维码内容（微信 Native）。
	CodeURL string `json:"code_url,omitempty" example:"weixin://wxpay/bizpayurl?pr=xxxxxx"`
	// Status 仅余额支付返回，固定 "SUCCESS"。
	Status string `json:"status,omitempty" example:"SUCCESS"`
	// Balance 仅余额支付返回：支付后的钱包余额，**单位：元**。
	Balance float64 `json:"balance,omitempty" example:"80.6"`
}

// EpaySignatureRequest 易支付签名自测请求体（POST /payment/test/epay-signature）。
//
// params 是待签名的原始参数集合（key-value 字符串）。签名算法见 service.epaySign：
// 按 key 升序拼 a=b&c=d（排除 sign / sign_type / 空值）后接 appKey 取 MD5。
//
// params 带 omitempty：请求体本身不受空值规范约束（docs/21 §四例外），
// 但带上它能让 Apifox Mock / 文档示例在未填该字段时不渲染 `"params": null`，
// 且对 gin 的绑定行为零影响（不传即零值 map）。
type EpaySignatureRequest struct {
	Params map[string]string `json:"params,omitempty"`
}

// EpaySignatureResultDto 易支付签名自测结果（POST /payment/test/epay-signature）。
//
// ⚠️ sign_type 是**历史 snake_case 字段名**，与全局 camelCase 口径不一致，但属既有对外
// 契约（MCP 对拍过），改名是 breaking change，本轮保持原样，仅在此标注（P3 一致性）。
type EpaySignatureResultDto struct {
	Sign     string `json:"sign" example:"d41d8cd98f00b204e9800998ecf8427e"`
	SignType string `json:"sign_type" example:"MD5"`
}

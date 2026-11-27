// 支付域 HTTP 处理层（对齐原 payment.controller.ts，含 7 操作 + epay 签名自测）。
//
// 三个回调端点必须 NoAuth（网关直连，无 Authorization 头）：
//
//	POST /payment/notify/alipay  → 返回 "success"/"failure" 纯文本（支付宝规范）
//	POST /payment/notify/wechat  → 返回 {code,message}（微信 v3 规范）
//	GET  /payment/notify/epay    → 返回 "success"（易支付规范）
//
// 注解口径（docs/18）：非回调端点一律 `{data=dto.Xxx}` 具名；回调端点**故意不具名**——
// 它们不走统一响应体（AGENTS「响应契约与 i18n」第 7 条的例外），套 ResponseBody 会让
// 支付平台验签/解析失败。
package handler

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// PaymentHandler 支付端点。
type PaymentHandler struct {
	Svc *service.PaymentService
}

// Create POST /payment/create（发起支付：alipay|wechat|epay|balance）。
//
//	@Summary		发起支付
//	@Description	按渠道（alipay/wechat/epay/balance）生成支付参数；渠道开关与密钥来自系统配置表
//	@Tags			支付
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200		{object}	dto.ResponseBody{data=dto.CreatePaymentResultDto}
//	@Failure		400		{object}	dto.ResponseBody
//	@ID			PaymentController_createPayment
//	@Param		body body dto.CreatePaymentRequest true "请求体"
//	@Router			/payment/create [post]
func (h *PaymentHandler) Create(c *gin.Context) {
	var in service.CreatePaymentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	data, err := h.Svc.CreatePayment(middleware.CurrentUserID(c), in)
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, data)
}

// NotifyAlipay POST /payment/notify/alipay（NoAuth；支付宝要求返回纯文本 success）。
//
//	@Summary		支付宝异步回调
//	@Description	验签通过并完成订单状态机后返回文本 "success"，否则返回 "failure: 原因"
//	@Tags			支付
//	@Accept			application/x-www-form-urlencoded
//	@Produce		plain
//	@Success		200	{string}	string	"success"
//	@ID			PaymentController_alipayNotify
//	@Router			/payment/notify/alipay [post]
func (h *PaymentHandler) NotifyAlipay(c *gin.Context) {
	ok, msg := h.Svc.HandleAlipayNotify(c.Request)
	if ok {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "failure: "+msg)
}

// NotifyWechat POST /payment/notify/wechat（NoAuth；微信要求 200 + {"code":"SUCCESS"}）。
//
//	@Summary		微信支付异步回调
//	@Description	验签 + 解密后按微信 v3 规范返回 {"code":"SUCCESS"}；失败返 500 + {"code":"FAIL"}
//	@Tags			支付
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@ID			PaymentController_wechatNotify
//	@Router			/payment/notify/wechat [post]
func (h *PaymentHandler) NotifyWechat(c *gin.Context) {
	// 注意：这是微信回调契约（微信 v3 规范要求 {"code":"SUCCESS"/"FAIL","message":...}），
	// 不用项目的统一响应体（{data,message,status_code}），不能走 response.Success/Fail。
	ok, msg := h.Svc.HandleWechatNotify(c.Request)
	if ok {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "OK"})
		return
	}
	// msg 为英文短句（见 HandleWechatNotify），作为失败原因透传给微信服务器
	c.JSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": msg})
}

// NotifyEpay GET /payment/notify/epay（NoAuth；易支付要求返回纯文本 success）。
//
//	@Summary		易支付异步回调
//	@Description	MD5 排序验签（排除 sign/sign_type）后返回文本 "success"，否则 "fail: 原因"
//	@Tags			支付
//	@Produce		plain
//	@Success		200	{string}	string	"success"
//	@ID			PaymentController_epayNotify
//	@Router			/payment/notify/epay [get]
func (h *PaymentHandler) NotifyEpay(c *gin.Context) {
	query := map[string]string{}
	for k := range c.Request.URL.Query() {
		query[k] = c.Query(k)
	}
	ok, msg := h.Svc.HandleEpayNotify(query)
	if ok {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "fail: "+msg)
}

// Record GET /payment/record/:id（查询支付记录）。
//
//	@Summary	支付记录详情
//	@Tags		支付
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.PaymentDto}
//	@ID			PaymentController_findPaymentRecord
//	@Router		/payment/record/{id} [get]
func (h *PaymentHandler) Record(c *gin.Context) {
	p, err := h.Svc.GetPaymentRecord(c.Param("id"), middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, p)
}

// ByOrder GET /payment/order/:orderId（查询订单支付记录）。
//
//	@Summary		按订单查支付记录
//	@Description	该订单尚无支付记录时返回 200 且 data 省略（不是 null）
//	@Tags			支付
//	@Produce		json
//	@Security		BearerAuth
//	@Param		order_id path string true "orderId" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success		200	{object}	dto.ResponseBody{data=dto.PaymentDto}
//	@ID			PaymentController_findPaymentByOrderId
//	@Router			/payment/order/{order_id} [get]
func (h *PaymentHandler) ByOrder(c *gin.Context) {
	p, err := h.Svc.GetPaymentByOrder(c.Param("order_id"), middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, p)
}

// Mine GET /payment/user（查询用户支付记录）。
//
//	@Summary	我的支付记录
//	@Tags		支付
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.PaymentDto}}
//	@ID			PaymentController_findUserPayments
//	@Router		/payment/user [get]
func (h *PaymentHandler) Mine(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, err := h.Svc.ListUserPayments(middleware.CurrentUserID(c), page, limit)
	if err != nil {
		failTx(c, err)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// EpaySignature POST /payment/test/epay-signature（管理端：易支付签名自测，不真实下单）。
//
//	@Summary	易支付签名调试
//	@Tags		支付
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.EpaySignatureRequest true "待签名的参数集合"
//	@Success	200	{object}	dto.ResponseBody{data=dto.EpaySignatureResultDto}
//	@ID			PaymentController_epaySignature
//	@Router		/payment/test/epay-signature [post]
func (h *PaymentHandler) EpaySignature(c *gin.Context) {
	var body dto.EpaySignatureRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	sign, err := h.Svc.TestEpaySignature(body.Params)
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, gin.H{"sign": sign, "sign_type": "MD5"})
}

// 让 dto 包成为本文件的真实 import：swag 按「注释所在文件的 import」解析跨包类型，
// 本文件的注解写了 dto.Xxx，这里必须引用一次（不参与运行逻辑）。
var _ = dto.ResponseBody{}

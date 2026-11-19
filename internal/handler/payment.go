// 支付域 HTTP 处理层（对齐原 payment.controller.ts，含 7 操作 + epay 签名自测）。
//
// 三个回调端点必须 NoAuth（网关直连，无 Authorization 头）：
//
//	POST /payment/notify/alipay  → 返回 "success"/"failure" 纯文本（支付宝规范）
//	POST /payment/notify/wechat  → 返回 {code,message}（微信 v3 规范）
//	GET  /payment/notify/epay    → 返回 "success"（易支付规范）
package handler

import (
	"net/http"

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
func (h *PaymentHandler) NotifyAlipay(c *gin.Context) {
	ok, msg := h.Svc.HandleAlipayNotify(c.Request)
	if ok {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "failure: "+msg)
}

// NotifyWechat POST /payment/notify/wechat（NoAuth；微信要求 200 + {"code":"SUCCESS"}）。
func (h *PaymentHandler) NotifyWechat(c *gin.Context) {
	ok, msg := h.Svc.HandleWechatNotify(c.Request)
	if ok {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "FAIL", "message": msg})
}

// NotifyEpay GET /payment/notify/epay（NoAuth；易支付要求返回纯文本 success）。
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
func (h *PaymentHandler) Record(c *gin.Context) {
	p, err := h.Svc.GetPaymentRecord(c.Param("id"), middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, p)
}

// ByOrder GET /payment/order/:orderId（查询订单支付记录）。
func (h *PaymentHandler) ByOrder(c *gin.Context) {
	p, err := h.Svc.GetPaymentByOrder(c.Param("orderId"), middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, p)
}

// Mine GET /payment/user（查询用户支付记录）。
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
func (h *PaymentHandler) EpaySignature(c *gin.Context) {
	var body struct {
		Params map[string]string `json:"params"`
	}
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

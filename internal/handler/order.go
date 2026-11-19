// 订单域 HTTP 处理层（对齐原 order.controller.ts，10 操作）。
package handler

import (
	"strconv"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// OrderHandler 订单端点。
type OrderHandler struct {
	Svc *service.OrderService
}

// failTx 交易域统一错误响应（order/payment/audit 共用；命名带域后缀避免与其它域同名）。
func failTx(c *gin.Context, err error) {
	if e, ok := err.(*errcode.Err); ok {
		response.Fail(c, e)
		return
	}
	response.Fail(c, errcode.ErrInternal)
}

// pageTx 交易域分页参数解析。
func pageTx(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	return page, limit
}

// bindOrderQuery 解析列表查询条件。
func bindOrderQuery(c *gin.Context) model.OrderQuery {
	var q model.OrderQuery
	_ = c.ShouldBindQuery(&q)
	return q
}

// ListAll GET /order（管理员：所有订单）。
func (h *OrderHandler) ListAll(c *gin.Context) {
	q := bindOrderQuery(c)
	q.Page, q.Limit = pageTx(c)
	list, total, err := h.Svc.ListOrders(q, "", true)
	if err != nil {
		failTx(c, err)
		return
	}
	response.SuccessPage(c, list, total, q.Page, q.Limit)
}

// ListMine GET /order/user（我的订单分页）。
func (h *OrderHandler) ListMine(c *gin.Context) {
	q := bindOrderQuery(c)
	q.Page, q.Limit = pageTx(c)
	list, total, err := h.Svc.ListOrders(q, middleware.CurrentUserID(c), false)
	if err != nil {
		failTx(c, err)
		return
	}
	response.SuccessPage(c, list, total, q.Page, q.Limit)
}

// ListPending GET /order/pending（待支付订单）。
func (h *OrderHandler) ListPending(c *gin.Context) {
	list, err := h.Svc.ListPendingOrders(middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, list)
}

// Get GET /order/:id（订单详情）。
func (h *OrderHandler) Get(c *gin.Context) {
	order, err := h.Svc.GetOrder(c.Param("id"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// GetByNo GET /order/no/:orderNo。
func (h *OrderHandler) GetByNo(c *gin.Context) {
	order, err := h.Svc.GetOrderByNo(c.Param("orderNo"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// Cancel PUT /order/:id/cancel（PENDING→CANCELLED）。
func (h *OrderHandler) Cancel(c *gin.Context) {
	order, err := h.Svc.CancelOrder(c.Param("id"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// Refund POST /order/:id/refund（申请退款）。
func (h *OrderHandler) Refund(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	order, err := h.Svc.RequestRefund(c.Param("id"), middleware.CurrentUserID(c), body.Reason, hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// CreateArticle POST /order/article（创建文章订单）。
func (h *OrderHandler) CreateArticle(c *gin.Context) {
	var body struct {
		ArticleID string `json:"articleId" binding:"required"`
		Remark    string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	order, err := h.Svc.CreateArticleOrder(middleware.CurrentUserID(c), body.ArticleID, body.Remark)
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// CreateMembership POST /order/membership（创建会员充值订单）。
func (h *OrderHandler) CreateMembership(c *gin.Context) {
	var body struct {
		Plan     string `json:"plan"`
		Duration int    `json:"duration"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	order, err := h.Svc.CreateMembershipOrder(middleware.CurrentUserID(c), body.Plan, body.Duration, body.Remark)
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// WalletBalance GET /order/wallet/balance（钱包余额）。
func (h *OrderHandler) WalletBalance(c *gin.Context) {
	wallet, err := h.Svc.GetWalletBalance(middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, gin.H{"userId": middleware.CurrentUserID(c), "wallet": wallet})
}

// hasManage 当前用户是否具备 order:manage 权限（越权范围控制）。
func hasManage(c *gin.Context) bool {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		return false
	}
	for _, p := range claims.Permissions {
		if p == "order:manage" {
			return true
		}
	}
	return false
}

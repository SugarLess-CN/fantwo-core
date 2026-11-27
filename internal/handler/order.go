// 订单域 HTTP 处理层（对齐原 order.controller.ts，10 操作）。
//
// 注解口径（docs/18）：每个 @Success 都用 `{data=dto.Xxx}` 嵌套覆盖指明 data 的具名类型；
// 请求体一律具名（原来全是 `body object` 内联，等于没有请求体定义）。
//
// 数组型响应（/order/pending）不是分页：ListPendingOrders 直接返回数组，
// 所以注解用 `{data=[]dto.OrderDto}` 而不是 ResponsePageBody（原来写错成页结构）。
package handler

import (
	"strconv"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
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
//
//	@Summary	全部订单（管理端）
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int		false	"页码"
//	@Param		limit	query	int		false	"每页条数"
//	@Param		status	query	string	false	"订单状态 PENDING/PAID/CANCELLED/REFUNDED"
//	@Param		type	query	string	false	"订单类型 MEMBERSHIP/PRODUCT/SERVICE/ARTICLE"
//	@Param		user_id	query	string	false	"买家用户 ID"
//	@Param		keyword	query	string	false	"订单号模糊搜索"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.OrderDto}}
//	@ID			OrderController_getAllOrders
//	@Router		/order [get]
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
//
//	@Summary	我的订单
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int		false	"页码"
//	@Param		limit	query	int		false	"每页条数"
//	@Param		status	query	string	false	"订单状态 PENDING/PAID/CANCELLED/REFUNDED"
//	@Param		type	query	string	false	"订单类型 MEMBERSHIP/PRODUCT/SERVICE/ARTICLE"
//	@Param		keyword	query	string	false	"订单号模糊搜索"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.OrderDto}}
//	@ID			OrderController_getUserOrders
//	@Router		/order/user [get]
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

// ListPending GET /order/pending（待支付订单，数组不分页）。
//
//	@Summary	我的待支付订单
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.OrderDto}
//	@ID			OrderController_getPendingOrders
//	@Router		/order/pending [get]
func (h *OrderHandler) ListPending(c *gin.Context) {
	list, err := h.Svc.ListPendingOrders(middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, list)
}

// Get GET /order/:id（订单详情）。
//
//	@Summary	订单详情
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"订单 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.OrderDto}
//	@Failure	404	{object}	dto.ResponseBody
//	@ID			OrderController_findOne
//	@Router		/order/{id} [get]
func (h *OrderHandler) Get(c *gin.Context) {
	order, err := h.Svc.GetOrder(c.Param("id"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// GetByNo GET /order/no/:orderNo。
//
//	@Summary	按订单号查询
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Param		orderNo path string true "orderNo"
//	@Success	200	{object}	dto.ResponseBody{data=dto.OrderDto}
//	@ID			OrderController_findByOrderNo
//	@Router		/order/no/{orderNo} [get]
func (h *OrderHandler) GetByNo(c *gin.Context) {
	order, err := h.Svc.GetOrderByNo(c.Param("orderNo"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// Cancel PUT /order/:id/cancel（PENDING→CANCELLED）。
//
//	@Summary	取消订单
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	dto.ResponseBody{data=dto.OrderDto}
//	@ID			OrderController_cancelOrder
//	@Router		/order/{id}/cancel [put]
func (h *OrderHandler) Cancel(c *gin.Context) {
	order, err := h.Svc.CancelOrder(c.Param("id"), middleware.CurrentUserID(c), hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// Refund POST /order/:id/refund（申请退款）。
//
//	@Summary		申请退款
//	@Description	余额支付原路退余额；第三方支付提交原路退款申请（均落 payment 记录）
//	@Tags			订单
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"订单 ID"
//	@Param			body	body		dto.OrderRefundRequest	false	"退款原因（可不传）"
//	@Success		200		{object}	dto.ResponseBody{data=dto.OrderDto}
//	@Failure		400		{object}	dto.ResponseBody
//	@ID			OrderController_requestRefund
//	@Router			/order/{id}/refund [post]
func (h *OrderHandler) Refund(c *gin.Context) {
	var body dto.OrderRefundRequest
	_ = c.ShouldBindJSON(&body)
	order, err := h.Svc.RequestRefund(c.Param("id"), middleware.CurrentUserID(c), body.Reason, hasManage(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, order)
}

// CreateArticle POST /order/article（创建文章订单）。
//
//	@Summary		创建文章购买订单
//	@Tags			订单
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		dto.CreateArticleOrderRequest	true	"文章订单入参"
//	@Success		200		{object}	dto.ResponseBody{data=dto.OrderDto}
//	@Failure		400		{object}	dto.ResponseBody
//	@ID			OrderController_createArticleOrder
//	@Router			/order/article [post]
func (h *OrderHandler) CreateArticle(c *gin.Context) {
	var body dto.CreateArticleOrderRequest
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
//
//	@Summary		创建会员订单
//	@Tags			订单
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		dto.CreateMembershipOrderRequest	true	"会员订单入参（plan 与 duration 二选一）"
//	@Success		200		{object}	dto.ResponseBody{data=dto.OrderDto}
//	@Failure		400		{object}	dto.ResponseBody
//	@ID			OrderController_createMembershipOrder
//	@Router			/order/membership [post]
func (h *OrderHandler) CreateMembership(c *gin.Context) {
	var body dto.CreateMembershipOrderRequest
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
//
//	@Summary	钱包余额
//	@Tags		订单
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.OrderWalletDto}
//	@ID			OrderController_getWalletBalance
//	@Router		/order/wallet/balance [get]
func (h *OrderHandler) WalletBalance(c *gin.Context) {
	uid := middleware.CurrentUserID(c)
	wallet, err := h.Svc.GetWalletBalance(uid)
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, dto.OrderWalletDto{UserID: uid, Wallet: wallet})
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

// 让 dto 包成为本文件的真实 import：swag 按「注释所在文件的 import」解析跨包类型，
// 本文件的注解写了 dto.Xxx，这里必须引用一次（不参与运行逻辑）。
var _ = dto.ResponseBody{}

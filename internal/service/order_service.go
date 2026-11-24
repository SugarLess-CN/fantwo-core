// 订单服务：创建/详情/分页/取消/退款，对齐原 order.service.ts。
// 订单号规则对齐原仓库：ORDER + 毫秒时间戳 + 3 位随机（0-999 补零）。
package service

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"gorm.io/gorm"
)

// OrderService 订单业务。
type OrderService struct {
	DB *gorm.DB
}

// NewOrderService 构造。
func NewOrderService(db *gorm.DB) *OrderService { return &OrderService{DB: db} }

// GenerateOrderNo 生成订单号：ORDER<ms><3位随机>（对齐原 generateOrderNo）。
func GenerateOrderNo() string {
	return fmt.Sprintf("ORDER%d%03d", time.Now().UnixMilli(), rand.Intn(1000))
}

// ErrOrderNotPending 非 PENDING 状态不可取消。
var ErrOrderNotPending = errcode.ErrBadRequest.WithKey(i18n.OrderNotPending)

// CreateArticleOrder 创建文章订单。
func (s *OrderService) CreateArticleOrder(userID string, articleID, remark string) (*model.Order, error) {
	var article model.ArticleOrderView
	if err := s.DB.First(&article, "id = ?", articleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)
		}
		return nil, err
	}
	if !article.RequirePayment {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ArticleNotPayable)
	}
	if article.ViewPrice <= 0 {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ArticlePriceInvalid)
	}
	// 已购买检查
	var cnt int64
	if err := s.DB.Model(&model.Order{}).Where(
		"user_id = ? AND article_id = ? AND status = ?", userID, articleID, model.OrderStatusPaid,
	).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt > 0 {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ArticleAlreadyPurchased)
	}

	order := &model.Order{
		UserID:    userID,
		AuthorID:  article.AuthorID,
		ArticleID: article.ID,
		OrderNo:   GenerateOrderNo(),
		Type:      model.OrderTypeArticle,
		Title:     "购买文章：" + article.Title,
		Amount:    article.ViewPrice,
		Status:    model.OrderStatusPending,
		Details: map[string]interface{}{
			"articleId":    article.ID,
			"articleTitle": article.Title,
			"remark":       remark,
		},
		Remark: remark,
	}
	if err := s.DB.Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}

// membershipPlan 会员套餐定义（对齐原 createMembershipOrder planMap）。
var membershipPlans = map[string]struct {
	months   int
	priceKey string
	title    string
	lifetime bool
}{
	"1m":       {1, "membership_price_1m", "充值VIP会员 1个月", false},
	"3m":       {3, "membership_price_3m", "充值VIP会员 3个月", false},
	"6m":       {6, "membership_price_6m", "充值VIP会员 6个月", false},
	"12m":      {12, "membership_price_12m", "充值VIP会员 12个月", false},
	"lifetime": {0, "membership_price_lifetime", "充值VIP会员 永久", true},
}

// getMembershipConfig 从 config 表读会员配置（key-value，缺省对齐原仓库）。
func (s *OrderService) getMembershipConfig(key, def string) string {
	var c model.ConfigKV
	if err := s.DB.First(&c, "\"key\" = ?", key).Error; err != nil || c.Value == "" {
		return def
	}
	return c.Value
}

// CreateMembershipOrder 创建会员充值订单。
func (s *OrderService) CreateMembershipOrder(userID, plan string, duration int, remark string) (*model.Order, error) {
	var user model.UserPaymentView
	if err := s.DB.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.UserNotFound)
		}
		return nil, err
	}
	// 永久会员不可再充
	if user.MembershipStatus == "ACTIVE" && user.MembershipEndDate == nil {
		return nil, errcode.ErrBadRequest.WithKey(i18n.MembershipLifetimeActive)
	}

	basePrice, _ := strconv.ParseFloat(s.getMembershipConfig("membership_price", "19.9"), 64)
	memberName := s.getMembershipConfig("membership_name", "VIP会员")

	var title string
	var amount float64
	details := map[string]interface{}{"membershipLevel": 1, "membershipName": memberName, "remark": remark}

	if plan != "" {
		p, ok := membershipPlans[plan]
		if !ok {
			return nil, errcode.ErrBadRequest.WithKey(i18n.MembershipPlanInvalid)
		}
		title = fmt.Sprintf("充值%s %s", memberName, map[bool]string{true: "永久", false: fmt.Sprintf("%d个月", p.months)}[p.lifetime])
		if pv := s.getMembershipConfig(p.priceKey, ""); pv != "" {
			amount, _ = strconv.ParseFloat(pv, 64)
		}
		if amount <= 0 && !p.lifetime {
			amount = basePrice * float64(p.months)
		}
		details["plan"] = plan
		details["isLifetime"] = p.lifetime
		if !p.lifetime {
			details["duration"] = p.months
		}
	} else {
		if duration < 1 || duration > 120 {
			return nil, errcode.ErrBadRequest.WithKey(i18n.MembershipDurationInvalid)
		}
		title = fmt.Sprintf("充值%s %d个月", memberName, duration)
		amount = basePrice * float64(duration)
		details["duration"] = duration
	}
	details["basePrice"] = basePrice
	details["totalAmount"] = amount

	order := &model.Order{
		UserID:   userID,
		AuthorID: "1", // 平台单（对齐原仓库 authorId=1）
		OrderNo:  GenerateOrderNo(),
		Type:     model.OrderTypeMembership,
		Title:    title,
		Amount:   amount,
		Status:   model.OrderStatusPending,
		Details:  details,
		Remark:   remark,
	}
	if err := s.DB.Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}

// findByIDWithScope 按 id 查订单；非 order:manage 权限时限定本人。
func (s *OrderService) findByIDWithScope(id string, userID string, hasManage bool) (*model.Order, error) {
	var order model.Order
	q := s.DB
	if !hasManage {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.First(&order, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.OrderNotFound)
		}
		return nil, err
	}
	return &order, nil
}

// GetOrder 订单详情。
func (s *OrderService) GetOrder(id, userID string, hasManage bool) (*model.Order, error) {
	return s.findByIDWithScope(id, userID, hasManage)
}

// GetOrderByNo 按订单号查询。
func (s *OrderService) GetOrderByNo(orderNo, userID string, hasManage bool) (*model.Order, error) {
	var order model.Order
	q := s.DB
	if !hasManage {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.First(&order, "order_no = ?", orderNo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.OrderNotFound)
		}
		return nil, err
	}
	return &order, nil
}

// ListOrders 订单分页（管理员传全量范围；用户限定本人）。
func (s *OrderService) ListOrders(q model.OrderQuery, userID string, hasManage bool) ([]model.Order, int64, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 10
	}
	db := s.DB.Model(&model.Order{})
	if !hasManage {
		db = db.Where("user_id = ?", userID)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Type != "" {
		db = db.Where("type = ?", q.Type)
	}
	if q.UserID != "" && hasManage {
		db = db.Where("user_id = ?", q.UserID)
	}
	if q.Keyword != "" {
		db = db.Where("order_no LIKE ?", "%"+q.Keyword+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Order
	if err := db.Order("created_at DESC").Offset((q.Page - 1) * q.Limit).Limit(q.Limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListPendingOrders 待支付订单。
func (s *OrderService) ListPendingOrders(userID string) ([]model.Order, error) {
	var list []model.Order
	err := s.DB.Where("user_id = ? AND status = ?", userID, model.OrderStatusPending).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

// CancelOrder 取消订单（PENDING→CANCELLED）。
func (s *OrderService) CancelOrder(id, userID string, hasManage bool) (*model.Order, error) {
	order, err := s.findByIDWithScope(id, userID, hasManage)
	if err != nil {
		return nil, err
	}
	if !CanTransition(order.Status, model.OrderStatusCancelled) {
		return nil, ErrOrderNotPending
	}
	if err := s.DB.Model(order).Update("status", model.OrderStatusCancelled).Error; err != nil {
		return nil, err
	}
	order.Status = model.OrderStatusCancelled
	return order, nil
}

// RequestRefund 申请退款（PAID→REFUNDED；余额支付原路退回钱包，其余仅改状态待人工处理）。
// 事务边界：钱包更新 + 订单状态在同一事务，用户行悲观锁。
func (s *OrderService) RequestRefund(id, userID, reason string, hasManage bool) (*model.Order, error) {
	var order model.Order
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(dbLocking()).Where("status = ?", model.OrderStatusPaid)
		if !hasManage {
			q = q.Where("user_id = ?", userID)
		}
		if err := q.First(&order, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.ErrNotFound.WithKey(i18n.OrderNotFound)
			}
			return err
		}

		now := time.Now()
		refund := map[string]interface{}{
			"reason":       reason,
			"requestedAt":  now,
			"refundAmount": order.Amount,
		}
		if order.PaymentMethod == model.PayMethodBalance {
			// 余额支付：退回钱包（悲观锁）
			var user model.UserPaymentView
			if err := tx.Clauses(dbLocking()).First(&user, "id = ?", order.UserID).Error; err != nil {
				return err
			}
			previous := user.Wallet
			user.Wallet += order.Amount
			if err := tx.Model(&model.UserPaymentView{}).Where("id = ?", user.ID).
				Update("wallet", user.Wallet).Error; err != nil {
				return err
			}
			refund["refundMethod"] = "BALANCE"
			refund["previousBalance"] = previous
			refund["currentBalance"] = user.Wallet
			refund["refundedAt"] = now
		} else {
			refund["refundMethod"] = order.PaymentMethod
			refund["status"] = "PENDING" // 待人工处理
		}

		if order.Details == nil {
			order.Details = map[string]interface{}{}
		}
		order.Details["refund"] = refund
		order.Status = model.OrderStatusRefunded
		return tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]interface{}{
			"status":  model.OrderStatusRefunded,
			"details": order.Details,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// ---------- 订单状态机（可测性抽取；表驱动单测覆盖） ----------

// orderTransitions 订单状态机合法迁移白名单。
//
// 状态机（对齐 docs/01：PENDING→PAID/CANCELLED/REFUNDED）：
//
//	PENDING   → PAID / CANCELLED / REFUNDED
//	PAID      → REFUNDED
//	CANCELLED → （终态）
//	REFUNDED  → （终态）
//
// 防什么 bug：回调乱序/重复投递时，若不做状态守卫就会出现
// 「已退款订单被回调改回 PAID」「已取消订单被回调改成 PAID」这类脏写。
var orderTransitions = map[string][]string{
	model.OrderStatusPending:   {model.OrderStatusPaid, model.OrderStatusCancelled, model.OrderStatusRefunded},
	model.OrderStatusPaid:      {model.OrderStatusRefunded},
	model.OrderStatusCancelled: {},
	model.OrderStatusRefunded:  {},
}

// CanTransition 判断订单状态迁移是否合法（未知状态一律拒绝）。
func CanTransition(from, to string) bool {
	next, ok := orderTransitions[from]
	if !ok {
		return false
	}
	for _, s := range next {
		if s == to {
			return true
		}
	}
	return false
}

// MarkPaidInTx 事务内标记订单已支付（供 payment 回调复用；状态机守卫：
// 非 PENDING 时返回 ErrOrderNotPending，由调用方决定幂等返回）。
func MarkPaidInTx(tx *gorm.DB, orderID, method, thirdNo string) (*model.Order, error) {
	var order model.Order
	if err := tx.Clauses(dbLocking()).First(&order, "id = ?", orderID).Error; err != nil {
		return nil, err
	}
	if order.Status == model.OrderStatusPaid {
		return &order, nil // 已支付：幂等返回（回调重投不报错、不重复写）
	}
	// 状态机守卫：只有 PENDING 能迁到 PAID
	if !CanTransition(order.Status, model.OrderStatusPaid) {
		return nil, ErrOrderNotPending
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status":         model.OrderStatusPaid,
		"payment_method": method,
		"paid_at":        now,
	}
	if thirdNo != "" {
		updates["payment_order_no"] = thirdNo
	}
	if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	order.Status = model.OrderStatusPaid
	order.PaymentMethod = method
	order.PaidAt = &now
	return &order, nil
}

// GetWalletBalance 钱包余额（GET /order/wallet/balance）。
func (s *OrderService) GetWalletBalance(userID string) (float64, error) {
	var user model.UserPaymentView
	if err := s.DB.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errcode.ErrNotFound.WithKey(i18n.UserNotFound)
		}
		return 0, err
	}
	return user.Wallet, nil
}

// publishOrderPaid 事务提交后发布支付成功事件。
//
// 两件事：
//  1. order.paid —— 通用事件（积分/成就/通知/邀请域消费）；
//  2. membership.purchased —— 会员单专属事件（成就域 BECOME_MEMBER 依赖，
//     与 ops 从 order.paid 里按 Type==MEMBERSHIP 的兜底触发并存且幂等）。
func publishOrderPaid(order *model.Order, commission *event.CommissionInfo) {
	event.Default().Publish(event.EventOrderPaid, event.OrderPaidPayload{
		OrderID:    order.ID,
		OrderNo:    order.OrderNo,
		UserID:     order.UserID,
		AuthorID:   order.AuthorID,
		Type:       order.Type,
		Amount:     order.Amount,
		Method:     order.PaymentMethod,
		ArticleID:  order.ArticleID,
		Commission: commission,
	})

	// 会员订单：额外发布 membership.purchased（载荷严格对齐
	// event.MembershipPurchasedPayload：UserID + OrderID）。
	if order.Type == model.OrderTypeMembership {
		event.Default().Publish(event.EventMembershipPurchased, event.MembershipPurchasedPayload{
			UserID:  order.UserID,
			OrderID: order.ID,
		})
	}
}

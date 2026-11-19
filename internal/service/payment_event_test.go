// 交易域事件发布单测：order.paid + membership.purchased（会员单专属）。
//
// 验证点：
//  1. MEMBERSHIP 订单 → 同时发 order.paid 与 membership.purchased，载荷字段严格对齐；
//  2. 非 MEMBERSHIP 订单 → 只发 order.paid，不发 membership.purchased；
//  3. 事件总线用独立实例，避免污染全局 bus。
package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// captureBus 临时替换全局总线并返回收集容器。
func captureBus(t *testing.T) (*event.Bus, *[]string, *map[string]interface{}) {
	t.Helper()
	prev := event.Default()
	bus := event.NewBus()
	event.SetDefault(bus)
	t.Cleanup(func() { event.SetDefault(prev) })

	var topics []string
	payloads := make(map[string]interface{})
	bus.Subscribe(event.EventOrderPaid, func(p interface{}) {
		topics = append(topics, event.EventOrderPaid)
		payloads[event.EventOrderPaid] = p
	})
	bus.Subscribe(event.EventMembershipPurchased, func(p interface{}) {
		topics = append(topics, event.EventMembershipPurchased)
		payloads[event.EventMembershipPurchased] = p
	})
	return bus, &topics, &payloads
}

func TestPublishOrderPaid_MembershipOrder(t *testing.T) {
	_, topics, payloads := captureBus(t)

	order := &model.Order{
		BaseModel:     model.BaseModel{ID: "order-1"},
		UserID:        "user-1",
		AuthorID:      "1",
		OrderNo:       "ORDER123",
		Type:          model.OrderTypeMembership,
		Amount:        19.9,
		PaymentMethod: model.PayMethodBalance,
		Status:        model.OrderStatusPaid,
	}
	publishOrderPaid(order, nil)

	if len(*topics) != 2 {
		t.Fatalf("应发布 2 个事件，实际 %d: %v", len(*topics), *topics)
	}
	if (*topics)[0] != event.EventOrderPaid || (*topics)[1] != event.EventMembershipPurchased {
		t.Fatalf("事件顺序不对: %v", *topics)
	}

	p, ok := (*payloads)[event.EventMembershipPurchased].(event.MembershipPurchasedPayload)
	if !ok {
		t.Fatalf("membership.purchased 载荷类型错误")
	}
	if p.UserID != "user-1" || p.OrderID != "order-1" {
		t.Fatalf("载荷字段不匹配: %+v", p)
	}

	paid, ok := (*payloads)[event.EventOrderPaid].(event.OrderPaidPayload)
	if !ok {
		t.Fatalf("order.paid 载荷类型错误")
	}
	if paid.Type != model.OrderTypeMembership || paid.Amount != 19.9 {
		t.Fatalf("order.paid 载荷字段不匹配: %+v", paid)
	}
}

func TestPublishOrderPaid_NonMembershipOrder(t *testing.T) {
	_, topics, _ := captureBus(t)

	order := &model.Order{
		BaseModel:     model.BaseModel{ID: "order-2"},
		UserID:        "user-2",
		AuthorID:      "author-2",
		OrderNo:       "ORDER456",
		Type:          model.OrderTypeArticle,
		ArticleID:     "article-1",
		Amount:        5,
		PaymentMethod: model.PayMethodAlipay,
		Status:        model.OrderStatusPaid,
	}
	publishOrderPaid(order, nil)

	if len(*topics) != 1 {
		t.Fatalf("非会员单应只发 1 个事件，实际 %d: %v", len(*topics), *topics)
	}
	if (*topics)[0] != event.EventOrderPaid {
		t.Fatalf("事件应为 order.paid，实际 %v", (*topics)[0])
	}
}

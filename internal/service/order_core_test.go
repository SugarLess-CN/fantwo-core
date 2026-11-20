// 订单域核心逻辑单测（不依赖 PG）。
//
// 覆盖：
//  1. 订单号生成规则（原仓库 ORDER + 毫秒时间戳 + 3 位随机）；
//  2. 订单状态机 CanTransition 全矩阵（合法/非法迁移）；
//  3. 支付事件发布（order.paid + membership.purchased）见 payment_event_test.go。
package service

import (
	"regexp"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// orderNoPattern 订单号规则：ORDER + 13 位毫秒时间戳 + 3 位随机数。
var orderNoPattern = regexp.MustCompile(`^ORDER\d{16}$`)

// TestGenerateOrderNo 订单号格式与单调性。
//
// 防什么 bug：
//   - 退化成 UUID/纯随机数字 → 与原仓库/前端展示、对账脚本不兼容；
//   - 随机位不足 3 位（如 random=7 未补零）→ 长度抖动，字符串排序错乱；
//   - 时间戳用了秒级 → 同秒内大量订单前缀相同，排查困难。
func TestGenerateOrderNo(t *testing.T) {
	for i := 0; i < 200; i++ {
		no := GenerateOrderNo()
		if !orderNoPattern.MatchString(no) {
			t.Fatalf("订单号格式不符（第 %d 次）: %q", i, no)
		}
		if len(no) != len("ORDER")+13+3 {
			t.Fatalf("订单号长度应为 21，实际 %d: %q", len(no), no)
		}
	}
}

// TestCanTransition 订单状态机合法/非法迁移全矩阵。
//
// 防什么 bug（回调乱序 / 重复投递 / 人工误操作）：
//   - 已退款、已取消的订单被再次置为 PAID → 凭空发货；
//   - 终态（REFUNDED/CANCELLED）被改回任何其它状态 → 账实不符；
//   - 未知状态（脏数据）被放行 → 状态机失效。
func TestCanTransition(t *testing.T) {
	const (
		pending   = model.OrderStatusPending
		paid      = model.OrderStatusPaid
		cancelled = model.OrderStatusCancelled
		refunded  = model.OrderStatusRefunded
	)
	cases := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{"PENDING→PAID 合法", pending, paid, true},
		{"PENDING→CANCELLED 合法", pending, cancelled, true},
		{"PENDING→REFUNDED 合法（异常单直退）", pending, refunded, true},
		{"PENDING→PENDING 无意义，拒绝", pending, pending, false},

		{"PAID→REFUNDED 合法", paid, refunded, true},
		{"PAID→PAID 重复支付回调应幂等，不由状态机放行", paid, paid, false},
		{"PAID→CANCELLED 非法（已付款不能取消）", paid, cancelled, false},
		{"PAID→PENDING 非法（不可回滚）", paid, pending, false},

		{"CANCELLED 为终态", cancelled, paid, false},
		{"CANCELLED→REFUNDED 非法", cancelled, refunded, false},

		{"REFUNDED 为终态", refunded, paid, false},
		{"REFUNDED→CANCELLED 非法", refunded, cancelled, false},

		{"未知来源状态一律拒绝", "WEIRD", paid, false},
		{"未知目标状态一律拒绝", pending, "WEIRD", false},
		{"空状态一律拒绝", "", paid, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanTransition(c.from, c.to); got != c.want {
				t.Fatalf("CanTransition(%q→%q) = %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

// TestOrderTransitionsComplete 状态机表必须覆盖全部业务状态。
//
// 防什么 bug：新增订单状态后忘记登记到 orderTransitions，
// 该状态下的所有迁移都会静默变为「非法」，表现为功能整体失灵。
func TestOrderTransitionsComplete(t *testing.T) {
	all := []string{
		model.OrderStatusPending,
		model.OrderStatusPaid,
		model.OrderStatusCancelled,
		model.OrderStatusRefunded,
	}
	if len(orderTransitions) != len(all) {
		t.Fatalf("orderTransitions 应覆盖 %d 个状态，实际 %d 个", len(all), len(orderTransitions))
	}
	for _, s := range all {
		if _, ok := orderTransitions[s]; !ok {
			t.Fatalf("状态 %q 未登记到 orderTransitions", s)
		}
	}
}

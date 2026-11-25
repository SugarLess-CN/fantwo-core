// 订单域 DTO 空值回归测试（docs/21）。
//
// 重点保三件事：
//  1. PaidAt 为 nil 时字段整个不传（不是 null）；
//  2. 列表空时是 [] 不是 null；
//  3. 金额精度**原样透传**（不做舍入，改精度会破前端金额计算）；
//  4. 会员单 authorId 哨兵值 "1" 保持不变（已知 P3，本轮不动）。
package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestOrderDtoNoNull 订单：未支付（paidAt=nil）不得输出 null。
func TestOrderDtoNoNull(t *testing.T) {
	pending := toOrderDto(&model.Order{
		BaseModel: model.BaseModel{ID: "o-1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		UserID:    "u-1",
		OrderNo:   "ORDER1764050000000123",
		Title:     "充值VIP会员 1个月",
		Amount:    19.9,
		Status:    model.OrderStatusPending,
	})
	assertNoNullJSON(t, "OrderDto(待支付)", pending)

	b, _ := json.Marshal(pending)
	if strings.Contains(string(b), "paidAt") {
		t.Fatalf("paidAt 为 nil 时不应出现该字段：%s", b)
	}
	// 中文业务数据必须原样保留（data 内容不翻译，AGENTS 响应契约第 8 条）
	if pending.Title != "充值VIP会员 1个月" {
		t.Fatalf("订单标题应保留中文原文，实际 %q", pending.Title)
	}

	now := time.Now()
	paid := toOrderDto(&model.Order{Status: model.OrderStatusPaid, PaidAt: &now})
	assertNoNullJSON(t, "OrderDto(已支付)", paid)
	if paid.PaidAt == nil {
		t.Fatal("已支付时 paidAt 应为非空")
	}
}

// TestOrderDtoAmountPrecision 金额精度必须原样透传（float64 不四舍五入）。
//
// 防什么 bug：若在 DTO 层"顺便"做 round2 或换算成分，
// 前端拿到的数值会与库内 decimal(10,2) 不一致，对账直接错。
func TestOrderDtoAmountPrecision(t *testing.T) {
	cases := []float64{19.9, 0.01, 12345.67, 0}
	for _, amount := range cases {
		d := toOrderDto(&model.Order{Amount: amount})
		if d.Amount != amount {
			t.Fatalf("金额应原样透传：want %v, got %v", amount, d.Amount)
		}
	}
}

// TestOrderDtoAuthorIDSentinelPreserved 会员单 authorId 哨兵值 "1" 保持原样。
//
// 这是已知 P3（order_service.go 的 CreateMembershipOrder 写死 "1"）：
// 本轮 DTO 改造**不改行为**，只让文档标注它。若这里挂了，说明有人顺手"修"了哨兵，
// 那属于另一个任务，必须单独评估前端影响。
func TestOrderDtoAuthorIDSentinelPreserved(t *testing.T) {
	d := toOrderDto(&model.Order{Type: model.OrderTypeMembership, AuthorID: "1"})
	if d.AuthorID != "1" {
		t.Fatalf("会员单 authorId 哨兵值应为 \"1\"，实际 %q", d.AuthorID)
	}
}

// TestOrderDtosEmptyIsArray 列表空时必须是 [] 而不是 null。
func TestOrderDtosEmptyIsArray(t *testing.T) {
	out := toOrderDtos(nil)
	if out == nil {
		t.Fatal("toOrderDtos(nil) 必须返回非 nil 切片（否则序列化成 null）")
	}
	b, _ := json.Marshal(out)
	if string(b) != "[]" {
		t.Fatalf("空列表应输出 []，实际 %s", b)
	}
	assertNoNullJSON(t, "空 OrderDto 列表", out)

	// 有元素时逐项转换正确
	rows := []model.Order{{OrderNo: "ORDER1"}, {OrderNo: "ORDER2"}}
	got := toOrderDtos(rows)
	if len(got) != 2 || got[0].OrderNo != "ORDER1" || got[1].OrderNo != "ORDER2" {
		t.Fatalf("批量转换结果不符：%+v", got)
	}
}

// TestOrderWalletDtoNoNull 钱包余额视图（两个字段都是值类型）。
func TestOrderWalletDtoNoNull(t *testing.T) {
	assertNoNullJSON(t, "OrderWalletDto(零值)", dto.OrderWalletDto{})
	assertNoNullJSON(t, "OrderWalletDto(典型值)", dto.OrderWalletDto{UserID: "u-1", Wallet: 100.5})
}

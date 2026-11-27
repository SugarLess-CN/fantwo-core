// 支付域 DTO 空值回归测试（docs/21）。
//
// 重点保三件事：
//  1. PaidAt 为 nil 时字段整个不传（不是 null）；
//  2. 列表空时是 [] 不是 null；详情查不到时返回 nil（data 省略，不是 null）；
//  3. 金额精度原样透传。
package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestPaymentDtoNoNull 支付记录：未支付（paidAt=nil）不得输出 null。
func TestPaymentDtoNoNull(t *testing.T) {
	pending := toPaymentDto(&model.Payment{
		BaseModel:     model.BaseModel{ID: "p-1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		OrderID:       "o-1",
		UserID:        "u-1",
		PaymentMethod: model.PayMethodAlipay,
		Amount:        19.9,
		Status:        model.PaymentStatusPending,
	})
	assertNoNullJSON(t, "PaymentDto(待支付)", pending)

	b, _ := json.Marshal(pending)
	if strings.Contains(string(b), "paid_at") {
		t.Fatalf("paidAt 为 nil 时不应出现该字段：%s", b)
	}

	now := time.Now()
	paid := toPaymentDto(&model.Payment{Status: model.PaymentStatusSuccess, PaidAt: &now})
	assertNoNullJSON(t, "PaymentDto(已支付)", paid)
	if paid.PaidAt == nil {
		t.Fatal("已支付时 paidAt 应为非空")
	}
}

// TestPaymentDtoNilReturnsNil 查不到记录时转换返回 nil（handler 交给 response.Success，
// data 为 nil 时整个 data 字段省略 —— 既有契约，不是 null）。
func TestPaymentDtoNilReturnsNil(t *testing.T) {
	if got := toPaymentDto(nil); got != nil {
		t.Fatalf("nil 入参应返回 nil，实际 %+v", got)
	}
}

// TestPaymentDtoAmountPrecision 金额原样透传，不做舍入/换算。
func TestPaymentDtoAmountPrecision(t *testing.T) {
	for _, amount := range []float64{19.9, 0.01, 1234.56, 0} {
		if got := toPaymentDto(&model.Payment{Amount: amount}).Amount; got != amount {
			t.Fatalf("金额应原样透传：want %v, got %v", amount, got)
		}
	}
}

// TestPaymentDtosEmptyIsArray 列表空时必须是 [] 而不是 null。
func TestPaymentDtosEmptyIsArray(t *testing.T) {
	out := toPaymentDtos(nil)
	if out == nil {
		t.Fatal("toPaymentDtos(nil) 必须返回非 nil 切片")
	}
	b, _ := json.Marshal(out)
	if string(b) != "[]" {
		t.Fatalf("空列表应输出 []，实际 %s", b)
	}
	assertNoNullJSON(t, "空 PaymentDto 列表", out)
}

// TestPaymentContractDtoNoNull 请求/响应契约结构（请求体含指针 map，出参不得有 null 隐患）。
func TestPaymentContractDtoNoNull(t *testing.T) {
	assertNoNullJSON(t, "CreatePaymentRequest(零值)", dto.CreatePaymentRequest{})

	// CreatePaymentResultDto 各渠道形态：全空 / 支付宝 / 微信 / 余额
	assertNoNullJSON(t, "CreatePaymentResultDto(空)", dto.CreatePaymentResultDto{})
	assertNoNullJSON(t, "CreatePaymentResultDto(ALIPAY)", dto.CreatePaymentResultDto{
		PaymentID: "p-1", PaymentMethod: model.PayMethodAlipay, PaymentURL: "https://openapi.alipay.com/x",
	})
	assertNoNullJSON(t, "CreatePaymentResultDto(WECHAT)", dto.CreatePaymentResultDto{
		PaymentID: "p-1", PaymentMethod: model.PayMethodWechat, CodeURL: "weixin://wxpay/bizpayurl?pr=x",
	})
	assertNoNullJSON(t, "CreatePaymentResultDto(BALANCE)", dto.CreatePaymentResultDto{
		PaymentID: "p-1", PaymentMethod: model.PayMethodBalance, Status: "SUCCESS", Balance: 80.6,
	})

	// EpaySignatureResultDto / EpaySignatureRequest
	assertNoNullJSON(t, "EpaySignatureResultDto(零值)", dto.EpaySignatureResultDto{})
	assertNoNullJSON(t, "EpaySignatureRequest(空 params)", dto.EpaySignatureRequest{})
}

// TestEpaySignatureKeepsSnakeCaseField sign_type 是既有对外契约字段名，本轮不改名。
//
// 防什么 bug：有人按 camelCase 口径「顺手」把 sign_type 改成 signType，
// 前端/对账脚本按旧字段名取值会拿到 undefined（breaking change，需要单独立项）。
func TestEpaySignatureKeepsSnakeCaseField(t *testing.T) {
	b, err := json.Marshal(dto.EpaySignatureResultDto{Sign: "abc", SignType: "MD5"})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(b), `"sign_type":"MD5"`) {
		t.Fatalf("sign_type 字段名必须保持 snake_case（既有契约），实际 %s", b)
	}
}

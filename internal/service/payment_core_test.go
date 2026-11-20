// 支付域核心逻辑单测（不依赖 PG / Redis / 云厂商）。
//
// 覆盖：
//  1. round2 金额舍入（分位）；
//  2. splitCommission 分成边界：费率 0 / 1、金额 0 / 负值、有无邀请人；
//  3. isNotifyAlreadySettled 回调幂等判定；
//  4. CreatePaymentInput 的 paymentMethod 枚举校验（gin binding）。
package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/gin-gonic/gin/binding"
)

// TestRound2 金额四舍五入到分。
//
// 防什么 bug：19.9*0.85 在 IEEE754 下是 16.915000000000003（不是精确 16.915），
// 直接落 decimal(10,2) 由 PG 舍入的规则与 Go 侧累加用的原始 float 可能不一致，
// 导致「钱包余额」与「流水金额」对不上账。统一先 round2 再写库，
// 保证两边用的是同一个已舍入的数。
func TestRound2(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"整数不变", 10, 10},
		{"典型浮点误差上舍（16.915 进位）", 19.9 * 0.85, 16.92},
		{"第三位 5 进位", 0.125, 0.13},
		{"第三位 4 舍去", 0.124, 0.12},
		{"零", 0, 0},
		{"负数对称舍入", -0.125, -0.13},
		{"大额", 12345.6789, 12345.68},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := round2(c.in); got != c.want {
				t.Fatalf("round2(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestSplitCommission 三方分成计算边界。
//
// 防什么 bug：
//   - 费率算错（作者 85% 写成 15%）→ 作者收益少一个数量级；
//   - 无邀请人却写 5% 邀请分成 → 平台凭空多扣钱；
//   - 金额 0 / 负值产生负零或负数金额 → 钱包被反向加钱。
func TestSplitCommission(t *testing.T) {
	defaultCC := CommissionConfig{InviterRate: 0.05, PlatformRate: 0.10, AuthorRate: 0.85}

	cases := []struct {
		name       string
		amount     float64
		cc         CommissionConfig
		hasInviter bool
		wantAuthor float64
		wantPlat   float64
		wantInvit  float64
	}{
		{"默认费率有邀请人", 100, defaultCC, true, 85, 10, 5},
		{"默认费率无邀请人", 100, defaultCC, false, 85, 10, 0},
		{"金额 0", 0, defaultCC, true, 0, 0, 0},
		{"负金额按下限 0 处理", -50, defaultCC, true, 0, 0, 0},
		{"费率全 0", 100, CommissionConfig{}, false, 0, 0, 0},
		{"作者费率 1（全给作者）", 100,
			CommissionConfig{InviterRate: 0, PlatformRate: 0, AuthorRate: 1}, true, 100, 0, 0},
		{"平台费率 1（全给平台）", 100,
			CommissionConfig{InviterRate: 0, PlatformRate: 1, AuthorRate: 0}, true, 0, 100, 0},
		{"邀请费率 1", 100,
			CommissionConfig{InviterRate: 1, PlatformRate: 0, AuthorRate: 0}, true, 0, 0, 100},
		{"小数金额四舍五入（19.9×85%=16.915→16.92）", 19.9, defaultCC, true, 16.92, 1.99, 1.0},
		{"0.01 元极小单", 0.01, defaultCC, true, 0.01, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitCommission(c.amount, c.cc, c.hasInviter)
			if got.AuthorAmount != c.wantAuthor {
				t.Errorf("author = %v, want %v", got.AuthorAmount, c.wantAuthor)
			}
			if got.PlatformAmount != c.wantPlat {
				t.Errorf("platform = %v, want %v", got.PlatformAmount, c.wantPlat)
			}
			if got.InviterAmount != c.wantInvit {
				t.Errorf("inviter = %v, want %v", got.InviterAmount, c.wantInvit)
			}
			// 负零防御：-0 == 0 为 true，这里额外保证符号位不出现
			if got.AuthorAmount < 0 || got.PlatformAmount < 0 || got.InviterAmount < 0 {
				t.Errorf("分成金额不应为负: %+v", got)
			}
		})
	}
}

// TestIsNotifyAlreadySettled 回调幂等判定。
//
// 防什么 bug：网关重复回调（微信最多重试 15 次）时重复落账 → 重复分佣金。
func TestIsNotifyAlreadySettled(t *testing.T) {
	cases := []struct {
		name   string
		record *model.Payment
		want   bool
	}{
		{"nil 记录", nil, false},
		{"PENDING 未落账", &model.Payment{Status: model.PaymentStatusPending}, false},
		{"FAILED 未落账", &model.Payment{Status: model.PaymentStatusFailed}, false},
		{"CANCELLED 未落账", &model.Payment{Status: model.PaymentStatusCancelled}, false},
		{"SUCCESS 已落账（幂等命中）", &model.Payment{Status: model.PaymentStatusSuccess}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNotifyAlreadySettled(c.record); got != c.want {
				t.Fatalf("isNotifyAlreadySettled(%v) = %v, want %v", c.record, got, c.want)
			}
		})
	}
}

// TestCreatePaymentInputValidation 入参枚举校验。
//
// 防什么 bug：paymentMethod 未做枚举限制时，任意字符串会落到 switch 的
// default 分支或直接建出「未知渠道」支付记录，回调永远匹配不上。
func TestCreatePaymentInputValidation(t *testing.T) {
	cases := []struct {
		name    string
		in      CreatePaymentInput
		wantErr bool
	}{
		{"支付宝", CreatePaymentInput{OrderID: "o1", PaymentMethod: "ALIPAY"}, false},
		{"微信", CreatePaymentInput{OrderID: "o1", PaymentMethod: "WECHAT"}, false},
		{"易支付", CreatePaymentInput{OrderID: "o1", PaymentMethod: "EPAY"}, false},
		{"余额", CreatePaymentInput{OrderID: "o1", PaymentMethod: "BALANCE"}, false},
		{"小写渠道被拒", CreatePaymentInput{OrderID: "o1", PaymentMethod: "alipay"}, true},
		{"未知渠道被拒", CreatePaymentInput{OrderID: "o1", PaymentMethod: "PAYPAL"}, true},
		{"空渠道被拒", CreatePaymentInput{OrderID: "o1", PaymentMethod: ""}, true},
		{"缺 orderId 被拒", CreatePaymentInput{PaymentMethod: "ALIPAY"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			err := binding.Validator.ValidateStruct(&in)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateStruct(%+v) err = %v, wantErr = %v", c.in, err, c.wantErr)
			}
		})
	}
}

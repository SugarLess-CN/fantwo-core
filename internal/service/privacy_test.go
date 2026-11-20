package service

import (
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// 隐私脱敏与状态判定的回归测试。
//
// 这类函数不依赖 DB，但直接影响**安全与合规**：
// 脱敏漏了会泄露手机号/邮箱，会员判定错了会让未付费用户获得权益。

// TestMaskPhone 手机号脱敏。
//
// 防：脱敏不足（暴露完整号码）、越界 panic（短号码）、脱敏后无法识别用户。
func TestMaskPhone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"标准11位", "13800138000", "138****8000"},
		{"8位", "12345678", "123****5678"},
		{"7位边界", "1234567", "123****4567"},
		{"6位返回原值（不足7位不脱敏）", "123456", "123456"},
		{"空串", "", ""},
		{"3位", "123", "123"},
		{"含国际区号", "+8613800138000", "+86****8000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := maskPhone(c.in)
			if got != c.want {
				t.Errorf("maskPhone(%q) = %q, want %q", c.in, got, c.want)
			}
			// 安全断言：7 位以上必须真的被遮蔽
			if len(c.in) >= 7 && c.in[3:len(c.in)-4] != "" && !strings.Contains(got, "****") {
				t.Errorf("maskPhone(%q) = %q 未包含遮蔽符，可能泄露完整号码", c.in, got)
			}
		})
	}
}

// TestMaskEmail 邮箱脱敏。
//
// 防：域名被错误遮蔽（用户无法确认是哪个邮箱）、@ 缺失时 panic。
func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"标准", "alice@example.com", "a****@example.com"},
		{"单字符本地部分", "a@example.com", "a****@example.com"},
		{"长本地部分只保留首字符", "verylongname@example.com", "v****@example.com"},
		{"无@返回原值", "not-an-email", "not-an-email"},
		{"@在开头", "@example.com", "@example.com"},
		{"空串", "", ""},
		{"多@取最后一个", "a@b@example.com", "a****@example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskEmail(c.in); got != c.want {
				t.Errorf("maskEmail(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestIsMembershipActive 会员有效性判定。
//
// 防：过期会员仍享权益（少收钱）、未设结束时间被当成永久会员（白送权益）。
func TestIsMembershipActive(t *testing.T) {
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name       string
		status     string
		endDate    *time.Time
		wantActive bool
	}{
		{"ACTIVE且未到期", model.MembershipStatusActive, &future, true},
		{"ACTIVE但已过期", model.MembershipStatusActive, &past, false},
		{"ACTIVE但无结束时间（不是永久会员）", model.MembershipStatusActive, nil, false},
		{"非ACTIVE状态但未到期", "EXPIRED", &future, false},
		{"非ACTIVE且已过期", "CANCELLED", &past, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := &model.User{MembershipStatus: c.status, MembershipEndDate: c.endDate}
			if got := isMembershipActive(u); got != c.wantActive {
				t.Errorf("isMembershipActive(status=%q, end=%v) = %v, want %v",
					c.status, c.endDate, got, c.wantActive)
			}
		})
	}
}

// TestNormPage 分页参数规范化（与 repository/pagination.go 独立的 handler 层兜底）。
//
// 防：page=0/负数导致 offset 为负（PG 报错）、limit 超大拖垮数据库。
func TestNormPage(t *testing.T) {
	cases := []struct {
		page, limit         int
		wantPage, wantLimit int
	}{
		{1, 10, 1, 10},
		{0, 0, 1, 10},   // 默认值
		{-5, -3, 1, 10}, // 负数归一到默认
		{2, 100, 2, 100},
		{1, 1000, 1, 100}, // 超上限截断为 100
	}
	for _, c := range cases {
		p, l := normPage(c.page, c.limit)
		if p != c.wantPage || l != c.wantLimit {
			t.Errorf("normPage(%d,%d) = (%d,%d), want (%d,%d)",
				c.page, c.limit, p, l, c.wantPage, c.wantLimit)
		}
	}
}

// TestCalculateLevelBoundaries 等级门槛边界（对齐原 level.service.ts 的 LEVEL_CONFIG）。
//
// 原配置：0:0 / 1:100 / 2:300 / 3:600 / 4:1000 / 5:1500 / 6:2100，上限 6 级。
// 防：边界取整错误导致「差 1 点经验升级」或「升级后等级回落」。
func TestCalculateLevelBoundaries(t *testing.T) {
	cases := []struct {
		exp  int
		want int
	}{
		{0, 0}, {99, 0},
		{100, 1}, {299, 1},
		{300, 2}, {599, 2},
		{600, 3}, {999, 3},
		{1000, 4}, {1499, 4},
		{1500, 5}, {2099, 5},
		{2100, 6}, {99999, 6}, // 上限 6，不继续增长
	}
	for _, c := range cases {
		if got := calculateLevel(c.exp); got != c.want {
			t.Errorf("calculateLevel(%d) = %d, want %d", c.exp, got, c.want)
		}
	}
}

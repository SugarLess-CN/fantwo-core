package service

import (
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestMaskPhone 手机号掩码：中间四位打码，长度不足原样返回。
//
// 防的 bug：掩码规则写错（如只留后 2 位）→ 前端展示不全；
// 或短号越界 panic（切片越界导致 profile 接口 500）。
func TestMaskPhoneEdgeCases(t *testing.T) {
	cases := map[string]string{
		"13800138000": "138****8000",
		"1234567":     "123****4567",
		"123456":      "123456", // 不足 7 位原样返回（不 panic）
		"":            "",
	}
	for in, want := range cases {
		if got := maskPhone(in); got != want {
			t.Errorf("maskPhone(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestMaskEmail 邮箱掩码：保留首字符 + 域名。
//
// 防的 bug：无 @ 时切片越界 panic；或把域名也打码导致前端无法展示。
func TestMaskEmailEdgeCases(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "a****@example.com",
		"a@example.com":     "a****@example.com", // 单字符用户名
		"no-at-sign":        "no-at-sign",        // 无 @ 原样返回
		"@leading.com":      "@leading.com",      // @ 在首位（at<=0）原样返回
		"":                  "",
	}
	for in, want := range cases {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestToMapCamelCaseKeys 用户视图转 map 时键名必须全部 camelCase（全局 JSON 口径）。
//
// 防的 bug：新增字段写成 snake_case 混进响应（2026-11-19 全局决策：对外一律 camelCase，
// DB 列名才用 snake_case），前端按 camelCase 取值为 undefined。
func TestToMapCamelCaseKeys(t *testing.T) {
	nick, avatar, desc := "小画家", "https://cdn/a.png", "简介"
	u := &model.User{
		Username: "alice", Nickname: &nick, Avatar: &avatar, Description: &desc,
		Status: model.UserStatusActive,
	}
	u.ID = "u-1"
	m := toMap(ViewUser(u, false))
	if len(m) == 0 {
		t.Fatal("视图 map 不应为空")
	}
	for k := range m {
		if strings.Contains(k, "_") {
			t.Errorf("响应键 %q 不是 camelCase（snake_case 会破坏前端契约）", k)
		}
	}
	for _, required := range []string{"id", "username", "nickname", "avatar", "isMember",
		"articleCount", "followerCount", "membershipLevelName"} {
		if _, ok := m[required]; !ok {
			t.Errorf("缺少必备字段 %q", required)
		}
	}
}

// TestToMapIncludesCertification 有认证信息时才输出 certification（无则不输出 null）。
//
// 防的 bug：无认证也输出 certification:null → 前端按非空判断显示认证标识。
func TestToMapIncludesCertification(t *testing.T) {
	u := &model.User{Username: "bob", Status: model.UserStatusActive}
	u.ID = "u-2"
	v := ViewUser(u, false)
	if _, ok := toMap(v)["certification"]; ok {
		t.Error("无认证时不应输出 certification")
	}
	v.Certification = map[string]interface{}{"id": "c-1", "type": model.CertTypeCreator}
	if _, ok := toMap(v)["certification"]; !ok {
		t.Error("有认证时应输出 certification")
	}
}

// TestViewUserMasksRoles 角色简述随视图输出，displayName 为 nil 时输出空串（不 panic）。
func TestViewUserMasksRoles(t *testing.T) {
	u := &model.User{Username: "carol", Status: model.UserStatusActive}
	u.ID = "u-3"
	u.Roles = []model.Role{
		{Name: "admin", DisplayName: nil},
		{Name: "user", DisplayName: strptr("普通用户")},
	}
	v := ViewUser(u, true)
	if len(v.Roles) != 2 {
		t.Fatalf("角色数量不符: %d", len(v.Roles))
	}
	if v.Roles[0].DisplayName != "" {
		t.Errorf("nil displayName 应回落空串，实际 %q", v.Roles[0].DisplayName)
	}
	if v.Roles[1].DisplayName != "普通用户" {
		t.Errorf("displayName 丢失: %q", v.Roles[1].DisplayName)
	}
}

// TestDayStart 签到日期归一：任意时刻都归到当天零点，跨天比较才可靠。
//
// 防的 bug：用 time.Now() 直接比较 → 同一天不同时刻被判定为两次签到（重复发积分）。
func TestDayStart(t *testing.T) {
	base := time.Date(2026, 11, 21, 15, 30, 45, 123456789, time.Local)
	got := dayStart(base)
	want := time.Date(2026, 11, 21, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("归一结果不符: want=%v got=%v", want, got)
	}
	// 同一时刻的两个不同时间点应归一到同一零点
	if !dayStart(base).Equal(dayStart(base.Add(-3 * time.Hour))) {
		t.Error("同日不同时刻应归一到同一零点")
	}
	if dayStart(base).Equal(dayStart(base.Add(24 * time.Hour))) {
		t.Error("跨天应归一到不同零点")
	}
}

// TestIsMembershipActive 会员有效性判定矩阵（status + 到期时间双条件）。
//
// 防的 bug：只看 status 不看 endDate → 过期会员永久享受权益。
func TestIsMembershipActiveMatrix(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	cases := []struct {
		name   string
		status string
		end    *time.Time
		want   bool
	}{
		{"有效会员", model.MembershipStatusActive, &future, true},
		{"已过期", model.MembershipStatusActive, &past, false},
		{"无到期时间", model.MembershipStatusActive, nil, false},
		{"状态非活跃", model.MembershipStatusInactive, &future, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &model.User{MembershipStatus: tc.status, MembershipEndDate: tc.end}
			if got := isMembershipActive(u); got != tc.want {
				t.Errorf("期望 %v，实际 %v", tc.want, got)
			}
			if got := IsMember(u); got != tc.want {
				t.Errorf("IsMember 应与 isMembershipActive 一致，实际 %v", got)
			}
		})
	}
}

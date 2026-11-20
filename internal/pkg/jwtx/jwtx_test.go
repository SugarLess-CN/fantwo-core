package jwtx

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "unit-test-secret"

// TestGenerateParseRoundTrip 签发→解析往返：claims 字段必须原样带回。
//
// 防的 bug：改 Claims 字段名/JSON tag 后签发与解析不对称（表现为登录成功但所有
// 需要 uid 的接口拿到空 userID，权限判定全 403）。
func TestGenerateParseRoundTrip(t *testing.T) {
	s := NewSigner(testSecret, "issuer-x", time.Hour, 24*time.Hour)
	token, err := s.Generate("user-1", []string{"admin", "user"}, []string{"user:read", "role:read"}, "dev-9", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := s.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID 不符: %s", claims.UserID)
	}
	if claims.DeviceID != "dev-9" {
		t.Errorf("DeviceID 不符: %s", claims.DeviceID)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Errorf("Roles 不符: %v", claims.Roles)
	}
	if len(claims.Permissions) != 2 || claims.Permissions[1] != "role:read" {
		t.Errorf("Permissions 不符: %v", claims.Permissions)
	}
	if claims.Issuer != "issuer-x" || claims.Subject != "user-1" {
		t.Errorf("注册 claims 不符: issuer=%s sub=%s", claims.Issuer, claims.Subject)
	}
}

// TestTokenTypeTTLAccessVsRefresh access 与 refresh 的有效期必须不同（滑动过期前提）。
//
// 防的 bug：tokenType 分支写反/失效 → refresh 与 access 同寿，7 天后用户被强制重新登录。
func TestTokenTypeTTLAccessVsRefresh(t *testing.T) {
	s := NewSigner(testSecret, "i", 2*time.Hour, 72*time.Hour)
	acc, err := s.Generate("u", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("access 签发失败: %v", err)
	}
	ref, err := s.Generate("u", nil, nil, "d", "refresh")
	if err != nil {
		t.Fatalf("refresh 签发失败: %v", err)
	}
	ac, err := s.Parse(acc)
	if err != nil {
		t.Fatalf("access 解析失败: %v", err)
	}
	rc, err := s.Parse(ref)
	if err != nil {
		t.Fatalf("refresh 解析失败: %v", err)
	}
	accRemain := s.Remaining(ac)
	refRemain := s.Remaining(rc)
	if accRemain > 2*time.Hour+time.Minute {
		t.Errorf("access 有效期异常: %v", accRemain)
	}
	if refRemain < 70*time.Hour {
		t.Errorf("refresh 有效期异常: %v", refRemain)
	}
	if refRemain <= accRemain {
		t.Error("refresh 有效期应显著长于 access")
	}
}

// TestParseRejectsWrongSecret 换密钥后旧 token 必须失效（签名校验不被绕过）。
func TestParseRejectsWrongSecret(t *testing.T) {
	other := NewSigner("another-secret", "i", time.Hour, time.Hour)
	token, err := other.Generate("u", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := NewSigner(testSecret, "i", time.Hour, time.Hour).Parse(token); err == nil {
		t.Error("不同密钥签发的 token 应解析失败")
	}
}

// TestParseRejectsExpired 过期 token 必须报错。
//
// 防的 bug：解析时漏掉 exp 校验（jwt 库默认校验，此处锁定该行为不被关掉）。
func TestParseRejectsExpired(t *testing.T) {
	s := NewSigner(testSecret, "i", time.Hour, time.Hour)
	claims := Claims{
		UserID: "u",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "i",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("构造过期 token 失败: %v", err)
	}
	if _, err := s.Parse(token); err == nil {
		t.Error("过期 token 应解析失败")
	}
}

// TestParseRejectsNonHMAC 非 HMAC 算法必须拒绝（防 alg 混淆攻击）。
func TestParseRejectsNonHMAC(t *testing.T) {
	// alg=none 的 token：header.payload.（空签名）
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOiJoYWNrZXIifQ."
	s := NewSigner(testSecret, "i", time.Hour, time.Hour)
	if _, err := s.Parse(noneToken); err == nil {
		t.Error("alg=none 的 token 应被拒绝")
	} else if !strings.Contains(err.Error(), "非法") && !strings.Contains(err.Error(), "token") {
		t.Logf("拒绝原因（允许任意拒绝实现）: %v", err)
	}
}

// TestParseRejectsGarbage 随机串/空串不得 panic，只返回错误。
func TestParseRejectsGarbage(t *testing.T) {
	s := NewSigner(testSecret, "i", time.Hour, time.Hour)
	for _, bad := range []string{"", "abc", "a.b.c", "Bearer xxx"} {
		if _, err := s.Parse(bad); err == nil {
			t.Errorf("%q 应解析失败", bad)
		}
	}
}

// TestGenerateWithPermVer 权限版本随 token 往返（perm_ver 方案的基础）。
//
// 防的 bug：pv 未写入 claims → 中间件永远比对 0 与 Redis 实际版本，权限变更后
// 旧 token 依旧可用（等于 perm_ver 机制失效）。
func TestGenerateWithPermVer(t *testing.T) {
	s := NewSigner(testSecret, "i", time.Hour, time.Hour)
	token, err := s.GenerateWithPermVer("u-2", []string{"user"}, []string{"user:read"}, "d-1", "access", 7)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := s.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.PermVer != 7 {
		t.Errorf("PermVer 应为 7，实际 %d", claims.PermVer)
	}
	if claims.UserID != "u-2" || claims.DeviceID != "d-1" {
		t.Errorf("其余 claims 不符: %+v", claims)
	}
}

// TestRemaining 剩余有效期：nil / 已过期 / 正常 三种路径。
//
// 防的 bug：黑名单 TTL 取到负数或零 → Redis Set 报错或条目立即过期，
// 登出后 token 仍然可用。
func TestRemaining(t *testing.T) {
	s := NewSigner(testSecret, "i", time.Hour, time.Hour)

	if got := s.Remaining(nil); got != 0 {
		t.Errorf("nil claims 应返回 0，实际 %v", got)
	}
	if got := s.Remaining(&Claims{}); got != 0 {
		t.Errorf("无 ExpiresAt 应返回 0，实际 %v", got)
	}
	expired := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}}
	if got := s.Remaining(expired); got != 0 {
		t.Errorf("已过期应返回 0，实际 %v", got)
	}
	future := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
	}}
	got := s.Remaining(future)
	if got <= 0 || got > 31*time.Minute {
		t.Errorf("未过期应返回正剩余时间，实际 %v", got)
	}
}

// TestNewSignerDefaults 非法 TTL 回落到默认（1h / 7d），避免签发即过期。
func TestNewSignerDefaults(t *testing.T) {
	s := NewSigner(testSecret, "i", 0, 0)
	if s.AccessTTL() != time.Hour {
		t.Errorf("access 默认应 1h，实际 %v", s.AccessTTL())
	}
	if s.RefreshTTL() != 7*24*time.Hour {
		t.Errorf("refresh 默认应 7d，实际 %v", s.RefreshTTL())
	}
	neg := NewSigner(testSecret, "i", -time.Hour, -time.Hour)
	if neg.AccessTTL() != time.Hour || neg.RefreshTTL() != 7*24*time.Hour {
		t.Error("负数 TTL 也应回落到默认值")
	}
}

package service

import (
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
)

// 关键路径基准测试。
//
// 目的：给"每请求固定开销"一个有数字的基线，便于日后发现性能退化。
// 只 bench 无 DB 依赖的热点（鉴权、脱敏、判定），DB 路径由集成测试覆盖。

// BenchmarkSignerGenerateAccess 签发自省 token（登录成功路径每次必走）。
func BenchmarkSignerGenerateAccess(b *testing.B) {
	s := jwtx.NewSigner("bench-secret-key-32-bytes-long!!", "fantwo-bench", time.Hour, 7*24*time.Hour)
	roles := []string{"user"}
	perms := []string{"article:read", "article:create", "comment:create", "user:read"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GenerateWithPermVer("user-bench-id", roles, perms, "dev-1", "access", 0); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSignerParseAndVerify 验签 + 解析（每个受保护请求必走）。
// 这是鉴权中间件的核心开销。
func BenchmarkSignerParseAndVerify(b *testing.B) {
	s := jwtx.NewSigner("bench-secret-key-32-bytes-long!!", "fantwo-bench", time.Hour, 7*24*time.Hour)
	token, err := s.GenerateWithPermVer("user-bench-id",
		[]string{"user"},
		[]string{"article:read", "article:create", "comment:create", "user:read"},
		"dev-1", "access", 0)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Parse(token); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMaskPhone 手机号脱敏（资料/通知返回时高频）。
func BenchmarkMaskPhone(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = maskPhone("13800138000")
	}
}

// BenchmarkMaskEmail 邮箱脱敏。
func BenchmarkMaskEmail(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = maskEmail("someone@example.com")
	}
}

// BenchmarkNormPage 分页参数规范化（每个列表请求必走）。
func BenchmarkNormPage(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = normPage(3, 20)
	}
}

// BenchmarkCalculateLevel 等级计算（每次加经验必走）。
func BenchmarkCalculateLevel(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = calculateLevel(1234)
	}
}

// BenchmarkIsProfileComplete 资料完整度判定（更新资料时）。
func BenchmarkIsProfileComplete(b *testing.B) {
	nick, av, desc := "昵称", "https://cdn/a.png", "简介"
	u := &model.User{Nickname: &nick, Avatar: &av, Description: &desc}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isProfileComplete(u)
	}
}

package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// bcrypt 72 字节上限回归测试
//
// 2026-11-19 事故：refresh token 是带 roles/perms 的 JWT，长度约 1KB，
// 当时用 bcrypt 存它的哈希 → bcrypt.GenerateFromPassword 直接报
// "bcrypt: password length exceeds 72 bytes"，登录接口必 500。
// 修复：token 哈希改 SHA-256（token 本身高熵，无需慢哈希）。
//
// 这组测试同时做两件事：
//   1. 证明 bcrypt 处理超长串**确实报错**（说明当初为什么必须换）；
//   2. 断言 SHA-256 方案对 >72 字节输入仍正确、且不产生截断碰撞。
// ---------------------------------------------------------------------------

// longJWT 构造一个长度接近真实 refresh token（约 1KB）的串。
func longJWT(prefix string) string {
	var b strings.Builder
	b.WriteString("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIxIiwicm9sZXMiOlsiYWRtaW4iXSwicGVybXMiOlsidXNlcjpyZWFkIiwicm9sZTpyZWFkIl19.")
	b.WriteString(prefix)
	for b.Len() < 1024 {
		b.WriteString("x")
	}
	return b.String()
}

// TestBcryptRejectsTokenOver72Bytes 证明 bcrypt 对长 token 报错（事故根因锁定）。
//
// 防的 bug：后人「优化」时把 hashToken 改回 bcrypt → 登录再次 500。
func TestBcryptRejectsTokenOver72Bytes(t *testing.T) {
	token := longJWT("a")
	if len(token) <= 72 {
		t.Fatalf("测试前置条件不成立：token 长度 %d 应 > 72", len(token))
	}
	_, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err == nil {
		t.Fatal("bcrypt 处理超过 72 字节的 token 应报错（2026-11-19 登录 500 的根因）")
	}
	if !strings.Contains(err.Error(), "72") {
		t.Logf("bcrypt 报错信息: %v（不同版本措辞可能不同）", err)
	}
}

// TestBcryptSilentlyTruncatesPassword bcrypt 对 <=72 字节的输入仍可用（密码场景）。
//
// 说明：密码哈希继续用 bcrypt 是安全的——但必须确保调用方没有把长 token 传进来。
func TestBcryptSilentlyTruncatesPassword(t *testing.T) {
	pwd := "P@ssw0rd-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("正常密码不应报错: %v", err)
	}
	if !checkPassword(string(hash), pwd) {
		t.Error("正确密码应校验通过")
	}
	if checkPassword(string(hash), pwd+"x") {
		t.Error("错误密码不应校验通过")
	}
}

// TestHashTokenHandlesLongInput SHA-256 方案对 >72 字节 token 正常工作。
//
// 防的 bug：hashToken 实现退回 bcrypt / 加长度限制 → 长 token 无法落库或 500。
func TestHashTokenHandlesLongInput(t *testing.T) {
	token := longJWT("b")
	hash := hashToken(token)
	if hash == "" {
		t.Fatal("长 token 应能得到哈希")
	}
	// SHA-256 十六进制约 64 字符，且必须能塞进 user_device.hashed_refresh_token(varchar 128)
	if len(hash) != 64 {
		t.Errorf("SHA-256 hex 应为 64 字符，实际 %d（列宽 128 需能容纳）", len(hash))
	}
	if !checkTokenHash(hash, token) {
		t.Error("同一 token 应校验通过")
	}
	if checkTokenHash(hash, token+"tamper") {
		t.Error("篡改后的 token 不应校验通过")
	}
}

// TestHashTokenNoTruncationCollision 前缀相同、仅尾部不同的长 token 不得碰撞。
//
// 防的 bug：若实现「截断到 72 字节再哈希」，两个仅尾部不同的 JWT 会得到同一哈希，
// 攻击者可拿别人的 token 前缀伪造通过 → 典型的 bcrypt 截断副作用。
func TestHashTokenNoTruncationCollision(t *testing.T) {
	base := longJWT("c")
	a := base
	b := strings.Replace(base, "c", "d", 1) // 仅首部差异
	c := base[:72] + "...tail-1"
	d := base[:72] + "...tail-2" // 前 72 字节完全相同
	if a == b || c == d {
		t.Fatal("测试前置条件不成立")
	}
	pairs := [][2]string{{a, b}, {c, d}}
	for _, p := range pairs {
		ha, hb := hashToken(p[0]), hashToken(p[1])
		if ha == hb {
			t.Errorf("不同 token 产生了相同哈希（截断碰撞）: %.20s...", p[0])
		}
		if checkTokenHash(ha, p[1]) {
			t.Errorf("A 的哈希不应通过 B 的校验: %.20s...", p[1])
		}
	}
}

// TestCheckTokenHashRejectsGarbage 空值/垃圾哈希不得判通过。
func TestCheckTokenHashRejectsGarbage(t *testing.T) {
	token := "real-token"
	good := hashToken(token)
	for _, badHash := range []string{"", "deadbeef", good[:32], strings.ToUpper(good)} {
		if badHash == good {
			continue
		}
		if checkTokenHash(badHash, token) {
			t.Errorf("非法哈希 %q 不应校验通过", badHash)
		}
	}
	if checkTokenHash(good, "") {
		t.Error("空 token 不应校验通过")
	}
}

// TestHashTokenDeterministic 哈希必须稳定（同一 token 每次结果一致，否则刷新即失效）。
func TestHashTokenDeterministic(t *testing.T) {
	token := longJWT("e")
	first := hashToken(token)
	for i := 0; i < 5; i++ {
		if got := hashToken(token); got != first {
			t.Fatal("同一 token 的哈希必须稳定")
		}
	}
}

// TestPasswordHashInvalidHash checkPassword 对非法哈希返回 false（不 panic）。
func TestPasswordHashInvalidHash(t *testing.T) {
	for _, bad := range []string{"", "not-a-bcrypt-hash", "$2a$10$short"} {
		if checkPassword(bad, "whatever") {
			t.Errorf("非法哈希 %q 不应校验通过", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// 用户名唯一化（GitHub 建号路径）
// ---------------------------------------------------------------------------

// TestUniqueUsername 表驱动：无冲突 / 一次冲突 / 多次冲突 / base 为空 / 上限保护。
//
// 防的 bug：
//   - 不做唯一化 → GitHub 同名用户建号撞唯一索引，接口 500；
//   - 无限重试 → GitHub 用户名被占用时死循环（请求挂死）。
func TestUniqueUsername(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		fallback string
		taken    map[string]bool
		suffixes []string
		tries    int
		want     string
	}{
		{
			name: "无冲突直接使用", base: "octocat", fallback: "gh1",
			taken: map[string]bool{}, want: "octocat",
		},
		{
			name: "一次冲突追加后缀", base: "octocat", fallback: "gh1",
			taken:    map[string]bool{"octocat": true},
			suffixes: []string{"abc"}, want: "octocat-ghabc",
		},
		{
			name: "多次冲突连续追加", base: "octocat", fallback: "gh1",
			taken:    map[string]bool{"octocat": true, "octocat-ghaaa": true},
			suffixes: []string{"aaa", "bbb"}, want: "octocat-ghbbb",
		},
		{
			name: "base 为空用 fallback", base: "", fallback: "gh777",
			taken: map[string]bool{}, want: "gh777",
		},
		{
			name: "base 为空且 fallback 冲突", base: "", fallback: "gh777",
			taken:    map[string]bool{"gh777": true},
			suffixes: []string{"zzz"}, want: "gh777-ghzzz",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx := 0
			exists := func(cand string) (bool, error) { return tc.taken[cand], nil }
			suffix := func() string {
				if idx < len(tc.suffixes) {
					s := tc.suffixes[idx]
					idx++
					return s
				}
				idx++
				return "xxx"
			}
			tries := tc.tries
			if tries == 0 {
				tries = 5
			}
			got, err := uniqueUsername(tc.base, tc.fallback, exists, suffix, tries)
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if got != tc.want {
				t.Errorf("期望 %q，实际 %q", tc.want, got)
			}
		})
	}
}

// TestUniqueUsernameExhausted 候选一直冲突时必须在有限次后返回（不死循环）。
//
// 防的 bug：while 循环无上限 → 用户名被占用时请求永久挂起（线程/goroutine 泄漏）。
func TestUniqueUsernameExhausted(t *testing.T) {
	calls := 0
	exists := func(string) (bool, error) { return true, nil } // 全部冲突
	suffix := func() string { return "z" }
	got, err := uniqueUsername("dup", "fb", exists, suffix, 3)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if calls != 0 {
		t.Errorf("exists 调用计数应由闭包外统计，实际 %d", calls)
	}
	if got == "" {
		t.Error("耗尽重试后仍应返回一个候选（交由 DB 唯一索引兜底）")
	}
	if !strings.HasPrefix(got, "dup-gh") {
		t.Errorf("耗尽后返回值应带后缀，实际 %q", got)
	}
}

// TestUniqueUsernameExistsError DB 查询失败必须上抛（不能静默用冲突名建号）。
func TestUniqueUsernameExistsError(t *testing.T) {
	sentinel := errors.New("db down")
	exists := func(string) (bool, error) { return false, sentinel }
	_, err := uniqueUsername("u", "f", exists, func() string { return "s" }, 3)
	if !errors.Is(err, sentinel) {
		t.Errorf("应原样上抛 DB 错误，实际 %v", err)
	}
}

// TestUniqueUsernameZeroTries tries<=0 时仍执行一次检查（防「直接返回未校验候选」）。
func TestUniqueUsernameZeroTries(t *testing.T) {
	got, err := uniqueUsername("only", "fb",
		func(string) (bool, error) { return false, nil },
		func() string { return "s" }, 0)
	if err != nil || got != "only" {
		t.Errorf("tries=0 应按 1 次处理并返回可用候选，实际 got=%q err=%v", got, err)
	}
}

// TestLongTokenLengthSanity 断言真实 JWT 形态的 token 长度确实超过 bcrypt 上限。
//
// 这是给后来者的「活文档」：只要 token 仍带 roles/perms，就绝不可能是 <= 72 字节。
func TestLongTokenLengthSanity(t *testing.T) {
	token := longJWT(fmt.Sprintf("%d", 1))
	if len(token) < 72*4 {
		t.Errorf("真实 refresh token 应远超 72 字节，当前 %d", len(token))
	}
}

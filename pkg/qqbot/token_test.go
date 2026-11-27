package qqbot

// token.go 的单测：四条必须做对的点 + 并发只刷一次。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenNormalFetch(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	tok, err := c.tokens.get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if tok != "TOK-1" {
		t.Errorf("token = %q, want TOK-1", tok)
	}
	if _, send := m.counts(); send != 0 {
		t.Errorf("取 token 不该触发发送请求，实际 %d 次", send)
	}

	// 请求体字段名是驼峰 appId / clientSecret（官方协议，不是 snake_case）
	m.mu.Lock()
	req := m.lastTokenReq
	m.mu.Unlock()
	if req["appId"] != "11111111" {
		t.Errorf("appId = %v", req["appId"])
	}
	if req["clientSecret"] != "DG5g3B4j9X2KOErG" {
		t.Errorf("clientSecret = %v", req["clientSecret"])
	}
}

// expires_in 是字符串 "7200" —— 解析成 7200 秒并扣掉 60s 安全边界。
func TestTokenExpiresInIsString(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"T","expires_in":"7200"}`, // 官方形态（字符串）
		`{"access_token":"T","expires_in":7200}`,   // 数字形态也要能吃
	} {
		m := newMockUpstream(t)
		m.setToken(t, body, 0)
		c := m.newTestClient(t)

		if _, err := c.tokens.get(context.Background()); err != nil {
			t.Fatalf("body=%s → %v", body, err)
		}
		c.tokens.mu.Lock()
		got := c.tokens.expireAt.Sub(time.Now())
		c.tokens.mu.Unlock()

		// 期望 ≈ 7200 - 60 = 7140 秒（留 5s 容差）
		if got < 7135*time.Second || got > 7145*time.Second {
			t.Errorf("body=%s → 缓存有效期 = %v, want ≈7140s（7200-60s 边界）", body, got)
		}
	}
}

// 业务错误走响应体 code，HTTP 仍是 200 —— 必须靠 code 判失败。
func TestTokenBusinessErrorWithHTTP200(t *testing.T) {
	cases := []struct {
		name string
		body string
		kind ErrorKind
		code int
	}{
		{"appid 非法", `{"code":100007,"message":"appid invalid"}`, KindConfig, 100007},
		{"secret 错", `{"code":100016,"message":"invalid appid or secret"}`, KindConfig, 100016},
		{"机器人不存在", `{"code":10004,"message":"robot not found"}`, KindConfig, 10004},
		{"限频", `{"code":100001,"message":"Too many requests"}`, KindRetryable, 100001},
		{"err_code 形态", `{"err_code":100016,"msg":"bad secret"}`, KindConfig, 100016},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockUpstream(t)
			m.setToken(t, tc.body, 0) // HTTP 200
			c := m.newTestClient(t)

			_, err := c.tokens.get(context.Background())
			if err == nil {
				t.Fatal("HTTP 200 但业务码非 0 必须报错")
			}
			var qe *Error
			if !errors.As(err, &qe) {
				t.Fatalf("err 应为 *Error，实际 %T", err)
			}
			if qe.Code != tc.code {
				t.Errorf("Code = %d, want %d", qe.Code, tc.code)
			}
			if qe.Kind != tc.kind {
				t.Errorf("Kind = %v, want %v", qe.Kind, tc.kind)
			}
			if qe.Op != opGetToken {
				t.Errorf("Op = %q, want %q", qe.Op, opGetToken)
			}
		})
	}
}

// 并发 10 goroutine 只允许刷一次 token。
func TestTokenConcurrentRefreshOnce(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	const n = 10
	var wg sync.WaitGroup
	var okCount int64
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tok, err := c.tokens.get(context.Background())
			if err == nil && tok == "TOK-1" {
				atomic.AddInt64(&okCount, 1)
			}
		}()
	}
	wg.Wait()

	tokenCalls, _ := m.counts()
	if tokenCalls != 1 {
		t.Errorf("上游被调 %d 次，want 1（并发只刷一次）", tokenCalls)
	}
	if okCount != n {
		t.Errorf("成功拿到 token 的 goroutine = %d, want %d", okCount, n)
	}
}

// 缓存未过期不重复请求。
func TestTokenCacheHitNoRefetch(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	for i := 0; i < 5; i++ {
		if _, err := c.tokens.get(context.Background()); err != nil {
			t.Fatalf("get #%d: %v", i, err)
		}
	}
	if tokenCalls, _ := m.counts(); tokenCalls != 1 {
		t.Errorf("缓存有效期内请求 %d 次，want 1", tokenCalls)
	}
}

// 到期后必须重新取（用注入的 now 控制时间，不真的等 2 小时）。
func TestTokenRefreshAfterExpiry(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	// 固定时钟
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	var offset atomic.Int64
	c.tokens.now = func() time.Time { return base.Add(time.Duration(offset.Load())) }

	if _, err := c.tokens.get(context.Background()); err != nil {
		t.Fatalf("first get: %v", err)
	}
	// 7140s 边界内 → 仍命中缓存
	offset.Store(int64(7130 * time.Second))
	if _, err := c.tokens.get(context.Background()); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if tokenCalls, _ := m.counts(); tokenCalls != 1 {
		t.Errorf("未过 60s 边界就刷新了：%d 次", tokenCalls)
	}
	// 越过边界 → 必须刷新
	offset.Store(int64(7141 * time.Second))
	if _, err := c.tokens.get(context.Background()); err != nil {
		t.Fatalf("third get: %v", err)
	}
	if tokenCalls, _ := m.counts(); tokenCalls != 2 {
		t.Errorf("越过边界后请求 %d 次，want 2", tokenCalls)
	}
}

// 上游没给 expires_in（或非正数）必须失败，不能猜一个默认值把过期 token 留在内存。
func TestTokenMissingExpiresInFails(t *testing.T) {
	// 缺 / 0 / 负数 → ErrAccessToken（协议异常，配置类）
	for _, body := range []string{
		`{"access_token":"T"}`,
		`{"access_token":"T","expires_in":"0"}`,
		`{"access_token":"T","expires_in":-1}`,
	} {
		m := newMockUpstream(t)
		m.setToken(t, body, 0)
		c := m.newTestClient(t)
		_, err := c.tokens.get(context.Background())
		if !errors.Is(err, ErrAccessToken) {
			t.Errorf("body=%s → err = %v, want ErrAccessToken", body, err)
		}
	}

	// 非整数字符串 → 解析层直接报错（不静默变 0，否则会当成「无有效期」继续走）
	m := newMockUpstream(t)
	m.setToken(t, `{"access_token":"T","expires_in":"abc"}`, 0)
	c := m.newTestClient(t)
	_, err := c.tokens.get(context.Background())
	if err == nil {
		t.Fatal("expires_in 非整数必须报错，不能静默变 0")
	}
	var qe *Error
	if !errors.As(err, &qe) {
		t.Errorf("err 应为 *Error，实际 %T: %v", err, err)
	}
	if qe.Kind != KindUnknown {
		t.Errorf("Kind = %v, want KindUnknown（解析失败，按可重试处理）", qe.Kind)
	}
}

// 401 → 丢缓存（下一次重新取），并给可重试错误。
func TestTokenInvalidatedOn401(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	if _, err := c.tokens.get(context.Background()); err != nil {
		t.Fatalf("warm up: %v", err)
	}
	m.setSend(t, `{"code":11244,"message":"token invalid"}`, 401)

	_, err := c.SendC2C("USER1", SendRequest{MsgType: MsgTypeText, Content: "hi"})
	if err == nil {
		t.Fatal("401 应当报错")
	}
	var qe *Error
	if !errors.As(err, &qe) {
		t.Fatalf("err 应为 *Error，实际 %T", err)
	}
	if qe.Kind != KindConfig {
		// 11244 不在分类表里 → KindUnknown（Retryable），断言「可重试」这一实质
		if !qe.Kind.Retryable() {
			t.Errorf("401 应可重试，实际 Kind=%v", qe.Kind)
		}
	}

	// 缓存已丢：下一次请求应重新取 token
	m.setSend(t, `{"id":"OK","timestamp":"t"}`, 0)
	before, _ := m.counts()
	if _, err := c.SendC2C("USER1", SendRequest{MsgType: MsgTypeText, Content: "hi"}); err != nil {
		t.Fatalf("retry after 401: %v", err)
	}
	after, _ := m.counts()
	if after != before+1 {
		t.Errorf("401 后未重新取 token（token 请求 %d → %d）", before, after)
	}
}

// tokenExpiry 边界：极短有效期不能被 60s 边界压成负数。
func TestTokenExpiryShortLived(t *testing.T) {
	if got := tokenExpiry(7200); got != 7140*time.Second {
		t.Errorf("tokenExpiry(7200) = %v, want 7140s", got)
	}
	if got := tokenExpiry(40); got != 20*time.Second {
		t.Errorf("tokenExpiry(40) = %v, want 20s（退化为一半，不能为负）", got)
	}
	if got := tokenExpiry(0); got != 0 {
		t.Errorf("tokenExpiry(0) = %v, want 0", got)
	}
}

// Ping 不吃缓存（缓存命中的 200 会把失效凭据一直报成健康）。
func TestPingForcesFetch(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	if err := c.Ping(); err != nil {
		t.Fatalf("Ping #1: %v", err)
	}
	if err := c.Ping(); err != nil {
		t.Fatalf("Ping #2: %v", err)
	}
	if tokenCalls, _ := m.counts(); tokenCalls != 2 {
		t.Errorf("Ping 两次应各取一次 token，实际 %d 次", tokenCalls)
	}
}

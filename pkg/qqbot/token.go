package qqbot

// access_token 管理。
//
// # 官方语义（docs/29 §二，四条都必须做对）
//
//	POST https://api.bot.qq.com/app/getAppAccessToken
//	{"appId":"...","clientSecret":"..."}
//	→ {"access_token":"...","expires_in":"7200"}
//
//  1. **expires_in 是字符串 "7200"**，不是数字 → 用 flexInt64 兼容，别用 int 直接接
//  2. **业务错误走响应体 code，HTTP 仍是 200** → 不能只看 HTTP 状态码判断成败
//  3. **并发安全**：多人同时发消息只能刷一次（互斥 + 双检，见 get）
//  4. **提前 60 秒刷新**：官方语义是「临近过期 60 秒内才返回新值，旧值这 60 秒仍有效」
//
// 缓存策略：内存存 token + expireAt，expireAt = now + expires_in - 60s，到期才刷。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// tokenRefreshMargin 提前刷新的安全边界（官方 60 秒语义）。
	tokenRefreshMargin = 60 * time.Second
	// defaultHTTPTimeout 未配置 HTTPTimeout 时的单次请求超时。
	defaultHTTPTimeout = 10 * time.Second
	// maxResponseBytes 响应体上限 1 MiB，防异常响应打爆内存。
	maxResponseBytes = 1 << 20
	// opGetToken Ping / 取 token 失败时的 Op 标签。
	opGetToken = "get_token"
)

// flexInt64 兼容 JSON 里「字符串」与「数字」两种整型形态。
//
// 官方 `expires_in` 是字符串 `"7200"`（docs/29 §二明确写「不是数字」），
// 而错误码 `code` 是数字 —— 同一类结构里混着两种形态，必须兼容。
//
// **解析失败返回 error，不静默变 0**（两条后果都是误判）：
//   - expires_in 静默变 0 → 缓存立即失效 → 每条消息都去刷 token，白撞限频；
//   - code 静默变 0 → 把「平台报错」当成成功。
type flexInt64 int64

// UnmarshalJSON 字符串 / 数字 / null / 浮点 / 科学计数都能解。
func (v *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*v = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*v = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*v = flexInt64(n)
		return nil
	}
	// 兜底浮点 / 科学计数（"7200.0"、"7.2e3"）
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f > math.MaxInt64 || f < math.MinInt64 {
		return fmt.Errorf("qqbot: integer field %q is not a valid integer", s)
	}
	*v = flexInt64(int64(f))
	return nil
}

// tokenResponse getAppAccessToken 响应。
//
// 成功与失败共用同一结构：失败时 code/message 有值、access_token 为空，
// 且 **HTTP 状态码仍是 200**（docs/29 §二「错误判定」）。
type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresIn   flexInt64 `json:"expires_in"`
	// Code 业务错误码；官方两处命名（code / err_code），两个都读。
	Code    flexInt64 `json:"code"`
	ErrCode flexInt64 `json:"err_code"`
	Message string    `json:"message"`
	Msg     string    `json:"msg"`
}

// codeOrErr 返回实际业务错误码（0 表示无错）。
func (r *tokenResponse) codeOrErr() int {
	if c := int(r.Code); c != 0 {
		return c
	}
	return int(r.ErrCode)
}

// messageText 错误信息（官方两处命名）。
func (r *tokenResponse) messageText() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}

// tokenSource access_token 缓存与刷新。
//
// 互斥锁在整个刷新过程中持有（含 HTTP 往返），配合「进锁后双检缓存」，
// 让并发调用只产生**一次**上游请求：第一个 goroutine 刷新，其余进锁时已看到新值。
// 没有引入 singleflight —— 标准库一个 Mutex 就够，且不会因为依赖包的语义差异出偏差。
type tokenSource struct {
	httpClient *http.Client
	tokenURL   string
	appID      string
	appSecret  string
	// now 可注入，便于单测控制时间（默认 time.Now）。
	now func() time.Time

	mu       sync.Mutex
	token    string
	expireAt time.Time
}

// get 返回可用 token；缓存有效直接返回，过期（含 60s 安全边界）才刷新。
func (s *tokenSource) get(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && s.now().Before(s.expireAt) {
		return s.token, nil
	}
	if err := s.fetchLocked(ctx); err != nil {
		return "", err
	}
	return s.token, nil
}

// force 强制刷新一次（Ping 用：验证凭据可用，不吃缓存）。
func (s *tokenSource) force(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fetchLocked(ctx); err != nil {
		return "", err
	}
	return s.token, nil
}

// invalidate 丢弃缓存（上游返回 401 时调用，让下一次重新取）。
func (s *tokenSource) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = ""
	s.expireAt = time.Time{}
}

// fetchLocked 真正发一次请求。**调用方必须已持有 s.mu**。
func (s *tokenSource) fetchLocked(ctx context.Context) error {
	if s.appID == "" || s.appSecret == "" {
		return &Error{Kind: KindConfig, Op: opGetToken, Err: ErrNotConfigured}
	}

	payload, err := json.Marshal(map[string]string{
		// 官方字段名是驼峰 appId / clientSecret（不是 snake_case），照抄协议。
		"appId":        s.appID,
		"clientSecret": s.appSecret,
	})
	if err != nil {
		return wrapErr(opGetToken, KindUnknown, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(string(payload)))
	if err != nil {
		return wrapErr(opGetToken, KindUnknown, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return wrapErr(opGetToken, KindRetryable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return wrapErr(opGetToken, KindRetryable, err)
	}

	var tr tokenResponse
	decoded := json.Unmarshal(body, &tr) == nil

	// 业务错误优先：**HTTP 200 也可能是失败**（docs/29 §二）。
	if decoded {
		if code := tr.codeOrErr(); code != 0 {
			return newErr(opGetToken, code, tr.messageText())
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// 非 2xx 且无业务码：上游/网关层异常，可重试。
		// 错误信息只带状态码，不带响应体（响应体可能含 token，见 docs/14 §7.1.2 禁令 2 同口径）。
		return wrapErr(opGetToken, KindRetryable,
			fmt.Errorf("qqbot: token endpoint http %d", resp.StatusCode))
	}
	if !decoded {
		return wrapErr(opGetToken, KindUnknown, fmt.Errorf("qqbot: token endpoint response is not json"))
	}
	if tr.AccessToken == "" {
		return &Error{Kind: KindConfig, Op: opGetToken, Err: ErrAccessToken}
	}
	if int64(tr.ExpiresIn) <= 0 {
		// 没给有效期就无法安全缓存：宁可直接失败（配置/协议异常），
		// 也不要猜一个默认值把过期 token 长期留在内存里。
		return &Error{Kind: KindConfig, Op: opGetToken,
			Err: fmt.Errorf("%w: expires_in missing or non-positive", ErrAccessToken)}
	}

	s.token = tr.AccessToken
	s.expireAt = s.now().Add(tokenExpiry(tr.ExpiresIn))
	return nil
}

// tokenExpiry 由 expires_in 推导缓存有效期（已扣掉 60s 安全边界）。
//
// expires_in 极小（<= 60s，理论上不会出现）时安全边界会把它压成负数，
// 导致「每条消息都刷 token」—— 这种情形退化为取一半有效期，保证仍能缓存。
func tokenExpiry(expiresIn flexInt64) time.Duration {
	d := time.Duration(expiresIn) * time.Second
	if d <= tokenRefreshMargin {
		return d / 2
	}
	return d - tokenRefreshMargin
}

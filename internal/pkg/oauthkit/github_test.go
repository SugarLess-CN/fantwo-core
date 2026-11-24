package oauthkit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 测试脚手架：可替换 token 端点 + API base 的 mock GitHub
// ---------------------------------------------------------------------------

// githubMock 同时扮演 GitHub 的 token 端点与 REST API。
//
// 计数（hits）是缺陷 1/2 回归测试的关键证据：
//   - 「email 有值时不调 /user/emails」→ hits[emails] == 0
//   - 「scope 明确不含 user:email 时不去打必然 403 的端点」→ hits[emails] == 0
type githubMock struct {
	srv *httptest.Server

	mu    sync.Mutex
	hits  map[string]int
	forms map[string]string
	meth  map[string]string
	heads map[string]http.Header

	tokenStatus  int
	tokenBody    string
	userStatus   int
	userBody     string
	emailsStatus int
	emailsBody   string
}

func newGitHubMock(t *testing.T) *githubMock {
	t.Helper()
	m := &githubMock{
		hits:         map[string]int{},
		forms:        map[string]string{},
		meth:         map[string]string{},
		heads:        map[string]http.Header{},
		tokenStatus:  http.StatusOK,
		userStatus:   http.StatusOK,
		emailsStatus: http.StatusOK,
	}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))

		m.mu.Lock()
		m.hits[r.URL.Path]++
		m.forms[r.URL.Path] = string(body)
		m.meth[r.URL.Path] = r.Method
		m.heads[r.URL.Path] = r.Header.Clone()
		var status int
		var respBody string
		switch r.URL.Path {
		case "/login/oauth/access_token":
			status, respBody = m.tokenStatus, m.tokenBody
		case githubUserPath:
			status, respBody = m.userStatus, m.userBody
		case githubEmailsPath:
			status, respBody = m.emailsStatus, m.emailsBody
		default:
			status, respBody = http.StatusNotFound, `{"message":"Not Found"}`
		}
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *githubMock) setToken(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenStatus, m.tokenBody = status, body
}

func (m *githubMock) setUser(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userStatus, m.userBody = status, body
}

func (m *githubMock) setEmails(status int, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emailsStatus, m.emailsBody = status, body
}

func (m *githubMock) hit(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits[path]
}

func (m *githubMock) form(path string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forms[path]
}

func (m *githubMock) method(path string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.meth[path]
}

func (m *githubMock) header(path, key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.heads[path]; ok {
		return h.Get(key)
	}
	return ""
}

// provider 构造一个把 token 端点与 API base 都指向 mock 的 GitHub 实例。
func (m *githubMock) provider() *githubProvider {
	cfg := Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  "https://app.example.com/oauth/github/callback",
		Scope:        DefaultScopeGitHub,
	}
	p := newGitHub(cfg)
	p.apiBase = m.srv.URL
	p.oc.Endpoint.TokenURL = m.srv.URL + "/login/oauth/access_token"
	return p
}

func githubCheck(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

// ---------------------------------------------------------------------------
// 1. AuthURL
// ---------------------------------------------------------------------------

func TestGitHubAuthURL(t *testing.T) {
	p := newGitHub(Config{
		ClientID:     "cid-123",
		ClientSecret: "sec-456",
		RedirectURI:  "https://app.example.com/oauth/github/callback",
		Scope:        DefaultScopeGitHub,
	})

	raw, err := p.AuthURL("state-abc789")
	if err != nil {
		t.Fatalf("AuthURL 返回错误: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("AuthURL 不是合法 URL: %q, %v", raw, err)
	}
	githubCheck(t, u.Scheme == "https", "scheme = %q, want https", u.Scheme)
	githubCheck(t, u.Host == "github.com", "host = %q, want github.com", u.Host)
	githubCheck(t, u.Path == "/login/oauth/authorize", "path = %q", u.Path)

	q := u.Query()
	githubCheck(t, q.Get("client_id") == "cid-123", "client_id = %q", q.Get("client_id"))
	githubCheck(t, q.Get("redirect_uri") == "https://app.example.com/oauth/github/callback",
		"redirect_uri = %q", q.Get("redirect_uri"))
	// scope 请求侧是空格分隔（响应侧才是逗号）
	githubCheck(t, q.Get("scope") == "read:user user:email", "scope = %q, want %q", q.Get("scope"), "read:user user:email")
	githubCheck(t, q.Get("state") == "state-abc789", "state = %q", q.Get("state"))
	githubCheck(t, q.Get("response_type") == "code", "response_type = %q", q.Get("response_type"))

	// 空 state 必须拒绝：它是唯一的 CSRF 凭据。
	if _, err := p.AuthURL("  "); err == nil {
		t.Error("AuthURL(空 state) 应当返回错误")
	}

	// 未配置时降级为 ErrNotConfigured（调用方据此映射业务码 20026）。
	bare := newGitHub(Config{})
	if _, err := bare.AuthURL("s"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("未配置时 AuthURL err = %v, want ErrNotConfigured", err)
	}
}

// ---------------------------------------------------------------------------
// 2. Exchange 成功（含 Accept 头与 form 编码校验）
// ---------------------------------------------------------------------------

func TestGitHubExchangeSuccess(t *testing.T) {
	m := newGitHubMock(t)
	m.setToken(http.StatusOK, `{"access_token":"gho_16C7e42F292c6912E7710c838347Ae178B4a","scope":"read:user,user:email","token_type":"bearer"}`)
	p := m.provider()

	tok, err := p.Exchange(context.Background(), "code-xyz")
	if err != nil {
		t.Fatalf("Exchange 返回错误: %v", err)
	}

	githubCheck(t, tok.AccessToken == "gho_16C7e42F292c6912E7710c838347Ae178B4a",
		"AccessToken = %q", tok.AccessToken)
	// scope 原样保留（响应侧逗号分隔）
	githubCheck(t, tok.Scope == "read:user,user:email", "Scope = %q, want %q", tok.Scope, "read:user,user:email")
	githubCheck(t, tok.Verified, "Verified = false, want true（token 由本方 Exchange 换得）")
	githubCheck(t, tok.Channel == ChannelWeb, "Channel = %q, want %q", tok.Channel, ChannelWeb)
	githubCheck(t, len(tok.Raw) > 0, "Raw 为空，排错时没有原始响应")

	// 请求形态：POST + Accept: application/json + form 编码带 client 凭据与 code。
	githubCheck(t, m.method("/login/oauth/access_token") == http.MethodPost,
		"token 端点方法 = %q, want POST", m.method("/login/oauth/access_token"))
	accept := m.header("/login/oauth/access_token", "Accept")
	githubCheck(t, strings.Contains(accept, "application/json"),
		"Accept = %q, want 含 application/json（官方要求，否则返回 form 编码）", accept)
	form := m.form("/login/oauth/access_token")
	githubCheck(t, strings.Contains(form, "grant_type=authorization_code"), "body 缺 grant_type: %q", form)
	githubCheck(t, strings.Contains(form, "code=code-xyz"), "body 缺 code: %q", form)
	githubCheck(t, strings.Contains(form, "client_id=test-client-id"), "body 缺 client_id: %q", form)
	githubCheck(t, strings.Contains(form, "client_secret=test-client-secret"), "body 缺 client_secret: %q", form)
}

// ---------------------------------------------------------------------------
// 3. Exchange 错误：bad_verification_code → ErrCodeInvalid
// ---------------------------------------------------------------------------

func TestGitHubExchangeBadVerificationCode(t *testing.T) {
	m := newGitHubMock(t)
	m.setToken(http.StatusBadRequest, `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired.","error_uri":"/apps/managing-oauth-apps/troubleshooting-oauth-app-access-token-request-errors/#bad-verification-code"}`)
	p := m.provider()

	tok, err := p.Exchange(context.Background(), "expired-code")
	if tok != nil {
		t.Fatalf("Exchange 失败却返回了 token: %+v", tok)
	}
	if !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("err = %v, want ErrCodeInvalid", err)
	}
	// error_uri 必须带出来（旧实现丢弃它，§6.4 差异 #4）
	githubCheck(t, strings.Contains(err.Error(), "bad_verification_code"), "错误信息缺原始 error code: %v", err)
	githubCheck(t, strings.Contains(err.Error(), "error_uri") || strings.Contains(err.Error(), "troubleshooting"),
		"错误信息缺 error_uri（排错时少了官方文档锚点）: %v", err)
}

// ---------------------------------------------------------------------------
// 4. UserInfo：/user 的 email 有值 → 直接用，不调 /user/emails
// ---------------------------------------------------------------------------

func TestGitHubUserInfoUsesProfileEmail(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusOK, `{"id":583231,"login":"octocat","name":"The Octocat","email":"octocat@github.com","avatar_url":"https://avatars.githubusercontent.com/u/583231"}`)
	m.setEmails(http.StatusOK, `[{"email":"other@example.com","primary":true,"verified":true}]`)
	p := m.provider()

	tok := &Token{AccessToken: "gho_x", Scope: "read:user,user:email", Channel: ChannelWeb, Verified: true}
	u, err := p.UserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("UserInfo 返回错误: %v", err)
	}
	githubCheck(t, u.Provider == ProviderGitHub, "Provider = %q", u.Provider)
	githubCheck(t, u.ID == "583231", "ID = %q, want 583231（id 转 string 作主键）", u.ID)
	githubCheck(t, u.Nickname == "The Octocat", "Nickname = %q", u.Nickname)
	githubCheck(t, u.Email == "octocat@github.com", "Email = %q", u.Email)
	githubCheck(t, u.AvatarURL == "https://avatars.githubusercontent.com/u/583231", "AvatarURL = %q", u.AvatarURL)
	githubCheck(t, m.hit(githubEmailsPath) == 0,
		"/user 已给 email，却仍调用了 /user/emails %d 次（多一次无谓往返）", m.hit(githubEmailsPath))
	// /user 必须带 UA（GitHub 缺 UA 直接 403）
	githubCheck(t, m.header(githubUserPath, "User-Agent") == githubUserAgent,
		"User-Agent = %q, want %q", m.header(githubUserPath, "User-Agent"), githubUserAgent)
	githubCheck(t, m.header(githubUserPath, "Authorization") == "Bearer gho_x",
		"Authorization = %q", m.header(githubUserPath, "Authorization"))
}

// ---------------------------------------------------------------------------
// 5. UserInfo：email 为 null → 补拉 /user/emails 取 primary && verified
// ---------------------------------------------------------------------------

func TestGitHubUserInfoFallsBackToEmails(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusOK, `{"id":1,"login":"octocat","name":null,"email":null,"avatar_url":"https://a.example.com/x.png"}`)
	m.setEmails(http.StatusOK, `[
		{"email":"unverified@example.com","primary":false,"verified":true,"visibility":"private"},
		{"email":"a@b.c","primary":true,"verified":true,"visibility":"private"},
		{"email":"second@b.c","primary":true,"verified":false,"visibility":"private"}
	]`)
	p := m.provider()

	tok := &Token{AccessToken: "gho_x", Scope: "read:user,user:email", Channel: ChannelWeb, Verified: true}
	u, err := p.UserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("UserInfo 返回错误: %v", err)
	}
	githubCheck(t, u.Email == "a@b.c", "Email = %q, want a@b.c（取 primary && verified 的那条）", u.Email)
	githubCheck(t, u.Nickname == "octocat", "Nickname = %q, want login 回落（name 为 null）", u.Nickname)
	githubCheck(t, m.hit(githubEmailsPath) == 1, "/user/emails 调用次数 = %d, want 1", m.hit(githubEmailsPath))
}

// ---------------------------------------------------------------------------
// 6. 【缺陷 2 回归】scope 不含 user:email 且 email 为 null → ErrScopeMissing
//    且不去打那个必然 403 的端点
// ---------------------------------------------------------------------------

func TestGitHubUserInfoScopeMissingFromToken(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusOK, `{"id":7,"login":"private-email-user","name":"P","email":null,"avatar_url":""}`)
	// 万一被调用，这里会给 403 —— 断言 hits==0 才能证明"根本没打"
	m.setEmails(http.StatusForbidden, `{"message":"Resource not accessible by integration"}`)
	p := m.provider()

	tok := &Token{AccessToken: "gho_x", Scope: "read:user", Channel: ChannelWeb, Verified: true}
	u, err := p.UserInfo(context.Background(), tok)
	if u != nil {
		t.Fatalf("UserInfo 应失败却返回了 user: %+v", u)
	}
	if !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("err = %v, want ErrScopeMissing", err)
	}
	if errors.Is(err, ErrEmailMissing) {
		t.Fatalf("err = %v，不能把「没权限」误报成 ErrEmailMissing", err)
	}
	githubCheck(t, m.hit(githubEmailsPath) == 0,
		"scope 明确不含 user:email 时仍然请求了 /user/emails %d 次（缺陷 2 未修好）", m.hit(githubEmailsPath))

	// 官方口径：user ⊃ user:email → scope 只有 user 时必须照常去拉。
	m2 := newGitHubMock(t)
	m2.setUser(http.StatusOK, `{"id":7,"login":"u","name":"U","email":null,"avatar_url":""}`)
	m2.setEmails(http.StatusOK, `[{"email":"u@example.com","primary":true,"verified":true}]`)
	p2 := m2.provider()
	tok2 := &Token{AccessToken: "gho_x", Scope: "user", Channel: ChannelWeb, Verified: true}
	u2, err := p2.UserInfo(context.Background(), tok2)
	if err != nil {
		t.Fatalf("scope=user 时 UserInfo 返回错误: %v", err)
	}
	githubCheck(t, u2.Email == "u@example.com", "scope=user 时 Email = %q", u2.Email)
}

// ---------------------------------------------------------------------------
// 7. 【缺陷 1 回归】/user/emails 返回 403 → ErrScopeMissing（不是静默跳过）
// ---------------------------------------------------------------------------

func TestGitHubUserInfoEmailsForbidden(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusOK, `{"id":9,"login":"u","name":null,"email":null,"avatar_url":""}`)
	m.setEmails(http.StatusForbidden, `{"message":"Resource not accessible by integration","documentation_url":"https://docs.github.com/rest"}`)
	p := m.provider()

	// scope 为空 = 无从判断（例如手写 token），必须真的去拉，并把 403 翻成 ErrScopeMissing。
	tok := &Token{AccessToken: "gho_x", Channel: ChannelWeb, Verified: true}
	u, err := p.UserInfo(context.Background(), tok)
	if u != nil {
		t.Fatalf("UserInfo 应失败却返回了 user: %+v", u)
	}
	if !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("err = %v, want ErrScopeMissing（旧实现这里是静默跳过，返回 email 为空）", err)
	}
	if errors.Is(err, ErrEmailMissing) {
		t.Fatalf("err = %v，403 是「没权限」不是「没邮箱」", err)
	}
	githubCheck(t, m.hit(githubEmailsPath) == 1, "/user/emails 调用次数 = %d, want 1", m.hit(githubEmailsPath))
}

// ---------------------------------------------------------------------------
// 8. 没有 primary && verified → ErrEmailMissing（只在这一种情况下用）
// ---------------------------------------------------------------------------

func TestGitHubUserInfoNoVerifiedPrimaryEmail(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusOK, `{"id":11,"login":"u","name":null,"email":null,"avatar_url":""}`)
	m.setEmails(http.StatusOK, `[
		{"email":"unverified-primary@example.com","primary":true,"verified":false},
		{"email":"verified-secondary@example.com","primary":false,"verified":true}
	]`)
	p := m.provider()

	tok := &Token{AccessToken: "gho_x", Scope: "read:user,user:email", Channel: ChannelWeb, Verified: true}
	u, err := p.UserInfo(context.Background(), tok)
	if u != nil {
		t.Fatalf("UserInfo 应失败却返回了 user: %+v", u)
	}
	if !errors.Is(err, ErrEmailMissing) {
		t.Fatalf("err = %v, want ErrEmailMissing", err)
	}
	if errors.Is(err, ErrScopeMissing) {
		t.Fatalf("err = %v，有权限但没可用邮箱，不能报 ErrScopeMissing", err)
	}
}

// ---------------------------------------------------------------------------
// 9. Refresh → ErrRefreshUnsupported
// ---------------------------------------------------------------------------

func TestGitHubRefreshUnsupported(t *testing.T) {
	p := newGitHub(Config{ClientID: "cid", ClientSecret: "sec", Scope: DefaultScopeGitHub})
	tok, err := p.Refresh(context.Background(), &Token{AccessToken: "gho_x", Channel: ChannelWeb})
	if tok != nil {
		t.Fatalf("Refresh 应返回 nil token, got %+v", tok)
	}
	if !errors.Is(err, ErrRefreshUnsupported) {
		t.Fatalf("err = %v, want ErrRefreshUnsupported（GitHub 默认不过期）", err)
	}
}

// ---------------------------------------------------------------------------
// 10. 无 expires_in → ExpiresIn = 0、ExpiresAt 零值（不过期）
// ---------------------------------------------------------------------------

func TestGitHubExchangeNoExpiry(t *testing.T) {
	m := newGitHubMock(t)
	m.setToken(http.StatusOK, `{"access_token":"gho_x","scope":"read:user,user:email","token_type":"bearer"}`)
	p := m.provider()

	tok, err := p.Exchange(context.Background(), "c")
	if err != nil {
		t.Fatalf("Exchange 返回错误: %v", err)
	}
	githubCheck(t, tok.ExpiresIn == 0, "ExpiresIn = %d, want 0（不过期）", tok.ExpiresIn)
	githubCheck(t, tok.ExpiresAt.IsZero(), "ExpiresAt = %v, want 零值", tok.ExpiresAt)
	githubCheck(t, tok.RefreshToken == "", "RefreshToken = %q, want 空", tok.RefreshToken)
}

// 附：开启了 expiring tokens 的响应仍要正确换算（28800 秒 = 8 小时）。
func TestGitHubExchangeWithExpiry(t *testing.T) {
	m := newGitHubMock(t)
	m.setToken(http.StatusOK, `{"access_token":"gho_x","expires_in":28800,"refresh_token":"ghr_y","refresh_token_expires_in":15897600,"scope":"repo,gist","token_type":"bearer"}`)
	p := m.provider()

	tok, err := p.Exchange(context.Background(), "c")
	if err != nil {
		t.Fatalf("Exchange 返回错误: %v", err)
	}
	githubCheck(t, tok.RefreshToken == "ghr_y", "RefreshToken = %q", tok.RefreshToken)
	githubCheck(t, tok.ExpiresIn > 28790 && tok.ExpiresIn <= 28800, "ExpiresIn = %d, want ≈28800", tok.ExpiresIn)
	githubCheck(t, time.Until(tok.ExpiresAt) > 0, "ExpiresAt 应指向未来: %v", tok.ExpiresAt)
}

// 附：ValidateToken 直接放行并置 Verified；Name/Channel/Configured 契约。
func TestGitHubValidateTokenAndIdentity(t *testing.T) {
	p := newGitHub(Config{ClientID: "cid", ClientSecret: "sec", Scope: DefaultScopeGitHub})
	githubCheck(t, p.Name() == ProviderGitHub, "Name = %q", p.Name())
	githubCheck(t, p.Channel() == ChannelWeb, "Channel = %q", p.Channel())
	githubCheck(t, p.Configured(), "Configured = false, want true")
	githubCheck(t, !newGitHub(Config{ClientID: "cid"}).Configured(), "只有 client_id 时也应视为未配置")

	in := &Token{AccessToken: "gho_x"}
	out, err := p.ValidateToken(context.Background(), in)
	if err != nil {
		t.Fatalf("ValidateToken 返回错误: %v", err)
	}
	githubCheck(t, out == in, "ValidateToken 应返回同一个 token 指针")
	githubCheck(t, out.Verified, "Verified = false, want true")
	githubCheck(t, out.Channel == ChannelWeb, "Channel = %q, want web", out.Channel)

	if _, err := p.ValidateToken(context.Background(), &Token{}); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("空 access_token 时 err = %v, want ErrTokenExpired", err)
	}
}

// 附：/user 401 → ErrTokenExpired。
func TestGitHubUserInfoUnauthorized(t *testing.T) {
	m := newGitHubMock(t)
	m.setUser(http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	p := m.provider()

	_, err := p.UserInfo(context.Background(), &Token{AccessToken: "gho_bad", Channel: ChannelWeb, Verified: true})
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

// 附：scope 切分同时吃逗号与空格（Good: "user:email" 单值）。
func TestGitHubScopeParse(t *testing.T) {
	cases := []struct {
		scope string
		want  bool
	}{
		{"read:user,user:email", true},
		{"read:user user:email", true},
		{"read:user, user:email", true},
		{"read:user", false},
		{"", false},
		{"user", true}, // 判断 want=user 时成立
	}
	for _, c := range cases {
		got := githubScopeGranted(c.scope, githubScopeEmail)
		if c.scope == "user" {
			got = githubScopeGranted(c.scope, githubScopeUser)
		}
		if got != c.want {
			t.Errorf("githubScopeGranted(%q) = %v, want %v", c.scope, got, c.want)
		}
	}
}

// GitHub Provider（web 单渠道）。
//
// 设计依据：docs/14-第三方登录调研.md
//   - §3    四个端点的全部参数与官方响应/错误体
//   - §3.3  四个官方错误码（bad_verification_code / incorrect_client_credentials /
//     redirect_uri_mismatch / unverified_user_email）
//   - §6.4  现有 service 版实现与官方文档的 8 条差异（本文件修掉 #2 改为 form 编码、
//     #3 scope 静默吞错、#4 error_uri、#6 错误码分类共四条；#8 的超时仍是常量 15s，
//     未做可配置 —— 放进 Config 没有业务价值，将来有需求再加）
//   - §7.1  Provider 接口；§7.1.1 多渠道修订（GitHub 只有 web，device 未实现）
//
// # 引用 golang.org/x/oauth2（BSD-3-Clause，go.mod 仅 1 个传递依赖）
//
// 交给库的部分：
//   - oauth2.Config.AuthCodeURL：client_id / redirect_uri / scope（空格拼装）/ state
//   - oauth2.Config.Exchange  ：POST form 编码 + 标准错误体解析（*oauth2.RetrieveError
//     里已经带了 error / error_description / error_uri 三个字段）
//
// 自己写的部分（x/oauth2 的 Endpoint.UserInfoURL 只是声明位，不负责调用与解析）：
//   - GET /user 与 GET /user/emails 两次调用 + primary && verified 筛选
//   - 四个错误码 → 哨兵错误的分类映射
//   - scope 解析与 user:email 授权校验（本文件的核心价值，见 UserInfo 注释）
//
// # 修掉的两个已知缺陷（对比 internal/service/github_oauth.go）
//
//	缺陷 1：旧实现 `if err := g.getJSON(emailsAPI, ...); err == nil { ... }` 把
//	        /user/emails 的 403（token 没带 user:email）连错误一起吞掉 → 建号无邮箱、
//	        无日志、无错误，只能靠猜。本实现在 Exchange 阶段解析并保留 scope，
//	        UserInfo 里把 403 明确翻译成 ErrScopeMissing，ErrEmailMissing 只留给
//	        「确实没有 primary && verified 邮箱」这一种情况。
//	缺陷 2：旧实现完全不知道 token 的 scope，无法在拉邮箱前就判断"必然失败"。
//	        本实现若 scope 明确不含 user:email（且不含 user，官方口径 user ⊃ user:email），
//	        直接返回 ErrScopeMissing，不去打那个必然 403 的端点 —— 省一次往返，
//	        且错误原因准确（403 与"没有主邮箱"是两件事）。
package oauthkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// GitHub 端点与常量。
const (
	githubAuthorizeURL   = "https://github.com/login/oauth/authorize"
	githubTokenURL       = "https://github.com/login/oauth/access_token"
	githubDefaultAPIBase = "https://api.github.com"
	githubUserPath       = "/user"
	githubEmailsPath     = "/user/emails"

	// githubUserAgent GitHub 强制要求 UA：缺 UA 的请求一律 403。
	githubUserAgent = "fantwo-core"
	// githubAPIVersion 固定 REST API 版本，避免上游默认版本漂移改变字段语义。
	githubAPIVersion = "2022-11-28"

	// githubScopeEmail 补拉邮箱所需作用域。
	githubScopeEmail = "user:email"
	// githubScopeUser 官方口径：user 包含 user:email 与 user:follow（docs/14 §3.2.5）。
	githubScopeUser = "user"

	githubHTTPTimeout = 15 * time.Second
	githubMaxBody     = 1 << 20
)

// githubProvider GitHub OAuth 实现（web 单渠道）。
//
// 字段设计刻意留了两个测试注入点：
//   - oc      指针，测试替换 oc.Endpoint.TokenURL 指向 httptest 服务
//   - apiBase 可替换，测试替换成 httptest 服务的地址
//
// 两者都是包内字段，不污染 New() 的公开签名。
type githubProvider struct {
	cfg     Config
	oc      *oauth2.Config
	apiBase string
	client  *http.Client
}

// newGitHub 构造 GitHub Provider。
func newGitHub(cfg Config) *githubProvider {
	scope := strings.TrimSpace(cfg.Scope)
	if scope == "" {
		scope = DefaultScopeGitHub
	}
	p := &githubProvider{
		cfg:     cfg,
		apiBase: githubDefaultAPIBase,
		client:  &http.Client{Timeout: githubHTTPTimeout},
	}
	p.oc = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		// 请求时 scope 是空格分隔（官方口径），oauth2 用 strings.Join(Scopes, " ") 拼装。
		Scopes: strings.Fields(scope),
		Endpoint: oauth2.Endpoint{
			AuthURL:  githubAuthorizeURL,
			TokenURL: githubTokenURL,
			// 官方文档示例把 client_id / client_secret 放 body（form 编码）。
			// 显式指定 InParams，避免 AuthStyleUnknown 的"先试 Basic 头、失败再试 body"
			// 探测行为 —— 那会让每个首次换 token 的请求打两次上游。
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	return p
}

// Name 平台标识。
func (g *githubProvider) Name() string { return ProviderGitHub }

// Channel 渠道标识：GitHub 本轮只支持 web（device flow 未实现，见 docs/14 §8.4）。
func (g *githubProvider) Channel() string { return ChannelWeb }

// Configured 是否已配置齐全。
func (g *githubProvider) Configured() bool {
	if g == nil {
		return false
	}
	return strings.TrimSpace(g.cfg.ClientID) != "" && strings.TrimSpace(g.cfg.ClientSecret) != ""
}

// AuthURL 生成授权跳转 URL。
//
// 由 oauth2.Config.AuthCodeURL 负责拼装：client_id / redirect_uri（非空才带）/
// scope（空格分隔）/ state，并附带 response_type=code（GitHub 接受）。
// state 必须非空：它是唯一的 CSRF 凭据，空值等于关掉防护。
func (g *githubProvider) AuthURL(state string) (string, error) {
	if !g.Configured() {
		return "", ErrNotConfigured
	}
	if strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("oauthkit: github AuthURL requires non-empty state")
	}
	return g.oc.AuthCodeURL(state), nil
}

// Exchange 用授权 code 换 access_token。
//
// x/oauth2 的 Config.Exchange 负责 POST form 编码与标准错误体解析；这里额外做三件事：
//  1. 注入带超时的 http.Client（否则 x/oauth2 用 http.DefaultClient，无超时），
//     并通过 acceptJSONTransport 补上 `Accept: application/json`；
//  2. 把 scope 解析出来保留到 Token.Scope（旧实现丢弃 scope，导致下游无法判断授权范围）；
//  3. Expiry 零值 = 不过期（GitHub 默认），ExpiresIn 保持 0。
func (g *githubProvider) Exchange(ctx context.Context, code string) (*Token, error) {
	if !g.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(code) == "" {
		return nil, fmt.Errorf("%w: empty authorization code", ErrCodeInvalid)
	}

	// x/oauth2 通过 ctx 里的 oauth2.HTTPClient 取 client（ctx.go: ContextClient）。
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Timeout:   githubHTTPTimeout,
		Transport: acceptJSONTransport{base: g.client.Transport},
	})

	tk, err := g.oc.Exchange(ctx, code)
	if err != nil {
		return nil, githubExchangeError(err)
	}

	out := &Token{
		AccessToken:  tk.AccessToken,
		RefreshToken: tk.RefreshToken,
		Scope:        githubTokenScope(tk),
		Channel:      ChannelWeb,
		// token 只可能来自本方 Exchange（GitHub 本轮只有 web 渠道，客户端不上报 token），
		// 天然可信，直接置 Verified。
		Verified: true,
	}
	if !tk.Expiry.IsZero() {
		out.ExpiresAt = tk.Expiry
		// ExpiresIn 由 Expiry 反推（x/oauth2 只在响应带 expires_in 时才非零）。
		if secs := int64(time.Until(tk.Expiry).Seconds()); secs > 0 {
			out.ExpiresIn = secs
		} else {
			out.ExpiresIn = tk.ExpiresIn
		}
	}
	out.Raw = githubTokenRaw(tk)
	return out, nil
}

// ValidateToken GitHub 无需远端校验，直接标记为已验证。
//
// 理由：GitHub 本轮只有 web 渠道，token 一定是后端自己用 code 换来的
// （客户端永远拿不到、也不会向后端上报 access_token），所以不存在
// 「客户端伪造 token」这一攻击面 —— 与 QQ app 渠道的性质完全不同。
// 也刻意不做 GET /user 探活：多一次网络往返只换来"用户可能已撤销授权"的信号，
// 而调用方紧接着的 UserInfo 本来就要打 /user，有效性由它判。
func (g *githubProvider) ValidateToken(ctx context.Context, tok *Token) (*Token, error) {
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil, ErrTokenExpired
	}
	tok.Verified = true
	if tok.Channel == "" {
		tok.Channel = ChannelWeb
	}
	return tok, nil
}

// UserInfo GET /user，email 为 null 时按修正后的逻辑补拉 /user/emails。
func (g *githubProvider) UserInfo(ctx context.Context, tok *Token) (*User, error) {
	if !g.Configured() {
		return nil, ErrNotConfigured
	}
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil, fmt.Errorf("%w: empty access token", ErrTokenExpired)
	}

	var raw struct {
		ID        int64   `json:"id"`
		Login     string  `json:"login"`
		Name      *string `json:"name"`
		Email     *string `json:"email"`
		AvatarURL string  `json:"avatar_url"`
	}
	if err := g.getJSON(ctx, githubUserPath, tok.AccessToken, &raw); err != nil {
		return nil, githubUserError(err)
	}

	u := &User{
		Provider:  ProviderGitHub,
		ID:        strconv.FormatInt(raw.ID, 10),
		AvatarURL: raw.AvatarURL,
	}
	// name 可为 null；回落到 login（login 可变，只能当展示名，不能当主键）。
	u.Nickname = githubDerefTrim(raw.Name)
	if u.Nickname == "" {
		u.Nickname = strings.TrimSpace(raw.Login)
	}

	// /user 的 email 有值（用户主邮箱是 public）→ 直接用，不浪费一次请求。
	if email := githubDerefTrim(raw.Email); email != "" {
		u.Email = email
		return u, nil
	}

	// email 为 null 是常态（用户把主邮箱设为 private），必须补拉 /user/emails。
	//
	// 【缺陷 2 修正】动手前先看 Exchange 时保留下来的 Scope：
	// scope 明确不含 user:email（也不含 user）时直接判 ErrScopeMissing ——
	// 不去打那个必然 403 的端点。注意 scope 为空时"无法判断"，仍要尝试。
	if sc := strings.TrimSpace(tok.Scope); sc != "" &&
		!githubScopeGranted(sc, githubScopeEmail) && !githubScopeGranted(sc, githubScopeUser) {
		return nil, fmt.Errorf("%w: github token scope %q does not include %q",
			ErrScopeMissing, sc, githubScopeEmail)
	}

	email, err := g.primaryEmail(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	u.Email = email
	return u, nil
}

// Refresh GitHub 默认不过期，没有刷新语义。
//
// 只有 OAuth App 显式开启 token expiration（或请求 offline_access）后才会返回
// expires_in=28800 + refresh_token，那时才能用 POST /login/oauth/access_token
// 带 grant_type=refresh_token 刷新。本轮不支持；要支持时在这里补一个分支即可
// （ExpiresAt 非零的 token 走刷新，零值的继续返回 ErrRefreshUnsupported）。
func (g *githubProvider) Refresh(ctx context.Context, tok *Token) (*Token, error) {
	return nil, ErrRefreshUnsupported
}

// primaryEmail GET /user/emails，取第一条 primary && verified。
//
// 只认 primary && verified：拿 unverified 的邮箱做账号合并等于允许别人抢注
// （docs/14 §3.2.4 取值策略）。
func (g *githubProvider) primaryEmail(ctx context.Context, accessToken string) (string, error) {
	var list []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := g.getJSON(ctx, githubEmailsPath, accessToken, &list); err != nil {
		// 【缺陷 1 修正】403 在这条路径上只有一种含义：token 没带 user:email scope。
		// 旧实现用 `if err == nil` 把它连错误一起吞掉，最终 email 为空 → 建号无邮箱。
		// 现在原样翻译成 ErrScopeMissing，让调用方能给出「请重新授权并允许读取邮箱」
		// 而不是静默建一个无邮箱账号。
		var se *githubStatusError
		if errors.As(err, &se) && se.status == http.StatusForbidden {
			return "", fmt.Errorf("%w: github %s returned 403, token lacks %q scope",
				ErrScopeMissing, githubEmailsPath, githubScopeEmail)
		}
		return "", err
	}
	for _, e := range list {
		if e.Primary && e.Verified {
			if email := strings.TrimSpace(e.Email); email != "" {
				return email, nil
			}
		}
	}
	// 有权限、也拿到了列表，但没有 primary && verified —— 这才是真正的"没邮箱"，
	// 与上面的"没权限"严格区分。
	return "", fmt.Errorf("%w: github %s has no primary verified entry", ErrEmailMissing, githubEmailsPath)
}

// getJSON 带 Bearer 的 GET + JSON 解析。
func (g *githubProvider) getJSON(ctx context.Context, path, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.apiURL(path), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", githubUserAgent) // GitHub 缺 UA 直接 403
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GET %s: %v", ErrUpstream, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, githubMaxBody))
	if err != nil {
		return fmt.Errorf("%w: GET %s: read body: %v", ErrUpstream, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &githubStatusError{status: resp.StatusCode, path: path, body: string(raw)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: GET %s: decode body: %v", ErrUpstream, path, err)
	}
	return nil
}

// apiURL 拼 API 地址（apiBase 可被测试替换）。
func (g *githubProvider) apiURL(path string) string {
	return strings.TrimRight(g.apiBase, "/") + path
}

// githubStatusError 上游非 2xx 响应。保留状态码供上层做语义映射（401/403 含义不同）。
type githubStatusError struct {
	status int
	path   string
	body   string
}

func (e *githubStatusError) Error() string {
	return fmt.Sprintf("github %s returned HTTP %d: %s", e.path, e.status, githubTruncate(e.body, 256))
}

// githubUserError 把 /user 的失败映射成哨兵错误。
func githubUserError(err error) error {
	var se *githubStatusError
	if errors.As(err, &se) {
		switch se.status {
		case http.StatusUnauthorized:
			// token 无效 / 已被用户撤销。
			return fmt.Errorf("%w: github %s returned 401", ErrTokenExpired, se.path)
		case http.StatusForbidden:
			// /user 的 403 不是 scope 问题（读 profile 只要 token 有效），
			// 通常是限流（5000 req/h）或 SAML SSO 未授权 —— 归上游异常。
			return fmt.Errorf("%w: %v", ErrUpstream, se)
		}
		return fmt.Errorf("%w: %v", ErrUpstream, se)
	}
	if errors.Is(err, ErrUpstream) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUpstream, err)
}

// githubExchangeError 把 x/oauth2 的换 token 错误映射成哨兵错误（docs/14 §3.3 四码 + 兜底）。
func githubExchangeError(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		detail := githubErrorDetail(re)
		switch strings.ToLower(strings.TrimSpace(re.ErrorCode)) {
		case "bad_verification_code", "invalid_grant":
			// code 错误 / 过期 / 已被用过 / 与 client 不匹配 → 让用户重新登录。
			return fmt.Errorf("%w: github token endpoint: %s", ErrCodeInvalid, detail)
		case "incorrect_client_credentials", "invalid_client":
			// 应用凭据被拒 → 这是配置事故，调用方应告警而不是提示用户重试。
			return fmt.Errorf("%w: github token endpoint: %s", ErrCredentialBad, detail)
		case "unverified_user_email":
			// 官方第四码：用户必须有一个已验证的主邮箱。
			return fmt.Errorf("%w: github token endpoint: %s", ErrEmailMissing, detail)
		case "redirect_uri_mismatch", "access_denied":
			// 回调地址与开放平台登记不一致 / 用户在授权页点了拒绝。
			return fmt.Errorf("%w: github token endpoint: %s", ErrUpstream, detail)
		default:
			return fmt.Errorf("%w: github token endpoint: %s", ErrUpstream, detail)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: github token endpoint: %v", ErrUpstream, err)
	}
	return fmt.Errorf("%w: github token endpoint: %v", ErrUpstream, err)
}

// githubErrorDetail 拼错误详情。error_uri 也带上：旧实现丢弃它（§6.4 差异 #4），
// 排错时少了官方文档锚点。
func githubErrorDetail(re *oauth2.RetrieveError) string {
	parts := make([]string, 0, 3)
	if re.ErrorCode != "" {
		parts = append(parts, "error="+re.ErrorCode)
	}
	if re.ErrorDescription != "" {
		parts = append(parts, "description="+re.ErrorDescription)
	}
	if re.ErrorURI != "" {
		parts = append(parts, "uri="+re.ErrorURI)
	}
	if len(parts) == 0 {
		return githubTruncate(string(re.Body), 256)
	}
	return strings.Join(parts, " ")
}

// githubTokenScope 从 x/oauth2.Token 里取实际授予的 scope。
//
// GitHub 的 scope 在 token 响应里是**逗号分隔**（与请求时的空格分隔不同，§3.4 坑 3）。
// 这里原样保留字符串（Token.Scope 的契约是"原样保留"），判断时再切分。
func githubTokenScope(tk *oauth2.Token) string {
	if tk == nil {
		return ""
	}
	if s, ok := tk.Extra("scope").(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// githubTokenRaw 还原一份排错用的原始响应（x/oauth2 只暴露单键 Extra，不暴露整个 body）。
//
// 注意：这里含 token 明文，按 Token.Raw 的约定不要整段写日志。
func githubTokenRaw(tk *oauth2.Token) []byte {
	raw := map[string]any{
		"access_token": tk.AccessToken,
		"token_type":   tk.TokenType,
	}
	if s := githubTokenScope(tk); s != "" {
		raw["scope"] = s
	}
	if tk.RefreshToken != "" {
		raw["refresh_token"] = tk.RefreshToken
	}
	if tk.ExpiresIn != 0 {
		raw["expires_in"] = tk.ExpiresIn
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return b
}

// githubScopeGranted scope 列表里是否包含 want。
//
// 同时吃逗号与空格两种分隔符：请求侧空格、响应侧逗号（§3.4 坑 3），
// 手写 token（ValidateToken 场景）可能两种都有，不做任何一种假设。
func githubScopeGranted(scope, want string) bool {
	if want == "" {
		return false
	}
	for _, f := range strings.FieldsFunc(scope, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

// githubDerefTrim 解 *string（GitHub 大量字段可为 null）。
func githubDerefTrim(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// githubTruncate 截断过长文本，避免错误信息/日志爆炸。
func githubTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// acceptJSONTransport 给发往 token 端点的请求补 `Accept: application/json`。
//
// 为什么需要它（本文件实测源码得出的结论，与 docs/14 §3.5 的说法相反）：
// x/oauth2 v0.38.0 的 internal.newTokenRequest 只设 Content-Type，
// **不设 Accept**（internal/token.go 的 newTokenRequest）。缺这个头 GitHub 就按默认
// 返回 form 编码（access_token=...&scope=...）—— 虽然 x/oauth2 的 form 分支也能解析、
// Extra("scope") 对 url.Values 同样有效，但那是在依赖未文档化的兼容路径；
// 官方明确要求带 Accept 才是 JSON（§3.2.2）。所以这里显式补齐。
type acceptJSONTransport struct {
	base http.RoundTripper
}

func (t acceptJSONTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 不原地改调用方传进来的 req（RoundTripper 的约定）。
	clone := req.Clone(req.Context())
	if clone.Header.Get("Accept") == "" {
		clone.Header.Set("Accept", "application/json")
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// 编译期断言：GitHub 实现满足 Provider 接口。
var _ Provider = (*githubProvider)(nil)

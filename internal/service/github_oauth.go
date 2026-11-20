// Package service GitHub OAuth provider。
//
// 「可配置即生效、未配置即降级」：
//   - 配置键从 config 表读：github_client_id / github_client_secret /
//     github_redirect_uri / github_success_redirect_url / github_scope；
//   - 未配置（client_id 或 secret 为空）→ Configured()=false，端点返回 400/4022（保持原占位语义）；
//   - 已配置 → 真实走 GitHub authorize → 换 access_token → 拉 user info →
//     按 github_id 找/建用户 → 签发双 token（见 AuthService.GithubCallback）。
//
// 依赖：标准库 net/http + encoding/json，零新增依赖。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GitHub OAuth 配置键。
const (
	cfgGithubClientID     = "github_client_id"
	cfgGithubClientSecret = "github_client_secret"
	cfgGithubRedirectURI  = "github_redirect_uri"
	cfgGithubSuccessURL   = "github_success_redirect_url"
	cfgGithubScope        = "github_scope"

	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserAPI      = "https://api.github.com/user"
	githubEmailsAPI    = "https://api.github.com/user/emails"

	defaultGithubScope = "read:user user:email"

	// state 存 Redis 的前缀与有效期（防 CSRF）。
	githubStatePrefix = "oauth:github:state:"
	githubStateTTL    = 10 * time.Minute
)

// GithubProfile GitHub 用户资料。
type GithubProfile struct {
	ID        string
	Login     string
	Name      string
	Email     string
	AvatarURL string
}

// GithubOAuth GitHub OAuth 客户端。
type GithubOAuth struct {
	d      *Deps
	client *http.Client
}

// NewGithubOAuth 构造。
func NewGithubOAuth(d *Deps) *GithubOAuth {
	return &GithubOAuth{d: d, client: &http.Client{Timeout: 15 * time.Second}}
}

// Configured 是否已配置（client_id + client_secret 均非空）。
func (g *GithubOAuth) Configured() bool {
	if g == nil || g.d == nil || g.d.Cfg == nil {
		return false
	}
	return g.d.Cfg.GetString(cfgGithubClientID, "") != "" &&
		g.d.Cfg.GetString(cfgGithubClientSecret, "") != ""
}

// SuccessRedirectURL 前端成功跳转地址（未配置回落到 "/"）。
func (g *GithubOAuth) SuccessRedirectURL() string {
	u := g.d.Cfg.GetString(cfgGithubSuccessURL, "")
	if u == "" {
		return "/"
	}
	return u
}

// AuthorizeURL 生成授权链接并把 state 写入 Redis（未配置返回 ErrProviderNotConfigured）。
func (g *GithubOAuth) AuthorizeURL(ctx context.Context, state string) (string, error) {
	if !g.Configured() {
		return "", ErrProviderNotConfigured
	}
	if state == "" {
		return "", fmt.Errorf("state 不能为空")
	}
	if st := g.d.AuthTok; st != nil {
		if err := st.Set(ctx, githubStatePrefix+state, 1, githubStateTTL); err != nil {
			g.d.Logger.Warn("github state 写入失败（本次仍允许授权，回调将跳过 state 校验）")
		}
	}
	q := url.Values{}
	q.Set("client_id", g.d.Cfg.GetString(cfgGithubClientID, ""))
	q.Set("scope", g.d.Cfg.GetString(cfgGithubScope, defaultGithubScope))
	q.Set("state", state)
	if redirect := g.d.Cfg.GetString(cfgGithubRedirectURI, ""); redirect != "" {
		q.Set("redirect_uri", redirect)
	}
	return githubAuthorizeURL + "?" + q.Encode(), nil
}

// VerifyState 校验并消费 state（Redis 不可用时放行，返回 true）。
func (g *GithubOAuth) VerifyState(ctx context.Context, state string) bool {
	if g == nil || g.d == nil || g.d.AuthTok == nil {
		return true
	}
	if state == "" {
		return false
	}
	key := githubStatePrefix + state
	n, err := g.d.AuthTok.Exists(ctx, key)
	if err != nil || n == 0 {
		return false
	}
	_ = g.d.AuthTok.Del(ctx, key)
	return true
}

// Exchange 用 code 换 token 并拉取用户资料。
func (g *GithubOAuth) Exchange(ctx context.Context, code string) (*GithubProfile, error) {
	if !g.Configured() {
		return nil, ErrProviderNotConfigured
	}
	if code == "" {
		return nil, fmt.Errorf("code 不能为空")
	}
	token, err := g.exchangeToken(ctx, code)
	if err != nil {
		return nil, err
	}
	return g.fetchProfile(ctx, token)
}

// exchangeToken POST /login/oauth/access_token。
func (g *GithubOAuth) exchangeToken(ctx context.Context, code string) (string, error) {
	payload := map[string]string{
		"client_id":     g.d.Cfg.GetString(cfgGithubClientID, ""),
		"client_secret": g.d.Cfg.GetString(cfgGithubClientSecret, ""),
		"code":          code,
	}
	if redirect := g.d.Cfg.GetString(cfgGithubRedirectURI, ""); redirect != "" {
		payload["redirect_uri"] = redirect
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub 换取 token 失败: HTTP %s", strconv.Itoa(resp.StatusCode))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("GitHub 换取 token 响应解析失败: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("GitHub 换取 token 失败: %s %s", out.Error, out.Description)
	}
	return out.AccessToken, nil
}

// fetchProfile GET /user（email 为空时补 /user/emails 取 primary）。
func (g *GithubOAuth) fetchProfile(ctx context.Context, token string) (*GithubProfile, error) {
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := g.getJSON(ctx, githubUserAPI, token, &user); err != nil {
		return nil, err
	}
	profile := &GithubProfile{
		ID:        strconv.FormatInt(user.ID, 10),
		Login:     user.Login,
		Name:      user.Name,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
	}
	if profile.Email == "" {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := g.getJSON(ctx, githubEmailsAPI, token, &emails); err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					profile.Email = e.Email
					break
				}
			}
		}
	}
	return profile, nil
}

// getJSON 带 Bearer 的 GET + JSON 解析。
func (g *GithubOAuth) getJSON(ctx context.Context, u, token string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub 接口 %s 返回 HTTP %s", u, strconv.Itoa(resp.StatusCode))
	}
	return json.Unmarshal(raw, out)
}

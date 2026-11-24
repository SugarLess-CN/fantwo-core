package oauthkit

// 微信 Provider 实现（web = 网站应用扫码 / app = 移动应用）。
//
// 协议依据：docs/14-第三方登录调研.md
//   - §1     微信登录（4 个端点全参数表 + 官方原文样例 + 21 条错误码 + 坑）
//   - §8.1   三种接入形态的差异（移动应用 vs 网站应用）
//
// 两个渠道的协议事实（不许凭记忆改写）：
//  1. 换 token 是 **GET**，参数全在 query，密钥参数名是 `secret` 不是 `client_secret`；
//  2. 授权 URL 必须以 `#wechat_redirect` 收尾（§1.4 坑 1，少写会白屏）；
//  3. `openid` 在换 token 时就返回，不用等 userinfo；
//  4. `unionid` 可空：app 渠道 scope 只能是 snsapi_userinfo，正常授权必带；
//     web 渠道按可空处理；
//  5. `refresh_token` 有效期 web 30 天 / app 180 天（官方两页各写各的）
//     → 这正是 Token.Channel 必须落库的原因；
//  6. access_token `expires_in` = 7200 秒，但一律用响应原值，不写死；
//  7. 错误体是 {"errcode":N,"errmsg":"..."}，**HTTP 状态码恒 200**
//     → 成败只能靠解析 errcode 判断。
//
// 渠道差异汇总：
//
//	web  AuthURL(qrconnect + #wechat_redirect) → Exchange → ValidateToken → UserInfo → Refresh
//	app  AuthURL 不可用（App 走微信客户端 SDK，后端不出 URL）→ 其余四个方法完全复用
//	     （同一个 sns/oauth2/access_token 端点，只是 appid/secret 是移动应用那一套）

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

// 微信开放平台端点与常量（docs/14 §1.2 / §8.1.6 端点复用矩阵）。
const (
	// wechatAuthEndpoint 网站应用扫码授权页。App / 公众号各有其它入口，后端用不到。
	wechatAuthEndpoint = "https://open.weixin.qq.com/connect/qrconnect"
	// wechatAPIEndpoint 前端接口域名，四个后端端点同源。
	wechatAPIEndpoint = "https://api.weixin.qq.com"
	// wechatPathAccessToken 通过 code 换取 access_token（GET）。
	wechatPathAccessToken = "/sns/oauth2/access_token"
	// wechatPathRefreshToken 刷新 / 续期 access_token（GET）。
	wechatPathRefreshToken = "/sns/oauth2/refresh_token"
	// wechatPathUserInfo 获取用户个人信息（GET）。
	wechatPathUserInfo = "/sns/userinfo"
	// wechatPathAuth 校验 access_token 是否有效（GET）。
	wechatPathAuth = "/sns/auth"

	// wechatRedirectAnchor 授权 URL 的强制锚点，少写会被微信拒绝（§1.4 坑 1）。
	wechatRedirectAnchor = "#wechat_redirect"
	// wechatGrantTypeCode 换 token 的 grant_type 固定值。
	wechatGrantTypeCode = "authorization_code"
	// wechatGrantTypeRefresh 刷新 token 的 grant_type 固定值。
	wechatGrantTypeRefresh = "refresh_token"

	// wechatRequestTimeout 与 internal/service/github_oauth.go 保持一致。
	wechatRequestTimeout = 15 * time.Second
	// wechatMaxBodyBytes 响应体上限，防超大响应打爆内存。
	wechatMaxBodyBytes = 1 << 20
)

// wechatHTTPClient 包级共享 client（Transport 可复用连接池）。
var wechatHTTPClient = &http.Client{Timeout: wechatRequestTimeout}

// wechatProvider 微信 (web | app) 渠道实现，满足 Provider 接口。
type wechatProvider struct {
	channel string
	cfg     Config
	// apiBase / authBase 预留可替换点：单测把 apiBase 指向 httptest server，
	// 生产恒为官方域名。
	apiBase  string
	authBase string
	http     *http.Client
}

// 编译期断言：wechatProvider 必须满足 Provider。
var _ Provider = (*wechatProvider)(nil)

// newWechat 构造微信 Provider（由 New() 调用，签名不可改）。
//
// channel 已在 New() 中收敛为 ChannelWeb / ChannelApp；
// scope 缺失时 Config.normalize 已补默认值（web=snsapi_login / app=snsapi_userinfo）。
func newWechat(channel string, cfg Config) *wechatProvider {
	return &wechatProvider{
		channel:  channel,
		cfg:      cfg,
		apiBase:  wechatAPIEndpoint,
		authBase: wechatAuthEndpoint,
		http:     wechatHTTPClient,
	}
}

// Name 平台标识，两渠道同为 "wechat"。
func (p *wechatProvider) Name() string { return ProviderWechat }

// Channel 渠道标识："web" / "app"。
func (p *wechatProvider) Channel() string { return p.channel }

// Configured appid 与 appsecret 均非空才算配置齐全。
//
// 未配置时网络方法返回 ErrNotConfigured，调用方据此降级为业务码 20026。
func (p *wechatProvider) Configured() bool {
	return strings.TrimSpace(p.cfg.ClientID) != "" && strings.TrimSpace(p.cfg.ClientSecret) != ""
}

// scope 返回本渠道的授权作用域。
//
// Config.normalize 已按渠道补过默认值，这里再兜一层是为了覆盖
// 「调用方直接构造 provider 绕过 New()」的路径。
func (p *wechatProvider) scope() string {
	if s := strings.TrimSpace(p.cfg.Scope); s != "" {
		return s
	}
	if p.channel == ChannelApp {
		return DefaultScopeWechatApp
	}
	return DefaultScopeWechatWeb
}

// AuthURL 生成网站应用扫码授权 URL。
//
// app 渠道返回 ErrAuthURLUnsupported：移动应用由客户端 SDK（WXApi.sendReq）拉起微信，
// 后端不生成任何 URL（§8.1.2 第一步）。
//
// state 必须非空：它既是防 CSRF 的唯一凭据，也是回调时把 code 关联到本次会话的依据。
// 微信把 state 列为可选，但「平台允许」不等于「我们可以不要」—— 空 state 等于关掉
// 防护，与 github.go 的立场保持一致（那边同样拒绝空 state）。
//
// 参数顺序严格按官方模板手写拼接，**不能用 url.Values.Encode()** ——
// 它按 key 字母序重排（appid, redirect_uri, response_type, scope, state 恰好字母序，
// 但一旦加入 forcePopup / lang 就会错位），而微信对授权链接做正则强匹配校验（§8.1.4 坑 1）。
// redirect_uri 必须 urlEncode（微信硬要求），否则未编码的 `https://` 里的 `:` `/` 会让
// 上游参数解析错位。末尾必须带 #wechat_redirect。
func (p *wechatProvider) AuthURL(state string) (string, error) {
	if p.channel == ChannelApp {
		return "", fmt.Errorf("oauthkit: wechat channel %q cannot build auth url (client SDK does): %w",
			p.channel, ErrAuthURLUnsupported)
	}
	if !p.Configured() {
		return "", fmt.Errorf("oauthkit: wechat AuthURL: %w", ErrNotConfigured)
	}
	if strings.TrimSpace(p.cfg.RedirectURI) == "" {
		return "", fmt.Errorf("oauthkit: wechat AuthURL requires redirect_uri: %w", ErrNotConfigured)
	}
	state = strings.TrimSpace(state)
	if state == "" {
		// 空 state 是调用方编程错误：重试无用，也不该被误判成「用户没给授权范围」，
		// 故套 ErrInvalidArgument 而非任何平台侧哨兵（取舍见 types.go 该哨兵注释）。
		return "", fmt.Errorf("%w: wechat AuthURL requires non-empty state", ErrInvalidArgument)
	}

	var b strings.Builder
	b.WriteString(p.authBase)
	b.WriteString("?appid=")
	b.WriteString(url.QueryEscape(p.cfg.ClientID))
	b.WriteString("&redirect_uri=")
	b.WriteString(url.QueryEscape(p.cfg.RedirectURI))
	b.WriteString("&response_type=code")
	b.WriteString("&scope=")
	b.WriteString(url.QueryEscape(p.scope()))
	b.WriteString("&state=")
	b.WriteString(url.QueryEscape(state))
	b.WriteString(wechatRedirectAnchor)
	return b.String(), nil
}

// Exchange 用授权 code 换取 access_token（GET，参数全在 query）。
//
// web 与 app 走同一个端点，只是 appid/secret 是移动应用那一套；
// 返回的 Token.Channel 标记 token 属哪个渠道（refresh_token 有效期不同）。
func (p *wechatProvider) Exchange(ctx context.Context, code string) (*Token, error) {
	if !p.Configured() {
		return nil, fmt.Errorf("oauthkit: wechat Exchange: %w", ErrNotConfigured)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		// 空 code 不要发给微信：会白吃一个 41008 missing code，且污染限频配额。
		return nil, fmt.Errorf("oauthkit: wechat Exchange requires code: %w", ErrCodeInvalid)
	}

	q := url.Values{}
	q.Set("appid", p.cfg.ClientID)
	q.Set("secret", p.cfg.ClientSecret) // 官方参数名是 secret，不是 client_secret
	q.Set("code", code)
	q.Set("grant_type", wechatGrantTypeCode)

	var resp wechatTokenResponse
	raw, err := p.getJSON(ctx, wechatPathAccessToken, q, &resp)
	if err != nil {
		return nil, fmt.Errorf("oauthkit: wechat Exchange: %w", err)
	}
	if err := resp.err(); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat Exchange: %w", err)
	}

	tok := resp.token(p.channel)
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("oauthkit: wechat Exchange: %w: empty access_token in response", ErrUpstream)
	}
	// openid 在换 token 阶段就返回（§1.2.2 响应字段），登录主键在此刻即可落地。
	if tok.OpenID == "" {
		return nil, fmt.Errorf("oauthkit: wechat Exchange: %w: empty openid in response", ErrUpstream)
	}
	tok.Raw = raw
	return tok, nil
}

// ValidateToken 调 sns/auth 校验 access_token 有效性。
//
// openid 必传：sns/auth 会把 access_token 与 openid 配对校验，
// 客户端伪造「别人的 token + 自己的 openid」组合会拿到 40003 invalid openid
// → 映射为 ErrTokenMismatch。校验通过则置 Verified=true。
func (p *wechatProvider) ValidateToken(ctx context.Context, tok *Token) (*Token, error) {
	if !p.Configured() {
		return nil, fmt.Errorf("oauthkit: wechat ValidateToken: %w", ErrNotConfigured)
	}
	if tok == nil {
		return nil, fmt.Errorf("oauthkit: wechat ValidateToken requires token: %w", ErrTokenMismatch)
	}
	accessToken := strings.TrimSpace(tok.AccessToken)
	openid := strings.TrimSpace(tok.OpenID)
	if accessToken == "" || openid == "" {
		return nil, fmt.Errorf("oauthkit: wechat ValidateToken requires access_token and openid: %w", ErrTokenMismatch)
	}

	q := url.Values{}
	q.Set("access_token", accessToken)
	q.Set("openid", openid)

	var resp wechatErrResponse
	if _, err := p.getJSON(ctx, wechatPathAuth, q, &resp); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat ValidateToken: %w", err)
	}
	if err := resp.err(); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat ValidateToken: %w", err)
	}

	out := *tok // 拷贝，不改调用方持有的原 token
	out.Channel = p.channel
	out.Verified = true
	// Raw 保留原换 token 响应，不被校验响应覆盖（校验响应无新增有用信息）。
	return &out, nil
}

// UserInfo 用 token 拉取归一化用户信息。
//
// 安全硬约束：app 渠道的 token 可能由客户端上报，必须先过 ValidateToken，
// 否则直接拒绝（ErrTokenNotVerified），防止伪造 openid + token 组合建号。
//
// sex / province / city / country 自 2021-10-20 起官方不再返回（§1.4 坑 4），
// 这里解析为可空字段但不进入 User，避免上游字段抖动影响业务。
func (p *wechatProvider) UserInfo(ctx context.Context, tok *Token) (*User, error) {
	if !p.Configured() {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo: %w", ErrNotConfigured)
	}
	if tok == nil {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo requires token: %w", ErrTokenMismatch)
	}
	if tok.Channel == ChannelApp && !tok.Verified {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo: channel %q token not verified: %w",
			tok.Channel, ErrTokenNotVerified)
	}

	accessToken := strings.TrimSpace(tok.AccessToken)
	openid := strings.TrimSpace(tok.OpenID)
	if accessToken == "" || openid == "" {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo requires access_token and openid: %w", ErrTokenMismatch)
	}

	q := url.Values{}
	q.Set("access_token", accessToken)
	q.Set("openid", openid)

	var resp wechatUserInfoResponse
	// UserInfo 的原始响应不入 Token.Raw（那里保留的是换 token 响应），故丢弃。
	if _, err := p.getJSON(ctx, wechatPathUserInfo, q, &resp); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo: %w", err)
	}
	if err := resp.err(); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo: %w", err)
	}
	// 拦截 `{}` / `null` / 非微信格式的零值响应：官方成功响应至少带 openid，
	// 业务字段全空说明这根本不是一条 userinfo 响应，不能当成「用户昵称就叫空串」。
	if resp.empty() {
		return nil, fmt.Errorf("oauthkit: wechat UserInfo: %w: empty userinfo response", ErrUpstream)
	}

	id := strings.TrimSpace(resp.OpenID)
	if id == "" {
		// 响应缺 openid 时退回 token 里的 openid（二者本应一致）。
		id = openid
	}

	// unionid 可空：拿到就带上，拿不到退回 token 里的值，不报错（§1.4 坑 3）。
	unionID := strings.TrimSpace(resp.UnionID)
	if unionID == "" {
		unionID = strings.TrimSpace(tok.UnionID)
	}

	return &User{
		Provider:  ProviderWechat,
		ID:        id,
		UnionID:   unionID,
		Nickname:  resp.Nickname,
		AvatarURL: resp.HeadImgURL,
	}, nil
}

// Refresh 刷新 / 续期 access_token。
//
// 官方语义（§1.2.3）：access_token 未超时的情况下刷新**不会改变 access_token**，
// 只延长超时时间 → 实现必须以响应原值为准覆盖，不能靠「token 是否变化」判断成功。
// unionid 不在刷新响应里，保留原值。
func (p *wechatProvider) Refresh(ctx context.Context, tok *Token) (*Token, error) {
	if !p.Configured() {
		return nil, fmt.Errorf("oauthkit: wechat Refresh: %w", ErrNotConfigured)
	}
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		// 没有 refresh_token 就只能重新授权（QQ app 无 refresh_token 同此语义）。
		return nil, fmt.Errorf("oauthkit: wechat Refresh requires refresh_token: %w", ErrRefreshDead)
	}

	q := url.Values{}
	q.Set("appid", p.cfg.ClientID)
	q.Set("grant_type", wechatGrantTypeRefresh)
	q.Set("refresh_token", tok.RefreshToken)

	var resp wechatTokenResponse
	raw, err := p.getJSON(ctx, wechatPathRefreshToken, q, &resp)
	if err != nil {
		return nil, fmt.Errorf("oauthkit: wechat Refresh: %w", err)
	}
	if err := resp.err(); err != nil {
		return nil, fmt.Errorf("oauthkit: wechat Refresh: %w", err)
	}

	out := *tok
	if accessToken := strings.TrimSpace(resp.AccessToken); accessToken != "" {
		out.AccessToken = accessToken
	} else {
		return nil, fmt.Errorf("oauthkit: wechat Refresh: %w: empty access_token in response", ErrUpstream)
	}
	if refreshToken := strings.TrimSpace(resp.RefreshToken); refreshToken != "" {
		out.RefreshToken = refreshToken
	}
	if openid := strings.TrimSpace(resp.OpenID); openid != "" {
		out.OpenID = openid
	}
	if scope := strings.TrimSpace(resp.Scope); scope != "" {
		out.Scope = scope
	}
	if expiresIn := int64(resp.ExpiresIn); expiresIn > 0 {
		out.ExpiresIn = expiresIn
		out.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	if out.Channel == "" {
		out.Channel = p.channel
	}
	out.Raw = raw
	return &out, nil
}

// getJSON 发起 GET 请求并解析 JSON 响应，返回原始响应体。
//
// 微信「HTTP 状态码恒 200」是官方约定，所以业务成败一律由调用方看 errcode；
// 非 200 说明是网关 / 网络层异常，直接按 ErrUpstream 处理。
// 响应体读取用 io.LimitReader 限流，错误信息里不回显响应体（含 token 明文）。
func (p *wechatProvider) getJSON(ctx context.Context, path string, q url.Values, out any) ([]byte, error) {
	endpoint := p.apiBase + path
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request %s: %v", ErrUpstream, path, err)
	}

	client := p.http
	if client == nil {
		client = wechatHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request %s: %v", ErrUpstream, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, wechatMaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s response: %v", ErrUpstream, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("%w: %s returned http %d (%d bytes)", ErrUpstream, path, resp.StatusCode, len(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return raw, fmt.Errorf("%w: decode %s response: %v", ErrUpstream, path, err)
	}
	return raw, nil
}

// wechatFlexInt64 兼容数字与字符串两种 JSON 编码。
//
// 微信多数接口把数字写成数字，但历史上存在把 expires_in / errcode 写成字符串的响应，
// 统一解析避免「字段类型一变就整体失败」。
type wechatFlexInt64 int64

// UnmarshalJSON 实现 json.Unmarshaler。
func (n *wechatFlexInt64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("oauthkit: wechat cannot parse integer %q: %w", s, err)
	}
	*n = wechatFlexInt64(v)
	return nil
}

// wechatErrResponse 只含错误字段的响应，**专供 sns/auth 使用**。
//
// 这里必须是 ErrCode 指针 + 缺失即失败，理由是这个端点的特殊性：
// sns/auth 的成功体明确是 {"errcode":0,"errmsg":"ok"}（docs/14 §1.2.5），
// 所以「有 errcode 字段」本身就是「这是一条微信业务响应」的证据。字段缺失
// 意味着上游给了别的东西（网关错误页 / 空 body / 格式变更），此时**必须拒绝**。
//
// 若在这里放行，`{}` / `null` 会被判校验通过，ValidateToken 会给客户端伪造的
// token 盖上 Verified 章 —— 直接绕过 UserInfo 的 ErrTokenNotVerified 硬约束，
// 等于把 app 渠道的账号防伪整条拆掉。这是全文件唯一不能宽容的地方。
//
// 注意：换 token / userinfo 的成功响应体**没有** errcode 字段（官方样例如此），
// 所以那两个响应类型不能用这套判据，它们各自靠业务字段校验兜底。
type wechatErrResponse struct {
	ErrCode *wechatFlexInt64 `json:"errcode"`
	ErrMsg  string           `json:"errmsg"`
}

// err 把 errcode 非 0 的响应转成哨兵错误；0 表示成功；字段缺失按 ErrUpstream 拒绝。
func (r wechatErrResponse) err() error {
	if r.ErrCode == nil {
		return fmt.Errorf("%w: wechat response missing errcode field (errmsg=%q)", ErrUpstream, r.ErrMsg)
	}
	code := int(*r.ErrCode)
	if code == 0 {
		return nil
	}
	return wechatErrf(code, r.ErrMsg)
}

// wechatTokenResponse 换 token / 刷新 token 共用的响应结构（两接口字段一致）。
//
// ErrCode 用值类型：**微信换 token 的成功响应体里根本没有 errcode 字段**
// （官方样例见 docs/14 §1.2.2，只有 access_token/expires_in/... 六个业务字段），
// 失败时才带 errcode。所以「缺失」= 成功，不能按字段缺失拒绝 —— 这一点与
// sns/auth 相反（那边成功体明确是 {"errcode":0,"errmsg":"ok"}）。
//
// 相应的空响应防护不靠 errcode，而是靠业务字段校验：Exchange / Refresh 都会检查
// access_token 非空，`{}` / `null` 这类响应因此仍被 ErrUpstream 拒掉。
type wechatTokenResponse struct {
	AccessToken  string          `json:"access_token"`
	ExpiresIn    wechatFlexInt64 `json:"expires_in"`
	RefreshToken string          `json:"refresh_token"`
	OpenID       string          `json:"openid"`
	Scope        string          `json:"scope"`
	UnionID      string          `json:"unionid"`
	ErrCode      wechatFlexInt64 `json:"errcode"`
	ErrMsg       string          `json:"errmsg"`
}

// err 把 errcode 非 0 的响应转成哨兵错误；0 或缺省都表示成功。
func (r wechatTokenResponse) err() error {
	code := int(r.ErrCode)
	if code == 0 {
		return nil
	}
	return wechatErrf(code, r.ErrMsg)
}

// token 把响应归一化为 Token。
//
// expires_in 一律用响应值，不硬编码 7200（官方两渠道数值相同但语义仍属响应字段）。
func (r wechatTokenResponse) token(channel string) *Token {
	tok := &Token{
		AccessToken:  strings.TrimSpace(r.AccessToken),
		RefreshToken: strings.TrimSpace(r.RefreshToken),
		ExpiresIn:    int64(r.ExpiresIn),
		Scope:        strings.TrimSpace(r.Scope),
		OpenID:       strings.TrimSpace(r.OpenID),
		UnionID:      strings.TrimSpace(r.UnionID),
		Channel:      channel,
	}
	if tok.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return tok
}

// wechatUserInfoResponse sns/userinfo 响应。
//
// ErrCode 与 wechatTokenResponse 一致用值类型：官方成功响应样例（docs/14 §1.2.4）
// 只有 openid/nickname/headimgurl/... 等业务字段，没有 errcode；失败时才带。
// 空响应防护靠 UserInfo 侧的「业务字段全空」检查，不靠 errcode 是否存在。
type wechatUserInfoResponse struct {
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	HeadImgURL string `json:"headimgurl"`
	UnionID    string `json:"unionid"`

	// 以下四项自 2021-10-20 起官方不再返回（§1.4 坑 4），
	// 解析成可空字段仅为兼容旧响应，不进入 User，更不落库。
	Sex      wechatFlexInt64 `json:"sex"`
	Province string          `json:"province"`
	City     string          `json:"city"`
	Country  string          `json:"country"`

	ErrCode wechatFlexInt64 `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
}

// err 把 errcode 非 0 的响应转成哨兵错误；0 或缺省都表示成功。
func (r wechatUserInfoResponse) err() error {
	code := int(r.ErrCode)
	if code == 0 {
		return nil
	}
	return wechatErrf(code, r.ErrMsg)
}

// empty 判断是否为「零值响应」（非微信业务响应的特征）。
//
// 用于拦截 `{}` / `null` 这类会被 json.Unmarshal 静默解成零值结构的响应：
// 若放行，UserInfo 会返回昵称与头像都是空串的 User，调用方拿它建号或覆盖
// 已有昵称 → 静默污染用户数据，比直接报错难查得多。
func (r wechatUserInfoResponse) empty() bool {
	return r.OpenID == "" && r.Nickname == "" && r.HeadImgURL == "" && r.UnionID == ""
}

// wechatErrorSentinel 把微信全局返回码映射为哨兵错误。
//
// 映射依据 docs/14 §1.3 全表 + §8.1.2 移动应用补充码：
//
//	40001 invalid credential      → ErrCredentialBad（appsecret 错 / token 不是最新）
//	40003 invalid openid          → ErrTokenMismatch
//	40013 invalid appid           → ErrCredentialBad
//	40014 invalid access_token    → ErrTokenExpired
//	40029 invalid code            → ErrCodeInvalid
//	40030 invalid refresh_token   → ErrRefreshDead
//	40066 invalid url             → ErrCredentialBad（回调域名未登记）
//	40163 code been used          → ErrCodeInvalid
//	40164 ip not in whitelist     → ErrCredentialBad
//	41002 appid missing           → ErrNotConfigured
//	41003 refresh_token missing   → ErrRefreshDead
//	41004 appsecret missing       → ErrNotConfigured
//	41008 missing code            → ErrCodeInvalid
//	42001 access_token expired    → ErrTokenExpired
//	42002 refresh_token expired   → ErrRefreshDead
//	48001 api unauthorized        → ErrScopeMissing
//
// 其余（-1 系统繁忙 / 40002 grant_type 非法 / 41001 / 41009 缺参数 /
// 45009 日限额 / 45011 分钟限频 / 10060 未上架应用日限额 / 未知码）统一按
// ErrUpstream 处理，并保留原始 errcode 便于排错。
func wechatErrorSentinel(code int) error {
	switch code {
	case 40029, 40163, 41008:
		return ErrCodeInvalid
	case 40030, 42002, 41003:
		return ErrRefreshDead
	case 40001, 40013, 40066, 40164:
		return ErrCredentialBad
	case 40014, 42001:
		return ErrTokenExpired
	case 40003:
		return ErrTokenMismatch
	case 41002, 41004:
		return ErrNotConfigured
	case 48001:
		return ErrScopeMissing
	default:
		return ErrUpstream
	}
}

// wechatErrf 构造带哨兵错误的微信错误，保留原始码便于排错。
//
// 调用方用 errors.Is 判断哨兵；错误文本里的 errcode / errmsg 原样保留。
func wechatErrf(code int, msg string) error {
	return fmt.Errorf("%w: wechat errcode=%d errmsg=%s", wechatErrorSentinel(code), code, msg)
}

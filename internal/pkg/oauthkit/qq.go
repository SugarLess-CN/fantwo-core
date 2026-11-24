// Package oauthkit QQ（QQ 互联）实现。
//
// # 一个平台混用三种响应格式 —— 本实现唯一真正的难点
//
//	oauth2.0/token       → 裸 query string：`access_token=...&expires_in=...&refresh_token=...`
//	                       （既不是 JSON，也不是表单体；没有 Content-Type 保证）
//	oauth2.0/me          → JSONP：`callback( {...} );`（传 fmt=json 时为纯 JSON，但官方从未给纯 JSON 样例）
//	user/get_user_info   → 标准 JSON，但错误用 `ret`/`msg`（不是 errcode/errmsg），且 ret != 0 时 HTTP 仍 200
//
// 依据：docs/14-第三方登录调研.md §2（端点全表 + 三种响应格式原文 + 错误码全表）
// 与 §8.2（网页端 / 移动端形态对比）。三个解析器都写成幂等纯函数，便于单测：
//
//	qqParseTokenBody(body []byte) (*qqTokenPayload, error)  // query string 与 JSON 双吃
//	stripJSONP(body []byte) []byte                          // JSONP 剥壳 + 清尾逗号 + 压空白 → 干净 JSON
//	qqCodeError(code, desc) error                           // 100000-100068 / 110401-110510 映射
//
// # 渠道差异（docs/14 §8.2.1，不许为「接口好看」抹平）
//
//	网页端  后端用自己的 appkey 换 token（60 天）+ me 取 openid → token 天然可信
//	移动端  SDK 直接给 access_token + openid（90 天，无 refresh_token），后端收不到 code
//	        → token 由客户端上报，可信度为零，必须 ValidateToken（me 校验 + openid 比对）
//
// 有效期一律读响应里的 expires_in，不硬编码天数（两套口径都真实存在，见 §2.4 第 5 条）。
package oauthkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QQ 互联开放平台根地址。四个端点都在同一 host 下，测试时整段替换即可。
const qqDefaultBaseURL = "https://graph.qq.com"

// QQ 端点路径（与官方 WIKI 逐字一致，不要改）。
const (
	qqPathAuthorize = "/oauth2.0/authorize" // 授权页（网页端）
	qqPathToken     = "/oauth2.0/token"     // 换 token / 刷新 token（同端点，靠 grant_type 区分）
	qqPathMe        = "/oauth2.0/me"        // 取 openid（JSONP）；app 端用于校验 token
	qqPathUserInfo  = "/user/get_user_info" // 用户信息（JSON，错误走 ret/msg）
)

// qqHTTPTimeout QQ 接口整体超时。
const qqHTTPTimeout = 15 * time.Second

// qqMaxRespBytes 响应体上限 1 MiB，防止异常响应打爆内存。
const qqMaxRespBytes = 1 << 20

var _ Provider = (*qqProvider)(nil)

// qqProvider QQ 实现，一个实例对应一个渠道（web / app）。
type qqProvider struct {
	channel string
	cfg     Config
	// baseURL 默认 https://graph.qq.com；单测指向 httptest.Server。
	baseURL string
	// httpClient 默认 15s 超时；单测用 httptest 的 client。
	httpClient *http.Client
}

// newQQ 构造 QQ Provider。channel 由 New() 校验过（web / app）。
func newQQ(channel string, cfg Config) *qqProvider {
	return &qqProvider{
		channel:    channel,
		cfg:        cfg,
		baseURL:    qqDefaultBaseURL,
		httpClient: &http.Client{Timeout: qqHTTPTimeout},
	}
}

// Name 平台标识。
func (p *qqProvider) Name() string { return ProviderQQ }

// Channel 渠道标识。
func (p *qqProvider) Channel() string { return p.channel }

// Configured 是否配置齐全。
//
// 网页端需要 appid + appkey（换 token 必须带 appkey）；
// 移动端只需要 appid —— SDK 自己换 token，后端不做换票，用不到 appkey（少一个泄漏面）。
func (p *qqProvider) Configured() bool {
	if p.cfg.ClientID == "" {
		return false
	}
	if p.channel == ChannelApp {
		return true
	}
	return p.cfg.ClientSecret != ""
}

// AuthURL 生成授权跳转 URL（仅网页端）。
//
// 移动端由客户端 SDK（Tencent.login）自己拉登录页，后端没有 URL 可给 → ErrAuthURLUnsupported。
func (p *qqProvider) AuthURL(state string) (string, error) {
	if p.channel != ChannelWeb {
		return "", ErrAuthURLUnsupported
	}
	if !p.Configured() {
		return "", ErrNotConfigured
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURI)
	// state 官方标「必须」（GitHub 只是强烈建议），回调原样带回，调用方必须校验。
	q.Set("state", state)
	if p.cfg.Scope != "" {
		q.Set("scope", p.cfg.Scope)
	}
	return p.baseURL + qqPathAuthorize + "?" + q.Encode(), nil
}

// Exchange 用 authorization code 换 token（仅网页端）。
//
// 两步：① GET oauth2.0/token（裸 query string）→ ② 若响应里没带 openid，再 GET oauth2.0/me。
// 请求里带 need_openid=1 让 token 接口尽量顺带返回 openid，省一次往返；拿不到再兜底调 me。
func (p *qqProvider) Exchange(ctx context.Context, code string) (*Token, error) {
	if p.channel != ChannelWeb {
		// 移动端后端根本收不到 code（SDK 内部已换票）→ 明确拒绝，不要让调用方误以为能用。
		return nil, ErrExchangeUnsupported
	}
	if !p.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(code) == "" {
		return nil, ErrCodeInvalid
	}

	q := url.Values{}
	q.Set("grant_type", "authorization_code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("client_secret", p.cfg.ClientSecret)
	q.Set("code", code)
	if p.cfg.RedirectURI != "" {
		// 必须与授权那一步完全一致，否则 100010 回调地址不合法。
		q.Set("redirect_uri", p.cfg.RedirectURI)
	}
	q.Set("need_openid", "1")
	// 带 fmt=json 规避裸 query string；解析层仍然双吃，不依赖这一参数（官方没保证全端点支持）。
	q.Set("fmt", "json")

	body, err := p.get(ctx, qqPathToken, q)
	if err != nil {
		return nil, err
	}
	payload, err := qqParseTokenBody(body)
	if err != nil {
		return nil, err
	}
	if err := payload.errOrNil(); err != nil {
		return nil, err
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("%w: qq token response has no access_token", ErrUpstream)
	}

	tok := &Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresIn:    int64(payload.ExpiresIn),
		Scope:        p.cfg.Scope,
		OpenID:       payload.OpenID,
		Channel:      ChannelWeb,
		Raw:          body,
	}
	tok.ExpiresAt = qqExpiresAt(tok.ExpiresIn)

	// token 接口没带 openid → 再调 me 取（官方两种形态都可能出现）。
	if tok.OpenID == "" {
		me, err := p.fetchMe(ctx, tok.AccessToken, false)
		if err != nil {
			return nil, err
		}
		tok.OpenID = me.OpenID
		tok.UnionID = me.UnionID
	}
	if tok.OpenID == "" {
		return nil, fmt.Errorf("%w: qq me response has no openid", ErrUpstream)
	}
	// 网页端 token 是后端用 appkey 换来的，视为已校验。
	tok.Verified = true
	return tok, nil
}

// ValidateToken 校验 access_token 并归一化。
//
// 这是移动端渠道的核心能力（也是网页端的加保险动作）：
//  1. 调 oauth2.0/me 校验 token 有效性（顺带拿到该 token 真正对应的 openid）；
//  2. 比对 me 返回的 openid 与 tok.OpenID —— 不一致说明是「A 的 token 配 B 的 openid」，
//     客户端伪造组合，必须拒绝（ErrTokenMismatch）；
//  3. 通过后置 Verified=true，UserInfo 才肯放行。
//
// tok 会被就地补全（OpenID / UnionID / Channel）并置 Verified，同时作为返回值。
func (p *qqProvider) ValidateToken(ctx context.Context, tok *Token) (*Token, error) {
	if !p.Configured() {
		return nil, ErrNotConfigured
	}
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil, ErrTokenExpired
	}

	me, err := p.fetchMe(ctx, tok.AccessToken, false)
	if err != nil {
		return nil, err
	}
	if me.OpenID == "" {
		return nil, fmt.Errorf("%w: qq me response has no openid", ErrUpstream)
	}
	// 客户端上报了 openid 就必须一致；上报为空则以 me 的权威值为准（me 是 QQ 侧返回的）。
	if tok.OpenID != "" && tok.OpenID != me.OpenID {
		return nil, fmt.Errorf("%w: qq me openid mismatch", ErrTokenMismatch)
	}
	tok.OpenID = me.OpenID
	if me.UnionID != "" {
		tok.UnionID = me.UnionID
	}
	if tok.Channel == "" {
		tok.Channel = p.channel
	}
	tok.Verified = true
	return tok, nil
}

// UserInfo 拉取归一化用户信息（两个渠道同一端点、同一套参数）。
//
// 安全硬约束：移动端的 token 由客户端上报，未经过 ValidateToken 的组合一律拒绝 ——
// 否则客户端可以拿自己的 token 配别人的 openid 直接建号（account takeover）。
func (p *qqProvider) UserInfo(ctx context.Context, tok *Token) (*User, error) {
	if !p.Configured() {
		return nil, ErrNotConfigured
	}
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return nil, ErrTokenExpired
	}
	// 只要「实例是 app 渠道」或「token 标记为 app 渠道」，就必须先校验过。
	if p.channel == ChannelApp || tok.Channel == ChannelApp {
		if !tok.Verified {
			return nil, ErrTokenNotVerified
		}
	}
	if tok.OpenID == "" {
		// 本端点通用参数三个必传，openid 由 Exchange / ValidateToken 填好。
		return nil, fmt.Errorf("%w: qq openid missing, exchange or validate the token first", ErrUpstream)
	}

	q := url.Values{}
	q.Set("access_token", tok.AccessToken)
	// appid 在 QQ OpenAPI 里叫 oauth_consumer_key（不是 client_id / appid），写错会得 110404。
	q.Set("oauth_consumer_key", p.cfg.ClientID)
	q.Set("openid", tok.OpenID)

	body, err := p.get(ctx, qqPathUserInfo, q)
	if err != nil {
		return nil, err
	}

	var ui qqUserInfoResponse
	if err := json.Unmarshal(stripJSONP(body), &ui); err != nil {
		return nil, fmt.Errorf("%w: qq user info decode: %v", ErrUpstream, err)
	}
	// ret != 0 时 HTTP 仍可能是 200（docs/14 §2.4 第 3 条），必须判 ret。
	if ret := int64(ui.Ret); ret != 0 {
		return nil, qqCodeError(ret, ui.Msg)
	}
	if ui.Nickname == "" && ui.AvatarURL() == "" {
		return nil, fmt.Errorf("%w: qq user info empty", ErrUpstream)
	}

	return &User{
		Provider:  ProviderQQ,
		ID:        tok.OpenID,
		UnionID:   tok.UnionID,
		Nickname:  ui.Nickname,
		AvatarURL: ui.AvatarURL(),
	}, nil
}

// Refresh 刷新 token（仅网页端）。
//
// 续期规则：refresh_token 一次有效，每次返回新的，整个续票链最长 3 个月
// （所以必须用响应原值覆盖，失败则 ErrRefreshDead 让用户重新授权）。
// 移动端 SDK 不返回 refresh_token，token 过期只能重新登录 → ErrRefreshUnsupported。
func (p *qqProvider) Refresh(ctx context.Context, tok *Token) (*Token, error) {
	if p.channel != ChannelWeb {
		return nil, ErrRefreshUnsupported
	}
	if !p.Configured() {
		return nil, ErrNotConfigured
	}
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, ErrRefreshDead
	}

	q := url.Values{}
	q.Set("grant_type", "refresh_token")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("client_secret", p.cfg.ClientSecret)
	q.Set("refresh_token", tok.RefreshToken)
	q.Set("fmt", "json")

	body, err := p.get(ctx, qqPathToken, q)
	if err != nil {
		return nil, err
	}
	payload, err := qqParseTokenBody(body)
	if err != nil {
		return nil, err
	}
	if err := payload.errOrNil(); err != nil {
		return nil, err
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("%w: qq refresh response has no access_token", ErrUpstream)
	}

	out := &Token{
		AccessToken: payload.AccessToken,
		// 官方每次返回最新 refresh_token；若上游没给（异常形态），保留旧值不丢。
		RefreshToken: qqFirstNonEmpty(payload.RefreshToken, tok.RefreshToken),
		ExpiresIn:    int64(payload.ExpiresIn),
		Scope:        tok.Scope,
		OpenID:       tok.OpenID,
		UnionID:      tok.UnionID,
		Channel:      ChannelWeb,
		// 刷新走的是自己的 appkey，token 仍属本方换取，但强制要求重新 ValidateToken
		// 没有成本，这里保守置 false，由调用方决定是否再校验。
		Verified: false,
		Raw:      body,
	}
	if out.Scope == "" {
		out.Scope = p.cfg.Scope
	}
	out.ExpiresAt = qqExpiresAt(out.ExpiresIn)
	return out, nil
}

// ---- 内部实现 ----

// get 发起一次 GET 并读回响应体（限 1 MiB）。
func (p *qqProvider) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	endpoint := p.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: qq request build: %v", ErrUpstream, err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: qq request %s: %v", ErrUpstream, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, qqMaxRespBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: qq read body %s: %v", ErrUpstream, path, err)
	}
	// QQ 的业务错误全在 body 里，正常都回 200；非 2xx 一定是网关层问题。
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: qq http %d on %s", ErrUpstream, resp.StatusCode, path)
	}
	return body, nil
}

// qqMeResponse oauth2.0/me 响应（JSONP 或纯 JSON，剥壳后同一结构）。
type qqMeResponse struct {
	ClientID         string    `json:"client_id"`
	OpenID           string    `json:"openid"`
	UnionID          string    `json:"unionid"`
	Error            qqFlexInt `json:"error"`
	ErrorDescription string    `json:"error_description"`
}

// fetchMe 调 oauth2.0/me：校验 access_token 并取该 token 真正对应的 openid。
//
// wantUnionID 传 true 会带 unionid=1，但需先在 connect.qq.com 申请 UnionID 权限，
// 否则返回 100048（→ ErrScopeMissing）。默认不请求，避免把「没权限」变成登录失败。
func (p *qqProvider) fetchMe(ctx context.Context, accessToken string, wantUnionID bool) (*qqMeResponse, error) {
	q := url.Values{}
	q.Set("access_token", accessToken)
	// fmt=json 让官方尽量回纯 JSON；解析层对 JSONP 同样兼容（剥壳幂等），不依赖它。
	q.Set("fmt", "json")
	if wantUnionID {
		q.Set("unionid", "1")
	}

	body, err := p.get(ctx, qqPathMe, q)
	if err != nil {
		return nil, err
	}

	var me qqMeResponse
	// 错误体样例 `callback( {\n"error":100016,\n"error_description":"...",\n} );`
	// 带换行与尾逗号，不是合法 JSON —— stripJSONP 负责剥壳、清尾逗号并压掉空白。
	if err := json.Unmarshal(stripJSONP(body), &me); err != nil {
		return nil, fmt.Errorf("%w: qq me decode: %v", ErrUpstream, err)
	}
	if code := int64(me.Error); code != 0 {
		return nil, qqCodeError(code, me.ErrorDescription)
	}
	return &me, nil
}

// qqUserInfoResponse user/get_user_info 响应。
//
// 只解需要的字段：官方样例字段远多于我们落库所需（pay_token / pf / pfkey 之类不落库），
// 且不同渠道返回字段集合不同（移动端 SDK 内部走简化版，没有 province/city），
// 因此绝不能用 DisallowUnknownFields，缺失字段一律按空值处理。
type qqUserInfoResponse struct {
	Ret          qqFlexInt `json:"ret"`
	Msg          string    `json:"msg"`
	Nickname     string    `json:"nickname"`
	Gender       string    `json:"gender"`
	FigureURL    string    `json:"figureurl"`      // 30×30
	FigureURL1   string    `json:"figureurl_1"`    // 50×50
	FigureURL2   string    `json:"figureurl_2"`    // 100×100
	FigureURLQQ  string    `json:"figureurl_qq"`   // QQ 空间头像
	FigureURLQQ1 string    `json:"figureurl_qq_1"` // 40×40
	FigureURLQQ2 string    `json:"figureurl_qq_2"` // 100×100（首选）
}

// AvatarURL 按优先级挑头像：figureurl_qq_2（100×100）> figureurl_2 > figureurl_qq_1 > figureurl_1 > figureurl。
func (r *qqUserInfoResponse) AvatarURL() string {
	for _, u := range []string{r.FigureURLQQ2, r.FigureURL2, r.FigureURLQQ1, r.FigureURL1, r.FigureURLQQ, r.FigureURL} {
		if u != "" {
			return u
		}
	}
	return ""
}

// qqTokenPayload oauth2.0/token 响应，兼容三种可能的形态：
//   - 裸 query string（官方默认，`access_token=...&expires_in=...`）
//   - 纯 JSON（fmt=json）
//   - JSONP 包裹的错误体（callback( {...} )，尾逗号）
type qqTokenPayload struct {
	AccessToken      string    `json:"access_token"`
	ExpiresIn        qqFlexInt `json:"expires_in"`
	RefreshToken     string    `json:"refresh_token"`
	OpenID           string    `json:"openid"`
	Error            qqFlexInt `json:"error"`
	ErrorDescription string    `json:"error_description"`
	Ret              qqFlexInt `json:"ret"`
	Msg              string    `json:"msg"`
}

// errOrNil 把响应里的错误字段翻译成哨兵错误；无错返回 nil。
//
// QQ 的 token 端点出错时字段名不统一（`error`/`error_description` 与 `ret`/`msg` 都出现过），
// 两个通道都判一遍。
func (t *qqTokenPayload) errOrNil() error {
	if code := int64(t.Error); code != 0 {
		return qqCodeError(code, t.ErrorDescription)
	}
	if code := int64(t.Ret); code != 0 {
		return qqCodeError(code, t.Msg)
	}
	return nil
}

// qqParseTokenBody 解析 oauth2.0/token 的响应体（裸 query string / JSON 双吃）。
//
// 这是 QQ 分支最关键的一个纯函数：官方默认返回的整段 body 就是 `a=1&b=2`，
// 既不是 JSON 也不是带 Content-Type 的表单体 → 用 url.ParseQuery 解。
func qqParseTokenBody(body []byte) (*qqTokenPayload, error) {
	raw := stripJSONP(body)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: qq token response empty", ErrUpstream)
	}

	// 形态一：JSON（fmt=json，或被 callback 包裹的错误体剥壳后）
	if trimmed[0] == '{' {
		var payload qqTokenPayload
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, fmt.Errorf("%w: qq token json decode: %v", ErrUpstream, err)
		}
		return &payload, nil
	}

	// 形态二：裸 query string
	values, err := url.ParseQuery(string(trimmed))
	if err != nil {
		return nil, fmt.Errorf("%w: qq token body is neither json nor query string: %v", ErrUpstream, err)
	}
	payload := &qqTokenPayload{
		AccessToken:      values.Get("access_token"),
		RefreshToken:     values.Get("refresh_token"),
		OpenID:           values.Get("openid"),
		ErrorDescription: values.Get("error_description"),
	}
	if payload.ErrorDescription == "" {
		payload.ErrorDescription = values.Get("msg")
	}
	payload.Error = qqParseFlexInt(values.Get("error"))
	payload.Ret = qqParseFlexInt(values.Get("ret"))
	payload.ExpiresIn = qqParseFlexInt(values.Get("expires_in"))
	return payload, nil
}

// qqTrailingComma 去掉 JSON 对象 / 数组结尾多余的逗号（官方 JSONP 错误体样例带尾逗号）。
var qqTrailingComma = regexp.MustCompile(`,\s*([}\]])`)

// stripJSONP 幂等剥壳，返回**干净 JSON**：无 `callback(` 包裹时原样可解（fmt=json 的纯 JSON）。
//
// 官方样例：`callback( {"client_id":"...","openid":"..."} );`
// 错误样例（带换行、缩进与尾逗号，严格 JSON 解析必失败）：
//
//	callback(
//	{
//	"error":100016,
//	"error_description":"access token check failed",
//	}
//	);
//
// 处理顺序（顺序不能反）：
//  1. 剥掉 `callback(` 与末尾 `)`（不含包裹时跳过）；
//  2. **TrimSpace** —— 首尾空白（含 \n / \t）先清掉，否则第 3 步的正则看不到 `"…",\n\t}` 这类形态；
//  3. 清尾逗号（`,\s*}` / `,\s*]`），对上例的 `,\n}` 同样生效；
//  4. 用 json.Compact 压掉所有无语义空白（字符串内部的空格/换行原样保留）——
//     微信/QQ 的 JSONP 错误体常带换行与缩进，函数契约是「返回干净 JSON」，不能把这些交给下游。
//
// 不是合法 JSON 时（例如上游返回纯文本）只做 TrimSpace 后原样返回，不吞掉内容。
func stripJSONP(body []byte) []byte {
	s := strings.TrimSpace(string(body))
	if i := strings.Index(s, "callback("); i >= 0 {
		s = strings.TrimSpace(s[i+len("callback("):])
		if j := strings.LastIndex(s, ")"); j >= 0 {
			s = s[:j]
		}
	}
	// 先 trim 再清尾逗号：`"…",\n\t}` 这种形态必须先去空白才落进正则
	s = strings.TrimSpace(qqTrailingComma.ReplaceAllString(strings.TrimSpace(s), "$1"))
	if s == "" {
		return []byte(s)
	}
	// 压缩无语义空白（json.Compact 尊重字符串字面量，不会改动 "a b" 里的空格）
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err == nil {
		return bytes.TrimSpace(buf.Bytes())
	}
	return []byte(s)
}

// qqFlexInt 兼容 JSON 里「数字」与「带引号字符串」两种形态的整型。
//
// 移动端 SDK 登录回调里 expires_in 是字符串 `"7776000"`，网页端 token 接口是数字 `5184000`，
// 错误码在 JSONP 里是数字、偶尔被拼成字符串 —— 一并兜住（docs/14 §8.2.2 实现要点 1）。
type qqFlexInt int64

// UnmarshalJSON 数字 / 字符串 / null 都能解；非数字形态按 0 处理，
// 不让一个次要字段毁掉整段解析（必填字段仍会在判空时暴露）。
func (v *qqFlexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		*v = 0
		return nil
	}
	*v = qqFlexInt(n)
	return nil
}

// qqParseFlexInt 把 query string 里的字符串值转成 qqFlexInt（空串 / 非数字 → 0）。
func qqParseFlexInt(s string) qqFlexInt {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return qqFlexInt(n)
}

// qqExpiresAt 由有效期秒数推导到期时间；<=0 返回零值表示「上游未给有效期」。
func qqExpiresAt(expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

// qqFirstNonEmpty 返回第一个非空串。
func qqFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// qqCodeError 把 QQ 互联公共返回码映射成哨兵错误（docs/14 §2.3 全表）。
//
// 判定口径按官方语义，不按「听起来像哪类」：
//   - 100013/100014/100015/100016 都是 access_token 层面的问题 → 重新登录（ErrTokenExpired）；
//   - 100020 是 code 被重复使用（一次性），语义是「重新登录且不要重试 token 接口」→ ErrCodeInvalid；
//   - 100021/100036~100039/100042 是 refresh_token 层面的问题 → ErrRefreshDead；
//   - 100048/100049 是 unionid 权限未申请 → ErrScopeMissing；
//   - 其余按「凭据 / 作用域 / openid 不匹配 / 上游异常」归类，未列举的一律 ErrUpstream。
func qqCodeError(code int64, desc string) error {
	switch code {
	case 0:
		return nil

	// code 层面：让用户重新登录，且不要重试 token 接口
	case 100018, 100019, 100020, 100067, 100068:
		return qqWrapCode(ErrCodeInvalid, code, desc)

	// access_token 层面：失效 / 过期 / 废除 / 验证失败 / 用户改密 → 重新授权
	case 100013, 100014, 100015, 100016, 100045:
		return qqWrapCode(ErrTokenExpired, code, desc)

	// refresh_token 层面：已废 / 过期 / 撤销 / 达上限 / 非法 → 重新授权
	case 100021, 100022, 100036, 100037, 100038, 100039, 100042:
		return qqWrapCode(ErrRefreshDead, code, desc)

	// 应用凭据被拒：appid 不存在 / appkey 非法 / 参数缺 client_id·client_secret
	case 100001, 100002, 100008, 100009, 100040, 100041, 110401, 110404:
		return qqWrapCode(ErrCredentialBad, code, desc)

	// 作用域 / 权限不足：用户未授权该 api、应用无该 api 权限、unionid 权限未申请
	case 100030, 100031, 100048, 100049, 100052, 100057, 100066:
		return qqWrapCode(ErrScopeMissing, code, desc)

	// openid 与 appid 不匹配 / openid 非法 → 客户端伪造组合的特征
	case 100050, 100058:
		return qqWrapCode(ErrTokenMismatch, code, desc)
	}

	// 未知码 / 其余上游异常（110405 登录受限、110406 未过审、110500~110510、110407 等文档未列码）
	return fmt.Errorf("%w: qq ret=%d msg=%s", ErrUpstream, code, desc)
}

// qqWrapCode 在哨兵错误上附上原始码与描述，保留 errors.Is 语义。
func qqWrapCode(sentinel error, code int64, desc string) error {
	if desc == "" {
		return fmt.Errorf("%w: qq ret=%d", sentinel, code)
	}
	return fmt.Errorf("%w: qq ret=%d msg=%s", sentinel, code, desc)
}

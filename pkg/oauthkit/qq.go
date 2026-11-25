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
	"math"
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
	// state 必须非空：我们的 CSRF 方案完全依赖它回传后比对。
	// 空 state 不是「QQ 允许省略」，而是调用方漏了保护 —— 静默拼一个不带 state 的 URL
	// 等于让 CSRF 保护无声失效，且这种失败在联调阶段完全看不出来（授权页照常打开）。
	//
	// 用 ErrInvalidArgument 而不是 ErrScopeMissing：后者会让调用方误判成
	// 「用户没授权范围」→ 多做一次无意义的重新授权引导。这类错误重试无用，只能改代码。
	if strings.TrimSpace(state) == "" {
		return "", ErrInvalidArgument
	}
	// redirect_uri 必填：QQ 要求它必须与开放平台登记的回调地址完全一致，
	// 空值会被上游直接拒绝（且错误码模糊，联调时极难定位）。
	// 这与 wechat.go 的 AuthURL 保持一致 —— 缺 redirect_uri 属于**配置不全**（ErrNotConfigured），
	// 不是「用户没授权」，端点应降级为 20026 让运维去补，而不是生成一个必然失败的 URL。
	if strings.TrimSpace(p.cfg.RedirectURI) == "" {
		return "", fmt.Errorf("oauthkit: qq AuthURL requires redirect_uri: %w", ErrNotConfigured)
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
	// 空 code 是我们自己的编程错误，不是「平台说 code 无效」。
	// ErrCodeInvalid 的语义是「平台侧判定 code 无效/已用」→ 调用方会引导用户重新授权；
	// 空 code 引导用户重新授权毫无意义（下次还是空），只会把调用方带进死循环。
	// 这类错误重试与重新授权都无用，只能改代码 → ErrInvalidArgument。
	if strings.TrimSpace(code) == "" {
		return nil, ErrInvalidArgument
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
	// 客户端上报了 openid 就必须与 QQ 侧返回值一致；上报为空则以 me 的权威值为准。
	//
	// 比对用**精确相等**（大小写敏感），不做 EqualFold：
	// QQ openid 是平台分配的 hex 串，官方文档未声明大小写不敏感，也没有任何「大小写等价」的约定；
	// 而 openid 是账号绑定主键 —— 放宽大小写等于把两个不同的标识折叠成一个，
	// 反而扩大「A 的 token 配 B 的 openid」的命中面。宁可让异常上报的大写形式失败，
	// 也不让绑定主键失去唯一性（客户端本应原样回传 SDK 给的值，不该有大小写漂移）。
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
	// tok 为空 / refresh_token 为空都是调用方没先完成 Exchange（或落库时丢了字段），
	// 属编程错误：带着空 token 打上游只会白耗 QQ 的限频配额，还得不到有效错误。
	// 同样不能给 ErrRefreshDead —— 那会让调用方提示用户「请重新授权」，
	// 而真正该做的是修调用链（先 Exchange 再存 refresh_token）。
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, ErrInvalidArgument
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

// ParseQQAppLoginPayload 解析 QQ 移动端 SDK 登录回调的 JSON，归一化为 Token。
//
// **为什么放在 SDK 里**：这是 QQ 平台协议解析（`expires_in` 是字符串 `"7776000"`、
// `msg` 拼成 `"sucess"`、还有 pay_token/pf/pfkey 等无关字段），属于 oauthkit 的职责；
// 让 service 层各写一遍必然分叉成两套口径。
//
// 输入是客户端 `onComplete(JSONObject)` 上报的原文（官方样例）：
//
//	{
//	  "ret":0, "pay_token":"...", "pf":"openmobile_android",
//	  "expires_in":"7776000", "openid":"...", "pfkey":"...",
//	  "msg":"sucess", "access_token":"..."
//	}
//
// 返回的 Token 恒为 Channel=app、Verified=**false** —— 调用方**必须**再调 ValidateToken：
// 这份 payload 来自客户端、完全不可信，ValidateToken 会用 oauth2.0/me 校验
// access_token 并比对 openid，通过后才允许 UserInfo。
//
// 错误：ret != 0 → 对应哨兵错误（按 QQ 公共返回码表）；
// access_token / openid 缺失 → ErrUpstream；JSON 非法或 expires_in 非整数 → ErrUpstream。
// 不对 msg 做任何字符串断言（官方样例是 "sucess"，漏一个 c）。
func ParseQQAppLoginPayload(raw []byte) (*Token, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: qq app login payload empty", ErrUpstream)
	}
	if trimmed[0] != '{' {
		// SDK 回调应当是 JSONObject；非 JSON 形态不猜，直接拒。
		return nil, fmt.Errorf("%w: qq app login payload is not json (%d bytes)", ErrUpstream, len(trimmed))
	}

	var payload qqTokenPayload
	// 不用 DisallowUnknownFields：SDK 字段多于所需（pay_token/pf/pfkey），必须容忍。
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, fmt.Errorf("%w: qq app login payload decode: %v", ErrUpstream, err)
	}
	// ret != 0 直接失败（SDK 层错误含用户拒绝、无权限等，靠 msg 定位）。
	if err := payload.errOrNil(); err != nil {
		return nil, err
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("%w: qq app login payload has no access_token", ErrUpstream)
	}
	if payload.OpenID == "" {
		// openid 是账号绑定主键，缺了就没有意义（也防止把空 ID 建成账号）。
		return nil, fmt.Errorf("%w: qq app login payload has no openid", ErrUpstream)
	}

	tok := &Token{
		AccessToken: payload.AccessToken,
		// SDK 不返回 refresh_token（§8.2.1），这里恒为空。
		ExpiresIn: int64(payload.ExpiresIn),
		OpenID:    payload.OpenID,
		Channel:   ChannelApp,
		// 客户端上报 → 未校验。UserInfo 会因为 !Verified 直接拒，直到 ValidateToken 放行。
		Verified: false,
		Raw:      trimmed,
	}
	tok.ExpiresAt = qqExpiresAt(tok.ExpiresIn)
	return tok, nil
}

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
	//
	// 与下面 query string 分支同理：**能 json.Unmarshal ≠ 是 QQ 的响应**。
	// 网关把 body 换成 `{"status":"error","message":"bad gateway"}` 这类合法 JSON 时，
	// 不拦就会拿到空 payload，最终报「no access_token」——把「上游是不是挂了」
	// 误导成「QQ 为什么不给 token」。所以必须至少命中一个已知字段。
	//
	// 判定用「字段有没有值」而不是只判 AccessToken 为空：合法的错误体
	// `{"error":100016,"error_description":"..."}` 必须放过，交给 errOrNil() 映射哨兵错误。
	if trimmed[0] == '{' {
		var payload qqTokenPayload
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, fmt.Errorf("%w: qq token json decode: %v", ErrUpstream, err)
		}
		if payload.AccessToken == "" && payload.Error == 0 && payload.Ret == 0 &&
			payload.ErrorDescription == "" && payload.ExpiresIn == 0 {
			// 只报字节数不报内容：错误信息会进日志，不给它夹带明文的机会
			return nil, fmt.Errorf("%w: qq token response json has no recognized field (%d bytes)",
				ErrUpstream, len(trimmed))
		}
		return &payload, nil
	}

	// 形态二：裸 query string
	//
	// url.ParseQuery 的容错度高得危险：空串返回空 map + nil error；HTML 错误页会被解成
	// 一个奇形怪状的 key；`access_token=` 得到空值。所以「能 ParseQuery 成功」绝不等于
	// 「拿到 token」—— 必须自己判「有没有解出任何已知字段」。
	values, err := url.ParseQuery(string(trimmed))
	if err != nil {
		return nil, fmt.Errorf("%w: qq token body is neither json nor query string: %v", ErrUpstream, err)
	}

	payload := &qqTokenPayload{}
	known := 0
	for _, k := range []string{"access_token", "refresh_token", "expires_in", "openid", "error", "ret", "error_description", "msg"} {
		if values.Has(k) {
			known++
		}
	}
	// 一个已知字段都没有 → 不是 QQ 的 token 响应（典型：网关 500 的 HTML 页）。
	// 不报错的话 AccessToken 为空会一路走到「成功但没 token」，比直接失败更难排查。
	// 注意：错误信息里**只放长度不放内容** —— 这个分支理论上不含 token 字段，
	// 但错误信息是会被写日志的，不能给它任何夹带明文的机会。
	if known == 0 {
		return nil, fmt.Errorf("%w: qq token response is not a recognizable query string (%d bytes)",
			ErrUpstream, len(trimmed))
	}

	payload.AccessToken = values.Get("access_token")
	payload.RefreshToken = values.Get("refresh_token")
	payload.OpenID = values.Get("openid")
	payload.ErrorDescription = values.Get("error_description")
	if payload.ErrorDescription == "" {
		payload.ErrorDescription = values.Get("msg")
	}
	// 数值字段解析失败即报错（静默变 0 会把「上游报错」当成功，见 qqFlexInt 注释）
	for _, f := range []struct {
		key string
		dst *qqFlexInt
	}{
		{"error", &payload.Error},
		{"ret", &payload.Ret},
		{"expires_in", &payload.ExpiresIn},
	} {
		v, err := qqParseFlexInt(values.Get(f.key))
		if err != nil {
			return nil, fmt.Errorf("%w: qq token %s: %v", ErrUpstream, f.key, err)
		}
		*f.dst = v
	}
	return payload, nil
}

// qqTrailingComma 去掉 JSON 对象 / 数组结尾多余的逗号（官方 JSONP 错误体样例带尾逗号）。
var qqTrailingComma = regexp.MustCompile(`,\s*([}\]])`)

// qqJSONPCallback QQ JSONP 的固定前缀。官方样例恒为 `callback( {...} );`。
const qqJSONPCallback = "callback"

// stripJSONP 幂等剥壳，返回**干净 JSON**：无 `callback` 包裹时原样可解（fmt=json 的纯 JSON）。
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
//  1. **只认前缀** `callback`：在任意位置搜 `callback(` 会被 JSON 字符串字面量误伤 ——
//     用户昵称叫 `callback(abc` 时（user/get_user_info 完全可能出现），
//     Index 会从昵称中间截断，把合法响应打成解析失败。前缀匹配是更弱的假设，更安全。
//  2. 右边界用**括号配平**而不是 `LastIndex(")")`：`{"nickname":"a)b"}` 这类值里的 `)`
//     会被 LastIndex 误当定界符（末尾无 `)` 的畸形包裹尤其致命）；配平同时尊重字符串
//     字面量与 \" 转义，只认真正匹配的闭括号。
//  3. **TrimSpace** —— 首尾空白（含 \n / \t）先清掉，否则第 4 步的正则看不到 `"…",\n\t}` 这类形态；
//  4. 清尾逗号（`,\s*}` / `,\s*]`），对上例的 `,\n}` 同样生效；
//  5. 用 json.Compact 压掉所有无语义空白（字符串内部的空格/换行原样保留）——
//     微信/QQ 的 JSONP 错误体常带换行与缩进，函数契约是「返回干净 JSON」，不能把这些交给下游。
//
// 不是合法 JSON 时（例如上游返回纯文本 / HTML 错误页）只做 TrimSpace 后原样返回，不吞掉内容。
func stripJSONP(body []byte) []byte {
	s := strings.TrimSpace(string(body))
	// 只在前缀处认包裹；允许 `callback (` 这种中间夹空白的写法，但空白之后必须是 '('。
	if strings.HasPrefix(s, qqJSONPCallback) {
		rest := s[len(qqJSONPCallback):]
		if j := strings.IndexByte(rest, '('); j >= 0 && strings.TrimSpace(rest[:j]) == "" {
			s = strings.TrimSpace(qqCutMatchedBrackets(strings.TrimSpace(rest[j+1:])))
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

// qqCutMatchedBrackets 从 s 的第一个 `{` / `[` 起做括号配平，返回截至匹配闭括号的子串。
//
// 用于剥 JSONP 右边界：比 `strings.LastIndex(s, ")")` 安全 —— 字符串字面量里的 `)`、`}`、
// `\]` 全部跳过（含 `\"` 转义），只认结构上真正闭合的那一个。
// 不以 `{` / `[` 开头（或括号配不平，说明包裹本身畸形）时原样返回，交给后续解析报错。
func qqCutMatchedBrackets(s string) string {
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return s
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s
}

// qqFlexInt 兼容 JSON 里「数字」与「带引号字符串」两种形态的整型。
//
// 移动端 SDK 登录回调里 expires_in 是字符串 `"7776000"`，网页端 token 接口是数字 `5184000`，
// 错误码在 JSONP 里是数字、偶尔被拼成字符串 —— 一并兜住（docs/14 §8.2.2 实现要点 1）。
type qqFlexInt int64

// UnmarshalJSON 数字 / 字符串 / null / 浮点 / 科学计数都能解。
//
// **解析失败必须报错，不能静默变 0**（这一点很关键，两种情况都会造成误判）：
//   - 错误码（error / ret）静默变 0 → 把「上游报错」当成「成功」；
//   - expires_in 静默变 0 → Token.ExpiresAt 取零值（types.go 约定零值 = 不过期）
//     → 过期 token 被长期当成有效。
//
// 因此非数字形态返回 error，让整段 json.Unmarshal 失败并上抛 ErrUpstream —— 宁可拒绝也不误判。
func (v *qqFlexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	// 显式 null / 空串 = 上游没给这个字段，按 0 处理（这是「缺失」，不是「非法」）
	if s == "null" {
		*v = 0
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		*v = 0
		return nil
	}
	// 整数形态（绝大多数情况）
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*v = qqFlexInt(n)
		return nil
	}
	// 兜底：浮点 / 科学计数（7776000.0、1e6）—— 取整后仍在 int64 内才接受
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f > math.MaxInt64 || f < math.MinInt64 {
		return fmt.Errorf("oauthkit: qq integer field %q is not a valid integer", s)
	}
	*v = qqFlexInt(int64(f))
	return nil
}

// qqParseFlexInt 把 query string 里的字符串值转成 qqFlexInt。
//
// 与 UnmarshalJSON 同口径：空串 = 缺失（0，无错）；给了但不是整数 = 报错。
func qqParseFlexInt(s string) (qqFlexInt, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var v qqFlexInt
	if err := v.UnmarshalJSON([]byte(s)); err != nil {
		return 0, err
	}
	return v, nil
}

// qqMaxExpiresIn 可安全换算成 time.Duration 的最大秒数。
//
// time.Duration 是 int64 纳秒，上限约 292 年 = 9,223,372,036 秒；
// 超过这个值 `time.Duration(n) * time.Second` 会**溢出成负数**，
// ExpiresAt 直接掉到过去（token 一拿到就被当成过期）。
// 实测：9223372037 → 1734-08-16；9223372036854775807 → 当前时间前 1 秒。
const qqMaxExpiresIn int64 = math.MaxInt64 / int64(time.Second)

// qqExpiresAt 由有效期秒数推导到期时间。
//
//   - <= 0：返回零值（types.go 约定「零值表示不过期」，仅用于上游确实没给有效期）；
//   - 超过 int64 纳秒上限：截到 qqMaxExpiresIn（约 292 年），不让乘法溢出成过去时间。
func qqExpiresAt(expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	if expiresIn > qqMaxExpiresIn {
		expiresIn = qqMaxExpiresIn
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

	// 未知码 / 其余上游异常 → 一律 ErrUpstream，但**保留 ret 与 msg 原文**便于排错。
	//
	// 已确认落在这一支的码（排错时不要怀疑是自己映射漏了）：
	//   - 110405 登录请求被限制（限频，可退避重试）
	//   - 110406 应用没有通过审核（配置问题，重试无用）
	//   - **110407：官方公共返回码全表（docs/14 §2.3）里不存在这个码** ——
	//     只在个别接口文档示例中出现，语义未定义。故不做猜测性映射（别以为它
	//     等价于 110406），走兜底原样抛出，真机遇到时按当时请求参数排查。
	//   - 110500~110510 授权 / token 相关服务端失败（110501 获取应用授权信息失败、
	//     110504 系统内部错误、110510 Disable 等）
	return fmt.Errorf("%w: qq ret=%d msg=%s", ErrUpstream, code, desc)
}

// qqWrapCode 在哨兵错误上附上原始码与描述，保留 errors.Is 语义。
func qqWrapCode(sentinel error, code int64, desc string) error {
	if desc == "" {
		return fmt.Errorf("%w: qq ret=%d", sentinel, code)
	}
	return fmt.Errorf("%w: qq ret=%d msg=%s", sentinel, code, desc)
}

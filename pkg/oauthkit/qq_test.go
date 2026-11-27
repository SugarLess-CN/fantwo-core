package oauthkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// qqMockServer QQ 四个端点的 mock，响应体可按用例改写。
//
// 目的：验证三种响应格式的解析 ——
//
//	tokenBody 用裸 query string（官方默认形态）、meBody 用 JSONP（官方默认形态）、
//	userBody 用标准 JSON（错误走 ret/msg）。
type qqMockServer struct {
	srv       *httptest.Server
	tokenBody string
	meBody    string
	userBody  string
	tokenCode int // 非 0 时覆盖 HTTP 状态码
	meCode    int
	userCode  int

	lastTokenQuery url.Values
	lastMeQuery    url.Values
	lastUserQuery  url.Values
	meCalls        int
}

func newQQMockServer(t *testing.T) *qqMockServer {
	t.Helper()
	m := &qqMockServer{}
	mux := http.NewServeMux()
	mux.HandleFunc(qqPathToken, func(w http.ResponseWriter, r *http.Request) {
		m.lastTokenQuery = r.URL.Query()
		writeMock(w, m.tokenCode, m.tokenBody)
	})
	mux.HandleFunc(qqPathMe, func(w http.ResponseWriter, r *http.Request) {
		m.lastMeQuery = r.URL.Query()
		m.meCalls++
		writeMock(w, m.meCode, m.meBody)
	})
	mux.HandleFunc(qqPathUserInfo, func(w http.ResponseWriter, r *http.Request) {
		m.lastUserQuery = r.URL.Query()
		writeMock(w, m.userCode, m.userBody)
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func writeMock(w http.ResponseWriter, status int, body string) {
	if status != 0 {
		w.WriteHeader(status)
	}
	_, _ = io.WriteString(w, body)
}

// provider 构造指向 mock server 的 QQ 实例。
func (m *qqMockServer) provider(channel string, cfg Config) *qqProvider {
	p := newQQ(channel, cfg)
	p.baseURL = m.srv.URL // 四个端点都在 baseURL 下
	p.httpClient = m.srv.Client()
	return p
}

// 默认配置：web 需要 appid + appkey；app 只需 appid。
func qqWebConfig() Config {
	return Config{ClientID: "APPID", ClientSecret: "APPKEY", RedirectURI: "https://example.com/cb", Scope: DefaultScopeQQ}
}

func qqAppConfig() Config {
	return Config{ClientID: "APPID_APP"}
}

func qqJSONPError(code, desc string) string {
	// 官方原文样例：JSONP 内嵌 error 对象，且对象末尾多一个逗号 → 不是合法 JSON
	return "callback(\n{\n\"error\":" + code + ",\n\"error_description\":\"" + desc + "\",\n}\n);"
}

// ---- 1. web Exchange：裸 query string ----

func TestQQWebExchangeRawQueryString(t *testing.T) {
	m := newQQMockServer(t)
	// 官方原文样例（未指定 fmt 时，整段 body 就是 query string）
	m.tokenBody = "access_token=FE04CCE2&expires_in=5184000&refresh_token=88E4BE14"
	m.meBody = `callback( {"client_id":"APPID","openid":"OPENID_WEB"} );`

	p := m.provider(ChannelWeb, qqWebConfig())
	tok, err := p.Exchange(context.Background(), "CODE-1")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "FE04CCE2" {
		t.Errorf("AccessToken = %q, want FE04CCE2", tok.AccessToken)
	}
	if tok.ExpiresIn != 5184000 {
		t.Errorf("ExpiresIn = %d, want 5184000（60 天，读响应不硬编码）", tok.ExpiresIn)
	}
	if tok.RefreshToken != "88E4BE14" {
		t.Errorf("RefreshToken = %q, want 88E4BE14", tok.RefreshToken)
	}
	if tok.ExpiresAt.IsZero() {
		t.Error("ExpiresAt 未推导")
	}
	if tok.Channel != ChannelWeb {
		t.Errorf("Channel = %q, want web", tok.Channel)
	}
	// 换 token 的参数口径：client_id / client_secret（不是 oauth_consumer_key）
	if got := m.lastTokenQuery.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q", got)
	}
	if got := m.lastTokenQuery.Get("client_secret"); got != "APPKEY" {
		t.Errorf("client_secret = %q", got)
	}
	if got := m.lastTokenQuery.Get("redirect_uri"); got != "https://example.com/cb" {
		t.Errorf("redirect_uri = %q", got)
	}
}

// ---- 2. web Exchange：openid 缺失时回落到 me，JSONP 剥壳 ----

func TestQQWebExchangeJSONPMe(t *testing.T) {
	m := newQQMockServer(t)
	m.tokenBody = "access_token=FE04CCE2&expires_in=5184000&refresh_token=88E4BE14" // 无 openid
	m.meBody = `callback( {"client_id":"APPID","openid":"OPENID_JSONP"} );`

	p := m.provider(ChannelWeb, qqWebConfig())
	tok, err := p.Exchange(context.Background(), "CODE-2")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.OpenID != "OPENID_JSONP" {
		t.Errorf("OpenID = %q, want OPENID_JSONP（JSONP 剥壳失败）", tok.OpenID)
	}
	if !tok.Verified {
		t.Error("web token 应视为已校验")
	}
	if m.meCalls != 1 {
		t.Errorf("me 调用次数 = %d, want 1", m.meCalls)
	}
}

// ---- 3. oauth2.0/me 返回纯 JSON（fmt=json）→ 幂等剥壳同样能解 ----

func TestQQWebExchangePlainJSONMe(t *testing.T) {
	m := newQQMockServer(t)
	m.tokenBody = "access_token=FE04CCE2&expires_in=5184000&refresh_token=88E4BE14"
	m.meBody = `{"client_id":"APPID","openid":"OPENID_PLAINJSON"}`

	p := m.provider(ChannelWeb, qqWebConfig())
	tok, err := p.Exchange(context.Background(), "CODE-3")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.OpenID != "OPENID_PLAINJSON" {
		t.Errorf("OpenID = %q, want OPENID_PLAINJSON", tok.OpenID)
	}
}

// ---- 4. web Exchange 错误：ret=100019 → ErrCodeInvalid ----

func TestQQWebExchangeCodeInvalid(t *testing.T) {
	t.Run("query string 形态", func(t *testing.T) {
		m := newQQMockServer(t)
		m.tokenBody = "error=100019&error_description=code to access token error"
		p := m.provider(ChannelWeb, qqWebConfig())
		_, err := p.Exchange(context.Background(), "BAD-CODE")
		if !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("err = %v, want ErrCodeInvalid", err)
		}
	})

	t.Run("ret=100019 JSON 形态", func(t *testing.T) {
		m := newQQMockServer(t)
		m.tokenBody = `{"ret":100019,"msg":"code to access token error"}`
		p := m.provider(ChannelWeb, qqWebConfig())
		_, err := p.Exchange(context.Background(), "BAD-CODE")
		if !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("err = %v, want ErrCodeInvalid", err)
		}
	})

	t.Run("me JSONP 错误体带尾逗号 → 100016 映射 ErrTokenExpired", func(t *testing.T) {
		m := newQQMockServer(t)
		m.tokenBody = "access_token=FE04CCE2&expires_in=5184000&refresh_token=88E4BE14"
		m.meBody = qqJSONPError("100016", "access token check failed")
		p := m.provider(ChannelWeb, qqWebConfig())
		_, err := p.Exchange(context.Background(), "CODE-4")
		if !errors.Is(err, ErrTokenExpired) {
			t.Fatalf("err = %v, want ErrTokenExpired", err)
		}
	})
}

// ---- 5. app 渠道三个不支持的方法 ----

func TestQQAppChannelUnsupported(t *testing.T) {
	m := newQQMockServer(t)
	p := m.provider(ChannelApp, qqAppConfig())

	if _, err := p.AuthURL("STATE"); !errors.Is(err, ErrAuthURLUnsupported) {
		t.Errorf("AuthURL err = %v, want ErrAuthURLUnsupported", err)
	}
	if _, err := p.Exchange(context.Background(), "CODE"); !errors.Is(err, ErrExchangeUnsupported) {
		t.Errorf("Exchange err = %v, want ErrExchangeUnsupported", err)
	}
	tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_A", Channel: ChannelApp}
	if _, err := p.Refresh(context.Background(), tok); !errors.Is(err, ErrRefreshUnsupported) {
		t.Errorf("Refresh err = %v, want ErrRefreshUnsupported", err)
	}
	// 三个方法都不应发出网络请求
	if m.lastTokenQuery != nil || m.lastMeQuery != nil {
		t.Error("不支持的方法不应请求上游")
	}
}

// ---- 6/7. app ValidateToken：openid 一致 / 不一致 ----

func TestQQAppValidateTokenOK(t *testing.T) {
	m := newQQMockServer(t)
	m.meBody = `callback( {"client_id":"APPID_APP","openid":"OPENID_A"} );`

	p := m.provider(ChannelApp, qqAppConfig())
	tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_A", Channel: ChannelApp}
	got, err := p.ValidateToken(context.Background(), tok)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !got.Verified {
		t.Error("Verified 应为 true")
	}
	if got.OpenID != "OPENID_A" {
		t.Errorf("OpenID = %q", got.OpenID)
	}
	if m.lastMeQuery.Get("access_token") != "APP_TOKEN" {
		t.Errorf("me 未带上 access_token：%v", m.lastMeQuery)
	}
	if m.lastMeQuery.Get("fmt") != "json" {
		t.Errorf("me 应带 fmt=json，实际 %q", m.lastMeQuery.Get("fmt"))
	}
	if m.lastMeQuery.Get("unionid") != "" {
		t.Error("默认不应请求 unionid（未申请权限会 100048）")
	}
}

func TestQQAppValidateTokenMismatch(t *testing.T) {
	m := newQQMockServer(t)
	m.meBody = `callback( {"client_id":"APPID_APP","openid":"OPENID_REAL"} );` // token 真正属于 OPENID_REAL

	p := m.provider(ChannelApp, qqAppConfig())
	tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_FORGED", Channel: ChannelApp}
	got, err := p.ValidateToken(context.Background(), tok)
	if !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("err = %v, want ErrTokenMismatch", err)
	}
	if got != nil {
		t.Error("校验失败不应返回 token")
	}
	if tok.Verified {
		t.Error("校验失败不得置 Verified")
	}
}

// ---- 8. app 端 expires_in 是字符串 "7776000"（官方 SDK 样例） ----

func TestQQAppExpiresInFlexibleString(t *testing.T) {
	// 官方 Android SDK onComplete 回调原文样例（含 pay_token/pf/pfkey 等无关字段）
	sdkPayload := []byte(`{
	  "ret":0,
	  "pay_token":"xxxxxxxxxxxxxxxx",
	  "pf":"openmobile_android",
	  "expires_in":"7776000",
	  "openid":"OPENID_MOBILE",
	  "pfkey":"xxxxxxxxxxxxxxxxxxx",
	  "msg":"sucess",
	  "access_token":"APP_ACCESS_TOKEN"
	}`)
	payload, err := qqParseTokenBody(sdkPayload)
	if err != nil {
		t.Fatalf("qqParseTokenBody: %v", err)
	}
	if err := payload.errOrNil(); err != nil {
		t.Fatalf("errOrNil: %v（msg 是 \"sucess\"，不许做字符串断言）", err)
	}
	if int64(payload.ExpiresIn) != 7776000 {
		t.Errorf("ExpiresIn = %d, want 7776000（90 天，字符串形态）", int64(payload.ExpiresIn))
	}
	if payload.OpenID != "OPENID_MOBILE" || payload.AccessToken != "APP_ACCESS_TOKEN" {
		t.Errorf("openid/token 解析错误：%+v", payload)
	}

	// 数字形态同样要能吃（网页端 token 接口是 5184000，无引号）
	numPayload, err := qqParseTokenBody([]byte(`{"access_token":"T","expires_in":5184000,"refresh_token":"R"}`))
	if err != nil {
		t.Fatalf("qqParseTokenBody(number): %v", err)
	}
	if int64(numPayload.ExpiresIn) != 5184000 {
		t.Errorf("ExpiresIn = %d, want 5184000（数字形态）", int64(numPayload.ExpiresIn))
	}

	// 裸 query string 里的 expires_in 是字符串语义，同样转成 int64
	qPayload, err := qqParseTokenBody([]byte("access_token=T&expires_in=5184000&refresh_token=R"))
	if err != nil {
		t.Fatalf("qqParseTokenBody(query): %v", err)
	}
	if int64(qPayload.ExpiresIn) != 5184000 {
		t.Errorf("ExpiresIn = %d, want 5184000（query string 形态）", int64(qPayload.ExpiresIn))
	}
}

// ---- 9. UserInfo 拒绝未校验的 app token ----

func TestQQUserInfoNotVerified(t *testing.T) {
	m := newQQMockServer(t)
	m.userBody = `{"ret":0,"nickname":"小罗","figureurl_qq_2":"http://q.qlogo.cn/x/100"}`

	t.Run("app 实例 + 未校验 token", func(t *testing.T) {
		p := m.provider(ChannelApp, qqAppConfig())
		tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_A", Channel: ChannelApp, Verified: false}
		_, err := p.UserInfo(context.Background(), tok)
		if !errors.Is(err, ErrTokenNotVerified) {
			t.Fatalf("err = %v, want ErrTokenNotVerified", err)
		}
		// 被拒时不得打上游
		if m.lastUserQuery != nil {
			t.Error("未校验的 token 不应请求 user/get_user_info")
		}
	})

	t.Run("web 实例 + token 标记为 app 且未校验", func(t *testing.T) {
		p := m.provider(ChannelWeb, qqWebConfig())
		tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_A", Channel: ChannelApp, Verified: false}
		_, err := p.UserInfo(context.Background(), tok)
		if !errors.Is(err, ErrTokenNotVerified) {
			t.Fatalf("err = %v, want ErrTokenNotVerified", err)
		}
	})
}

// ---- 10. UserInfo 成功路径 ----

func TestQQUserInfoOK(t *testing.T) {
	m := newQQMockServer(t)
	// 官方原文样例（移动端简化版：没有 province/city/year/constellation）
	m.userBody = `{
	  "is_yellow_year_vip": "0",
	  "ret": 0,
	  "figureurl_qq_1": "http://q.qlogo.cn/qqapp/APPID/OPENID_A/40",
	  "figureurl_qq_2": "http://q.qlogo.cn/qqapp/APPID/OPENID_A/100",
	  "nickname": "小罗",
	  "msg": "",
	  "figureurl": "http://qzapp.qlogo.cn/qqapp/APPID/OPENID_A/30",
	  "gender": "男"
	}`

	p := m.provider(ChannelApp, qqAppConfig())
	tok := &Token{AccessToken: "APP_TOKEN", OpenID: "OPENID_A", Channel: ChannelApp, Verified: true}
	user, err := p.UserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if user.Nickname != "小罗" {
		t.Errorf("Nickname = %q, want 小罗", user.Nickname)
	}
	if user.AvatarURL != "http://q.qlogo.cn/qqapp/APPID/OPENID_A/100" {
		t.Errorf("AvatarURL = %q, want figureurl_qq_2 优先", user.AvatarURL)
	}
	if user.Provider != ProviderQQ || user.ID != "OPENID_A" {
		t.Errorf("Provider/ID = %q/%q", user.Provider, user.ID)
	}
	// 通用参数名是 oauth_consumer_key，且 openid 必传
	if got := m.lastUserQuery.Get("oauth_consumer_key"); got != "APPID_APP" {
		t.Errorf("oauth_consumer_key = %q, want APPID_APP", got)
	}
	if got := m.lastUserQuery.Get("openid"); got != "OPENID_A" {
		t.Errorf("openid = %q, want OPENID_A", got)
	}
	if got := m.lastUserQuery.Get("access_token"); got != "APP_TOKEN" {
		t.Errorf("access_token = %q", got)
	}

	t.Run("ret != 0 时 HTTP 200 也要失败", func(t *testing.T) {
		m.userBody = `{"ret":100030,"msg":"user not authorized"}`
		_, err := p.UserInfo(context.Background(), tok)
		if !errors.Is(err, ErrScopeMissing) {
			t.Fatalf("err = %v, want ErrScopeMissing（ret=100030）", err)
		}
	})
}

// ---- 11. Configured()：app 只填 appid 即可，web 缺 secret 为 false ----

func TestQQConfigured(t *testing.T) {
	cases := []struct {
		name    string
		channel string
		cfg     Config
		want    bool
	}{
		{"app 只填 appid", ChannelApp, Config{ClientID: "APPID"}, true},
		{"app 空 appid", ChannelApp, Config{}, false},
		{"app 只填 appkey", ChannelApp, Config{ClientSecret: "KEY"}, false},
		{"web 齐备", ChannelWeb, Config{ClientID: "APPID", ClientSecret: "KEY"}, true},
		{"web 缺 secret", ChannelWeb, Config{ClientID: "APPID"}, false},
		{"web 缺 appid", ChannelWeb, Config{ClientSecret: "KEY"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newQQ(c.channel, c.cfg)
			if got := p.Configured(); got != c.want {
				t.Errorf("Configured() = %v, want %v", got, c.want)
			}
			if p.Name() != ProviderQQ {
				t.Errorf("Name() = %q, want qq", p.Name())
			}
			if p.Channel() != c.channel {
				t.Errorf("Channel() = %q, want %q", p.Channel(), c.channel)
			}
		})
	}
}

// ---- 附加：web AuthURL / Refresh / 错误码映射 ----

func TestQQWebAuthURL(t *testing.T) {
	p := newQQ(ChannelWeb, Config{
		ClientID:     "APPID",
		ClientSecret: "APPKEY",
		RedirectURI:  "https://example.com/cb",
		Scope:        DefaultScopeQQ,
	})
	raw, err := p.AuthURL("STATE-XYZ")
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	if u.Host != "graph.qq.com" || u.Path != qqPathAuthorize {
		t.Errorf("endpoint = %s%s", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != "APPID" ||
		q.Get("redirect_uri") != "https://example.com/cb" || q.Get("state") != "STATE-XYZ" {
		t.Errorf("query = %v", q)
	}
	if q.Get("scope") != DefaultScopeQQ {
		t.Errorf("scope = %q", q.Get("scope"))
	}

	// 未配置 → ErrNotConfigured（调用方据此降级为业务码，而不是 500）
	p2 := newQQ(ChannelWeb, Config{})
	if _, err := p2.AuthURL("s"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestQQWebRefresh(t *testing.T) {
	m := newQQMockServer(t)
	m.tokenBody = "access_token=NEW_TOKEN&expires_in=5184000&refresh_token=NEW_REFRESH"

	p := m.provider(ChannelWeb, qqWebConfig())
	old := &Token{AccessToken: "OLD", RefreshToken: "OLD_REFRESH", Channel: ChannelWeb, OpenID: "OPENID_WEB", Verified: true}
	tok, err := p.Refresh(context.Background(), old)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.AccessToken != "NEW_TOKEN" || tok.RefreshToken != "NEW_REFRESH" {
		t.Errorf("刷新结果 = %+v", tok)
	}
	if tok.OpenID != "OPENID_WEB" {
		t.Errorf("OpenID 应保留，实际 %q", tok.OpenID)
	}
	if m.lastTokenQuery.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q", m.lastTokenQuery.Get("grant_type"))
	}
	if m.lastTokenQuery.Get("refresh_token") != "OLD_REFRESH" {
		t.Errorf("refresh_token = %q", m.lastTokenQuery.Get("refresh_token"))
	}

	// refresh_token 失效 → ErrRefreshDead
	m.tokenBody = "error=100037&error_description=refresh token expired"
	if _, err := p.Refresh(context.Background(), old); !errors.Is(err, ErrRefreshDead) {
		t.Errorf("err = %v, want ErrRefreshDead（100037）", err)
	}

	// 空 refresh_token 直接拒，不打上游。
	//
	// 期望 ErrInvalidArgument 而非 ErrRefreshDead（2026-11-25 lead 裁定，规则见 docs/14 §7.1.2 禁令 3）：
	//   ErrRefreshDead     = 平台告知「这个 refresh_token 已失效或被撤销」→ 该引导用户重新授权；
	//   ErrInvalidArgument = 调用方传了空值（没先 Exchange，或落库时丢了字段）→ 该修调用链。
	// 两者处置相反：混用会让调用方对一个纯编程错误提示用户「请重新授权」，
	// 而用户重新授权后错误依旧（因为 refresh_token 仍然没被存下来），变成查不出的死循环。
	//
	// 原断言（期望 ErrRefreshDead）写于 ErrInvalidArgument 哨兵诞生之前 —— 那时没有更精确的语义可用。
	// 这不是「改测试迁就实现」，是新哨兵落地后旧断言语义不再正确。
	if _, err := p.Refresh(context.Background(), &Token{Channel: ChannelWeb}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument（空 refresh_token 是调用方错误，不是平台侧失效）", err)
	}
	// 反面断言：防止未来有人顺手改回 ErrRefreshDead —— 那会让调用方朝反方向处理。
	if _, err := p.Refresh(context.Background(), &Token{Channel: ChannelWeb}); errors.Is(err, ErrRefreshDead) {
		t.Error("空 refresh_token 被判成 ErrRefreshDead 会让调用方提示用户重新授权 —— 那是错的处置")
	}
}

func TestQQCodeErrorMapping(t *testing.T) {
	cases := []struct {
		code int64
		want error
	}{
		{100019, ErrCodeInvalid},   // code 换 token 失败
		{100020, ErrCodeInvalid},   // code 被重复使用（一次性）
		{100067, ErrCodeInvalid},   // code 过期
		{100068, ErrCodeInvalid},   // 非法 code
		{100013, ErrTokenExpired},  // access_token 失效
		{100014, ErrTokenExpired},  // access_token 过期（60 天）
		{100015, ErrTokenExpired},  // access_token 废除
		{100016, ErrTokenExpired},  // access_token 验证失败（移动端最常见）
		{100021, ErrRefreshDead},   // refresh_token 换取失败
		{100037, ErrRefreshDead},   // refresh_token 已过期
		{100009, ErrCredentialBad}, // appkey 非法
		{100008, ErrCredentialBad}, // appid 不存在
		{110401, ErrCredentialBad}, // 请求的应用不存在
		{100048, ErrScopeMissing},  // 未设置 companyID（未申请 unionid 权限）
		{100030, ErrScopeMissing},  // 用户未对该 api 授权
		{100050, ErrTokenMismatch}, // OPENID 解密失败（openid 与 appid 不匹配）
		{110405, ErrUpstream},      // 登录请求被限制
		{110406, ErrUpstream},      // 应用未通过审核
		{110407, ErrUpstream},      // 文档无此码 → 兜底
		{110501, ErrUpstream},      // 获取应用授权信息失败
		{110510, ErrUpstream},      // Disable
	}
	for _, c := range cases {
		err := qqCodeError(c.code, "desc")
		if !errors.Is(err, c.want) {
			t.Errorf("code %d → %v, want %v", c.code, err, c.want)
		}
		if err == nil || err.Error() == "" {
			t.Errorf("code %d 的错误信息不应为空", c.code)
		}
	}
	// 未知码：必须带 ErrUpstream 且保留 ret/msg 原文
	err := qqCodeError(999999, "boom")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("未知码 err = %v, want ErrUpstream", err)
	}
	if got := err.Error(); got != "oauthkit: upstream request failed: qq ret=999999 msg=boom" {
		t.Errorf("未知码错误文案 = %q", got)
	}
	if qqCodeError(0, "") != nil {
		t.Error("code 0 应返回 nil")
	}
}

func TestQQStripJSONPIdempotent(t *testing.T) {
	// 契约：>>> 返回值必须是「干净 JSON」<<< ——
	// 剥壳 + 先去首尾空白 + 再清尾逗号 + 压掉无语义空白，一步都不能少。
	cases := []struct{ in, want string }{
		{`callback( {"openid":"A"} );`, `{"openid":"A"}`}, // 官方默认形态
		{`callback({"a":1});`, `{"a":1}`},                 // 无空格
		{`{"openid":"A"}`, `{"openid":"A"}`},              // 纯 JSON（fmt=json）：原样可解
		{`  {"openid":"A"}  `, `{"openid":"A"}`},          // 纯 JSON 带首尾空白 → 也必须 trim
		{"\n\t{\"openid\":\"A\"}\n", `{"openid":"A"}`},    // 纯 JSON 带换行/制表符
		// 官方错误体样例：JSONP + 换行 + 尾逗号 → 必须变成干净合法 JSON
		{"callback(\n{\n\"error\":100016,\n\"error_description\":\"access token check failed\",\n}\n);",
			`{"error":100016,"error_description":"access token check failed"}`},
		// 尾逗号与 `}` 之间夹换行/制表符：先 trim 再清逗号才吃得下（顺序反了就漏）
		{"callback( {\"error\":100016,\"error_description\":\"x\",\n\t} );",
			`{"error":100016,"error_description":"x"}`},
		// 数组尾逗号同样清
		{`callback( {"list":["a","b",]} );`, `{"list":["a","b"]}`},
		// 字符串内部的空格/换行不许被压（json.Compact 尊重字面量）
		{"callback( {\"nickname\":\"小 罗\\nA\"} );", `{"nickname":"小 罗\nA"}`},
		// 不是 JSON（例如上游纯文本错误）→ 只 trim，不吞内容
		{"  error=100019&error_description=bad  ", "error=100019&error_description=bad"},
	}
	for _, c := range cases {
		got := string(stripJSONP([]byte(c.in)))
		if got != c.want {
			t.Errorf("stripJSONP(%q) = %q, want %q", c.in, got, c.want)
		}
		// 只要输入里带 JSON，输出就必须是合法 JSON（尾逗号已清、空白已压）
		if strings.Contains(c.in, "{") && !json.Valid([]byte(got)) {
			t.Errorf("stripJSONP(%q) = %q 不是合法 JSON", c.in, got)
		}
		if got != strings.TrimSpace(got) {
			t.Errorf("stripJSONP(%q) = %q 仍带首尾空白", c.in, got)
		}
	}
	// 幂等：对已剥壳结果再跑一次必须不变
	for _, in := range []string{`callback( {"openid":"A"} );`, "callback(\n{\n\"error\":1,\n}\n);"} {
		once := stripJSONP([]byte(in))
		if twice := stripJSONP(once); string(twice) != string(once) {
			t.Errorf("非幂等：%q → %q", once, twice)
		}
	}
}

// ============================================================
// 审计回归测试（R1-R7）：只钉「测试此前没覆盖、但生产会出事」的点。
// ============================================================

// R1｜`callback(` 出现在 JSON 字符串字面量里时不许误剥。
//
// 旧实现用 strings.Index 任意位搜索：昵称叫 `callback(abc` 会被从中间截断，
// 合法 userinfo 直接解析失败（这条路径真实可达 —— 昵称是用户自填的）。
func TestQQStripJSONPNoFalseStripOnLiteralCallback(t *testing.T) {
	t.Run("纯 JSON 的字符串值含 callback(", func(t *testing.T) {
		// user/get_user_info 真实可能出现的响应
		in := `{"ret":0,"nickname":"callback(abc","figureurl_qq_2":"http://q.qlogo.cn/x/100"}`
		got := string(stripJSONP([]byte(in)))
		if got != in {
			t.Errorf("stripJSONP 误剥：\n got=%q\nwant=%q", got, in)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("结果不是合法 JSON：%q", got)
		}
	})

	t.Run("纯 JSON 中间含 callback( 且无闭合括号", func(t *testing.T) {
		in := `{"nickname":"x callback(y","ret":0}`
		got := string(stripJSONP([]byte(in)))
		if got != in {
			t.Errorf("stripJSONP 误剥：\n got=%q\nwant=%q", got, in)
		}
	})

	t.Run("真 JSONP 包裹里的值含 callback(", func(t *testing.T) {
		in := `callback( {"openid":"A","nickname":"callback(z"} );`
		want := `{"openid":"A","nickname":"callback(z"}`
		if got := string(stripJSONP([]byte(in))); got != want {
			t.Errorf("stripJSONP = %q, want %q", got, want)
		}
	})

	t.Run("前缀不是 callback 的纯 JSON 原样返回", func(t *testing.T) {
		in := `{"callback(flag)":"v"}`
		if got := string(stripJSONP([]byte(in))); got != in {
			t.Errorf("stripJSONP = %q, want %q", got, in)
		}
	})
}

// R1b｜JSON 字符串值含 `)` 时不许被 LastIndex 截断。
func TestQQStripJSONPRightBoundaryBrackets(t *testing.T) {
	cases := []struct{ in, want string }{
		// 父括号没被 LastIndex 误当定界符（旧实现会剩 `");` 之类残渣）
		{`callback({"nickname":"a)b"});`, `{"nickname":"a)b"}`},
		{`callback( {"nickname":"a)b","openid":"O"} );`, `{"nickname":"a)b","openid":"O"}`},
		// 值里的花括号也必须跳过
		{`callback({"a":"}{","b":1});`, `{"a":"}{","b":1}`},
		// 转义引号不得提前结束字符串态
		{`callback({"a":"x\"y","b":")"});`, `{"a":"x\"y","b":")"}`},
		// 官方错误体（换行 + 尾逗号）
		{"callback(\n{\n\"error\":100016,\n\"error_description\":\"x\",\n}\n);",
			`{"error":100016,"error_description":"x"}`},
	}
	for _, c := range cases {
		got := string(stripJSONP([]byte(c.in)))
		if got != c.want {
			t.Errorf("stripJSONP(%q) = %q, want %q", c.in, got, c.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("stripJSONP(%q) = %q 不是合法 JSON", c.in, got)
		}
	}
}

// R2｜qqFlexInt 边界：非法形态必须报错（不许静默变 0），溢出必须被截断。
func TestQQFlexIntBoundaries(t *testing.T) {
	t.Run("合法形态", func(t *testing.T) {
		cases := []struct {
			in   string
			want int64
		}{
			{`7776000`, 7776000},
			{`"7776000"`, 7776000}, // app SDK 官方形态
			{`""`, 0},              // 空串 = 缺失
			{`null`, 0},            // null = 缺失
			{`7776000.0`, 7776000}, // 浮点兜底
			{`1e6`, 1000000},       // 科学计数兜底
			{`-1`, -1},             // 负数原样保留（由 qqExpiresAt 判 <=0）
			{`9223372036854775807`, 9223372036854775807},
		}
		for _, c := range cases {
			var v qqFlexInt
			if err := json.Unmarshal([]byte(c.in), &v); err != nil {
				t.Errorf("Unmarshal(%s) 意外报错：%v", c.in, err)
				continue
			}
			if int64(v) != c.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", c.in, int64(v), c.want)
			}
		}
	})

	t.Run("非法形态必须报错而非静默变 0", func(t *testing.T) {
		// 静默变 0 的两种致命后果：
		//   错误码 error/ret → 把「上游报错」当成功；
		//   expires_in → ExpiresAt 零值（= 不过期），过期 token 被长期当有效。
		for _, in := range []string{`999999999999999999999`, `"abc"`, `"12x"`, `{}`, `true`, `1.5e400`} {
			var v qqFlexInt
			if err := json.Unmarshal([]byte(in), &v); err == nil {
				t.Errorf("Unmarshal(%s) 应当报错，实际得到 %d", in, int64(v))
			}
		}
	})

	t.Run("错误码字段非法时整段解析失败", func(t *testing.T) {
		// error 是非数字 → 绝不能当成 error=0（成功）
		if _, err := qqParseTokenBody([]byte(`{"error":"boom","error_description":"x"}`)); err == nil {
			t.Error("非法 error 字段应导致解析失败")
		}
	})
}

// R2b｜expires_in 极大值不得让 ExpiresAt 溢出到过去。
func TestQQExpiresAtNoOverflow(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		expiresIn int64
	}{
		{"60 天", 5184000},
		{"90 天", 7776000},
		{"int64 上限（会溢出成负数）", math.MaxInt64},
		{"超过纳秒上限一秒", qqMaxExpiresIn + 1},
		{"天文数字", 999999999999999999},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := qqExpiresAt(c.expiresIn)
			if at.IsZero() {
				t.Fatalf("expires_in=%d 不应得到零值", c.expiresIn)
			}
			// 核心断言：截断后必须仍在未来（旧实现在 MaxInt64 时掉到「现在前 1 秒」）
			if !at.After(now) {
				t.Errorf("ExpiresAt = %v 不是未来时间（溢出），expires_in=%d", at, c.expiresIn)
			}
		})
	}
	if got := qqExpiresAt(0); !got.IsZero() {
		t.Errorf("expires_in=0 应得零值（不过期），实际 %v", got)
	}
	if got := qqExpiresAt(-1); !got.IsZero() {
		t.Errorf("expires_in=-1 应得零值，实际 %v", got)
	}
}

// R3｜url.ParseQuery 容错边界：解析成功 ≠ 拿到 token。
func TestQQParseTokenBodyQueryEdgeCases(t *testing.T) {
	t.Run("空 body", func(t *testing.T) {
		if _, err := qqParseTokenBody(nil); !errors.Is(err, ErrUpstream) {
			t.Error("空 body 应报 ErrUpstream")
		}
	})
	t.Run("只有空白", func(t *testing.T) {
		if _, err := qqParseTokenBody([]byte("   \n\t ")); !errors.Is(err, ErrUpstream) {
			t.Error("纯空白应报 ErrUpstream")
		}
	})
	t.Run("HTML 错误页不许被当成响应", func(t *testing.T) {
		// url.ParseQuery 会把它解成一个奇形怪状的 key（err=nil），旧实现因此「成功但无 token」
		html := []byte("<html><body>500 Internal Server Error</body></html>")
		_, err := qqParseTokenBody(html)
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("HTML 错误页 err = %v, want ErrUpstream", err)
		}
		// 错误信息不得夹带响应体内容（怕夹带明文 token）
		if strings.Contains(err.Error(), "<html>") || strings.Contains(err.Error(), "500") {
			t.Errorf("错误信息夹带了响应体内容：%v", err)
		}
	})
	t.Run("access_token 值为空 → 由 Exchange 判空拦截", func(t *testing.T) {
		payload, err := qqParseTokenBody([]byte("access_token=&expires_in=5184000"))
		if err != nil {
			t.Fatalf("解析本身不应报错：%v", err)
		}
		if payload.AccessToken != "" {
			t.Errorf("AccessToken = %q, want 空", payload.AccessToken)
		}
		// Exchange 层必须因此失败（不能拿空 token 继续）
		m := newQQMockServer(t)
		m.tokenBody = "access_token=&expires_in=5184000"
		p := m.provider(ChannelWeb, qqWebConfig())
		if _, err := p.Exchange(context.Background(), "CODE"); !errors.Is(err, ErrUpstream) {
			t.Errorf("空 access_token 时 Exchange err = %v, want ErrUpstream", err)
		}
	})
}

// R7｜导出 qqFlexInt 之外的公开入口：app SDK 登录载荷解析。
func TestParseQQAppLoginPayload(t *testing.T) {
	// 官方 Android SDK onComplete 原文样例（tmp/oauth/qq_login.txt:194-203）
	official := []byte(`{
	  "ret":0,
	  "pay_token":"xxxxxxxxxxxxxxxx",
	  "pf":"openmobile_android",
	  "expires_in":"7776000",
	  "openid":"8C75BBE3DC6B0E9A64BD31449A3C8CB0",
	  "pfkey":"xxxxxxxxxxxxxxxxxxx",
	  "msg":"sucess",
	  "access_token":"xxxxxxxxxxxxxxxxxxxxx"
	}`)

	t.Run("官方样例", func(t *testing.T) {
		tok, err := ParseQQAppLoginPayload(official)
		if err != nil {
			t.Fatalf("ParseQQAppLoginPayload: %v（msg 是 \"sucess\"，不许断言）", err)
		}
		if tok.AccessToken != "xxxxxxxxxxxxxxxxxxxxx" {
			t.Errorf("AccessToken = %q", tok.AccessToken)
		}
		if tok.OpenID != "8C75BBE3DC6B0E9A64BD31449A3C8CB0" {
			t.Errorf("OpenID = %q", tok.OpenID)
		}
		if tok.ExpiresIn != 7776000 {
			t.Errorf("ExpiresIn = %d, want 7776000（字符串形态）", tok.ExpiresIn)
		}
		if tok.Channel != ChannelApp {
			t.Errorf("Channel = %q, want app", tok.Channel)
		}
		if tok.Verified {
			t.Error("客户端上报的 token 必须 Verified=false（等 ValidateToken）")
		}
		if tok.RefreshToken != "" {
			t.Errorf("SDK 不返回 refresh_token，实际 %q", tok.RefreshToken)
		}
		if tok.ExpiresAt.IsZero() || !tok.ExpiresAt.After(time.Now()) {
			t.Errorf("ExpiresAt = %v，应为未来时间", tok.ExpiresAt)
		}
	})

	t.Run("ret != 0 → 哨兵错误", func(t *testing.T) {
		_, err := ParseQQAppLoginPayload([]byte(`{"ret":100016,"msg":"access token check failed"}`))
		if !errors.Is(err, ErrTokenExpired) {
			t.Errorf("err = %v, want ErrTokenExpired（100016）", err)
		}
	})

	t.Run("缺 access_token / openid → ErrUpstream", func(t *testing.T) {
		if _, err := ParseQQAppLoginPayload([]byte(`{"ret":0,"openid":"O"}`)); !errors.Is(err, ErrUpstream) {
			t.Errorf("缺 access_token err = %v", err)
		}
		if _, err := ParseQQAppLoginPayload([]byte(`{"ret":0,"access_token":"T"}`)); !errors.Is(err, ErrUpstream) {
			t.Errorf("缺 openid err = %v", err)
		}
	})

	t.Run("非 JSON / 空 → ErrUpstream", func(t *testing.T) {
		for _, in := range []string{"", "   ", "not json at all", `[1,2]`} {
			if _, err := ParseQQAppLoginPayload([]byte(in)); !errors.Is(err, ErrUpstream) {
				t.Errorf("输入 %q err = %v, want ErrUpstream", in, err)
			}
		}
	})

	t.Run("expires_in 非法 → 报错不静默", func(t *testing.T) {
		_, err := ParseQQAppLoginPayload([]byte(`{"ret":0,"access_token":"T","openid":"O","expires_in":"abc"}`))
		if !errors.Is(err, ErrUpstream) {
			t.Errorf("err = %v, want ErrUpstream", err)
		}
	})

	t.Run("解析后 UserInfo 仍被硬拒（未校验）", func(t *testing.T) {
		tok, err := ParseQQAppLoginPayload(official)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		m := newQQMockServer(t)
		p := m.provider(ChannelApp, qqAppConfig())
		if _, err := p.UserInfo(context.Background(), tok); !errors.Is(err, ErrTokenNotVerified) {
			t.Errorf("err = %v, want ErrTokenNotVerified", err)
		}
		if m.lastUserQuery != nil {
			t.Error("未校验的 token 不该打上游")
		}
	})
}

// R4｜openid 比对必须大小写敏感（绑定主键不允许折叠）。
func TestQQValidateTokenOpenIDCaseSensitive(t *testing.T) {
	m := newQQMockServer(t)
	m.meBody = `callback( {"client_id":"APP","openid":"8c75bbe3dc6b0e9a"} );` // 小写
	p := m.provider(ChannelApp, qqAppConfig())

	// 客户端上报大写形态 → 必须判 mismatch（不折叠大小写）
	tok := &Token{AccessToken: "T", OpenID: "8C75BBE3DC6B0E9A", Channel: ChannelApp}
	if _, err := p.ValidateToken(context.Background(), tok); !errors.Is(err, ErrTokenMismatch) {
		t.Errorf("err = %v, want ErrTokenMismatch（openid 比对须精确相等）", err)
	}

	// 完全一致才放行
	tok2 := &Token{AccessToken: "T", OpenID: "8c75bbe3dc6b0e9a", Channel: ChannelApp}
	got, err := p.ValidateToken(context.Background(), tok2)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if !got.Verified {
		t.Error("应置 Verified")
	}
}

// R5｜/oauth2.0/me 只带 access_token（官方通用参数表未要求 oauth_consumer_key），
// 且绝不应把 oauth_consumer_key 混进去 —— 参数错了会 110404。
func TestQQMeEndpointParams(t *testing.T) {
	m := newQQMockServer(t)
	m.meBody = `callback( {"openid":"O"} );`
	p := m.provider(ChannelApp, qqAppConfig())
	if _, err := p.ValidateToken(context.Background(), &Token{AccessToken: "T", OpenID: "O"}); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got := m.lastMeQuery.Get("access_token"); got != "T" {
		t.Errorf("me access_token = %q", got)
	}
	if got := m.lastMeQuery.Get("oauth_consumer_key"); got != "" {
		t.Errorf("me 不应传 oauth_consumer_key，实际 %q", got)
	}
	// user/get_user_info 则三个通用参数都必须有
	m.userBody = `{"ret":0,"nickname":"n"}`
	tok := &Token{AccessToken: "T", OpenID: "O", Channel: ChannelApp, Verified: true}
	if _, err := p.UserInfo(context.Background(), tok); err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	for _, k := range []string{"access_token", "oauth_consumer_key", "openid"} {
		if m.lastUserQuery.Get(k) == "" {
			t.Errorf("user/get_user_info 缺少必传参数 %s", k)
		}
	}
	if got := m.lastUserQuery.Get("oauth_consumer_key"); got != qqAppConfig().ClientID {
		t.Errorf("oauth_consumer_key = %q, want %q（app 实例的 ClientID = qq_app_appid）", got, qqAppConfig().ClientID)
	}
}

// R3 补漏｜JSON 分支的「必须是 QQ 响应」判定（与 query string 分支对称）。
//
// 原来只在 query 分支做了 known==0 拦截，JSON 分支漏了：网关把 body 换成
// `{"status":"error"}` 这类合法 JSON 时，会拿到空 payload 并报「no access_token」，
// 把「上游挂了」误导成「QQ 不给 token」。同时不能误拒**合法错误体**。
func TestQQParseTokenBodyJSONUnknownFields(t *testing.T) {
	t.Run("合法 JSON 但不含任何 QQ 字段 → ErrUpstream", func(t *testing.T) {
		for _, in := range []string{
			`{"not":"query string"}`,
			`{"status":"error","message":"bad gateway"}`, // 网关替换的真实形态
			`{}`,                     // 空对象
			`{"code":0,"data":null}`, // 别的平台的响应样式
		} {
			payload, err := qqParseTokenBody([]byte(in))
			if !errors.Is(err, ErrUpstream) {
				t.Errorf("%s → err = %v, want ErrUpstream", in, err)
			}
			if payload != nil {
				t.Errorf("%s → 失败时不应返回 payload：%+v", in, payload)
			}
		}
	})

	t.Run("合法错误体不许误拒", func(t *testing.T) {
		cases := []struct {
			in     string
			errFld int64
			retFld int64
			desc   string
			want   error
		}{
			{`{"error":100016,"error_description":"access token check failed"}`,
				100016, 0, "access token check failed", ErrTokenExpired},
			// ret 形态的错误信息在 msg 字段（不是 error_description），且 100019 属 code 类
			{`{"ret":100019,"msg":"code to access token error"}`,
				0, 100019, "code to access token error", ErrCodeInvalid},
			// 码被拼成字符串也要认
			{`{"error":"100021","error_description":"refresh failed"}`,
				100021, 0, "refresh failed", ErrRefreshDead},
		}
		for _, c := range cases {
			payload, err := qqParseTokenBody([]byte(c.in))
			if err != nil {
				t.Errorf("%s 是合法错误体，不应被拒：%v", c.in, err)
				continue
			}
			if int64(payload.Error) != c.errFld || int64(payload.Ret) != c.retFld {
				t.Errorf("%s 解析结果错：error=%d ret=%d", c.in, int64(payload.Error), int64(payload.Ret))
				continue
			}
			// 描述字段：error 形态在 ErrorDescription，ret 形态在 Msg
			gotDesc := payload.ErrorDescription
			if gotDesc == "" {
				gotDesc = payload.Msg
			}
			if gotDesc != c.desc {
				t.Errorf("%s → desc = %q, want %q", c.in, gotDesc, c.desc)
			}
			// 错误体必须能被 errOrNil 映射出哨兵错误（上层靠这个走分支）
			if err := payload.errOrNil(); !errors.Is(err, c.want) {
				t.Errorf("%s → errOrNil = %v, want %v", c.in, err, c.want)
			}
		}
	})

	t.Run("合法成功体照常通过", func(t *testing.T) {
		cases := []string{
			`{"access_token":"AT","expires_in":5184000}`,
			`{"access_token":"AT","expires_in":"7776000","openid":"O","refresh_token":"R"}`,
			`callback( {"access_token":"AT","expires_in":5184000} );`,
		}
		for _, in := range cases {
			payload, err := qqParseTokenBody([]byte(in))
			if err != nil {
				t.Errorf("%s 不应被拒：%v", in, err)
				continue
			}
			if payload.AccessToken != "AT" {
				t.Errorf("%s → AccessToken = %q", in, payload.AccessToken)
			}
			if err := payload.errOrNil(); err != nil {
				t.Errorf("%s → errOrNil 应为 nil，实际 %v", in, err)
			}
		}
	})

	t.Run("网关返回非 QQ JSON 时 Exchange 报错方向正确", func(t *testing.T) {
		m := newQQMockServer(t)
		m.tokenBody = `{"status":"error","message":"bad gateway"}`
		p := m.provider(ChannelWeb, qqWebConfig())
		_, err := p.Exchange(context.Background(), "CODE")
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("err = %v, want ErrUpstream", err)
		}
		// 关键：错误信息要指向「这不是 QQ 响应」，不能是「没有 access_token」
		if !strings.Contains(err.Error(), "no recognized field") {
			t.Errorf("错误信息未指出「不是 QQ 响应」：%v", err)
		}
		if strings.Contains(err.Error(), "bad gateway") {
			t.Errorf("错误信息夹带了响应体内容：%v", err)
		}
	})

	t.Run("query string 分支的同类形态仍然报错（对称性）", func(t *testing.T) {
		if _, err := qqParseTokenBody([]byte("status=error&message=bad+gateway")); !errors.Is(err, ErrUpstream) {
			t.Errorf("err = %v, want ErrUpstream（query 分支应同样拒）", err)
		}
	})
}

// 参数校验口径回归（lead 2026-11-25 裁定，规则见 docs/14 §7.1.2 禁令 3）：
// 调用方传空值 = 调用方编程错误（改代码），平台判定失败 = 引导用户重新授权。
// 两者处置相反，混用会让调用方对空值做无意义的重新授权引导。
//
// 每条都同时断言「正面是 ErrInvalidArgument」+「反面不是另一个哨兵」——
// 只断言正面的话，未来有人顺手改回去测试照样绿，这个纠正就白做了。
func TestQQInvalidArgumentSemantics(t *testing.T) {
	t.Run("AuthURL 空 state", func(t *testing.T) {
		p := newQQ(ChannelWeb, qqWebConfig())
		for _, state := range []string{"", "   ", "\t\n"} {
			_, err := p.AuthURL(state)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("state=%q → err = %v, want ErrInvalidArgument（调用方漏了 CSRF 保护）", state, err)
			}
			// 反面断言：不能让调用方误判成「用户没授权范围」→ 多余的重新授权引导
			if errors.Is(err, ErrScopeMissing) {
				t.Error("空 state 被判成 ErrScopeMissing 会让调用方引导用户重新授权 —— 那是错的")
			}
			// 也不该是「平台不支持此渠道」
			if errors.Is(err, ErrAuthURLUnsupported) {
				t.Error("空 state 被判成 ErrAuthURLUnsupported —— 那是错的")
			}
		}
		// 非空 state 必须照常出 URL（别把拦得过头）
		if _, err := p.AuthURL("STATE"); err != nil {
			t.Errorf("正常 state 不应报错：%v", err)
		}
	})

	t.Run("Exchange 空 code", func(t *testing.T) {
		m := newQQMockServer(t)
		p := m.provider(ChannelWeb, qqWebConfig())
		for _, code := range []string{"", "   "} {
			_, err := p.Exchange(context.Background(), code)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("code=%q → err = %v, want ErrInvalidArgument（调用方错误）", code, err)
			}
			// 关键反面断言：ErrCodeInvalid 对应「平台说 code 无效/已用」，
			// 判成它会让调用方引导用户重新授权 —— 空 code 再授权还是空，死循环。
			if errors.Is(err, ErrCodeInvalid) {
				t.Error("空 code 被判成 ErrCodeInvalid 会让调用方引导用户重新授权 —— 那是错的")
			}
		}
		// 不许打上游：空 code 白耗一次配额
		if m.lastTokenQuery != nil {
			t.Error("空 code 不应请求上游")
		}
	})

	t.Run("平台真的回 100019 时仍用 ErrCodeInvalid", func(t *testing.T) {
		// 反面保护：不能把「平台侧语义」一起改掉了
		m := newQQMockServer(t)
		m.tokenBody = "error=100019&error_description=code to access token error"
		p := m.provider(ChannelWeb, qqWebConfig())
		_, err := p.Exchange(context.Background(), "REAL-CODE")
		if !errors.Is(err, ErrCodeInvalid) {
			t.Errorf("err = %v, want ErrCodeInvalid（平台侧判定，不是参数错误）", err)
		}
		if errors.Is(err, ErrInvalidArgument) {
			t.Error("平台回 100019 不该被判成 ErrInvalidArgument")
		}
	})
}

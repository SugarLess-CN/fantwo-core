package oauthkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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

	// 空 refresh_token 直接拒，不打上游
	if _, err := p.Refresh(context.Background(), &Token{Channel: ChannelWeb}); !errors.Is(err, ErrRefreshDead) {
		t.Errorf("err = %v, want ErrRefreshDead", err)
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

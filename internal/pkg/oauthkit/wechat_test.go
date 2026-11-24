package oauthkit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// wechatTestConfig 单测用完整凭据（不走配置层，只验协议行为）。
func wechatTestConfig() Config {
	return Config{
		ClientID:     "wx_test_appid",
		ClientSecret: "wx_test_secret",
		RedirectURI:  "https://api.example.com/api/v1/auth/wechat/callback",
	}
}

// newWechatMockProvider 构造 API 端点指向 mock server 的 provider，并返回请求计数器。
//
// 计数器用来断言「本地就该拒绝的调用没有真的打到微信」。
func newWechatMockProvider(t *testing.T, channel string, h http.HandlerFunc) (*wechatProvider, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	p := newWechat(channel, wechatTestConfig())
	p.apiBase = srv.URL // 仅替换 API 域名，authBase 仍是官方授权页
	return p, &hits
}

// TestWechatAuthURLWeb 网站应用授权 URL：顺序 + 编码 + #wechat_redirect 锚点。
func TestWechatAuthURLWeb(t *testing.T) {
	p := newWechat(ChannelWeb, wechatTestConfig())

	got, err := p.AuthURL("STATE123")
	if err != nil {
		t.Fatalf("AuthURL: unexpected error: %v", err)
	}
	want := "https://open.weixin.qq.com/connect/qrconnect" +
		"?appid=wx_test_appid" +
		"&redirect_uri=https%3A%2F%2Fapi.example.com%2Fapi%2Fv1%2Fauth%2Fwechat%2Fcallback" +
		"&response_type=code" +
		"&scope=snsapi_login" +
		"&state=STATE123" +
		"#wechat_redirect"
	if got != want {
		t.Fatalf("AuthURL =\n  %q\nwant\n  %q", got, want)
	}
	if !strings.HasSuffix(got, "#wechat_redirect") {
		t.Errorf("AuthURL must end with #wechat_redirect, got %q", got)
	}
	for _, frag := range []string{"appid=", "redirect_uri=", "response_type=code", "scope=snsapi_login", "state=STATE123"} {
		if !strings.Contains(got, frag) {
			t.Errorf("AuthURL missing %q: %q", frag, got)
		}
	}
	// 参数顺序必须是官方模板顺序，不允许按字母序重排
	idx := []string{"?appid=", "&redirect_uri=", "&response_type=", "&scope=", "&state="}
	last := -1
	for _, key := range idx {
		cur := strings.Index(got, key)
		if cur <= last {
			t.Fatalf("AuthURL param order broken at %q: %q", key, got)
		}
		last = cur
	}
}

// TestWechatAuthURLAppUnsupported 移动应用不出授权 URL（客户端 SDK 拉起微信）。
func TestWechatAuthURLAppUnsupported(t *testing.T) {
	p := newWechat(ChannelApp, wechatTestConfig())

	got, err := p.AuthURL("STATE123")
	if !errors.Is(err, ErrAuthURLUnsupported) {
		t.Fatalf("AuthURL error = %v, want ErrAuthURLUnsupported", err)
	}
	if got != "" {
		t.Fatalf("AuthURL must return empty string for app channel, got %q", got)
	}
}

// TestWechatAuthURLScopeDefault app 渠道 scope 兜底为 snsapi_userinfo，web 为 snsapi_login。
func TestWechatAuthURLScopeDefault(t *testing.T) {
	if got := newWechat(ChannelApp, wechatTestConfig()).scope(); got != DefaultScopeWechatApp {
		t.Errorf("app scope = %q, want %q", got, DefaultScopeWechatApp)
	}
	if got := newWechat(ChannelWeb, wechatTestConfig()).scope(); got != DefaultScopeWechatWeb {
		t.Errorf("web scope = %q, want %q", got, DefaultScopeWechatWeb)
	}
}

// TestWechatConfigured 凭据判空。
func TestWechatConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"完整凭据", wechatTestConfig(), true},
		{"空配置", Config{}, false},
		{"缺 secret", Config{ClientID: "wx_test_appid"}, false},
		{"缺 appid", Config{ClientSecret: "wx_test_secret"}, false},
		{"空白字符", Config{ClientID: "  ", ClientSecret: "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newWechat(ChannelWeb, c.cfg)
			if got := p.Configured(); got != c.want {
				t.Fatalf("Configured() = %v, want %v", got, c.want)
			}
			if c.want {
				return
			}
			// 未配置时网络方法必须直接返回 ErrNotConfigured，不发请求
			if _, err := p.Exchange(context.Background(), "CODE"); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("Exchange err = %v, want ErrNotConfigured", err)
			}
			if _, err := p.AuthURL("STATE"); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("AuthURL err = %v, want ErrNotConfigured", err)
			}
			if _, err := p.Refresh(context.Background(), &Token{RefreshToken: "RT"}); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("Refresh err = %v, want ErrNotConfigured", err)
			}
			if _, err := p.UserInfo(context.Background(), &Token{AccessToken: "AT", OpenID: "OID"}); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("UserInfo err = %v, want ErrNotConfigured", err)
			}
			if _, err := p.ValidateToken(context.Background(), &Token{AccessToken: "AT", OpenID: "OID"}); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("ValidateToken err = %v, want ErrNotConfigured", err)
			}
		})
	}
}

// TestWechatExchangeSuccess code 换 token 成功路径（GET + query 参数 + 官方原文响应样例）。
func TestWechatExchangeSuccess(t *testing.T) {
	p, hits := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/sns/oauth2/access_token" {
			t.Errorf("path = %s, want /sns/oauth2/access_token", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("appid"); got != "wx_test_appid" {
			t.Errorf("appid = %q, want wx_test_appid", got)
		}
		if got := q.Get("secret"); got != "wx_test_secret" {
			t.Errorf("secret = %q, want wx_test_secret", got)
		}
		if got := q.Get("client_secret"); got != "" {
			t.Errorf("client_secret must not be sent, got %q", got)
		}
		if got := q.Get("code"); got != "CODE123" {
			t.Errorf("code = %q, want CODE123", got)
		}
		if got := q.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"ACCESS_TOKEN","expires_in":7200,"refresh_token":"REFRESH_TOKEN",`+
			`"openid":"OPENID","scope":"snsapi_userinfo","unionid":"o6_bmasdasdsad6_2sgVt7hMZOPfL"}`)
	})

	before := time.Now()
	tok, err := p.Exchange(context.Background(), " CODE123 ")
	if err != nil {
		t.Fatalf("Exchange: unexpected error: %v", err)
	}
	if *hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", *hits)
	}
	if tok.AccessToken != "ACCESS_TOKEN" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "REFRESH_TOKEN" {
		t.Errorf("RefreshToken = %q", tok.RefreshToken)
	}
	// openid 在换 token 阶段就返回，必须在这里填上
	if tok.OpenID != "OPENID" {
		t.Errorf("OpenID = %q, want OPENID", tok.OpenID)
	}
	if tok.UnionID != "o6_bmasdasdsad6_2sgVt7hMZOPfL" {
		t.Errorf("UnionID = %q", tok.UnionID)
	}
	if tok.ExpiresIn != 7200 {
		t.Errorf("ExpiresIn = %d, want 7200", tok.ExpiresIn)
	}
	if tok.Scope != "snsapi_userinfo" {
		t.Errorf("Scope = %q", tok.Scope)
	}
	if tok.Channel != ChannelApp {
		t.Errorf("Channel = %q, want %q", tok.Channel, ChannelApp)
	}
	if tok.Verified {
		t.Error("fresh token must not be Verified before ValidateToken")
	}
	if tok.ExpiresAt.Before(before.Add(7100*time.Second)) || tok.ExpiresAt.After(time.Now().Add(7300*time.Second)) {
		t.Errorf("ExpiresAt = %v, want ~now+7200s", tok.ExpiresAt)
	}
	if len(tok.Raw) == 0 {
		t.Error("Raw must keep the original response body")
	}
}

// TestWechatExchangeInvalidCode 40029 → ErrCodeInvalid（HTTP 状态码仍是 200）。
func TestWechatExchangeInvalidCode(t *testing.T) {
	p, _ := newWechatMockProvider(t, ChannelWeb, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"errcode":40029,"errmsg":"invalid code"}`)
	})

	tok, err := p.Exchange(context.Background(), "CODE123")
	if tok != nil {
		t.Fatalf("Exchange must return nil token on error, got %+v", tok)
	}
	if !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("Exchange err = %v, want ErrCodeInvalid", err)
	}
	if !strings.Contains(err.Error(), "errcode=40029") {
		t.Errorf("error must keep the raw errcode, got %v", err)
	}
}

// TestWechatExchangeEmptyCode 空 code 直接在本地拒绝，不打上游。
func TestWechatExchangeEmptyCode(t *testing.T) {
	p, hits := newWechatMockProvider(t, ChannelWeb, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for empty code")
	})

	if _, err := p.Exchange(context.Background(), "   "); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("Exchange err = %v, want ErrCodeInvalid", err)
	}
	if *hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", *hits)
	}
}

// TestWechatValidateToken sns/auth 校验：通过置 Verified，40003 → ErrTokenMismatch。
func TestWechatValidateToken(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		p, _ := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/sns/auth" {
				t.Errorf("path = %s, want /sns/auth", r.URL.Path)
			}
			q := r.URL.Query()
			if got := q.Get("access_token"); got != "AT" {
				t.Errorf("access_token = %q", got)
			}
			if got := q.Get("openid"); got != "OPENID" {
				t.Errorf("openid = %q", got)
			}
			fmt.Fprint(w, `{"errcode":0,"errmsg":"ok"}`)
		})

		src := &Token{AccessToken: "AT", OpenID: "OPENID"}
		out, err := p.ValidateToken(context.Background(), src)
		if err != nil {
			t.Fatalf("ValidateToken: unexpected error: %v", err)
		}
		if !out.Verified {
			t.Error("Verified must be true after sns/auth ok")
		}
		if out.Channel != ChannelApp {
			t.Errorf("Channel = %q, want %q", out.Channel, ChannelApp)
		}
		if src.Verified {
			t.Error("ValidateToken must not mutate the caller's token")
		}
	})

	t.Run("invalid_openid", func(t *testing.T) {
		p, _ := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"errcode":40003,"errmsg":"invalid openid"}`)
		})

		if _, err := p.ValidateToken(context.Background(), &Token{AccessToken: "AT", OpenID: "OPENID"}); !errors.Is(err, ErrTokenMismatch) {
			t.Fatalf("ValidateToken err = %v, want ErrTokenMismatch", err)
		}
	})

	t.Run("missing_fields", func(t *testing.T) {
		p, hits := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
			t.Error("upstream must not be called without access_token/openid")
		})
		if _, err := p.ValidateToken(context.Background(), &Token{AccessToken: "AT"}); !errors.Is(err, ErrTokenMismatch) {
			t.Fatalf("ValidateToken err = %v, want ErrTokenMismatch", err)
		}
		if *hits != 0 {
			t.Fatalf("upstream hits = %d, want 0", *hits)
		}
	})
}

// TestWechatUserInfoRequiresVerifiedAppToken app 渠道未校验的 token 必须被硬拒绝。
func TestWechatUserInfoRequiresVerifiedAppToken(t *testing.T) {
	p, hits := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called for an unverified app token")
	})

	tok := &Token{AccessToken: "AT", OpenID: "OPENID", Channel: ChannelApp, Verified: false}
	user, err := p.UserInfo(context.Background(), tok)
	if user != nil {
		t.Fatalf("UserInfo must return nil user, got %+v", user)
	}
	if !errors.Is(err, ErrTokenNotVerified) {
		t.Fatalf("UserInfo err = %v, want ErrTokenNotVerified", err)
	}
	if *hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", *hits)
	}
}

// TestWechatUserInfoAppVerified 已校验的 app token 正常拉取（官方原文响应样例）。
func TestWechatUserInfoAppVerified(t *testing.T) {
	const avatar = "https://thirdwx.qlogo.cn/mmopen/g3MonUZtNHkdmzicIlibx6iaFqAc56vxLSUfpb6n5WKSYVY0ChQKQkiaJSgQ1dZuTOgvLLrhJbERQQ4eMsv84eavHiaiceqxibJxCfHe/0"
	p, _ := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/userinfo" {
			t.Errorf("path = %s, want /sns/userinfo", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("access_token"); got != "AT" {
			t.Errorf("access_token = %q", got)
		}
		if got := q.Get("openid"); got != "OPENID" {
			t.Errorf("openid = %q", got)
		}
		fmt.Fprintf(w, `{"openid":"OPENID","nickname":"NICKNAME","sex":1,"province":"PROVINCE",`+
			`"city":"CITY","country":"COUNTRY","headimgurl":%q,"privilege":["PRIVILEGE1"],`+
			`"unionid":"o6_bmasdasdsad6_2sgVt7hMZOPfL"}`, avatar)
	})

	tok := &Token{AccessToken: "AT", OpenID: "OPENID", Channel: ChannelApp, Verified: true}
	user, err := p.UserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("UserInfo: unexpected error: %v", err)
	}
	if user.Provider != ProviderWechat {
		t.Errorf("Provider = %q", user.Provider)
	}
	if user.ID != "OPENID" {
		t.Errorf("ID = %q, want OPENID", user.ID)
	}
	if user.Nickname != "NICKNAME" {
		t.Errorf("Nickname = %q", user.Nickname)
	}
	if user.AvatarURL != avatar {
		t.Errorf("AvatarURL = %q", user.AvatarURL)
	}
	if user.UnionID != "o6_bmasdasdsad6_2sgVt7hMZOPfL" {
		t.Errorf("UnionID = %q", user.UnionID)
	}
	if user.Email != "" {
		t.Errorf("wechat has no email, got %q", user.Email)
	}
}

// TestWechatUserInfoUnionIDOptional unionid 缺失（web 渠道常见）不报错，且退回 token 里的值。
func TestWechatUserInfoUnionIDOptional(t *testing.T) {
	p, _ := newWechatMockProvider(t, ChannelWeb, func(w http.ResponseWriter, r *http.Request) {
		// sex/province/city/country 自 2021-10-20 起官方不再返回
		fmt.Fprint(w, `{"openid":"OPENID","nickname":"NICKNAME","headimgurl":""}`)
	})

	tok := &Token{AccessToken: "AT", OpenID: "OPENID", UnionID: "UNION_FROM_TOKEN", Channel: ChannelWeb}
	user, err := p.UserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("UserInfo: unexpected error: %v", err)
	}
	if user.UnionID != "UNION_FROM_TOKEN" {
		t.Errorf("UnionID = %q, want fallback to token value", user.UnionID)
	}
	if user.Nickname != "NICKNAME" {
		t.Errorf("Nickname = %q", user.Nickname)
	}
}

// TestWechatRefreshKeepsAccessToken 未超时刷新只续期不换 token，必须以响应原值覆盖。
func TestWechatRefreshKeepsAccessToken(t *testing.T) {
	p, _ := newWechatMockProvider(t, ChannelWeb, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/oauth2/refresh_token" {
			t.Errorf("path = %s, want /sns/oauth2/refresh_token", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("appid"); got != "wx_test_appid" {
			t.Errorf("appid = %q", got)
		}
		if got := q.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q", got)
		}
		if got := q.Get("refresh_token"); got != "OLD_REFRESH" {
			t.Errorf("refresh_token = %q", got)
		}
		fmt.Fprint(w, `{"access_token":"OLD_TOKEN","expires_in":7200,"refresh_token":"NEW_REFRESH",`+
			`"openid":"OPENID","scope":"snsapi_login"}`)
	})

	src := &Token{
		AccessToken:  "OLD_TOKEN",
		RefreshToken: "OLD_REFRESH",
		ExpiresIn:    100,
		ExpiresAt:    time.Now().Add(-time.Second),
		OpenID:       "OPENID",
		UnionID:      "UNION",
		Channel:      ChannelWeb,
	}
	out, err := p.Refresh(context.Background(), src)
	if err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if out.AccessToken != "OLD_TOKEN" {
		t.Errorf("AccessToken = %q, want unchanged OLD_TOKEN", out.AccessToken)
	}
	if out.RefreshToken != "NEW_REFRESH" {
		t.Errorf("RefreshToken = %q, want NEW_REFRESH", out.RefreshToken)
	}
	if out.ExpiresIn != 7200 {
		t.Errorf("ExpiresIn = %d, want 7200", out.ExpiresIn)
	}
	if !out.ExpiresAt.After(time.Now().Add(7000 * time.Second)) {
		t.Errorf("ExpiresAt = %v, want refreshed to ~now+7200s", out.ExpiresAt)
	}
	// 刷新响应不含 unionid，必须保留原值
	if out.UnionID != "UNION" {
		t.Errorf("UnionID = %q, want preserved UNION", out.UnionID)
	}
	if out.Channel != ChannelWeb {
		t.Errorf("Channel = %q, want %q", out.Channel, ChannelWeb)
	}
	if src.RefreshToken != "OLD_REFRESH" || !src.ExpiresAt.Before(time.Now()) {
		t.Error("Refresh must not mutate the caller's token")
	}
}

// TestWechatRefreshTokenExpired 42002 → ErrRefreshDead（只能重新授权）。
func TestWechatRefreshTokenExpired(t *testing.T) {
	p, _ := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"errcode":42002,"errmsg":"refresh_token expired"}`)
	})

	_, err := p.Refresh(context.Background(), &Token{RefreshToken: "OLD_REFRESH", Channel: ChannelApp})
	if !errors.Is(err, ErrRefreshDead) {
		t.Fatalf("Refresh err = %v, want ErrRefreshDead", err)
	}
}

// TestWechatRefreshWithoutToken 没有 refresh_token 只能重新授权。
func TestWechatRefreshWithoutToken(t *testing.T) {
	p, hits := newWechatMockProvider(t, ChannelApp, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called without refresh_token")
	})

	if _, err := p.Refresh(context.Background(), &Token{AccessToken: "AT"}); !errors.Is(err, ErrRefreshDead) {
		t.Fatalf("Refresh err = %v, want ErrRefreshDead", err)
	}
	if *hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", *hits)
	}
}

// TestWechatErrorCodeMapping 文档 §1.3 全表 + §8.1.2 补充码的映射断言。
func TestWechatErrorCodeMapping(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{-1, ErrUpstream},         // system error
		{40001, ErrCredentialBad}, // invalid credential
		{40002, ErrUpstream},      // invalid grant_type
		{40003, ErrTokenMismatch}, // invalid openid
		{40013, ErrCredentialBad}, // invalid appid
		{40014, ErrTokenExpired},  // invalid access_token
		{40029, ErrCodeInvalid},   // invalid code
		{40030, ErrRefreshDead},   // invalid refresh_token
		{40066, ErrCredentialBad}, // invalid url
		{40163, ErrCodeInvalid},   // code been used
		{40164, ErrCredentialBad}, // invalid ip, not in whitelist
		{41001, ErrUpstream},      // access_token missing
		{41002, ErrNotConfigured}, // appid missing
		{41003, ErrRefreshDead},   // refresh_token missing
		{41004, ErrNotConfigured}, // appsecret missing
		{41008, ErrCodeInvalid},   // missing code
		{41009, ErrUpstream},      // missing openid
		{42001, ErrTokenExpired},  // access_token expired
		{42002, ErrRefreshDead},   // refresh_token expired
		{45009, ErrUpstream},      // daily quota
		{45011, ErrUpstream},      // minute quota
		{48001, ErrScopeMissing},  // api unauthorized
		{10060, ErrUpstream},      // 未上架移动应用日限额
		{99999, ErrUpstream},      // 未知码
	}
	for _, c := range cases {
		err := wechatErrf(c.code, "boom")
		if !errors.Is(err, c.want) {
			t.Errorf("errcode %d: errors.Is(%v) = false, want %v", c.code, err, c.want)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("errcode=%d", c.code)) {
			t.Errorf("errcode %d: error must keep raw code, got %v", c.code, err)
		}
	}
}

// TestWechatErrcode40001StaysCredentialBad 锁定 40001 的映射，防后人「顺手统一 token 失效语义」改坏。
//
// 微信 40001 官方原文是 `invalid credential, access_token is invalid or not latest` ——
// 平台方自己就没区分「AppSecret 错」与「access_token 无效」，这是个语义分裂的码。
// 本项目取 ErrCredentialBad 的实用理由：该码一旦出现，运维必须先去核对凭据配置
// （凭据错是致命且最常见的部署事故）；而纯 token 失效有 40014 / 42001 两个专门的码覆盖，
// 不会因此漏判。若将来有人把 40001 改判成 ErrTokenExpired / ErrRefreshDead，
// 本条断言会立刻红掉 —— 那是需重新裁定的决策，不是顺手重构。
func TestWechatErrcode40001StaysCredentialBad(t *testing.T) {
	err := wechatErrf(40001, "invalid credential, access_token is invalid or not latest")

	if !errors.Is(err, ErrCredentialBad) {
		t.Fatalf("errcode 40001 must map to ErrCredentialBad, got %v", err)
	}
	for _, forbidden := range []error{ErrTokenExpired, ErrRefreshDead, ErrCodeInvalid} {
		if errors.Is(err, forbidden) {
			t.Errorf("errcode 40001 must NOT be classified as %v, got %v", forbidden, err)
		}
	}
	// 原始码与官方 errmsg 必须原样保留，便于运维按码查文档
	if !strings.Contains(err.Error(), "errcode=40001") {
		t.Errorf("error must keep raw errcode 40001, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid credential") {
		t.Errorf("error must keep raw errmsg, got %v", err)
	}
}

// TestWechatNameAndChannel 两渠道 Name 相同、Channel 不同。
func TestWechatNameAndChannel(t *testing.T) {
	web := newWechat(ChannelWeb, wechatTestConfig())
	app := newWechat(ChannelApp, wechatTestConfig())
	if web.Name() != ProviderWechat || app.Name() != ProviderWechat {
		t.Fatalf("Name() = %q / %q, want %q", web.Name(), app.Name(), ProviderWechat)
	}
	if web.Channel() != ChannelWeb {
		t.Errorf("web Channel() = %q", web.Channel())
	}
	if app.Channel() != ChannelApp {
		t.Errorf("app Channel() = %q", app.Channel())
	}
}

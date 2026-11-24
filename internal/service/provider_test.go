package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/oauthkit"
)

// TestRenderTemplate 短信/邮件模板占位符替换。
func TestRenderTemplate(t *testing.T) {
	out := renderTemplate(`{"phone":"{{phone}}","code":"{{code}}","scene":"{{scene}}"}`,
		map[string]string{"phone": "13800138000", "code": "123456", "scene": "login"})
	if !strings.Contains(out, `"phone":"13800138000"`) ||
		!strings.Contains(out, `"code":"123456"`) ||
		!strings.Contains(out, `"scene":"login"`) {
		t.Errorf("模板渲染结果不正确: %s", out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("仍有未替换占位符: %s", out)
	}
}

// TestProvidersNotConfiguredByDefault 未配置时三个 provider 一律降级（Configured=false）。
func TestProvidersNotConfiguredByDefault(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	if NewMailer(d).Configured() {
		t.Error("SMTP 未配置时应为 false")
	}
	if NewSMSSender(d).Configured() {
		t.Error("短信渠道未配置时应为 false")
	}
	// T-37c：GitHub OAuth 改走 oauthkit，未配置时 oauthProvider 返回业务码 20026。
	if _, e := NewAuthService(d).oauthProvider(oauthkit.ProviderGitHub, oauthkit.ChannelWeb); e == nil ||
		e.BizCode != errcode.BizOAuthNotConfigured {
		t.Errorf("GitHub OAuth 未配置时应返回业务码 %d，实际 %v", errcode.BizOAuthNotConfigured, e)
	}
	// 未配置下的投递调用返回 ErrProviderNotConfigured（由上层降级为「只写 Redis + log」）。
	if err := NewMailer(d).Send("a@b.com", "s", "b"); err != ErrProviderNotConfigured {
		t.Errorf("邮件未配置应返回 ErrProviderNotConfigured，实际 %v", err)
	}
	if err := NewSMSSender(d).Send(context.Background(), "13800138000", "123456", "login"); err != ErrProviderNotConfigured {
		t.Errorf("短信未配置应返回 ErrProviderNotConfigured，实际 %v", err)
	}
}

// TestSMSChannelParsingAndSend 渠道 JSON 解析 + 真实 HTTP 网关调用（httptest 假网关）。
func TestSMSChannelParsingAndSend(t *testing.T) {
	var gotPhone, gotCode, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		gotPhone, gotCode = payload["phone"], payload["code"]
		gotKey = r.Header.Get("X-App-Key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	channels := []SMSChannel{{
		Name: "test-gw", Enabled: true, URL: srv.URL,
		Method: "POST", ContentType: "application/json",
		Headers:         map[string]string{"X-App-Key": "APPKEY"},
		BodyTemplate:    `{"phone":"{{phone}}","code":"{{code}}"}`,
		SuccessContains: []string{`"ok":true`},
		TimeoutMs:       3000,
	}}
	raw, _ := json.Marshal(channels)

	d := NewDeps(nil, nil, nil, nil, nil)
	// 直接构造带配置的 Deps：用 Config 值注入（Cfg 为 nil 时降级，这里替换为桩）。
	d.Cfg = stubConfig{"sms_channels": string(raw)}
	sender := NewSMSSender(d)
	if !sender.Configured() {
		t.Fatal("渠道配置存在时应 Configured=true")
	}
	if err := sender.Send(context.Background(), "13800138000", "654321", "login"); err != nil {
		t.Fatalf("短信发送应成功: %v", err)
	}
	if gotPhone != "13800138000" || gotCode != "654321" || gotKey != "APPKEY" {
		t.Errorf("网关收到参数不符: phone=%s code=%s key=%s", gotPhone, gotCode, gotKey)
	}
}

// TestSMSChannelResponseMismatch 响应不匹配时应判定失败（不静默成功）。
func TestSMSChannelResponseMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"msg":"balance low"}`))
	}))
	defer srv.Close()
	d := NewDeps(nil, nil, nil, nil, nil)
	d.Cfg = stubConfig{"sms_channels": `[{"name":"bad","enabled":true,"url":"` + srv.URL +
		`","bodyTemplate":"{{code}}","successContains":["\"ok\":true"]}]`}
	if err := NewSMSSender(d).Send(context.Background(), "13800138000", "1", "login"); err == nil {
		t.Error("响应不匹配时应返回错误")
	}
}

// TestGithubAuthorizeURL 已配置时生成正确授权链接并写入 state。
func TestGithubAuthorizeURL(t *testing.T) {
	kv := newMemKV()
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = kv
	d.Cfg = stubConfig{
		oauthkit.ConfigKeyGitHubClientID:     "cid",
		oauthkit.ConfigKeyGitHubClientSecret: "secret",
		oauthkit.ConfigKeyGitHubRedirectURI:  "https://api.example.com/api/v1/user/oauth/github/callback",
	}
	out, e := NewAuthService(d).GithubAuthURL(context.Background())
	if e != nil {
		t.Fatalf("生成授权链接失败: %v", e)
	}
	u, _ := out["url"].(string)
	for _, want := range []string{"client_id=cid", "state=", "redirect_uri=", "scope="} {
		if !strings.Contains(u, want) {
			t.Errorf("授权链接缺少 %s: %s", want, u)
		}
	}
	state, _ := out["state"].(string)
	if state == "" || !kv.has(oauthStatePrefix(oauthkit.ProviderGitHub, oauthkit.ChannelWeb)+state) {
		t.Errorf("state 应写入 Redis 键前缀 %s", oauthStatePrefix(oauthkit.ProviderGitHub, oauthkit.ChannelWeb))
	}
	if got := NewAuthService(d).oauthSuccessURL(oauthkit.ProviderGitHub, oauthkit.ChannelWeb); got != "/" {
		t.Errorf("未配置前端跳转地址时应回落 /，实际 %s", got)
	}
}

// TestOAuthStateFailClosed Redis 不可用时 state 校验必须拒绝（fail-closed）。
//
// 防的 bug（docs/14 §6.4）：旧实现 Redis 不可用即放行，CSRF 防护整条消失。
func TestOAuthStateFailClosed(t *testing.T) {
	// ① AuthTok 为 nil（降级模式）→ 503，不放行。
	d := NewDeps(nil, nil, nil, nil, nil)
	if e := NewAuthService(d).verifyOAuthState(context.Background(),
		oauthkit.ProviderGitHub, oauthkit.ChannelWeb, "anything"); e == nil ||
		e.BizCode != errcode.BizDependencyDown {
		t.Errorf("AuthTok 为 nil 时应拒绝并返回 503，实际 %v", e)
	}
	// ② Redis 报错 → 503，不放行。
	kv := newMemKV()
	kv.fail = true
	d2 := NewDeps(nil, nil, nil, nil, nil)
	d2.AuthTok = kv
	if e := NewAuthService(d2).verifyOAuthState(context.Background(),
		oauthkit.ProviderGitHub, oauthkit.ChannelWeb, "anything"); e == nil ||
		e.BizCode != errcode.BizDependencyDown {
		t.Errorf("Redis 报错时应拒绝并返回 503，实际 %v", e)
	}
	// ③ 签发 state 也 fail-closed。
	if _, e := NewAuthService(d2).issueOAuthState(context.Background(),
		oauthkit.ProviderGitHub, oauthkit.ChannelWeb); e == nil ||
		e.BizCode != errcode.BizDependencyDown {
		t.Errorf("Redis 报错时不得签发 state，实际 %v", e)
	}
	// ④ 空 state 先判参数错（禁令 3）。
	if e := NewAuthService(d).verifyOAuthState(context.Background(),
		oauthkit.ProviderGitHub, oauthkit.ChannelWeb, "  "); e == nil || e.BizCode != errcode.BizInvalidParam {
		t.Errorf("空 state 应返回参数错误，实际 %v", e)
	}
}

// TestVerifyCodeMailContent 验证码邮件正文包含验证码与场景标题。
func TestVerifyCodeMailContent(t *testing.T) {
	subject, body := verifyCodeMail("reset_password", "135790")
	if !strings.Contains(subject, "重置密码") {
		t.Errorf("标题不符: %s", subject)
	}
	if !strings.Contains(body, "135790") {
		t.Errorf("正文未包含验证码: %s", body)
	}
}

// TestIsEmailTarget 目标形态判定（邮件 vs 手机号走不同 provider）。
func TestIsEmailTarget(t *testing.T) {
	if !isEmailTarget("a@b.com") {
		t.Error("含 @ 应判为邮箱")
	}
	if isEmailTarget("13800138000") {
		t.Error("纯数字应判为手机号")
	}
}

// TestMaskPhoneInLog 日志脱敏（phone 掩码）。
func TestMaskPhoneInLog(t *testing.T) {
	if got := maskPhone("13800138000"); got != "138****8000" {
		t.Errorf("手机号脱敏不符: %s", got)
	}
}

// stubConfig 测试用配置桩：实现与 infra.ConfigService 相同的读取语义。
type stubConfig map[string]string

// 说明：此处用接口适配而非直接依赖 infra.ConfigService（其字段私有无法构造），
// 因此 Deps.Cfg 的类型在测试中由 cfgReader 接口承接。
func (s stubConfig) GetString(key, def string) string {
	if v, ok := s[key]; ok && v != "" {
		return v
	}
	return def
}

func (s stubConfig) GetBool(key string, def bool) bool {
	raw := s.GetString(key, "")
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return def
	}
}

func (s stubConfig) GetInt(key string, def int) int {
	raw := s.GetString(key, "")
	if raw == "" {
		return def
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// ensure model 引用保留（wallet/用户视图测试共用）。
var _ = model.User{}

// TestGithubStateOneTimeUse state 一次性消费（防 CSRF 重放）。
//
// 防的 bug：state 校验后不删除 → 同一个 state 可被反复用于换 token（重放授权码）。
func TestGithubStateOneTimeUse(t *testing.T) {
	kv := newMemKV()
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = kv
	d.Cfg = stubConfig{
		oauthkit.ConfigKeyGitHubClientID:     "cid",
		oauthkit.ConfigKeyGitHubClientSecret: "secret",
	}
	svc := NewAuthService(d)
	state, e := svc.issueOAuthState(context.Background(), oauthkit.ProviderGitHub, oauthkit.ChannelWeb)
	if e != nil {
		t.Fatalf("签发 state 失败: %v", e)
	}
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderGitHub, oauthkit.ChannelWeb, state); e != nil {
		t.Fatalf("首次校验应通过: %v", e)
	}
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderGitHub, oauthkit.ChannelWeb, state); e == nil {
		t.Error("同一 state 第二次校验应失败（一次性）")
	}
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderGitHub, oauthkit.ChannelWeb, ""); e == nil {
		t.Error("空 state 应校验失败")
	}
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderGitHub, oauthkit.ChannelWeb, "never-issued"); e == nil {
		t.Error("未签发的 state 应校验失败")
	}
	// 跨渠道重放：微信 web 的 state 不能拿去校验 QQ web 的回调。
	stateWx, e := svc.issueOAuthState(context.Background(), oauthkit.ProviderWechat, oauthkit.ChannelWeb)
	if e != nil {
		t.Fatalf("签发微信 state 失败: %v", e)
	}
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderQQ, oauthkit.ChannelWeb, stateWx); e == nil {
		t.Error("跨渠道复用 state 应校验失败")
	}
}

// TestEncodeHeaderNonASCII 中文标题/发件人名用 RFC 2047 编码，纯 ASCII 原样。
//
// 防的 bug：中文主题未编码 → 收件箱显示乱码或被 SMTP 服务器拒收。
func TestEncodeHeaderNonASCII(t *testing.T) {
	if got := encodeHeader("Plain Subject"); got != "Plain Subject" {
		t.Errorf("ASCII 应原样返回，实际 %q", got)
	}
	got := encodeHeader("重置密码验证码")
	if !strings.HasPrefix(got, "=?UTF-8?B?") || !strings.HasSuffix(got, "?=") {
		t.Errorf("中文应做 RFC 2047 编码，实际 %q", got)
	}
}

// TestBase64LinesWrapping MIME base64 每行不超过 76 字符且以 CRLF 结尾。
//
// 防的 bug：不折行 → 部分 SMTP 网关截断/拒收长行（邮件乱码或丢失）。
func TestBase64LinesWrapping(t *testing.T) {
	out := base64Lines([]byte(strings.Repeat("PicArt-验证码-", 40)))
	if !strings.HasSuffix(out, "\r\n") {
		t.Error("MIME 正文应以 CRLF 结尾")
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n") {
		if len(line) > 76 {
			t.Errorf("base64 行超出 76 字符: %d", len(line))
		}
	}
}

// TestSMSSenderChannelFailover 多渠道：首个渠道失败自动切下一个（可用性要求）。
//
// 防的 bug：只尝试第一个渠道，主渠道挂了短信全发不出（用户无法登录）。
func TestSMSSenderChannelFailover(t *testing.T) {
	badCalls, goodCalls := 0, 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer good.Close()

	channels := []SMSChannel{
		{Name: "primary", Enabled: true, URL: bad.URL, BodyTemplate: "{{code}}"},
		{Name: "backup", Enabled: true, URL: good.URL, BodyTemplate: "{{code}}",
			SuccessContains: []string{`"ok":true`}},
	}
	raw, _ := json.Marshal(channels)
	d := NewDeps(nil, nil, nil, nil, nil)
	d.Cfg = stubConfig{"sms_channels": string(raw)}

	if err := NewSMSSender(d).Send(context.Background(), "13800138000", "111111", "login"); err != nil {
		t.Fatalf("备用渠道可用时应成功: %v", err)
	}
	if badCalls != 1 || goodCalls != 1 {
		t.Errorf("应各调用一次，实际 bad=%d good=%d", badCalls, goodCalls)
	}
}

// TestSMSSenderGETMethod GET 方式发送（部分网关只支持 query 传参）。
func TestSMSSenderGETMethod(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	raw, _ := json.Marshal([]SMSChannel{{
		Name: "get-gw", Enabled: true, URL: srv.URL, Method: "GET",
		BodyTemplate: `{"code":"{{code}}"}`, SuccessContains: []string{"success"},
	}})
	d := NewDeps(nil, nil, nil, nil, nil)
	d.Cfg = stubConfig{"sms_channels": string(raw)}
	if err := NewSMSSender(d).Send(context.Background(), "13800138000", "246810", "login"); err != nil {
		t.Fatalf("GET 渠道发送失败: %v", err)
	}
	if !strings.Contains(gotQuery, "payload=") {
		t.Errorf("GET 请求应把模板放进 query，实际 %q", gotQuery)
	}
}

// TestSMSChannelsDisabledIgnored 关闭的渠道/非法 JSON 视为未配置（降级为开发模式）。
//
// 防的 bug：enabled=false 的渠道仍被调用，或非法 JSON 直接 panic 让发送接口 500。
func TestSMSChannelsDisabledIgnored(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)

	d.Cfg = stubConfig{"sms_channels": `[{"name":"off","enabled":false,"url":"http://127.0.0.1:1/x"}]`}
	if NewSMSSender(d).Configured() {
		t.Error("全部渠道关闭时应 Configured=false")
	}
	d.Cfg = stubConfig{"sms_channels": `not-a-json`}
	if NewSMSSender(d).Configured() {
		t.Error("非法 JSON 应降级为未配置")
	}
	if err := NewSMSSender(d).Send(context.Background(), "13800138000", "1", "login"); err != ErrProviderNotConfigured {
		t.Errorf("非法配置时应返回 ErrProviderNotConfigured，实际 %v", err)
	}
}

package oauthkit_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
)

// Example 最小可用示例：构造微信网站应用 provider 并生成授权链接。
//
// 这是 SDK 最常见的起点：New → Configured → AuthURL。
// 授权链接必须以 #wechat_redirect 收尾（微信硬要求，少写会白屏）。
func Example() {
	p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelWeb, oauthkit.Config{
		ClientID:     "wx1234567890abcdef",
		ClientSecret: "app-secret",
		RedirectURI:  "https://api.example.com/oauth/callback",
	})
	if err != nil {
		fmt.Println("new failed:", err)
		return
	}
	if !p.Configured() {
		fmt.Println("provider not configured")
		return
	}

	url, err := p.AuthURL("state-abc123")
	if err != nil {
		fmt.Println("auth url failed:", err)
		return
	}
	fmt.Println(p.Name(), p.Channel())
	fmt.Println(url)
	// Output:
	// wechat web
	// https://open.weixin.qq.com/connect/qrconnect?appid=wx1234567890abcdef&redirect_uri=https%3A%2F%2Fapi.example.com%2Foauth%2Fcallback&response_type=code&scope=snsapi_login&state=state-abc123#wechat_redirect
}

// ExampleNew_appChannel 展示「渠道不支持某方法」的语义：微信移动应用不出授权 URL
// （登录由微信客户端 SDK 拉起），后端只做 code 换 token。
//
// 这类失败返回哨兵 ErrAuthURLUnsupported，而不是 panic 或空串 —— 调用方能区分
// 「渠道设计上不支持」与「配置错了」。
func ExampleNew_appChannel() {
	p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelApp, oauthkit.Config{
		ClientID:     "wxaabbccddeeff0011",
		ClientSecret: "app-secret",
	})
	if err != nil {
		fmt.Println("new failed:", err)
		return
	}

	_, err = p.AuthURL("state-abc123")
	fmt.Println(p.Name(), p.Channel())
	fmt.Println("AuthURL unsupported:", errors.Is(err, oauthkit.ErrAuthURLUnsupported))
	// Output:
	// wechat app
	// AuthURL unsupported: true
}

// Example_qqAppValidateToken 展示 QQ 移动端的正确用法：客户端上报的 token
// **不可信**，必须先 ValidateToken 再 UserInfo。
//
// 这里不发网络请求（调用方传的是空 token，SDK 在打上游之前就拒了），
// 目的是演示错误分类：空参数 → ErrTokenExpired / ErrTokenMismatch 之类的平台语义，
// 而「未校验就 UserInfo」→ ErrTokenNotVerified（运行时硬约束，不是文档提醒）。
func Example_qqAppValidateToken() {
	p, err := oauthkit.New(oauthkit.ProviderQQ, oauthkit.ChannelApp, oauthkit.Config{
		ClientID: "102065886", // QQ 移动端后端不需要 AppKey（不做换票）
	})
	if err != nil {
		fmt.Println("new failed:", err)
		return
	}

	// ❌ 直接拿客户端上报的 token 当可信 token 用：
	tok := &oauthkit.Token{AccessToken: "client-supplied", OpenID: "FAKE_OID", Channel: oauthkit.ChannelApp}
	_, err = p.UserInfo(context.Background(), tok)
	fmt.Println("unverified token rejected:", errors.Is(err, oauthkit.ErrTokenNotVerified))

	// ✅ 正确路径（真机需要真实 token，这里跳过网络）：先 ValidateToken 置 Verified
	fmt.Println("must call ValidateToken first")
	// Output:
	// unverified token rejected: true
	// must call ValidateToken first
}

// ExampleConfigKeys 展示配置键的唯一来源：键名由 SDK 给出，调用方不再手写字符串。
//
// 微信 web 与 app 是两套 appid，键名按 <provider>_<channel>_<field> 分开；
// QQ app 故意没有 appkey（后端不做换票，少一个泄漏面）。
func ExampleConfigKeys() {
	idKey, secretKey, redirectKey, scopeKey, successKey := oauthkit.ConfigKeys(
		oauthkit.ProviderWechat, oauthkit.ChannelWeb)
	fmt.Println(idKey, secretKey, redirectKey, scopeKey, successKey)

	idKey, secretKey, _, _, _ = oauthkit.ConfigKeys(oauthkit.ProviderQQ, oauthkit.ChannelApp)
	fmt.Println(idKey, "secret key empty:", secretKey == "")
	// Output:
	// wechat_web_appid wechat_web_secret wechat_web_redirect_uri  wechat_web_success_redirect_url
	// qq_app_appid secret key empty: true
}

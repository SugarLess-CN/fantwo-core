//go:build credentials

// 第 2 层：凭据有效性测试（真打上游，**不进常规 CI**）。
//
// 与第 1 层的分工：
//   - 第 1 层（fixtures_*_test.go）用固化的真实响应，零网络，CI 每次跑；
//   - 本层真的打微信 / QQ 上游，验证「凭据是不是还有效、错误分类是不是还准」，
//     只在上线前 / 换密钥后 / 怀疑上游变了时手动跑。
//
// 运行方式（凭据从环境变量读，**绝不硬编码**）：
//
//	set -a && source tmp/oauth-credentials.local.env && set +a
//	go test -tags credentials ./pkg/oauthkit/ -run 'Credentials' -v
//
// # 核心判读：验证凭据有效性不需要真 code
//
// 平台是「先校验 appid+secret，再查 code」，所以假 code 拿到的错误码就能反推凭据状态：
//
//	微信  {"errcode":40029}                → ✅ 凭据有效（code 无效而已）
//	微信  {"errcode":40013 / 40125}        → ❌ appid / secret 错
//	QQ    {"error":100005 / 100019}        → ✅ 凭据有效（code 错）
//	QQ    {"error":100007 / 100010}        → ❌ 凭据错
//
// # QQ 的已知限制（重要，别误判成「凭据坏了」）
//
// 从**服务器侧直连**时，`graph.qq.com` 的 `/oauth2.0/token` 与 `/oauth2.0/authorize`
// 会被腾讯 WAF 用 HTTP 501 + HTML 跳转页拦掉（实测：加 Referer/Accept/UA、
// 间隔 10 秒重试均无效）。而 `/oauth2.0/me` 与 `/user/get_user_info` 正常可达。
// 所以 QQ web 的凭据测试只能接受两种结果：ErrCodeInvalid（凭据有效）
// 或 ErrUpstream（WAF 拦），**不能**因 ErrUpstream 判失败。
// 真实浏览器跳转不受该风控影响。
package oauthkit_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
)

// qqThrottle QQ 上游有频率限制：连续快速请求会被 WAF 返回 501。
//
// 间隔 ≥ 3 秒即恢复（README §6）。测试里所有打 graph.qq.com 的地方都先过这里，
// 否则「跑得快」会被误读成「凭据坏了」。
var lastQQCall time.Time

func qqThrottle() {
	const gap = 3 * time.Second
	if d := time.Since(lastQQCall); d < gap {
		time.Sleep(gap - d)
	}
	lastQQCall = time.Now()
}

// envOrSkip 读环境变量；缺失则跳过（CI 无凭据时不应红）。
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("未配置 %s，跳过凭据测试（set -a && source 凭据文件 && set +a）", key)
	}
	return v
}

// TestWechatWebCredentialsValid 微信网站应用：假 code 应得 ErrCodeInvalid 而非 ErrCredentialBad。
func TestWechatWebCredentialsValid(t *testing.T) {
	p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelWeb, oauthkit.Config{
		ClientID:     envOrSkip(t, "WECHAT_WEB_APPID"),
		ClientSecret: envOrSkip(t, "WECHAT_WEB_SECRET"),
		RedirectURI:  "https://example.com/api/v1/user/oauth/wechat/web/callback",
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	_, err = p.Exchange(context.Background(), "FAKE_CODE_FOR_CREDENTIAL_CHECK")
	if err == nil {
		t.Fatal("假 code 不应换到 token")
	}
	if errors.Is(err, oauthkit.ErrCredentialBad) {
		t.Fatalf("凭据无效（appid/secret 错）：%v", err)
	}
	if !errors.Is(err, oauthkit.ErrCodeInvalid) {
		t.Fatalf("期望 ErrCodeInvalid（= 凭据有效、code 无效），实际 %v", err)
	}
	t.Logf("凭据有效：上游报 code 无效（错误分类 = %s）", "code_invalid")
}

// TestWechatAppCredentialsValid 微信移动应用（另一套 appid）。
func TestWechatAppCredentialsValid(t *testing.T) {
	p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelApp, oauthkit.Config{
		ClientID:     envOrSkip(t, "WECHAT_APP_APPID"),
		ClientSecret: envOrSkip(t, "WECHAT_APP_SECRET"),
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	_, err = p.Exchange(context.Background(), "FAKE_CODE_FOR_CREDENTIAL_CHECK")
	if errors.Is(err, oauthkit.ErrCredentialBad) {
		t.Fatalf("凭据无效（appid/secret 错）：%v", err)
	}
	if !errors.Is(err, oauthkit.ErrCodeInvalid) {
		t.Fatalf("期望 ErrCodeInvalid，实际 %v", err)
	}
}

// TestQQWebCredentialsOrWAF /token 的凭据测试。
//
// **两种结果都算通过**：ErrCodeInvalid（凭据有效且未被拦）或 ErrUpstream（WAF 拦）。
// 判失败只有一种情况：ErrCredentialBad（100007/100010 之类 → 凭据真的坏了）。
func TestQQWebCredentialsOrWAF(t *testing.T) {
	appID := envOrSkip(t, "QQ_WEB_APPID")
	appKey := envOrSkip(t, "QQ_WEB_APPKEY")
	qqThrottle()

	p, err := oauthkit.New(oauthkit.ProviderQQ, oauthkit.ChannelWeb, oauthkit.Config{
		ClientID:     appID,
		ClientSecret: appKey,
		RedirectURI:  "https://example.com/api/v1/user/oauth/qq/web/callback",
		Scope:        oauthkit.DefaultScopeQQ,
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	_, err = p.Exchange(context.Background(), "FAKE_CODE_FOR_CREDENTIAL_CHECK")
	switch {
	case err == nil:
		t.Fatal("假 code 不应换到 token")
	case errors.Is(err, oauthkit.ErrCredentialBad):
		t.Fatalf("凭据无效（100007/100010 之类）：%v", err)
	case errors.Is(err, oauthkit.ErrCodeInvalid):
		t.Logf("凭据有效且未被 WAF 拦：上游报 code 无效")
	case errors.Is(err, oauthkit.ErrUpstream):
		// 已知限制：/oauth2.0/token 在服务器侧直连会被 WAF 拦（HTTP 501 + HTML 页）。
		// SDK 把它正确归为 ErrUpstream，说明「能解析≠是平台响应」的判定在工作。
		t.Logf("被 WAF 拦（已知限制，非凭据问题）：%v", err)
	default:
		t.Fatalf("意外的错误分类：%v", err)
	}
}

// TestQQAppValidateTokenReachesUpstream QQ app 的 ValidateToken 打 /oauth2.0/me。
//
// 该端点**不受 WAF 影响**，所以能真正验证「上游可达 + 错误映射正确」：
// 假 token → 100013 → ErrTokenExpired（不是 ErrUpstream、不是 ErrCredentialBad）。
func TestQQAppValidateTokenReachesUpstream(t *testing.T) {
	appID := envOrSkip(t, "QQ_APP_APPID")
	qqThrottle()

	p, err := oauthkit.New(oauthkit.ProviderQQ, oauthkit.ChannelApp, oauthkit.Config{ClientID: appID})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	_, err = p.ValidateToken(context.Background(), &oauthkit.Token{
		AccessToken: "FAKE_ACCESS_TOKEN",
		OpenID:      "FAKE_OPENID",
		Channel:     oauthkit.ChannelApp,
	})
	if !errors.Is(err, oauthkit.ErrTokenExpired) {
		t.Fatalf("假 token 应得 ErrTokenExpired（100013），实际 %v", err)
	}
	t.Logf("上游可达且错误映射正确：100013 → ErrTokenExpired")
}

// TestQQAppUserInfoWithoutValidateNeverHitsUpstream 安全断言：未校验的 app token
// 连上游都不该打（否则等于用不可信数据换一次网络往返）。
//
// 这条**不需要凭据**（在打网络前就拒），但仍放在 credentials tag 下，
// 因为它验证的是「真实 provider 实例」而非 mock。
func TestQQAppUserInfoWithoutValidateNeverHitsUpstream(t *testing.T) {
	p, err := oauthkit.New(oauthkit.ProviderQQ, oauthkit.ChannelApp, oauthkit.Config{
		ClientID: "102065886", // 仅用于构造；本用例不会发请求
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	start := time.Now()
	_, err = p.UserInfo(context.Background(), &oauthkit.Token{
		AccessToken: "FAKE",
		OpenID:      "FAKE_OID",
		Channel:     oauthkit.ChannelApp,
		Verified:    false,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, oauthkit.ErrTokenNotVerified) {
		t.Fatalf("未校验的 app token 必须被 ErrTokenNotVerified 拒，实际 %v", err)
	}
	// 无网络往返的旁证：本地拒绝应在毫秒内返回（真打上游至少几十毫秒）。
	if elapsed > 200*time.Millisecond {
		t.Errorf("未校验 token 的拒绝应无网络往返，耗时 %v 偏长", elapsed)
	}
}

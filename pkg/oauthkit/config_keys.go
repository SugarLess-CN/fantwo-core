// Package oauthkit 配置键常量（唯一定义源）。
//
// 口径：`<provider>_<channel>_<field>`。这些常量同时被两处引用：
//   - `internal/model/config.go` 的 `DefaultConfigSeeds`（种子）
//   - `internal/model/config.go` 的 `ConfigSensitiveKeys`（打码白名单）
//   - 调用方从配置服务读值拼键
//
// 因此键名只在这里定义一次，任何地方都不许再写字面量。
//
// # 为什么微信 / QQ 各有 web 与 app 两套 appid
//
// 开放平台里「网站应用」与「移动应用」是两个独立应用，appid 不同：
//   - 同一用户的 openid 在两个 appid 下**不同**，unionid 相同；
//   - 所以登录查找必须「先按 unionid、查不到再按 openid」，否则同一人 PC + App 会变成两个账号。
//
// # 为什么 QQ app 没有 AppKey
//
// QQ 移动端由客户端 SDK 完成换 token（直接返回 access_token + openid），
// 后端只做 ValidateToken（校验 openid 一致性）+ UserInfo，用不到 AppKey。
// 少配一个凭据就少一个泄漏面。
package oauthkit

// 微信 - 网站应用（扫码登录）。
const (
	ConfigKeyWechatWebAppID       = "wechat_web_appid"
	ConfigKeyWechatWebSecret      = "wechat_web_secret"
	ConfigKeyWechatWebRedirectURI = "wechat_web_redirect_uri"
	ConfigKeyWechatWebSuccessURL  = "wechat_web_success_redirect_url"
)

// 微信 - 移动应用（App 内微信 SDK 授权，后端用 code 换 token）。
const (
	ConfigKeyWechatAppAppID  = "wechat_app_appid"
	ConfigKeyWechatAppSecret = "wechat_app_secret"
)

// QQ - 网站应用（网页登录）。
const (
	ConfigKeyQQWebAppID       = "qq_web_appid"
	ConfigKeyQQWebAppKey      = "qq_web_appkey"
	ConfigKeyQQWebRedirectURI = "qq_web_redirect_uri"
	ConfigKeyQQWebSuccessURL  = "qq_web_success_redirect_url"
)

// QQ - 移动应用（QQ SDK 直接给 token，后端不换 token；**无 AppKey**）。
const (
	ConfigKeyQQAppAppID = "qq_app_appid"
)

// GitHub - 网页 OAuth（唯一渠道）。
//
// 沿用既有键名（github_client_id 等），不破坏已有部署的 env 配置。
const (
	ConfigKeyGitHubClientID     = "github_client_id"
	ConfigKeyGitHubClientSecret = "github_client_secret"
	ConfigKeyGitHubRedirectURI  = "github_redirect_uri"
	ConfigKeyGitHubScope        = "github_scope"
	ConfigKeyGitHubSuccessURL   = "github_success_redirect_url"
)

// SensitiveConfigKeys 会被打码的配置键（管理端读值返回 ***）。
//
// 只列真正的凭据。appid / client_id 类只是公开标识，**不打码**（便于运维核对填错）。
// 注意 qq_app 没有 AppKey，所以不在列。
var SensitiveConfigKeys = []string{
	ConfigKeyWechatWebSecret,
	ConfigKeyWechatAppSecret,
	ConfigKeyQQWebAppKey,
	ConfigKeyGitHubClientSecret,
}

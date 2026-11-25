// Package oauthkit 配置与工厂。
//
// 配置键命名口径：`<provider>_<channel>_<field>`（docs/14 §8.5 问题 1）。
//
//	wechat_web_appid / wechat_web_secret / wechat_web_redirect_uri
//	wechat_app_appid / wechat_app_secret
//	qq_web_appid   / qq_web_appkey   / qq_web_redirect_uri
//	qq_app_appid                      ← 注意：没有 qq_app_appkey
//	github_client_id / github_client_secret / github_redirect_uri / github_scope
//
// 为什么 `qq_app_appkey` 故意不设：QQ 移动端由客户端 SDK 自己完成换 token，
// 后端只做 ValidateToken + UserInfo，用不到 appkey —— 少一个泄漏面。
package oauthkit

import (
	"fmt"
	"strings"
)

// Config 单个 provider 实例的配置。
//
// 由调用方从「DB 配置优先 / env 兜底」的读取层填充，SDK 不自取配置。
type Config struct {
	// ClientID 应用标识。微信=appid，QQ=appid，GitHub=client_id。
	ClientID string
	// ClientSecret 应用密钥。微信=secret，QQ=appkey，GitHub=client_secret。
	// QQ app 渠道留空（不需要）。
	ClientSecret string
	// RedirectURI 回调地址。GO 侧拼授权 URL 与换 token 都要带，必须与开放平台登记一致。
	RedirectURI string
	// Scope 授权作用域。
	//
	//	微信 web  snsapi_login（固定）
	//	微信 app  snsapi_userinfo（App 只能填这个，且授权后必带 unionid）
	//	QQ         get_user_info
	//	GitHub     read:user user:email
	Scope string
	// SuccessRedirectURL 登录成功后前端跳转地址（重定向模式用，可选）。
	SuccessRedirectURL string
}

// 各平台默认 scope（调用方不填时用）。
const (
	DefaultScopeWechatWeb = "snsapi_login"
	DefaultScopeWechatApp = "snsapi_userinfo"
	DefaultScopeQQ        = "get_user_info"
	DefaultScopeGitHub    = "read:user user:email"
)

// normalize 补默认值并做基本校验。
func (c *Config) normalize(provider, channel string) error {
	if c.Scope == "" {
		switch provider {
		case ProviderWechat:
			if channel == ChannelApp {
				c.Scope = DefaultScopeWechatApp
			} else {
				c.Scope = DefaultScopeWechatWeb
			}
		case ProviderQQ:
			c.Scope = DefaultScopeQQ
		case ProviderGitHub:
			c.Scope = DefaultScopeGitHub
		}
	}
	// 微信 / QQ 的 scope 与 GitHub 不同，不做空格切分校验，只做非空检查。
	if provider == ProviderGitHub && c.Scope == "" {
		return fmt.Errorf("oauthkit: github scope must not be empty")
	}
	return nil
}

// ConfigKeyPrefix 返回该 (provider, channel) 的配置键前缀，例如 "wechat_web_"。
//
// 供调用方从配置服务读值时拼键用，避免键名散落在多处硬编码。
func ConfigKeyPrefix(provider, channel string) string {
	return provider + "_" + channel + "_"
}

// ConfigKeys 返回该 (provider, channel) 需要的配置键全名。
//
// 返回顺序固定：clientID, clientSecret, redirectURI, scope, successRedirectURL。
// 不需要的键返回空串（如 qq_app 的 clientSecret）。
func ConfigKeys(provider, channel string) (clientID, clientSecret, redirectURI, scope, successURL string) {
	switch provider + "/" + channel {
	case ProviderWechat + "/" + ChannelWeb:
		return ConfigKeyWechatWebAppID, ConfigKeyWechatWebSecret,
			ConfigKeyWechatWebRedirectURI, "", ConfigKeyWechatWebSuccessURL
	case ProviderWechat + "/" + ChannelApp:
		return ConfigKeyWechatAppAppID, ConfigKeyWechatAppSecret, "", "", ""
	case ProviderQQ + "/" + ChannelWeb:
		return ConfigKeyQQWebAppID, ConfigKeyQQWebAppKey,
			ConfigKeyQQWebRedirectURI, "", ConfigKeyQQWebSuccessURL
	case ProviderQQ + "/" + ChannelApp:
		// qq_app 没有 appkey
		return ConfigKeyQQAppAppID, "", "", "", ""
	case ProviderGitHub + "/" + ChannelWeb:
		return ConfigKeyGitHubClientID, ConfigKeyGitHubClientSecret,
			ConfigKeyGitHubRedirectURI, ConfigKeyGitHubScope, ConfigKeyGitHubSuccessURL
	}
	return "", "", "", "", ""
}

// New 按 (provider, channel) 构造 Provider。
//
//	name:    ProviderWechat / ProviderQQ / ProviderGitHub
//	channel: ChannelWeb / ChannelApp（mp 与 device 本轮未实现）
func New(name, channel string, cfg Config) (Provider, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	channel = strings.ToLower(strings.TrimSpace(channel))

	if err := cfg.normalize(name, channel); err != nil {
		return nil, err
	}

	switch name {
	case ProviderWechat:
		switch channel {
		case ChannelWeb, ChannelApp:
			return newWechat(channel, cfg), nil
		case ChannelMP:
			return nil, fmt.Errorf("oauthkit: wechat channel %q not implemented yet", channel)
		}
	case ProviderQQ:
		switch channel {
		case ChannelWeb, ChannelApp:
			return newQQ(channel, cfg), nil
		}
	case ProviderGitHub:
		if channel == ChannelWeb {
			return newGitHub(cfg), nil
		}
		if channel == ChannelDevice {
			return nil, fmt.Errorf("oauthkit: github channel %q not implemented yet", channel)
		}
	}
	return nil, fmt.Errorf("oauthkit: unsupported provider %q channel %q", name, channel)
}

// QQ 机器人配置键 —— **唯一定义源**。
//
// 与 pkg/oauthkit/config_keys.go 同口径：业务代码引用这里的常量，
// model/config.go 的种子清单也从这里取，避免「裸字符串」在多个文件里各写一遍。
package qqbot

// 配置键名（DB 存储用的 key，snake_case 对齐全站规范）。
const (
	// CfgEnabled 业务侧总开关。SDK 不感知此键（它不读配置体系）；
	// 关闭时由调用方决定不构造 Client 或直接返回未配置错误。
	CfgEnabled = "qqbot_enabled"
	// CfgAppID 机器人 AppID（开放平台管理端获取）。
	CfgAppID = "qqbot_app_id"
	// CfgAppSecret 机器人 ClientSecret（获取 access_token 用）。**敏感**。
	CfgAppSecret = "qqbot_app_secret"
	// CfgBotSecret webhook 签名密钥（ed25519 seed 来源）。**敏感**。
	//
	// 注意与 AppSecret 是**两个不同凭据**：BotSecret 只用于校验 webhook 签名，
	// 拿 AppSecret 去算签名会静默验不过（平台侧叫「机器人密钥」）。
	CfgBotSecret = "qqbot_bot_secret"

	// CfgAPIBase API 根地址，默认 https://api.bot.qq.com。
	CfgAPIBase = "qqbot_api_base"
	// CfgTokenURL 取 access_token 的地址，默认 https://api.bot.qq.com/app/getAppAccessToken。
	CfgTokenURL = "qqbot_token_url"

	// CfgInviteAutoReply 收到邀请码申请时的自动回复文案。
	CfgInviteAutoReply = "qqbot_invite_auto_reply"
	// CfgInviteCommand 申请邀请码的关键字（默认「邀请码」）。
	CfgInviteCommand = "qqbot_invite_command"
)

// DefaultAPIBase 默认 API 根地址。
const DefaultAPIBase = "https://api.bot.qq.com"

// DefaultTokenURL 默认 token 地址。
const DefaultTokenURL = "https://api.bot.qq.com/app/getAppAccessToken"

// SensitiveConfigKeys 需要打码的配置项（对齐 model.ConfigSensitiveKeys 的语义）。
//
// 只列真正的凭据：AppSecret 与 BotSecret。AppID 不敏感（它会出现在回调请求头里）。
var SensitiveConfigKeys = []string{
	CfgAppSecret,
	CfgBotSecret,
}

// Config 客户端构造所需配置。
//
// 由调用方从自己的配置体系（本项目是 DB 优先 + env 兜底）组装后传入，
// SDK 本身不读 DB/env —— 保持它可被任意项目直接引用。
type Config struct {
	APIBase   string
	TokenURL  string
	AppID     string
	AppSecret string
	BotSecret string
	// HTTPTimeout 单次请求超时，0 表示用默认 10s。
	HTTPTimeout int
}

// normalize 补默认值。
func (c Config) normalize() Config {
	if c.APIBase == "" {
		c.APIBase = DefaultAPIBase
	}
	if c.TokenURL == "" {
		c.TokenURL = DefaultTokenURL
	}
	return c
}

// Validate 校验必填项。
func (c Config) Validate() error {
	if c.AppID == "" || c.AppSecret == "" {
		return ErrNotConfigured
	}
	return nil
}

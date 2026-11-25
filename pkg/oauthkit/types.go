// 本文件定义 Provider 接口 + 归一化数据结构（Token / User）+ 全部哨兵错误。
//
// 包级说明、快速开始示例、渠道矩阵与安全约束见 doc.go。
//
// 设计原则（细节见 doc.go）：
//  1. 只抽象四件事：拼授权 URL、code 换 token、token 换用户、刷新 token；
//     平台差异（QQ 的裸 query string / JSONP / 直给 token）全部收敛在各自实现内。
//  2. 不持有 Redis / DB / 日志：纯协议层。state 存取、账号绑定、双 token 签发
//     都属于调用方职责，SDK 不碰。
//  3. 按 (provider, channel) 实例化：微信移动应用与网站应用是两个独立 appid，
//     但账号唯一键仍按平台算 —— 所以 Name() 给平台、Channel() 给渠道，两者分开。
//  4. 安全约束做成运行时硬约束：QQ 移动端的 token 由客户端上报、可伪造，
//     UserInfo 对未经过 ValidateToken 的 app token 直接拒绝，不靠注释提醒。
package oauthkit

import (
	"context"
	"errors"
	"time"
)

// 平台标识（Name 返回值）。与配置前缀、DB 字段前缀、路由段一致。
const (
	ProviderWechat = "wechat"
	ProviderQQ     = "qq"
	ProviderGitHub = "github"
)

// 渠道标识（Channel 返回值）。
const (
	// ChannelWeb 网页渠道：微信网站应用扫码 / QQ 网页登录 / GitHub OAuth。
	ChannelWeb = "web"
	// ChannelApp 移动应用渠道：微信移动应用 / QQ 移动应用（客户端 SDK 授权）。
	ChannelApp = "app"
	// ChannelMP 微信公众号（微信内 H5 网页授权）。本轮不实现，预留。
	ChannelMP = "mp"
	// ChannelDevice GitHub Device Flow（CLI / 无浏览器场景）。本轮不实现，预留。
	ChannelDevice = "device"
)

// Provider 第三方登录提供方。
//
// 一个实例对应一个 (platform, channel) 组合，例如 New(ProviderWechat, ChannelWeb, cfg)。
// 实现方只在自身渠道支持的方法上工作，不支持的方法返回对应哨兵错误：
//   - AuthURL  → ErrAuthURLUnsupported
//   - Exchange → ErrExchangeUnsupported
//   - Refresh  → ErrRefreshUnsupported
type Provider interface {
	// Name 平台标识："wechat" / "qq" / "github"。
	Name() string

	// Channel 渠道标识："web" / "app" / "mp" / "device"。
	Channel() string

	// Configured 是否已配置齐全（appid + secret/appkey 非空）。
	//
	// 未配置时所有网络方法返回 ErrNotConfigured，调用方据此把端点降级为
	// 业务码 20026（AuthOAuthNotConfigured）而不是 500。
	Configured() bool

	// AuthURL 生成授权跳转 URL。
	//
	// state 由调用方生成并自行存储校验（SDK 不碰 Redis）。
	// 微信实现必须自动追加 #wechat_redirect 锚点 —— 少写会白屏，这是官方硬要求。
	AuthURL(state string) (string, error)

	// Exchange 用授权 code 换取 token。
	//
	// 微信 / QQ 在此阶段即可拿到 OpenID；GitHub 的用户 ID 要等 UserInfo。
	// 微信 app 渠道同样走这里（code 换 token 端点与 web 完全一致，只是 appid 不同）。
	Exchange(ctx context.Context, code string) (*Token, error)

	// ValidateToken 校验 token 真实性并归一化。
	//
	// 只对「token 由客户端上报」的渠道有意义（QQ app、微信 app 的 token 复用场景）。
	// QQ 实现必须调 oauth2.0/me 校验，并比对返回的 openid 与 tok.OpenID 一致；
	// GitHub 实现无需校验（token 只可能来自本方 Exchange），直接返回 tok 并置 Verified。
	ValidateToken(ctx context.Context, tok *Token) (*Token, error)

	// UserInfo 用 token 拉取归一化用户信息。
	//
	// 实现方必须拒绝 Channel == ChannelApp && !Verified 的 token（ErrTokenNotVerified），
	// 防止客户端伪造 openid + token 组合直接建号。
	UserInfo(ctx context.Context, tok *Token) (*User, error)

	// Refresh 刷新 token。
	//
	// 平台不支持时返回 ErrRefreshUnsupported（GitHub 默认不过期；QQ app 无 refresh_token）。
	// 微信「未超时时刷新只续期不换 token」，实现须以响应原值覆盖。
	Refresh(ctx context.Context, tok *Token) (*Token, error)
}

// Token 归一化 token。
type Token struct {
	AccessToken  string // 三家共同
	RefreshToken string // 微信 / QQ web 必有；QQ app 无；GitHub 仅过期 token 模式有
	// ExpiresIn 有效期秒数。0 表示不过期（GitHub 默认）。
	//
	// 不要硬编码平台常量：QQ web 是 5184000（60 天）、QQ app 是 7776000（90 天）、
	// 微信 access_token 7200、微信 refresh_token web 30 天 / app 180 天 —— 一律用响应值。
	ExpiresIn int64
	// ExpiresAt 由 ExpiresIn 推导。零值表示不过期。
	ExpiresAt time.Time
	// Scope 原样保留：微信逗号分隔、QQ 空、GitHub 空格或逗号分隔。
	Scope string
	// OpenID 平台内唯一标识：微信 openid / QQ openid。GitHub 留空（ID 在 User.ID）。
	OpenID string
	// UnionID 跨应用统一标识：微信 / QQ unionid，可空。
	//
	// 多端打通必须靠它：微信 web 与 app 是两套 appid，同一用户 openid 不同、unionid 相同。
	//
	// **空串一律视为「无 unionid」，不得参与任何查询**。微信 web 渠道经常不返回它，
	// 所以空串是常态而非异常。调用方按「先 unionid 后 openid」查绑定时必须先判空
	// （`if tok.UnionID != ""`），否则空串会匹配到另一个同样没有 unionid 的用户，
	// 把两个不同的人并成一个账号 —— 这是本节最危险的一个接线错误。
	UnionID string
	// Channel 该 token 所属渠道（"web" / "app"），落库时必须一起存 ——
	// 因为 refresh_token 有效期按渠道不同（微信 web 30 天 / app 180 天）。
	Channel string
	// Verified 是否已通过 ValidateToken 校验。
	Verified bool
	// Raw 原始响应体，仅排错用。不要整段写日志（含 token 明文）。
	Raw []byte
}

// User 归一化用户信息。
type User struct {
	Provider  string // "wechat" / "qq" / "github"
	ID        string // 平台内唯一标识：微信 / QQ 的 openid、GitHub 的 id（转 string）
	UnionID   string // 微信 / QQ unionid；GitHub 留空
	Nickname  string // 昵称 / 显示名
	AvatarURL string // 头像
	Email     string // 仅 GitHub 有，且可能为空（用户未授 user:email 或邮箱私有）
}

// 哨兵错误。
//
// 调用方用 errors.Is 判断，不要比对字符串。平台错误码的映射由各实现负责 ——
// 例如微信 40029（invalid code）→ ErrCodeInvalid。
var (
	// ErrNotConfigured 未配置齐全（appid / secret 缺失）。端点应降级为 20026。
	ErrNotConfigured = errors.New("oauthkit: provider not configured")

	// ErrInvalidArgument 调用方参数错误（空 state / 空 code / 非法配置组合）。
	//
	// 与「平台侧失败」严格区分：这一类**重试无用**，是调用方的编程错误 ——
	// 调用方该修代码或修配置，不是重试、也不是引导用户重新授权。
	//
	// 为什么必须有这个哨兵（实测踩过）：微信 AuthURL 的空 state 一度只能裸
	// fmt.Errorf，有人为了让调用方能 errors.Is 判断，硬套了 ErrScopeMissing ——
	// 结果调用方把「我忘了传 state」误判成「用户没给授权范围」，
	// 多做一次无意义的重新授权引导。报错语义错位比报错本身更贵。
	ErrInvalidArgument = errors.New("oauthkit: invalid argument supplied by caller")

	// ErrAuthURLUnsupported 该渠道不支持生成授权 URL（微信 app / QQ app）。
	ErrAuthURLUnsupported = errors.New("oauthkit: provider does not support auth url for this channel")

	// ErrExchangeUnsupported 该渠道不支持 code 换 token（QQ app）。
	ErrExchangeUnsupported = errors.New("oauthkit: provider does not support code exchange for this channel")

	// ErrRefreshUnsupported 平台 / 渠道不支持刷新（GitHub 默认不过期、QQ app 无 refresh_token）。
	ErrRefreshUnsupported = errors.New("oauthkit: provider does not support token refresh")

	// ErrTokenNotVerified 移动端上报的 token 未经过 ValidateToken，被硬拒绝。
	ErrTokenNotVerified = errors.New("oauthkit: client-supplied token must be validated first")

	// ErrTokenMismatch ValidateToken 时 openid 与 token 不匹配（客户端伪造组合）。
	ErrTokenMismatch = errors.New("oauthkit: token does not match the claimed openid")

	// 以下是三家共有的失败语义，各实现负责把平台错误码映射过来。

	// ErrCodeInvalid 授权码无效 / 过期 / 已使用（微信 40029、QQ 100019、GitHub bad_verification_code）。
	ErrCodeInvalid = errors.New("oauthkit: authorization code invalid, expired or already used")

	// ErrCredentialBad 应用凭据被拒（appid / secret 错误）。
	ErrCredentialBad = errors.New("oauthkit: client credentials rejected")

	// ErrTokenExpired access_token 已过期。
	ErrTokenExpired = errors.New("oauthkit: access token expired")

	// ErrRefreshDead refresh_token 过期或已撤销，需要用户重新授权。
	ErrRefreshDead = errors.New("oauthkit: refresh token expired or revoked, re-authorization required")

	// ErrScopeMissing 用户未授予必需的作用域。
	ErrScopeMissing = errors.New("oauthkit: required scope not granted")

	// ErrEmailMissing 拿不到已验证的主邮箱（GitHub 用户未授 user:email）。
	ErrEmailMissing = errors.New("oauthkit: no verified primary email available")

	// ErrUpstream 上游接口异常（网络错误 / 5xx / 响应无法解析）。
	ErrUpstream = errors.New("oauthkit: upstream request failed")
)

// 第三方账号绑定表（T-37 第三方登录 SDK）。
//
// 设计依据：docs/14-第三方登录调研.md §7.3「数据模型建议」—— 采用**方案 B 独立绑定表**，
// 而不是在 user 表上加 4 个 openid 列。理由：
//
//  1. **同一平台多渠道产生不同 openid**：微信「网站应用」与「移动应用」是两个独立 appid，
//     同一用户 PC 扫码得到 openid-A、App 登录得到 openid-B，但 unionid 相同。
//     单表列扩展（一个 wechat_openid 字段）物理上存不下两个值。
//  2. **将来加抖音/微博/B站零改动**：加平台只是多插几行数据，不改表结构。
//  3. `(provider, openid)` 复合唯一索引天然表达「同一平台内 openid 唯一」的语义，
//     比散落在 user 表上的多个 uniqueIndex 列更不容易写错。
//
// # 登录查找顺序（关键，写错会让同一人 PC + App 变成两个账号）
//
//	先按 provider + unionid 查（unionid 非空时），命中即复用该 user_id；
//	查不到再按 provider + openid 查；
//	都查不到才建新用户 + 建绑定。
//
// unionid 为空的情形：GitHub（无此概念）、QQ 未申请 unionid 权限（错误码 100048）。
// 此时退回纯 openid 查找 —— 单渠道场景下语义正确。
//
// # 为什么不存 access_token / refresh_token
//
// 平台 token 只在「登录那一刻」使用（换 userinfo），用完即弃；本站登录态用自签 JWT 双 token。
// 存平台 refresh_token 是纯粹的泄漏面（泄漏 = 持久访问受害者账号），没有对应的业务价值。
// 因此本表**只记录绑定关系 + 昵称头像快照**，不存任何可复用的平台凭据。
package model

// 第三方平台标识（与 oauthkit.ProviderXxx 一致）。
const (
	OAuthProviderWechat = "wechat"
	OAuthProviderQQ     = "qq"
	OAuthProviderGitHub = "github"
)

// 第三方渠道标识（与 oauthkit.ChannelXxx 一致）。
const (
	OAuthChannelWeb = "web"
	OAuthChannelApp = "app"
)

// UserOAuthBinding 第三方账号绑定。
//
// 一个用户可有多条绑定（同一平台的 web 与 app 各一条，或三个平台各一条）。
type UserOAuthBinding struct {
	BaseModel
	// UserID 本站用户 ID。
	UserID string `gorm:"column:user_id;type:varchar(36);index:idx_oauth_user" json:"userId"`
	// Provider wechat / qq / github。
	Provider string `gorm:"column:provider;type:varchar(32);not null;uniqueIndex:uk_oauth_provider_openid,priority:1" json:"provider"`
	// Channel web / app。与 provider 一起决定「用的是哪套 appid」。
	Channel string `gorm:"column:channel;type:varchar(16);not null" json:"channel"`
	// OpenID 该平台该渠道下的用户唯一标识（微信/QQ 的 openid、GitHub 的 numeric id）。
	OpenID string `gorm:"column:openid;type:varchar(191);not null;uniqueIndex:uk_oauth_provider_openid,priority:2" json:"openid"`
	// UnionID 跨应用统一标识。微信/QQ 可空；GitHub 恒空。
	// 登录查找优先用它，保证同一人跨端不分裂成两个账号。
	UnionID *string `gorm:"column:unionid;type:varchar(191);index:idx_oauth_unionid" json:"unionId"`
	// Nickname 平台昵称快照（仅展示用途，不参与登录判断）。
	Nickname string `gorm:"column:nickname;type:varchar(255)" json:"nickname"`
	// AvatarURL 平台头像快照。
	AvatarURL string `gorm:"column:avatar_url;type:varchar(512)" json:"avatarUrl"`
}

// TableName 表名。
func (UserOAuthBinding) TableName() string { return "user_oauth_binding" }

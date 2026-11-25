package dto

// 本文件覆盖 user / auth 域（用户、认证、关注、签到、钱包、认证申请、配置）的
// 全部对外请求与响应结构。
//
// 字段来源（不是想象，逐条可追）：
//   - 响应实体 ← internal/service/user_service.go 的 UserView / UserView 派生做 toMap
//   - 响应实体 ← internal/service/user_actions_service.go 的 map[string]interface{} 字面量键
//   - 响应实体 ← internal/model/user.go、model/wallet.go 的 json tag（UserProfile、WalletTransaction）
//   - 请求体   ← internal/handler/auth.go、user.go、user_actions.go 里解析的匿名 struct
//   - 请求体   ← internal/service 的 XxxInput 结构
//
// 字段名一律 camelCase（AGENTS「API 字段命名口径」）。

// ---------------------------------------------------------------------------
// 用户视图（响应）
// ---------------------------------------------------------------------------

// RoleBriefDto 角色简述（service.RoleBrief）。
type RoleBriefDto struct {
	ID          string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name        string `json:"name" example:"user"`
	DisplayName string `json:"displayName" example:"普通用户"`
}

// UserDto 用户对外视图（service.UserView，32 字段）。
//
// 用于：GET /user（列表项）、GET /user/{id}、POST /user、PATCH /user/{id}、
// 关注/粉丝列表项、登录响应里的 user 字段。
// certification 不在本结构里 —— 只有 GET /user/profile 会额外带它，见 UserProfileDto。
type UserDto struct {
	ID       string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Username string `json:"username" example:"alice"`

	// ---- 字符串字段：值类型，空时 ""（不是 null、不是省略）----
	// 见 docs/21-JSON序列化规范.md §一。model 层保持 *string（数据库要区分 NULL），
	// 转换发生在 service.buildUserView → strOrEmpty()。
	Nickname     string `json:"nickname" example:"爱丽丝"`
	Avatar       string `json:"avatar" example:"https://cdn.example.com/a.png"`
	Description  string `json:"description" example:"喜欢拍风景"`
	Background   string `json:"background" example:"https://cdn.example.com/bg.png"`
	Address      string `json:"address" example:"杭州"`
	Gender       string `json:"gender" example:"FEMALE"`
	Email        string `json:"email" example:"a***@example.com"`
	Phone        string `json:"phone" example:"138****8000"`
	MyInviteCode string `json:"myInviteCode" example:"AB12CD"`

	// ---- 时间字段：空时不传（时间没有"空"的自然表示，见规范 §一）----
	BirthDate         *string `json:"birthDate,omitempty" format:"date-time" example:"1998-06-01T00:00:00Z"`
	MembershipEndDate *string `json:"membershipEndDate,omitempty" format:"date-time"`

	Status              string  `json:"status" example:"ACTIVE"`
	ArticleCount        int     `json:"articleCount" example:"12"`
	FollowerCount       int     `json:"followerCount" example:"128"`
	FollowingCount      int     `json:"followingCount" example:"36"`
	Likes               int     `json:"likes" example:"512"`
	Level               int     `json:"level" example:"5"`
	Experience          int     `json:"experience" example:"430"`
	Wallet              float64 `json:"wallet" example:"100.5"`
	Points              int     `json:"points" example:"320"`
	MembershipLevel     int     `json:"membershipLevel" example:"1"`
	MembershipLevelName string  `json:"membershipLevelName" example:"普通用户"`
	MembershipStatus    string  `json:"membershipStatus" example:"INACTIVE"`
	InviteEarnings      float64 `json:"inviteEarnings" example:"0"`
	InviteCount         int     `json:"inviteCount" example:"0"`
	CreatedAt           string  `json:"createdAt" format:"date-time" example:"2026-11-25T10:00:00Z"`

	// ---- 数组字段：去掉 omitempty，空时输出 []（不是省略、不是 null）----
	Roles    []RoleBriefDto `json:"roles"`
	IsMember bool           `json:"isMember" example:"false"`
}

// UserProfileDto 当前用户资料（GET /user/profile）。
//
// 与 UserDto 的唯一差别：多一个 certification（已通过认证的详情，无则省略）。
// 运行时由 service.Profile 组装：UserView + certView。
type UserProfileDto struct {
	UserDto
	Certification *CertificationDto `json:"certification,omitempty"`
}

// CertificationDto 认证记录视图（service.certView，10 字段）。
type CertificationDto struct {
	ID     string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	UserID string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Type   string `json:"type" example:"CREATOR"`
	Status string `json:"status" example:"APPROVED"`
	// 字符串值类型：空时 ""（docs/21-JSON序列化规范.md）
	RealName         string `json:"realName" example:"张三"`
	OrganizationName string `json:"organizationName"`
	Description      string `json:"description" example:"签约摄影师"`
	Slogan           string `json:"slogan" example:"官方认证 · 签约摄影师"`
	ReviewReason     string `json:"reviewReason"`
	// 时间：空时不传
	ReviewedAt *string `json:"reviewedAt,omitempty" format:"date-time"`
	CreatedAt  string  `json:"createdAt" format:"date-time"`
}

// UserConfigDto 用户配置（GET /user/config，model.UserProfile 对外字段）。
//
// 表名是 user_config；含 4 项抽成费率 + 11 个通知开关 + 6 个隐藏开关 + 水印开关。
type UserConfigDto struct {
	ID                        string  `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt                 string  `json:"createdAt" format:"date-time"`
	UpdatedAt                 string  `json:"updatedAt" format:"date-time"`
	UserID                    string  `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ArticleCommissionRate     float64 `json:"articleCommissionRate" example:"10"`
	MembershipCommissionRate  float64 `json:"membershipCommissionRate" example:"5"`
	ProductCommissionRate     float64 `json:"productCommissionRate" example:"5"`
	ServiceCommissionRate     float64 `json:"serviceCommissionRate" example:"5"`
	EnableCustomCommission    bool    `json:"enableCustomCommission" example:"false"`
	EnableSystemNotification  bool    `json:"enableSystemNotification" example:"true"`
	EnableCommentNotification bool    `json:"enableCommentNotification" example:"true"`
	EnableLikeNotification    bool    `json:"enableLikeNotification" example:"true"`
	EnableFollowNotification  bool    `json:"enableFollowNotification" example:"true"`
	EnableMessageNotification bool    `json:"enableMessageNotification" example:"true"`
	EnableOrderNotification   bool    `json:"enableOrderNotification" example:"true"`
	EnablePaymentNotification bool    `json:"enablePaymentNotification" example:"true"`
	EnableInviteNotification  bool    `json:"enableInviteNotification" example:"true"`
	EnableEmailNotification   bool    `json:"enableEmailNotification" example:"true"`
	EnableSmsNotification     bool    `json:"enableSmsNotification" example:"true"`
	EnablePushNotification    bool    `json:"enablePushNotification" example:"true"`
	HideFavorites             bool    `json:"hideFavorites" example:"false"`
	HideComments              bool    `json:"hideComments" example:"false"`
	HideCollections           bool    `json:"hideCollections" example:"false"`
	HideFollowers             bool    `json:"hideFollowers" example:"false"`
	HideFollowings            bool    `json:"hideFollowings" example:"false"`
	HideTags                  bool    `json:"hideTags" example:"false"`
	WatermarkEnabled          bool    `json:"watermarkEnabled" example:"false"`
	Remark                    string  `json:"remark"`
}

// ---------------------------------------------------------------------------
// 认证 / 登录（请求 + 响应）
// ---------------------------------------------------------------------------

// LoginRequest 登录请求体（account 支持用户名或邮箱）。
type LoginRequest struct {
	Account  string `json:"account" binding:"required" example:"alice 或 alice@example.com"`
	Password string `json:"password" binding:"required" example:"123456"`
}

// RegisterRequest 注册请求体。
type RegisterRequest struct {
	Username string `json:"username" binding:"required" example:"alice"`
	Nickname string `json:"nickname" example:"爱丽丝"`
	Password string `json:"password" binding:"required" example:"123456"`
	Email    string `json:"email" example:"alice@example.com"`
	Phone    string `json:"phone" example:"13800138000"`
}

// RefreshTokenRequest 刷新 access token 请求体（另需 Device-Id 头）。
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// LoginResultDto 登录成功的 data。
//
// Login 额外带 username；微信/QQ/GitHub 的 OAuth 登录额外带 isMember。
type LoginResultDto struct {
	User         *UserDto `json:"user"`
	Token        string   `json:"token"`
	RefreshToken string   `json:"refreshToken"`
	ID           string   `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Username     string   `json:"username,omitempty" example:"alice"`
	IsMember     bool     `json:"isMember,omitempty" example:"false"`
}

// RefreshTokenResultDto 刷新结果（只回新 token）。
type RefreshTokenResultDto struct {
	Token string `json:"token"`
}

// SmsCodeRequest 发送短信验证码请求体。
type SmsCodeRequest struct {
	Phone string `json:"phone" binding:"required" example:"13800138000"`
	Scene string `json:"scene" example:"login"`
}

// SmsLoginRequest 手机验证码登录请求体。
type SmsLoginRequest struct {
	Phone string `json:"phone" binding:"required" example:"13800138000"`
	Code  string `json:"code" binding:"required" example:"123456"`
}

// EmailCodeRequest 发送邮箱验证码请求体。
type EmailCodeRequest struct {
	Email string `json:"email" binding:"required" example:"alice@example.com"`
	Type  string `json:"type" example:"verification"`
}

// ResetPasswordRequest 邮箱验证码重置密码请求体。
type ResetPasswordRequest struct {
	Email       string `json:"email" binding:"required" example:"alice@example.com"`
	Code        string `json:"code" binding:"required" example:"123456"`
	NewPassword string `json:"newPassword" binding:"required" example:"newpass123"`
}

// ChangePasswordRequest 修改密码请求体（原密码校验）。
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required" example:"oldpass123"`
	NewPassword string `json:"newPassword" binding:"required" example:"newpass123"`
}

// BindPhoneCodeRequest 发送绑定手机验证码请求体。
type BindPhoneCodeRequest struct {
	Phone string `json:"phone" binding:"required" example:"13800138000"`
}

// BindPhoneRequest 绑定手机号请求体（service.BindPhoneInput）。
type BindPhoneRequest struct {
	Phone string `json:"phone" binding:"required" example:"13800138000"`
	Code  string `json:"code" binding:"required" example:"123456"`
}

// BindPhoneResultDto 绑定手机号成功的 data。
type BindPhoneResultDto struct {
	Success bool   `json:"success" example:"true"`
	Phone   string `json:"phone" example:"13800138000"`
}

// GithubCallbackRequest GitHub 回调（SPA 模式）请求体。
//
// 微信 / QQ 的 web 回调是同一形状，请求体用同构的 OAuthCallbackRequest。
type GithubCallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

// OAuthCallbackRequest 微信 / QQ web 回调（SPA 模式）请求体。
type OAuthCallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

// OAuthCodeRequest 微信 app 登录请求体（只交 code）。
type OAuthCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// QqAppLoginRequest QQ app 登录请求体（交 accessToken + openId，或直接交 payload）。
type QqAppLoginRequest struct {
	AccessToken string `json:"accessToken"`
	OpenID      string `json:"openId"`
	Payload     string `json:"payload"`
}

// OAuthLinkRequest 已登录状态下绑定第三方账号（service.OAuthLinkInput，6 字段）。
//
// web（微信/QQ/GitHub）交 code+state；QQ app 交 accessToken+openId；微信 app 只交 code。
type OAuthLinkRequest struct {
	Provider    string `json:"provider" binding:"required" example:"wechat"`
	Channel     string `json:"channel" binding:"required" example:"web"`
	Code        string `json:"code"`
	State       string `json:"state"`
	AccessToken string `json:"accessToken"`
	OpenID      string `json:"openId"`
}

// OAuthURLDto 取授权链接的 data（url + state）。
type OAuthURLDto struct {
	URL   string `json:"url" example:"https://open.weixin.qq.com/connect/qrconnect?appid=..."`
	State string `json:"state" example:"7f3c9a1b2d4e"`
}

// OAuthBindingDto 单条第三方绑定（service.oauthBindingView，不含 openid/unionid）。
type OAuthBindingDto struct {
	Provider  string `json:"provider" example:"wechat"`
	Channel   string `json:"channel" example:"web"`
	Nickname  string `json:"nickname" example:"微信昵称"`
	AvatarURL string `json:"avatarUrl" example:"https://thirdwx.qlogo.cn/..."`
	BoundAt   string `json:"boundAt" format:"date-time" example:"2026-11-25T10:00:00Z"`
}

// OAuthBindingListDto 我的绑定列表（空列表是 [] 不是 null）。
type OAuthBindingListDto struct {
	Bindings []OAuthBindingDto `json:"bindings"`
}

// ---------------------------------------------------------------------------
// 资料 / 配置更新（请求）
// ---------------------------------------------------------------------------

// UpdateProfileRequest 更新资料请求体（service.UpdateProfileInput，指针语义：nil 不改）。
type UpdateProfileRequest struct {
	Nickname    *string `json:"nickname" example:"新昵称"`
	Avatar      *string `json:"avatar" example:"https://cdn.example.com/a.png"`
	Description *string `json:"description"`
	Background  *string `json:"background"`
	Address     *string `json:"address"`
	Gender      *string `json:"gender" example:"MALE"`
	BirthDate   *string `json:"birthDate" format:"date-time"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
}

// UpdateUserConfigRequest 更新抽成配置（service.UpdateConfigInput，5 指针字段）。
type UpdateUserConfigRequest struct {
	ArticleCommissionRate    *float64 `json:"articleCommissionRate"`
	MembershipCommissionRate *float64 `json:"membershipCommissionRate"`
	ProductCommissionRate    *float64 `json:"productCommissionRate"`
	ServiceCommissionRate    *float64 `json:"serviceCommissionRate"`
	EnableCustomCommission   *bool    `json:"enableCustomCommission"`
}

// UpdateNotificationRequest 更新通知偏好（service.NotificationInput，11 指针字段）。
type UpdateNotificationRequest struct {
	EnableSystemNotification  *bool `json:"enableSystemNotification"`
	EnableCommentNotification *bool `json:"enableCommentNotification"`
	EnableLikeNotification    *bool `json:"enableLikeNotification"`
	EnableFollowNotification  *bool `json:"enableFollowNotification"`
	EnableMessageNotification *bool `json:"enableMessageNotification"`
	EnableOrderNotification   *bool `json:"enableOrderNotification"`
	EnablePaymentNotification *bool `json:"enablePaymentNotification"`
	EnableInviteNotification  *bool `json:"enableInviteNotification"`
	EnableEmailNotification   *bool `json:"enableEmailNotification"`
	EnableSmsNotification     *bool `json:"enableSmsNotification"`
	EnablePushNotification    *bool `json:"enablePushNotification"`
}

// SetCommissionRequest 设置抽成配置（service.SetCommissionInput，6 字段）。
type SetCommissionRequest struct {
	ArticleCommissionRate    float64 `json:"articleCommissionRate" example:"10"`
	MembershipCommissionRate float64 `json:"membershipCommissionRate" example:"5"`
	ProductCommissionRate    float64 `json:"productCommissionRate" example:"5"`
	ServiceCommissionRate    float64 `json:"serviceCommissionRate" example:"5"`
	EnableCustomCommission   bool    `json:"enableCustomCommission" example:"false"`
	Remark                   string  `json:"remark"`
}

// CommissionConfigDto 抽成配置（CommissionConfig 的 data，5 字段）。
type CommissionConfigDto struct {
	ArticleCommissionRate    float64 `json:"articleCommissionRate" example:"10"`
	MembershipCommissionRate float64 `json:"membershipCommissionRate" example:"5"`
	ProductCommissionRate    float64 `json:"productCommissionRate" example:"5"`
	ServiceCommissionRate    float64 `json:"serviceCommissionRate" example:"5"`
	EnableCustomCommission   bool    `json:"enableCustomCommission" example:"false"`
}

// CalculateCommissionRequest 抽成试算请求体（handler 内匿名 struct）。
type CalculateCommissionRequest struct {
	Amount float64 `json:"amount" binding:"required" example:"100"`
	Type   string  `json:"type" binding:"required" example:"article"`
}

// CommissionResultDto 抽成试算结果（5 字段）。
type CommissionResultDto struct {
	Amount     float64 `json:"amount" example:"100"`
	Rate       float64 `json:"rate" example:"10"`
	Type       string  `json:"type" example:"article"`
	Commission float64 `json:"commission" example:"10"`
	Income     float64 `json:"income" example:"90"`
}

// ---------------------------------------------------------------------------
// 用户管理（管理员）
// ---------------------------------------------------------------------------

// CreateUserRequest 管理员创建用户（service.CreateUserInput，8 字段）。
type CreateUserRequest struct {
	Username   string   `json:"username" binding:"required" example:"alice"`
	Nickname   string   `json:"nickname" example:"爱丽丝"`
	Password   string   `json:"password" binding:"required" example:"123456"`
	Email      string   `json:"email" example:"alice@example.com"`
	Phone      string   `json:"phone" example:"13800138000"`
	RoleIDs    []string `json:"roleIds"`
	Wallet     float64  `json:"wallet" example:"0"`
	InviteCode string   `json:"inviteCode"`
}

// UpdateUserRequest 管理员更新用户（service.UpdateUserInput，9 字段）。
type UpdateUserRequest struct {
	Nickname    *string  `json:"nickname"`
	Avatar      *string  `json:"avatar"`
	Description *string  `json:"description"`
	Background  *string  `json:"background"`
	Address     *string  `json:"address"`
	Gender      *string  `json:"gender"`
	RoleIDs     []string `json:"roleIds"`
	Wallet      *float64 `json:"wallet"`
	Status      *string  `json:"status" example:"ACTIVE"`
}

// MembershipCheckDto 会员状态检查（2 字段）。
type MembershipCheckDto struct {
	UserID   string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsMember bool   `json:"isMember" example:"true"`
}

// BatchMembershipCheckDto 批量会员状态检查结果。
type BatchMembershipCheckDto struct {
	Updated int64 `json:"updated" example:"3"`
}

// ---------------------------------------------------------------------------
// 关注关系
// ---------------------------------------------------------------------------

// FollowResultDto 关注 / 取关结果（2 字段）。
type FollowResultDto struct {
	Following bool `json:"following" example:"true"`
	Changed   bool `json:"changed" example:"true"`
}

// IsFollowingDto 是否已关注。
type IsFollowingDto struct {
	Following bool `json:"following" example:"true"`
}

// FollowCountDto 粉丝数 / 关注数（单值接口）。
type FollowCountDto struct {
	Count int64 `json:"count" example:"128"`
}

// FollowStatsDto 粉丝与关注双向计数。
type FollowStatsDto struct {
	FollowerCount  int64 `json:"followerCount" example:"128"`
	FollowingCount int64 `json:"followingCount" example:"36"`
}

// ---------------------------------------------------------------------------
// 签到
// ---------------------------------------------------------------------------

// SignInResultDto 手动签到结果（service.Sign，7 字段）。
type SignInResultDto struct {
	Signed          bool   `json:"signed" example:"true"`
	Already         bool   `json:"already" example:"false"`
	ConsecutiveDays int    `json:"consecutiveDays" example:"8"`
	SignInDate      string `json:"signInDate" example:"2026-11-25"`
	PointsGranted   int    `json:"pointsGranted" example:"10"`
	Level           int    `json:"level" example:"5"`
	LeveledUp       bool   `json:"leveledUp" example:"false"`
}

// SignInRecordDto 签到记录项（service.SignRecords，3 字段）。
type SignInRecordDto struct {
	SignInDate      string `json:"signInDate" example:"2026-11-25"`
	ConsecutiveDays int    `json:"consecutiveDays" example:"8"`
	IsAuto          bool   `json:"isAuto" example:"false"`
}

// SignInStatsDto 签到统计（4 字段；lastSignInDate 在从未签到时省略）。
type SignInStatsDto struct {
	TotalDays       int64  `json:"totalDays" example:"32"`
	ConsecutiveDays int    `json:"consecutiveDays" example:"8"`
	SignedToday     bool   `json:"signedToday" example:"true"`
	LastSignInDate  string `json:"lastSignInDate,omitempty" example:"2026-11-25"`
}

// ---------------------------------------------------------------------------
// 钱包
// ---------------------------------------------------------------------------

// WalletBalanceDto 钱包余额（单字段）。
type WalletBalanceDto struct {
	Balance float64 `json:"balance" example:"100.5"`
}

// WalletTransactionDto 钱包流水（model.WalletTransaction + BaseModel 对外字段）。
type WalletTransactionDto struct {
	ID            string  `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt     string  `json:"createdAt" format:"date-time"`
	UpdatedAt     string  `json:"updatedAt" format:"date-time"`
	UserID        string  `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Type          string  `json:"type" example:"RECHARGE"`
	Amount        float64 `json:"amount" example:"100"`
	BalanceBefore float64 `json:"balanceBefore" example:"0.5"`
	BalanceAfter  float64 `json:"balanceAfter" example:"100.5"`
	OrderID       *string `json:"orderId"`
	PaymentID     *string `json:"paymentId"`
	Description   *string `json:"description" example:"Wallet recharge"`
	Remark        string  `json:"remark"`
}

// WalletTxSummaryDto 按类型聚合的收支（WalletStatistics 的 byType 值）。
type WalletTxSummaryDto struct {
	Total float64 `json:"total" example:"100"`
	Count int64   `json:"count" example:"2"`
}

// WalletStatisticsDto 钱包收支统计（byType + balance）。
type WalletStatisticsDto struct {
	ByType  map[string]WalletTxSummaryDto `json:"byType"`
	Balance float64                       `json:"balance" example:"100.5"`
}

// WithdrawRequest 提现请求体（service.WithdrawInput）。
type WithdrawRequest struct {
	Amount   float64                `json:"amount" binding:"required" example:"50"`
	BankInfo map[string]interface{} `json:"bankInfo"`
}

// WithdrawResultDto 提现结果（3 字段）。
type WithdrawResultDto struct {
	Success bool    `json:"success" example:"true"`
	Balance float64 `json:"balance" example:"50.5"`
	Amount  float64 `json:"amount" example:"50"`
}

// ---------------------------------------------------------------------------
// 账号注销
// ---------------------------------------------------------------------------

// RequestDeletionRequest 注销申请请求体。
type RequestDeletionRequest struct {
	Reason string `json:"reason" example:"不再使用"`
}

// DeletionRequestResultDto 注销申请结果（4 字段）。
// Status 是 int 枚举（0=无 1=待注销 2=已撤销），与 model.DeletionStatus* 同源 ——
// 不是字符串。改前这里写成 string，导致 service 无法直接赋值，是 dto 定义与真实契约不符。
type DeletionRequestResultDto struct {
	Status          int    `json:"status" example:"1"`
	ScheduledAt     string `json:"scheduledAt" format:"date-time"`
	CoolingDays     int    `json:"coolingDays" example:"7"`
	DeletionPending bool   `json:"deletionPending" example:"true"`
}

// DeletionStatusDto 注销状态（reason 仅在有待注销记录且填写过时出现）。
// Status 是 int 枚举（0=无 1=待注销 2=已撤销），与 model.DeletionStatus* 同源。
type DeletionStatusDto struct {
	Status          int    `json:"status" example:"0"`
	DeletionPending bool   `json:"deletionPending" example:"false"`
	ScheduledAt     string `json:"scheduledAt,omitempty" format:"date-time"`
	Reason          string `json:"reason,omitempty"`
}

// ---------------------------------------------------------------------------
// 认证申请（官方 / 创作者）
// ---------------------------------------------------------------------------

// ApplyCertificationRequest 提交认证申请（service.ApplyCertInput，6 字段）。
//
// 字段与 Apifox 的 ApplyCertificationDto 一致（交叉校验过）；只有 type 必填。
type ApplyCertificationRequest struct {
	Type             string   `json:"type" binding:"required" example:"CREATOR"`
	RealName         string   `json:"realName"`
	OrganizationName string   `json:"organizationName"`
	Description      string   `json:"description"`
	Slogan           string   `json:"slogan" example:"官方认证 · 签约摄影师"`
	Materials        []string `json:"materials"`
}

// AuditCertificationRequest 认证审核（service.AuditCertInput，2 字段）。
type AuditCertificationRequest struct {
	Status string `json:"status" binding:"required" example:"APPROVED"`
	Reason string `json:"reason"`
}

// RevokeCertificationRequest 撤销认证请求体。
type RevokeCertificationRequest struct {
	Reason string `json:"reason"`
}

// IdentityDto 用户身份标识（6 字段；type/slogan 仅在有已通过认证时出现）。
type IdentityDto struct {
	UserID     string `json:"userId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Identity   string `json:"identity" example:"creator"`
	IsOfficial bool   `json:"isOfficial" example:"false"`
	IsCreator  bool   `json:"isCreator" example:"true"`
	Type       string `json:"type,omitempty" example:"CREATOR"`
	Slogan     string `json:"slogan,omitempty" example:"官方认证 · 签约摄影师"`
}

// ---------------------------------------------------------------------------
// 收藏
// ---------------------------------------------------------------------------

// FavoriteItemDto 收藏项（service.FavoriteItem，6 字段）。
type FavoriteItemDto struct {
	ArticleID string `json:"articleId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Title     string `json:"title" example:"我的第一篇作品"`
	// 字符串值类型：空时 ""（docs/21）；指针会让空值输出 null。
	Cover       string `json:"cover" example:"https://cdn.example.com/c.png"`
	Summary     string `json:"summary"`
	AuthorID    string `json:"authorId"`
	FavoritedAt string `json:"favoritedAt" format:"date-time"`
}

// FavoriteDetailDto 收藏详情（3 字段；未收藏时 article 省略）。
type FavoriteDetailDto struct {
	Favorited   bool             `json:"favorited" example:"true"`
	FavoritedAt string           `json:"favoritedAt,omitempty" format:"date-time"`
	Article     *FavoriteItemDto `json:"article,omitempty"`
}

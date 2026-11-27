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
	DisplayName string `json:"display_name" example:"普通用户"`
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
	MyInviteCode string `json:"my_invite_code" example:"AB12CD"`

	// ---- 时间字段：空时不传（时间没有"空"的自然表示，见规范 §一）----
	BirthDate         *string `json:"birth_date,omitempty" format:"date-time" example:"1998-06-01T00:00:00Z"`
	MembershipEndDate *string `json:"membership_end_date,omitempty" format:"date-time"`

	Status              string  `json:"status" example:"ACTIVE"`
	ArticleCount        int     `json:"article_count" example:"12"`
	FollowerCount       int     `json:"follower_count" example:"128"`
	FollowingCount      int     `json:"following_count" example:"36"`
	Likes               int     `json:"likes" example:"512"`
	Level               int     `json:"level" example:"5"`
	Experience          int     `json:"experience" example:"430"`
	Wallet              float64 `json:"wallet" example:"100.5"`
	Points              int     `json:"points" example:"320"`
	MembershipLevel     int     `json:"membership_level" example:"1"`
	MembershipLevelName string  `json:"membership_level_name" example:"普通用户"`
	MembershipStatus    string  `json:"membership_status" example:"INACTIVE"`
	InviteEarnings      float64 `json:"invite_earnings" example:"0"`
	InviteCount         int     `json:"invite_count" example:"0"`
	CreatedAt           string  `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	// ---- 数组字段：去掉 omitempty，空时输出 []（不是省略、不是 null）----
	Roles    []RoleBriefDto `json:"roles"`
	IsMember bool           `json:"is_member" example:"false"`
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
	UserID string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Type   string `json:"type" example:"CREATOR"`
	Status string `json:"status" example:"APPROVED"`
	// 字符串值类型：空时 ""（docs/21-JSON序列化规范.md）
	RealName         string `json:"real_name" example:"张三"`
	OrganizationName string `json:"organization_name"`
	Description      string `json:"description" example:"签约摄影师"`
	Slogan           string `json:"slogan" example:"官方认证 · 签约摄影师"`
	ReviewReason     string `json:"review_reason"`
	// 时间：空时不传
	ReviewedAt *string `json:"reviewed_at,omitempty" format:"date-time"`
	CreatedAt  string  `json:"created_at" format:"date-time"`
}

// UserConfigDto 用户配置（GET /user/config，model.UserProfile 对外字段）。
//
// 表名是 user_config；含 4 项抽成费率 + 11 个通知开关 + 6 个隐藏开关 + 水印开关。
type UserConfigDto struct {
	ID                        string  `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt                 string  `json:"created_at" format:"date-time"`
	UpdatedAt                 string  `json:"updated_at" format:"date-time"`
	UserID                    string  `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ArticleCommissionRate     float64 `json:"article_commission_rate" example:"10"`
	MembershipCommissionRate  float64 `json:"membership_commission_rate" example:"5"`
	ProductCommissionRate     float64 `json:"product_commission_rate" example:"5"`
	ServiceCommissionRate     float64 `json:"service_commission_rate" example:"5"`
	EnableCustomCommission    bool    `json:"enable_custom_commission" example:"false"`
	EnableSystemNotification  bool    `json:"enable_system_notification" example:"true"`
	EnableCommentNotification bool    `json:"enable_comment_notification" example:"true"`
	EnableLikeNotification    bool    `json:"enable_like_notification" example:"true"`
	EnableFollowNotification  bool    `json:"enable_follow_notification" example:"true"`
	EnableMessageNotification bool    `json:"enable_message_notification" example:"true"`
	EnableOrderNotification   bool    `json:"enable_order_notification" example:"true"`
	EnablePaymentNotification bool    `json:"enable_payment_notification" example:"true"`
	EnableInviteNotification  bool    `json:"enable_invite_notification" example:"true"`
	EnableEmailNotification   bool    `json:"enable_email_notification" example:"true"`
	EnableSmsNotification     bool    `json:"enable_sms_notification" example:"true"`
	EnablePushNotification    bool    `json:"enable_push_notification" example:"true"`
	HideFavorites             bool    `json:"hide_favorites" example:"false"`
	HideComments              bool    `json:"hide_comments" example:"false"`
	HideCollections           bool    `json:"hide_collections" example:"false"`
	HideFollowers             bool    `json:"hide_followers" example:"false"`
	HideFollowings            bool    `json:"hide_followings" example:"false"`
	HideTags                  bool    `json:"hide_tags" example:"false"`
	WatermarkEnabled          bool    `json:"watermark_enabled" example:"false"`
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
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// LoginResultDto 登录成功的 data。
//
// **用户字段是扁平展开的，不是嵌套在 `user` 里**（用户 2026-11-27 指定）：
//
//	{"id":"...","username":"alice","nickname":"爱丽丝",...,"token":"...","refreshToken":"..."}
//
// 而不是：
//
//	{"user":{"id":"...","nickname":"..."},"token":"..."}
//
// 为什么展平：前端登录后马上要用 id/username/avatar 等字段，嵌一层只是多写 `.user`；
// 而且 UserDto 与 LoginResultDto 的字段名本来就不冲突（token/refreshToken 是登录独有的）。
//
// 组装必须走 service.LoginResult()，**不要在 handler 里手写 gin.H** ——
// 之前 4 处都用 gin.H 手写，导致「DTO 声明一份、运行时另一份」的契约漂移
// （docs/21 §四之三），而且字段加漏了编译期发现不了。
type LoginResultDto struct {
	// 内嵌 UserDto：字段全部提升到本层
	UserDto

	Token        string `json:"token" example:"eyJhbGciOiJIUzI1NiIs..."`
	RefreshToken string `json:"refresh_token" example:"eyJhbGciOiJIUzI1NiIs..."`

	// ID 与内嵌 UserDto.ID 同值，为兼容前端历史习惯额外保留一份顶层副本。
	//
	// Deprecated: 用内嵌的 id（同名字段，序列化后只出现一次，值相同）。
	// 保留它不产生任何输出差异，仅作为明确的兼容声明。
	ID string `json:"-" swaggerignore:"true"`
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
	NewPassword string `json:"new_password" binding:"required" example:"newpass123"`
}

// ChangePasswordRequest 修改密码请求体（原密码校验）。
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required" example:"oldpass123"`
	NewPassword string `json:"new_password" binding:"required" example:"newpass123"`
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
	AccessToken string `json:"access_token"`
	OpenID      string `json:"open_id"`
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
	AccessToken string `json:"access_token"`
	OpenID      string `json:"open_id"`
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
	AvatarURL string `json:"avatar_url" example:"https://thirdwx.qlogo.cn/..."`
	BoundAt   string `json:"bound_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
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
	BirthDate   *string `json:"birth_date" format:"date-time"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
}

// UpdateUserConfigRequest 更新抽成配置（service.UpdateConfigInput，5 指针字段）。
type UpdateUserConfigRequest struct {
	ArticleCommissionRate    *float64 `json:"article_commission_rate"`
	MembershipCommissionRate *float64 `json:"membership_commission_rate"`
	ProductCommissionRate    *float64 `json:"product_commission_rate"`
	ServiceCommissionRate    *float64 `json:"service_commission_rate"`
	EnableCustomCommission   *bool    `json:"enable_custom_commission"`
}

// UpdateNotificationRequest 更新通知偏好（service.NotificationInput，11 指针字段）。
type UpdateNotificationRequest struct {
	EnableSystemNotification  *bool `json:"enable_system_notification"`
	EnableCommentNotification *bool `json:"enable_comment_notification"`
	EnableLikeNotification    *bool `json:"enable_like_notification"`
	EnableFollowNotification  *bool `json:"enable_follow_notification"`
	EnableMessageNotification *bool `json:"enable_message_notification"`
	EnableOrderNotification   *bool `json:"enable_order_notification"`
	EnablePaymentNotification *bool `json:"enable_payment_notification"`
	EnableInviteNotification  *bool `json:"enable_invite_notification"`
	EnableEmailNotification   *bool `json:"enable_email_notification"`
	EnableSmsNotification     *bool `json:"enable_sms_notification"`
	EnablePushNotification    *bool `json:"enable_push_notification"`
}

// SetCommissionRequest 设置抽成配置（service.SetCommissionInput，6 字段）。
type SetCommissionRequest struct {
	ArticleCommissionRate    float64 `json:"article_commission_rate" example:"10"`
	MembershipCommissionRate float64 `json:"membership_commission_rate" example:"5"`
	ProductCommissionRate    float64 `json:"product_commission_rate" example:"5"`
	ServiceCommissionRate    float64 `json:"service_commission_rate" example:"5"`
	EnableCustomCommission   bool    `json:"enable_custom_commission" example:"false"`
	Remark                   string  `json:"remark"`
}

// CommissionConfigDto 抽成配置（CommissionConfig 的 data，5 字段）。
type CommissionConfigDto struct {
	ArticleCommissionRate    float64 `json:"article_commission_rate" example:"10"`
	MembershipCommissionRate float64 `json:"membership_commission_rate" example:"5"`
	ProductCommissionRate    float64 `json:"product_commission_rate" example:"5"`
	ServiceCommissionRate    float64 `json:"service_commission_rate" example:"5"`
	EnableCustomCommission   bool    `json:"enable_custom_commission" example:"false"`
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
	RoleIDs    []string `json:"role_ids"`
	Wallet     float64  `json:"wallet" example:"0"`
	InviteCode string   `json:"invite_code"`
}

// UpdateUserRequest 管理员更新用户（service.UpdateUserInput，9 字段）。
type UpdateUserRequest struct {
	Nickname    *string  `json:"nickname"`
	Avatar      *string  `json:"avatar"`
	Description *string  `json:"description"`
	Background  *string  `json:"background"`
	Address     *string  `json:"address"`
	Gender      *string  `json:"gender"`
	RoleIDs     []string `json:"role_ids"`
	Wallet      *float64 `json:"wallet"`
	Status      *string  `json:"status" example:"ACTIVE"`
}

// MembershipCheckDto 会员状态检查（2 字段）。
type MembershipCheckDto struct {
	UserID   string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsMember bool   `json:"is_member" example:"true"`
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
	FollowerCount  int64 `json:"follower_count" example:"128"`
	FollowingCount int64 `json:"following_count" example:"36"`
}

// ---------------------------------------------------------------------------
// 签到
// ---------------------------------------------------------------------------

// SignInResultDto 手动签到结果（service.Sign，7 字段）。
type SignInResultDto struct {
	Signed          bool   `json:"signed" example:"true"`
	Already         bool   `json:"already" example:"false"`
	ConsecutiveDays int    `json:"consecutive_days" example:"8"`
	SignInDate      string `json:"sign_in_date" example:"2026-11-25"`
	PointsGranted   int    `json:"points_granted" example:"10"`
	Level           int    `json:"level" example:"5"`
	LeveledUp       bool   `json:"leveled_up" example:"false"`
}

// SignInRecordDto 签到记录项（service.SignRecords，3 字段）。
type SignInRecordDto struct {
	SignInDate      string `json:"sign_in_date" example:"2026-11-25"`
	ConsecutiveDays int    `json:"consecutive_days" example:"8"`
	IsAuto          bool   `json:"is_auto" example:"false"`
}

// SignInStatsDto 签到统计（4 字段；lastSignInDate 在从未签到时省略）。
type SignInStatsDto struct {
	TotalDays       int64  `json:"total_days" example:"32"`
	ConsecutiveDays int    `json:"consecutive_days" example:"8"`
	SignedToday     bool   `json:"signed_today" example:"true"`
	LastSignInDate  string `json:"last_sign_in_date,omitempty" example:"2026-11-25"`
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
	CreatedAt     string  `json:"created_at" format:"date-time"`
	UpdatedAt     string  `json:"updated_at" format:"date-time"`
	UserID        string  `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Type          string  `json:"type" example:"RECHARGE"`
	Amount        float64 `json:"amount" example:"100"`
	BalanceBefore float64 `json:"balance_before" example:"0.5"`
	BalanceAfter  float64 `json:"balance_after" example:"100.5"`
	OrderID       *string `json:"order_id"`
	PaymentID     *string `json:"payment_id"`
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
	ByType  map[string]WalletTxSummaryDto `json:"by_type"`
	Balance float64                       `json:"balance" example:"100.5"`
}

// WithdrawRequest 提现请求体（service.WithdrawInput）。
type WithdrawRequest struct {
	Amount   float64                `json:"amount" binding:"required" example:"50"`
	BankInfo map[string]interface{} `json:"bank_info"`
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
	ScheduledAt     string `json:"scheduled_at" format:"date-time"`
	CoolingDays     int    `json:"cooling_days" example:"7"`
	DeletionPending bool   `json:"deletion_pending" example:"true"`
}

// DeletionStatusDto 注销状态（reason 仅在有待注销记录且填写过时出现）。
// Status 是 int 枚举（0=无 1=待注销 2=已撤销），与 model.DeletionStatus* 同源。
type DeletionStatusDto struct {
	Status          int    `json:"status" example:"0"`
	DeletionPending bool   `json:"deletion_pending" example:"false"`
	ScheduledAt     string `json:"scheduled_at,omitempty" format:"date-time"`
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
	RealName         string   `json:"real_name"`
	OrganizationName string   `json:"organization_name"`
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
	UserID     string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Identity   string `json:"identity" example:"creator"`
	IsOfficial bool   `json:"is_official" example:"false"`
	IsCreator  bool   `json:"is_creator" example:"true"`
	Type       string `json:"type,omitempty" example:"CREATOR"`
	Slogan     string `json:"slogan,omitempty" example:"官方认证 · 签约摄影师"`
}

// ---------------------------------------------------------------------------
// 收藏
// ---------------------------------------------------------------------------

// FavoriteItemDto 收藏项（service.FavoriteItem，6 字段）。
type FavoriteItemDto struct {
	ArticleID string `json:"article_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Title     string `json:"title" example:"我的第一篇作品"`
	// 字符串值类型：空时 ""（docs/21）；指针会让空值输出 null。
	Cover       string `json:"cover" example:"https://cdn.example.com/c.png"`
	Summary     string `json:"summary"`
	AuthorID    string `json:"author_id"`
	FavoritedAt string `json:"favorited_at" format:"date-time"`
}

// FavoriteDetailDto 收藏详情（3 字段；未收藏时 article 省略）。
type FavoriteDetailDto struct {
	Favorited   bool             `json:"favorited" example:"true"`
	FavoritedAt string           `json:"favorited_at,omitempty" format:"date-time"`
	Article     *FavoriteItemDto `json:"article,omitempty"`
}

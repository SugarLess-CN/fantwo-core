// Package model 用户域实体：User / UserProfile / UserDevice / UserSignIn /
// UserBlock / UserCertification / AccountDeletion。
//
// 对齐原 TypeORM 实体（tmp/picart/src/modules/user/entities/*.entity.ts）：
// 字段、状态枚举、默认值逐一平移；表名/列名 snake_case，PostgreSQL 类型。
// 主键沿用骨架 BaseModel 的 UUID(char36)（见 docs/04 §5.4，前端 id 字符串化）。
package model

import "time"

// 用户状态枚举（对齐原 user.entity.ts status enum）。
const (
	UserStatusActive   = "ACTIVE"
	UserStatusInactive = "INACTIVE"
	UserStatusBanned   = "BANNED"
)

// 会员状态枚举。
const (
	MembershipStatusActive   = "ACTIVE"
	MembershipStatusInactive = "INACTIVE"
)

// 性别枚举。
const (
	GenderMale   = "male"
	GenderFemale = "female"
	GenderOther  = "other"
)

// 认证类型 / 状态（对齐原 USER_CERTIFICATION_CONSTANTS）。
const (
	CertTypeOfficial = "OFFICIAL"
	CertTypeCreator  = "CREATOR"

	CertStatusPending  = "PENDING"
	CertStatusApproved = "APPROVED"
	CertStatusRejected = "REJECTED"
	CertStatusRevoked  = "REVOKED"
)

// 账号注销状态（三态：0 正常 / 1 注销冷静期（待注销）/ 2 已注销）。
const (
	DeletionStatusNone     = 0
	DeletionStatusPending  = 1
	DeletionStatusCanceled = 2
)

// User 用户表（原 user 表 / user.entity.ts）。
type User struct {
	BaseModel
	Username            string     `gorm:"column:username;type:varchar(64);uniqueIndex;not null" json:"username" example:"alice"`
	Nickname            *string    `gorm:"column:nickname;type:varchar(64);uniqueIndex" json:"nickname" example:"爱丽丝"`
	Password            string     `gorm:"column:password;type:varchar(255);not null" json:"-"`
	Email               *string    `gorm:"column:email;type:varchar(128);uniqueIndex" json:"email" example:"alice@example.com"`
	Phone               *string    `gorm:"column:phone;type:varchar(32);uniqueIndex" json:"phone" example:"13800138000"`
	Status              string     `gorm:"column:status;type:varchar(16);default:ACTIVE;index:idx_user_status_created,priority:1" json:"status" example:"PUBLISHED"`
	Banned              *time.Time `gorm:"column:banned" json:"banned"`
	BanReason           *string    `gorm:"column:ban_reason;type:varchar(255)" json:"ban_reason"`
	Avatar              *string    `gorm:"column:avatar;type:text" json:"avatar" example:"https://cdn.example.com/a.png"`
	Description         *string    `gorm:"column:description;type:varchar(255)" json:"description" example:"一句话说明"`
	Background          *string    `gorm:"column:background;type:text" json:"background" example:"https://cdn.example.com/bg.png"`
	Address             *string    `gorm:"column:address;type:text" json:"address" example:"杭州"`
	Gender              string     `gorm:"column:gender;type:varchar(16);default:other" json:"gender" example:"male"`
	BirthDate           *time.Time `gorm:"column:birth_date" json:"birth_date"`
	ArticleCount        int        `gorm:"column:article_count;default:0" json:"article_count"`
	FollowerCount       int        `gorm:"column:follower_count;default:0" json:"follower_count"`
	FollowingCount      int        `gorm:"column:following_count;default:0" json:"following_count"`
	Likes               int        `gorm:"column:likes;default:0" json:"likes"`
	Level               int        `gorm:"column:level;default:0" json:"level" example:"1"`
	Experience          int        `gorm:"column:experience;default:0" json:"experience"`
	Wallet              float64    `gorm:"column:wallet;type:numeric(12,2);default:0" json:"wallet" example:"100.50"`
	Points              int        `gorm:"column:points;default:0" json:"points"`
	MembershipLevel     int        `gorm:"column:membership_level;default:0" json:"membership_level"`
	MembershipLevelName string     `gorm:"column:membership_level_name;type:varchar(32);default:普通用户" json:"membership_level_name"`
	MembershipStatus    string     `gorm:"column:membership_status;type:varchar(16);default:INACTIVE;index:idx_user_membership,priority:1" json:"membership_status"`
	MembershipStartDate *time.Time `gorm:"column:membership_start_date" json:"membership_start_date"`
	MembershipEndDate   *time.Time `gorm:"column:membership_end_date;index:idx_user_membership,priority:2" json:"membership_end_date"`
	LastLoginAt         *time.Time `gorm:"column:last_login_at" json:"last_login_at"`
	LastActiveAt        *time.Time `gorm:"column:last_active_at;index" json:"last_active_at"`
	InviterID           *string    `gorm:"column:inviter_id;type:varchar(36)" json:"inviter_id"`
	MyInviteCode        *string    `gorm:"column:my_invite_code;type:varchar(50);uniqueIndex" json:"my_invite_code"`
	InviteCode          *string    `gorm:"column:invite_code;type:varchar(255)" json:"invite_code" example:"AB12CD"`
	InviteEarnings      float64    `gorm:"column:invite_earnings;type:numeric(12,2);default:0" json:"invite_earnings"`
	InviteCount         int        `gorm:"column:invite_count;default:0" json:"invite_count"`
	GithubID            *string    `gorm:"column:github_id;type:varchar(255);uniqueIndex" json:"github_id"`
	GithubUsername      *string    `gorm:"column:github_username;type:varchar(255)" json:"github_username"`

	// Roles RBAC 多对多（join 表 user_roles，对齐 TypeORM @JoinTable 默认命名）。
	Roles []Role `gorm:"many2many:user_roles;" json:"roles,omitempty"`
}

// TableName 表名。
func (User) TableName() string { return "user" }

// UserFollowing 用户关注关系表（多对多，对齐原 user.entity.ts 的
// @JoinTable name="user_followings"：follower → following）。
type UserFollowing struct {
	FollowerID  string    `gorm:"column:follower_id;type:varchar(36);primaryKey" json:"follower_id"`
	FollowingID string    `gorm:"column:following_id;type:varchar(36);primaryKey;index" json:"following_id"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名。
func (UserFollowing) TableName() string { return "user_followings" }

// UserProfile 用户配置表（原 user-config.entity.ts；含抽成配置与通知/隐私开关）。
type UserProfile struct {
	BaseModel
	UserID                    string  `gorm:"column:user_id;type:varchar(36);uniqueIndex;not null" json:"user_id"`
	ArticleCommissionRate     float64 `gorm:"column:article_commission_rate;type:numeric(5,2);default:0" json:"article_commission_rate" example:"0.10"`
	MembershipCommissionRate  float64 `gorm:"column:membership_commission_rate;type:numeric(5,2);default:0" json:"membership_commission_rate" example:"0.20"`
	ProductCommissionRate     float64 `gorm:"column:product_commission_rate;type:numeric(5,2);default:0" json:"product_commission_rate" example:"0.15"`
	ServiceCommissionRate     float64 `gorm:"column:service_commission_rate;type:numeric(5,2);default:0" json:"service_commission_rate" example:"0.05"`
	EnableCustomCommission    bool    `gorm:"column:enable_custom_commission;default:false" json:"enable_custom_commission" example:"false"`
	EnableSystemNotification  bool    `gorm:"column:enable_system_notification;default:true" json:"enable_system_notification" example:"true"`
	EnableCommentNotification bool    `gorm:"column:enable_comment_notification;default:true" json:"enable_comment_notification" example:"true"`
	EnableLikeNotification    bool    `gorm:"column:enable_like_notification;default:true" json:"enable_like_notification" example:"true"`
	EnableFollowNotification  bool    `gorm:"column:enable_follow_notification;default:true" json:"enable_follow_notification" example:"true"`
	EnableMessageNotification bool    `gorm:"column:enable_message_notification;default:true" json:"enable_message_notification" example:"true"`
	EnableOrderNotification   bool    `gorm:"column:enable_order_notification;default:true" json:"enable_order_notification" example:"true"`
	EnablePaymentNotification bool    `gorm:"column:enable_payment_notification;default:true" json:"enable_payment_notification" example:"true"`
	EnableInviteNotification  bool    `gorm:"column:enable_invite_notification;default:true" json:"enable_invite_notification" example:"true"`
	EnableEmailNotification   bool    `gorm:"column:enable_email_notification;default:true" json:"enable_email_notification" example:"false"`
	EnableSmsNotification     bool    `gorm:"column:enable_sms_notification;default:true" json:"enable_sms_notification" example:"false"`
	EnablePushNotification    bool    `gorm:"column:enable_push_notification;default:true" json:"enable_push_notification" example:"true"`
	HideFavorites             bool    `gorm:"column:hide_favorites;default:false" json:"hide_favorites"`
	HideComments              bool    `gorm:"column:hide_comments;default:false" json:"hide_comments"`
	HideCollections           bool    `gorm:"column:hide_collections;default:false" json:"hide_collections"`
	HideFollowers             bool    `gorm:"column:hide_followers;default:false" json:"hide_followers"`
	HideFollowings            bool    `gorm:"column:hide_followings;default:false" json:"hide_followings"`
	HideTags                  bool    `gorm:"column:hide_tags;default:false" json:"hide_tags"`
	WatermarkEnabled          bool    `gorm:"column:watermark_enabled;default:false" json:"watermark_enabled"`
	Remark                    *string `gorm:"column:remark;type:varchar(255)" json:"remark" example:"备注"`
}

// TableName 表名。
func (UserProfile) TableName() string { return "user_config" }

// UserDevice 用户设备表（多端在线，登录/刷新/登出靠它，原 user-device.entity.ts）。
type UserDevice struct {
	BaseModel
	UserID     string  `gorm:"column:user_id;type:varchar(36);index:idx_user_device,priority:1;not null" json:"user_id"`
	DeviceID   string  `gorm:"column:device_id;type:varchar(128);index:idx_user_device,priority:2;not null" json:"device_id"`
	DeviceType *string `gorm:"column:device_type;type:varchar(64)" json:"device_type"`
	DeviceName *string `gorm:"column:device_name;type:text" json:"device_name"`
	// refresh_token 存 JWT 原文：带 roles/perms 的 JWT 可达数百字节，用 text 兜住。
	// 曾因 varchar(512) 导致登录 500（SQLSTATE 22001 value too long）。
	RefreshToken *string `gorm:"column:refresh_token;type:text" json:"-"`
	// hashed_refresh_token 是 SHA-256 十六进制（64 字符），128 足够。
	HashedRefreshToken *string    `gorm:"column:hashed_refresh_token;type:varchar(128)" json:"-"`
	LoginAt            *time.Time `gorm:"column:login_at" json:"login_at"`
	LastActiveAt       *time.Time `gorm:"column:last_active_at" json:"last_active_at"`
}

// TableName 表名。
func (UserDevice) TableName() string { return "user_device" }

// UserSignIn 用户签到记录表（原 user-sign-in.entity.ts）。
type UserSignIn struct {
	BaseModel
	UserID          string    `gorm:"column:user_id;type:varchar(36);index:idx_sign_in_user_date,priority:1;not null" json:"user_id"`
	SignInDate      time.Time `gorm:"column:sign_in_date;type:date;index:idx_sign_in_user_date,priority:2;not null" json:"sign_in_date"`
	ConsecutiveDays int       `gorm:"column:consecutive_days;default:1" json:"consecutive_days"`
	IsAuto          bool      `gorm:"column:is_auto;default:false" json:"is_auto"`
}

// TableName 表名。
func (UserSignIn) TableName() string { return "user_sign_in" }

// UserBlock 用户拉黑表（原 user-block.entity.ts）。
type UserBlock struct {
	BaseModel
	UserID        string  `gorm:"column:user_id;type:varchar(36);uniqueIndex:uk_user_block,priority:1;not null" json:"user_id"`
	BlockedUserID string  `gorm:"column:blocked_user_id;type:varchar(36);uniqueIndex:uk_user_block,priority:2;not null" json:"blocked_user_id"`
	Reason        *string `gorm:"column:reason;type:varchar(255)" json:"reason" example:"违规内容"`
}

// TableName 表名。
func (UserBlock) TableName() string { return "user_block" }

// UserCertification 用户认证表（官方/创作者，原 user-certification.entity.ts）。
type UserCertification struct {
	BaseModel
	UserID           string     `gorm:"column:user_id;type:varchar(36);index:idx_cert_user_status,priority:1;not null" json:"user_id"`
	Type             string     `gorm:"column:type;type:varchar(16);index:idx_cert_type_status,priority:1;not null" json:"type" example:"image"`
	Status           string     `gorm:"column:status;type:varchar(16);default:PENDING;index:idx_cert_user_status,priority:2;index:idx_cert_type_status,priority:2" json:"status" example:"PUBLISHED"`
	RealName         *string    `gorm:"column:real_name;type:varchar(128)" json:"real_name" example:"张三"`
	OrganizationName *string    `gorm:"column:organization_name;type:varchar(128)" json:"organization_name" example:"某某工作室"`
	Description      *string    `gorm:"column:description;type:text" json:"description" example:"一句话说明"`
	Slogan           *string    `gorm:"column:slogan;type:varchar(80)" json:"slogan" example:"记录光"`
	Materials        string     `gorm:"column:materials;type:jsonb;default:'[]'" json:"materials"`
	ReviewerID       *string    `gorm:"column:reviewer_id;type:varchar(36)" json:"reviewer_id"`
	ReviewReason     *string    `gorm:"column:review_reason;type:text" json:"review_reason"`
	ReviewedAt       *time.Time `gorm:"column:reviewed_at" json:"reviewed_at"`

	// CertifiedAt 认证**通过**的时间（对外契约字段 certified_at）。
	//
	// 与 reviewed_at 的区别（两个都要，不要合并）：
	//   · reviewed_at  = 审核**动作**发生的时间 —— 通过/拒绝都有值；
	//   · certified_at = 认证**生效**的时间 —— 只有 APPROVED 有值，
	//                    拒绝时保持 NULL，撤销（REVOKED）时清回 NULL。
	// 前端用它显示「认证于 xxxx 年」，拿 reviewed_at 会在被拒场景显示错误的语义。
	CertifiedAt *time.Time `gorm:"column:certified_at" json:"certified_at"`
}

// TableName 表名。
func (UserCertification) TableName() string { return "user_certification" }

// AccountDeletion 账号注销表（三态 + 7 天冷静期，对齐任务书 §2 账号注销三态）。
type AccountDeletion struct {
	BaseModel
	UserID      string     `gorm:"column:user_id;type:varchar(36);uniqueIndex;not null" json:"user_id"`
	Status      int        `gorm:"column:status;default:1" json:"status"` // 0 正常 / 1 冷静期 / 2 已注销
	Reason      *string    `gorm:"column:reason;type:varchar(255)" json:"reason" example:"违规内容"`
	ScheduledAt time.Time  `gorm:"column:scheduled_at" json:"scheduled_at"` // 冷静期到期执行时间
	CanceledAt  *time.Time `gorm:"column:canceled_at" json:"canceled_at"`
	ExecutedAt  *time.Time `gorm:"column:executed_at" json:"executed_at"`
}

// TableName 表名。
func (AccountDeletion) TableName() string { return "account_deletion" }

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
	Username            string     `gorm:"column:username;type:varchar(64);uniqueIndex;not null" json:"username"`
	Nickname            *string    `gorm:"column:nickname;type:varchar(64);uniqueIndex" json:"nickname"`
	Password            string     `gorm:"column:password;type:varchar(255);not null" json:"-"`
	Email               *string    `gorm:"column:email;type:varchar(128);uniqueIndex" json:"email"`
	Phone               *string    `gorm:"column:phone;type:varchar(32);uniqueIndex" json:"phone"`
	Status              string     `gorm:"column:status;type:varchar(16);default:ACTIVE;index:idx_user_status_created,priority:1" json:"status"`
	Banned              *time.Time `gorm:"column:banned" json:"banned"`
	BanReason           *string    `gorm:"column:ban_reason;type:varchar(255)" json:"banReason"`
	Avatar              *string    `gorm:"column:avatar;type:text" json:"avatar"`
	Description         *string    `gorm:"column:description;type:varchar(255)" json:"description"`
	Background          *string    `gorm:"column:background;type:text" json:"background"`
	Address             *string    `gorm:"column:address;type:text" json:"address"`
	Gender              string     `gorm:"column:gender;type:varchar(16);default:other" json:"gender"`
	BirthDate           *time.Time `gorm:"column:birth_date" json:"birthDate"`
	ArticleCount        int        `gorm:"column:article_count;default:0" json:"articleCount"`
	FollowerCount       int        `gorm:"column:follower_count;default:0" json:"followerCount"`
	FollowingCount      int        `gorm:"column:following_count;default:0" json:"followingCount"`
	Likes               int        `gorm:"column:likes;default:0" json:"likes"`
	Level               int        `gorm:"column:level;default:0" json:"level"`
	Experience          int        `gorm:"column:experience;default:0" json:"experience"`
	Wallet              float64    `gorm:"column:wallet;type:numeric(12,2);default:0" json:"wallet"`
	Points              int        `gorm:"column:points;default:0" json:"points"`
	MembershipLevel     int        `gorm:"column:membership_level;default:0" json:"membershipLevel"`
	MembershipLevelName string     `gorm:"column:membership_level_name;type:varchar(32);default:普通用户" json:"membershipLevelName"`
	MembershipStatus    string     `gorm:"column:membership_status;type:varchar(16);default:INACTIVE;index:idx_user_membership,priority:1" json:"membershipStatus"`
	MembershipStartDate *time.Time `gorm:"column:membership_start_date" json:"membershipStartDate"`
	MembershipEndDate   *time.Time `gorm:"column:membership_end_date;index:idx_user_membership,priority:2" json:"membershipEndDate"`
	LastLoginAt         *time.Time `gorm:"column:last_login_at" json:"lastLoginAt"`
	LastActiveAt        *time.Time `gorm:"column:last_active_at;index" json:"lastActiveAt"`
	InviterID           *string    `gorm:"column:inviter_id;type:char(36)" json:"inviterId"`
	MyInviteCode        *string    `gorm:"column:my_invite_code;type:varchar(50);uniqueIndex" json:"myInviteCode"`
	InviteCode          *string    `gorm:"column:invite_code;type:varchar(255)" json:"inviteCode"`
	InviteEarnings      float64    `gorm:"column:invite_earnings;type:numeric(12,2);default:0" json:"inviteEarnings"`
	InviteCount         int        `gorm:"column:invite_count;default:0" json:"inviteCount"`
	GithubID            *string    `gorm:"column:github_id;type:varchar(255);uniqueIndex" json:"githubId"`
	GithubUsername      *string    `gorm:"column:github_username;type:varchar(255)" json:"githubUsername"`

	// Roles RBAC 多对多（join 表 user_roles，对齐 TypeORM @JoinTable 默认命名）。
	Roles []Role `gorm:"many2many:user_roles;" json:"roles,omitempty"`
}

// TableName 表名。
func (User) TableName() string { return "user" }

// UserFollowing 用户关注关系表（多对多，对齐原 user.entity.ts 的
// @JoinTable name="user_followings"：follower → following）。
type UserFollowing struct {
	FollowerID  string    `gorm:"column:follower_id;type:char(36);primaryKey" json:"followerId"`
	FollowingID string    `gorm:"column:following_id;type:char(36);primaryKey;index" json:"followingId"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

// TableName 表名。
func (UserFollowing) TableName() string { return "user_followings" }

// UserProfile 用户配置表（原 user-config.entity.ts；含抽成配置与通知/隐私开关）。
type UserProfile struct {
	BaseModel
	UserID                    string  `gorm:"column:user_id;type:char(36);uniqueIndex;not null" json:"userId"`
	ArticleCommissionRate     float64 `gorm:"column:article_commission_rate;type:numeric(5,2);default:0" json:"articleCommissionRate"`
	MembershipCommissionRate  float64 `gorm:"column:membership_commission_rate;type:numeric(5,2);default:0" json:"membershipCommissionRate"`
	ProductCommissionRate     float64 `gorm:"column:product_commission_rate;type:numeric(5,2);default:0" json:"productCommissionRate"`
	ServiceCommissionRate     float64 `gorm:"column:service_commission_rate;type:numeric(5,2);default:0" json:"serviceCommissionRate"`
	EnableCustomCommission    bool    `gorm:"column:enable_custom_commission;default:false" json:"enableCustomCommission"`
	EnableSystemNotification  bool    `gorm:"column:enable_system_notification;default:true" json:"enableSystemNotification"`
	EnableCommentNotification bool    `gorm:"column:enable_comment_notification;default:true" json:"enableCommentNotification"`
	EnableLikeNotification    bool    `gorm:"column:enable_like_notification;default:true" json:"enableLikeNotification"`
	EnableFollowNotification  bool    `gorm:"column:enable_follow_notification;default:true" json:"enableFollowNotification"`
	EnableMessageNotification bool    `gorm:"column:enable_message_notification;default:true" json:"enableMessageNotification"`
	EnableOrderNotification   bool    `gorm:"column:enable_order_notification;default:true" json:"enableOrderNotification"`
	EnablePaymentNotification bool    `gorm:"column:enable_payment_notification;default:true" json:"enablePaymentNotification"`
	EnableInviteNotification  bool    `gorm:"column:enable_invite_notification;default:true" json:"enableInviteNotification"`
	EnableEmailNotification   bool    `gorm:"column:enable_email_notification;default:true" json:"enableEmailNotification"`
	EnableSmsNotification     bool    `gorm:"column:enable_sms_notification;default:true" json:"enableSmsNotification"`
	EnablePushNotification    bool    `gorm:"column:enable_push_notification;default:true" json:"enablePushNotification"`
	HideFavorites             bool    `gorm:"column:hide_favorites;default:false" json:"hideFavorites"`
	HideComments              bool    `gorm:"column:hide_comments;default:false" json:"hideComments"`
	HideCollections           bool    `gorm:"column:hide_collections;default:false" json:"hideCollections"`
	HideFollowers             bool    `gorm:"column:hide_followers;default:false" json:"hideFollowers"`
	HideFollowings            bool    `gorm:"column:hide_followings;default:false" json:"hideFollowings"`
	HideTags                  bool    `gorm:"column:hide_tags;default:false" json:"hideTags"`
	WatermarkEnabled          bool    `gorm:"column:watermark_enabled;default:false" json:"watermarkEnabled"`
	Remark                    *string `gorm:"column:remark;type:varchar(255)" json:"remark"`
}

// TableName 表名。
func (UserProfile) TableName() string { return "user_config" }

// UserDevice 用户设备表（多端在线，登录/刷新/登出靠它，原 user-device.entity.ts）。
type UserDevice struct {
	BaseModel
	UserID             string     `gorm:"column:user_id;type:char(36);index:idx_user_device,priority:1;not null" json:"userId"`
	DeviceID           string     `gorm:"column:device_id;type:varchar(128);index:idx_user_device,priority:2;not null" json:"deviceId"`
	DeviceType         *string    `gorm:"column:device_type;type:varchar(64)" json:"deviceType"`
	DeviceName         *string    `gorm:"column:device_name;type:text" json:"deviceName"`
	// refresh_token 存 JWT 原文：带 roles/perms 的 JWT 可达数百字节，用 text 兜住。
	// 曾因 varchar(512) 导致登录 500（SQLSTATE 22001 value too long）。
	RefreshToken       *string    `gorm:"column:refresh_token;type:text" json:"-"`
	// hashed_refresh_token 是 SHA-256 十六进制（64 字符），128 足够。
	HashedRefreshToken *string    `gorm:"column:hashed_refresh_token;type:varchar(128)" json:"-"`
	LoginAt            *time.Time `gorm:"column:login_at" json:"loginAt"`
	LastActiveAt       *time.Time `gorm:"column:last_active_at" json:"lastActiveAt"`
}

// TableName 表名。
func (UserDevice) TableName() string { return "user_device" }

// UserSignIn 用户签到记录表（原 user-sign-in.entity.ts）。
type UserSignIn struct {
	BaseModel
	UserID          string    `gorm:"column:user_id;type:char(36);index:idx_sign_in_user_date,priority:1;not null" json:"userId"`
	SignInDate      time.Time `gorm:"column:sign_in_date;type:date;index:idx_sign_in_user_date,priority:2;not null" json:"signInDate"`
	ConsecutiveDays int       `gorm:"column:consecutive_days;default:1" json:"consecutiveDays"`
	IsAuto          bool      `gorm:"column:is_auto;default:false" json:"isAuto"`
}

// TableName 表名。
func (UserSignIn) TableName() string { return "user_sign_in" }

// UserBlock 用户拉黑表（原 user-block.entity.ts）。
type UserBlock struct {
	BaseModel
	UserID        string  `gorm:"column:user_id;type:char(36);uniqueIndex:uk_user_block,priority:1;not null" json:"userId"`
	BlockedUserID string  `gorm:"column:blocked_user_id;type:char(36);uniqueIndex:uk_user_block,priority:2;not null" json:"blockedUserId"`
	Reason        *string `gorm:"column:reason;type:varchar(255)" json:"reason"`
}

// TableName 表名。
func (UserBlock) TableName() string { return "user_block" }

// UserCertification 用户认证表（官方/创作者，原 user-certification.entity.ts）。
type UserCertification struct {
	BaseModel
	UserID           string     `gorm:"column:user_id;type:char(36);index:idx_cert_user_status,priority:1;not null" json:"userId"`
	Type             string     `gorm:"column:type;type:varchar(16);index:idx_cert_type_status,priority:1;not null" json:"type"`
	Status           string     `gorm:"column:status;type:varchar(16);default:PENDING;index:idx_cert_user_status,priority:2;index:idx_cert_type_status,priority:2" json:"status"`
	RealName         *string    `gorm:"column:real_name;type:varchar(128)" json:"realName"`
	OrganizationName *string    `gorm:"column:organization_name;type:varchar(128)" json:"organizationName"`
	Description      *string    `gorm:"column:description;type:text" json:"description"`
	Slogan           *string    `gorm:"column:slogan;type:varchar(80)" json:"slogan"`
	Materials        string     `gorm:"column:materials;type:jsonb;default:'[]'" json:"materials"`
	ReviewerID       *string    `gorm:"column:reviewer_id;type:char(36)" json:"reviewerId"`
	ReviewReason     *string    `gorm:"column:review_reason;type:text" json:"reviewReason"`
	ReviewedAt       *time.Time `gorm:"column:reviewed_at" json:"reviewedAt"`
}

// TableName 表名。
func (UserCertification) TableName() string { return "user_certification" }

// AccountDeletion 账号注销表（三态 + 7 天冷静期，对齐任务书 §2 账号注销三态）。
type AccountDeletion struct {
	BaseModel
	UserID      string     `gorm:"column:user_id;type:char(36);uniqueIndex;not null" json:"userId"`
	Status      int        `gorm:"column:status;default:1" json:"status"` // 0 正常 / 1 冷静期 / 2 已注销
	Reason      *string    `gorm:"column:reason;type:varchar(255)" json:"reason"`
	ScheduledAt time.Time  `gorm:"column:scheduled_at" json:"scheduledAt"` // 冷静期到期执行时间
	CanceledAt  *time.Time `gorm:"column:canceled_at" json:"canceledAt"`
	ExecutedAt  *time.Time `gorm:"column:executed_at" json:"executedAt"`
}

// TableName 表名。
func (AccountDeletion) TableName() string { return "account_deletion" }

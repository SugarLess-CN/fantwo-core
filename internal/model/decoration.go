// 装饰品域实体（对齐原 decoration 模块四张表：装饰品/用户装饰品/活动/用户活动进度）。
package model

import "time"

// 装饰品类型（对齐原 enum）。
const (
	DecorationTypeAvatarFrame      = "AVATAR_FRAME"
	DecorationTypeCommentBubble    = "COMMENT_BUBBLE"
	DecorationTypeAchievementBadge = "ACHIEVEMENT_BADGE"
)

// 获取方式（对齐原 enum）。
const (
	DecorationObtainPurchase    = "PURCHASE"
	DecorationObtainActivity    = "ACTIVITY"
	DecorationObtainGift        = "GIFT"
	DecorationObtainAchievement = "ACHIEVEMENT"
	DecorationObtainDefault     = "DEFAULT"
)

// 稀有度（装饰品与成就共用枚举，定义在 achievement.go；此处复用同名常量）。
// 装饰品稀有度（枚举值与成就稀有度一致，独立命名便于装饰品域自解释）。
const (
	DecorationRarityCommon    = AchievementRarityCommon
	DecorationRarityRare      = AchievementRarityRare
	DecorationRarityEpic      = AchievementRarityEpic
	DecorationRarityLegendary = AchievementRarityLegendary
)

// 装饰品状态（对齐原 enum）。
const (
	DecorationStatusActive   = "ACTIVE"
	DecorationStatusInactive = "INACTIVE"
)

// 活动类型（对齐原 enum）。
const (
	DecorationActivityLike     = "LIKE"
	DecorationActivityComment  = "COMMENT"
	DecorationActivityShare    = "SHARE"
	DecorationActivityRecharge = "RECHARGE"
	DecorationActivitySignIn   = "SIGN_IN"
	DecorationActivityCustom   = "CUSTOM"
)

// 活动状态（对齐原 enum）。
const (
	DecorationActivityStatusActive   = "ACTIVE"
	DecorationActivityStatusInactive = "INACTIVE"
	DecorationActivityStatusEnded    = "ENDED"
)

// Decoration 装饰品（头像框 / 评论气泡 / 成就勋章）。
type Decoration struct {
	BaseModel
	Name          string  `gorm:"type:varchar(100);not null;comment:装饰品名称" json:"name"`
	Type          string  `gorm:"type:varchar(20);index;comment:装饰品类型" json:"type"`
	Description   string  `gorm:"type:text;comment:描述" json:"description"`
	ImageURL      string  `gorm:"type:varchar(512);comment:图片地址" json:"imageUrl"`
	PreviewURL    string  `gorm:"type:varchar(512);comment:预览图地址" json:"previewUrl"`
	Rarity        string  `gorm:"type:varchar(20);default:COMMON;comment:稀有度" json:"rarity"`
	BubbleColor   string  `gorm:"type:varchar(20);comment:气泡颜色（评论气泡用）" json:"bubbleColor"`
	ObtainMethod  string  `gorm:"type:varchar(20);comment:获取方式" json:"obtainMethod"`
	IsPurchasable bool    `gorm:"not null;default:false;comment:是否可购买" json:"isPurchasable"`
	Price         float64 `gorm:"type:decimal(10,2);default:0;comment:购买价格（元）" json:"price"`
	IsPermanent   bool    `gorm:"not null;default:false;comment:是否永久" json:"isPermanent"`
	ValidDays     int     `gorm:"not null;default:0;comment:有效天数（非永久）" json:"validDays"`
	Sort          int     `gorm:"not null;default:0;comment:排序（越大越靠前）" json:"sort"`
	Status        string  `gorm:"type:varchar(20);default:ACTIVE;index;comment:状态" json:"status"`
	// ActivityID / AchievementID 归属来源，用于反查唯一来源（成就勋章按 achievement_id 唯一）。
	ActivityID    string `gorm:"type:varchar(36);index;comment:来源活动ID" json:"activityId"`
	AchievementID string `gorm:"type:varchar(36);index;comment:来源成就ID" json:"achievementId"`
}

// TableName 表名。
func (Decoration) TableName() string { return "decorations" }

// UserDecoration 用户持有的装饰品。
type UserDecoration struct {
	BaseModel
	UserID         string     `gorm:"type:varchar(36);index:idx_user_decoration,priority:1;comment:用户ID" json:"userId"`
	DecorationID   string     `gorm:"type:varchar(36);index:idx_user_decoration,priority:2;comment:装饰品ID" json:"decorationId"`
	ObtainMethod   string     `gorm:"type:varchar(20);comment:获取方式" json:"obtainMethod"`
	IsPermanent    bool       `gorm:"not null;default:false;comment:是否永久" json:"isPermanent"`
	ExpiresAt      *time.Time `json:"expiresAt"`
	IsUsing        bool       `gorm:"not null;default:false;index;comment:是否正在使用" json:"isUsing"`
	GiftFromUserID string     `gorm:"type:varchar(36);comment:赠送人ID" json:"giftFromUserId"`
	OrderID        string     `gorm:"type:varchar(36);comment:来源订单ID" json:"orderId"`
	ActivityID     string     `gorm:"type:varchar(36);comment:来源活动ID" json:"activityId"`
	Remark         string     `gorm:"type:text;comment:备注" json:"remark"`
}

// TableName 表名。
func (UserDecoration) TableName() string { return "user_decorations" }

// DecorationActivity 装饰品活动。
type DecorationActivity struct {
	BaseModel
	Name               string    `gorm:"type:varchar(100);not null;comment:活动名称" json:"name"`
	Description        string    `gorm:"type:text;comment:活动描述" json:"description"`
	Type               string    `gorm:"type:varchar(20);comment:活动类型" json:"type"`
	DecorationID       string    `gorm:"type:varchar(36);index;comment:奖励装饰品ID" json:"decorationId"`
	ArticleID          string    `gorm:"type:varchar(36);comment:关联文章ID（活动说明）" json:"articleId"`
	RequiredLikes      int       `gorm:"not null;default:0;comment:所需点赞数" json:"requiredLikes"`
	RequiredComments   int       `gorm:"not null;default:0;comment:所需评论数" json:"requiredComments"`
	RequiredShares     int       `gorm:"not null;default:0;comment:所需分享数" json:"requiredShares"`
	RequiredRecharge   float64   `gorm:"type:decimal(10,2);default:0;comment:所需充值金额" json:"requiredRecharge"`
	RequiredSignInDays int       `gorm:"not null;default:0;comment:所需签到天数" json:"requiredSignInDays"`
	IsPermanent        bool      `gorm:"not null;default:false;comment:奖励是否永久" json:"isPermanent"`
	ValidDays          int       `gorm:"not null;default:0;comment:奖励有效天数" json:"validDays"`
	StartTime          time.Time `gorm:"comment:活动开始时间" json:"startTime"`
	EndTime            time.Time `gorm:"comment:活动结束时间" json:"endTime"`
	Status             string    `gorm:"type:varchar(20);default:ACTIVE;index;comment:状态" json:"status"`
	ParticipantCount   int64     `gorm:"not null;default:0;comment:参与人数" json:"participantCount"`
	CompletedCount     int64     `gorm:"not null;default:0;comment:完成人数" json:"completedCount"`
}

// TableName 表名。
func (DecorationActivity) TableName() string { return "decoration_activities" }

// UserActivityProgress 用户装饰品活动进度。
type UserActivityProgress struct {
	BaseModel
	UserID            string     `gorm:"type:varchar(36);index:idx_user_activity_progress,priority:1;comment:用户ID" json:"userId"`
	ActivityID        string     `gorm:"type:varchar(36);index:idx_user_activity_progress,priority:2;comment:活动ID" json:"activityId"`
	CurrentLikes      int        `gorm:"not null;default:0;comment:当前点赞数" json:"currentLikes"`
	CurrentComments   int        `gorm:"not null;default:0;comment:当前评论数" json:"currentComments"`
	CurrentShares     int        `gorm:"not null;default:0;comment:当前分享数" json:"currentShares"`
	CurrentRecharge   float64    `gorm:"type:decimal(10,2);default:0;comment:当前充值金额" json:"currentRecharge"`
	CurrentSignInDays int        `gorm:"not null;default:0;comment:当前签到天数" json:"currentSignInDays"`
	IsCompleted       bool       `gorm:"not null;default:false;comment:是否已完成" json:"isCompleted"`
	IsRewarded        bool       `gorm:"not null;default:false;comment:是否已领取" json:"isRewarded"`
	CompletedAt       *time.Time `json:"completedAt"`
	RewardedAt        *time.Time `json:"rewardedAt"`
}

// TableName 表名。
func (UserActivityProgress) TableName() string { return "user_activity_progress" }

// DecorationQuery 装饰品列表查询条件。
type DecorationQuery struct {
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
	Type      string `form:"type"`
	Status    string `form:"status"`
	Keyword   string `form:"keyword"`
	SortBy    string `form:"sortBy"`
	SortOrder string `form:"sortOrder"`
}

// DecorationInput 装饰品创建/更新入参。
type DecorationInput struct {
	Name          *string  `json:"name"`
	Type          *string  `json:"type"`
	Description   *string  `json:"description"`
	ImageURL      *string  `json:"imageUrl"`
	PreviewURL    *string  `json:"previewUrl"`
	Rarity        *string  `json:"rarity"`
	BubbleColor   *string  `json:"bubbleColor"`
	ObtainMethod  *string  `json:"obtainMethod"`
	IsPurchasable *bool    `json:"isPurchasable"`
	Price         *float64 `json:"price"`
	IsPermanent   *bool    `json:"isPermanent"`
	ValidDays     *int     `json:"validDays"`
	Sort          *int     `json:"sort"`
	Status        *string  `json:"status"`
}

// DecorationActivityInput 活动创建/更新入参。
type DecorationActivityInput struct {
	Name               *string    `json:"name"`
	Description        *string    `json:"description"`
	Type               *string    `json:"type"`
	DecorationID       *string    `json:"decorationId"`
	ArticleID          *string    `json:"articleId"`
	RequiredLikes      *int       `json:"requiredLikes"`
	RequiredComments   *int       `json:"requiredComments"`
	RequiredShares     *int       `json:"requiredShares"`
	RequiredRecharge   *float64   `json:"requiredRecharge"`
	RequiredSignInDays *int       `json:"requiredSignInDays"`
	IsPermanent        *bool      `json:"isPermanent"`
	ValidDays          *int       `json:"validDays"`
	StartTime          *time.Time `json:"startTime"`
	EndTime            *time.Time `json:"endTime"`
	Status             *string    `json:"status"`
}

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
	Name          string  `gorm:"type:varchar(100);not null;comment:装饰品名称" json:"name" example:"示例名称"`
	Type          string  `gorm:"type:varchar(20);index;comment:装饰品类型" json:"type" example:"image"`
	Description   string  `gorm:"type:text;comment:描述" json:"description" example:"一句话说明"`
	ImageURL      string  `gorm:"type:varchar(512);comment:图片地址" json:"image_url" example:"https://cdn.example.com/banner.png"`
	PreviewURL    string  `gorm:"type:varchar(512);comment:预览图地址" json:"preview_url"`
	Rarity        string  `gorm:"type:varchar(20);default:COMMON;comment:稀有度" json:"rarity" example:"RARE"`
	BubbleColor   string  `gorm:"type:varchar(20);comment:气泡颜色（评论气泡用）" json:"bubble_color"`
	ObtainMethod  string  `gorm:"type:varchar(20);comment:获取方式" json:"obtain_method"`
	IsPurchasable bool    `gorm:"not null;default:false;comment:是否可购买" json:"is_purchasable"`
	Price         float64 `gorm:"type:decimal(10,2);default:0;comment:购买价格（元）" json:"price" example:"9.90"`
	IsPermanent   bool    `gorm:"not null;default:false;comment:是否永久" json:"is_permanent"`
	ValidDays     int     `gorm:"not null;default:0;comment:有效天数（非永久）" json:"valid_days" example:"0"`
	Sort          int     `gorm:"not null;default:0;comment:排序（越大越靠前）" json:"sort" example:"1"`
	Status        string  `gorm:"type:varchar(20);default:ACTIVE;index;comment:状态" json:"status" example:"PUBLISHED"`
	// ActivityID / AchievementID 归属来源，用于反查唯一来源（成就勋章按 achievement_id 唯一）。
	ActivityID    string `gorm:"type:varchar(36);index;comment:来源活动ID" json:"activity_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	AchievementID string `gorm:"type:varchar(36);index;comment:来源成就ID" json:"achievement_id"`
}

// TableName 表名。
func (Decoration) TableName() string { return "decorations" }

// UserDecoration 用户持有的装饰品。
type UserDecoration struct {
	BaseModel
	UserID         string     `gorm:"type:varchar(36);index:idx_user_decoration,priority:1;comment:用户ID" json:"user_id"`
	DecorationID   string     `gorm:"type:varchar(36);index:idx_user_decoration,priority:2;comment:装饰品ID" json:"decoration_id"`
	ObtainMethod   string     `gorm:"type:varchar(20);comment:获取方式" json:"obtain_method"`
	IsPermanent    bool       `gorm:"not null;default:false;comment:是否永久" json:"is_permanent"`
	ExpiresAt      *time.Time `json:"expires_at"`
	IsUsing        bool       `gorm:"not null;default:false;index;comment:是否正在使用" json:"is_using"`
	GiftFromUserID string     `gorm:"type:varchar(36);comment:赠送人ID" json:"gift_from_user_id"`
	OrderID        string     `gorm:"type:varchar(36);comment:来源订单ID" json:"order_id"`
	ActivityID     string     `gorm:"type:varchar(36);comment:来源活动ID" json:"activity_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Remark         string     `gorm:"type:text;comment:备注" json:"remark" example:"备注"`
}

// TableName 表名。
func (UserDecoration) TableName() string { return "user_decorations" }

// DecorationActivity 装饰品活动。
type DecorationActivity struct {
	BaseModel
	Name               string    `gorm:"type:varchar(100);not null;comment:活动名称" json:"name" example:"示例名称"`
	Description        string    `gorm:"type:text;comment:活动描述" json:"description" example:"一句话说明"`
	Type               string    `gorm:"type:varchar(20);comment:活动类型" json:"type" example:"image"`
	DecorationID       string    `gorm:"type:varchar(36);index;comment:奖励装饰品ID" json:"decoration_id"`
	ArticleID          string    `gorm:"type:varchar(36);comment:关联文章ID（活动说明）" json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	RequiredLikes      int       `gorm:"not null;default:0;comment:所需点赞数" json:"required_likes"`
	RequiredComments   int       `gorm:"not null;default:0;comment:所需评论数" json:"required_comments"`
	RequiredShares     int       `gorm:"not null;default:0;comment:所需分享数" json:"required_shares"`
	RequiredRecharge   float64   `gorm:"type:decimal(10,2);default:0;comment:所需充值金额" json:"required_recharge"`
	RequiredSignInDays int       `gorm:"not null;default:0;comment:所需签到天数" json:"required_sign_in_days"`
	IsPermanent        bool      `gorm:"not null;default:false;comment:奖励是否永久" json:"is_permanent"`
	ValidDays          int       `gorm:"not null;default:0;comment:奖励有效天数" json:"valid_days" example:"0"`
	StartTime          time.Time `gorm:"comment:活动开始时间" json:"start_time"`
	EndTime            time.Time `gorm:"comment:活动结束时间" json:"end_time"`
	Status             string    `gorm:"type:varchar(20);default:ACTIVE;index;comment:状态" json:"status" example:"PUBLISHED"`
	ParticipantCount   int64     `gorm:"not null;default:0;comment:参与人数" json:"participant_count"`
	CompletedCount     int64     `gorm:"not null;default:0;comment:完成人数" json:"completed_count"`
}

// TableName 表名。
func (DecorationActivity) TableName() string { return "decoration_activities" }

// UserActivityProgress 用户装饰品活动进度。
type UserActivityProgress struct {
	BaseModel
	UserID            string     `gorm:"type:varchar(36);index:idx_user_activity_progress,priority:1;comment:用户ID" json:"user_id"`
	ActivityID        string     `gorm:"type:varchar(36);index:idx_user_activity_progress,priority:2;comment:活动ID" json:"activity_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	CurrentLikes      int        `gorm:"not null;default:0;comment:当前点赞数" json:"current_likes"`
	CurrentComments   int        `gorm:"not null;default:0;comment:当前评论数" json:"current_comments"`
	CurrentShares     int        `gorm:"not null;default:0;comment:当前分享数" json:"current_shares"`
	CurrentRecharge   float64    `gorm:"type:decimal(10,2);default:0;comment:当前充值金额" json:"current_recharge"`
	CurrentSignInDays int        `gorm:"not null;default:0;comment:当前签到天数" json:"current_sign_in_days"`
	IsCompleted       bool       `gorm:"not null;default:false;comment:是否已完成" json:"is_completed"`
	IsRewarded        bool       `gorm:"not null;default:false;comment:是否已领取" json:"is_rewarded"`
	CompletedAt       *time.Time `json:"completed_at"`
	RewardedAt        *time.Time `json:"rewarded_at"`
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
	SortBy    string `form:"sort_by"`
	SortOrder string `form:"sort_order"`
}

// DecorationInput 装饰品创建/更新入参。
type DecorationInput struct {
	Name          *string  `json:"name" example:"示例名称"`
	Type          *string  `json:"type" example:"image"`
	Description   *string  `json:"description" example:"一句话说明"`
	ImageURL      *string  `json:"image_url" example:"https://cdn.example.com/banner.png"`
	PreviewURL    *string  `json:"preview_url"`
	Rarity        *string  `json:"rarity" example:"RARE"`
	BubbleColor   *string  `json:"bubble_color"`
	ObtainMethod  *string  `json:"obtain_method"`
	IsPurchasable *bool    `json:"is_purchasable"`
	Price         *float64 `json:"price" example:"9.90"`
	IsPermanent   *bool    `json:"is_permanent"`
	ValidDays     *int     `json:"valid_days" example:"0"`
	Sort          *int     `json:"sort" example:"1"`
	Status        *string  `json:"status" example:"PUBLISHED"`
}

// DecorationActivityInput 活动创建/更新入参。
type DecorationActivityInput struct {
	Name               *string    `json:"name" example:"示例名称"`
	Description        *string    `json:"description" example:"一句话说明"`
	Type               *string    `json:"type" example:"image"`
	DecorationID       *string    `json:"decoration_id"`
	ArticleID          *string    `json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	RequiredLikes      *int       `json:"required_likes"`
	RequiredComments   *int       `json:"required_comments"`
	RequiredShares     *int       `json:"required_shares"`
	RequiredRecharge   *float64   `json:"required_recharge"`
	RequiredSignInDays *int       `json:"required_sign_in_days"`
	IsPermanent        *bool      `json:"is_permanent"`
	ValidDays          *int       `json:"valid_days" example:"0"`
	StartTime          *time.Time `json:"start_time"`
	EndTime            *time.Time `json:"end_time"`
	Status             *string    `json:"status" example:"PUBLISHED"`
}

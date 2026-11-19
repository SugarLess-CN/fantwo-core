// 成就域实体（对齐原 achievement 模块：成就定义 + 用户成就进度）。
package model

import "time"

// 成就类型（对齐原 enum）。
const (
	AchievementTypeArticle = "ARTICLE"
	AchievementTypeComment = "COMMENT"
	AchievementTypeSocial  = "SOCIAL"
	AchievementTypeLevel   = "LEVEL"
	AchievementTypeSpecial = "SPECIAL"
)

// 稀有度（对齐原 enum）。
const (
	AchievementRarityCommon    = "COMMON"
	AchievementRarityRare      = "RARE"
	AchievementRarityEpic      = "EPIC"
	AchievementRarityLegendary = "LEGENDARY"
)

// Achievement 成就定义。
type Achievement struct {
	BaseModel
	Code        string `gorm:"type:varchar(64);index;comment:成就代码" json:"code"`
	Name        string `gorm:"type:varchar(128);not null;comment:成就名称" json:"name"`
	Description string `gorm:"type:text;comment:成就描述" json:"description"`
	Icon        string `gorm:"type:varchar(512);comment:成就图标" json:"icon"`
	Type        string `gorm:"type:varchar(20);comment:成就类型" json:"type"`
	Rarity      string `gorm:"type:varchar(20);default:COMMON;comment:稀有度" json:"rarity"`
	// Condition 完成条件（原 json 列）：{type, target, ...}，target 为目标值。
	Condition          AchievementCondition `gorm:"type:jsonb;serializer:json;comment:完成条件" json:"condition"`
	RewardPoints       int                  `gorm:"not null;default:0;comment:奖励积分" json:"rewardPoints"`
	RewardExp          int                  `gorm:"not null;default:0;comment:奖励经验" json:"rewardExp"`
	RewardDecorationID string               `gorm:"type:char(36);comment:奖励装饰品ID" json:"rewardDecorationId"`
	Hidden             bool                 `gorm:"not null;default:false;comment:未解锁前隐藏" json:"hidden"`
	Sort               int                  `gorm:"not null;default:0;comment:排序" json:"sort"`
	Enabled            bool                 `gorm:"not null;default:true;comment:是否启用" json:"enabled"`
}

// TableName 表名。
func (Achievement) TableName() string { return "achievements" }

// AchievementCondition 成就完成条件（JSON 列）。
// 原仓库只约定 {type, target}，额外键保留在 Extra 中以实现前向兼容。
type AchievementCondition struct {
	Type   string                 `json:"type"`
	Target int                    `json:"target,omitempty"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
}

// TargetValue 返回目标值，未配置时按 1 处理（对齐原 `condition.target || 1`）。
func (c AchievementCondition) TargetValue() int {
	if c.Target <= 0 {
		return 1
	}
	return c.Target
}

// UserAchievement 用户成就进度。
type UserAchievement struct {
	BaseModel
	UserID        string     `gorm:"type:char(36);index:idx_user_achievement,priority:1;comment:用户ID" json:"userId"`
	AchievementID string     `gorm:"type:char(36);index:idx_user_achievement,priority:2;comment:成就ID" json:"achievementId"`
	Progress      int        `gorm:"not null;default:0;comment:当前进度" json:"progress"`
	Completed     bool       `gorm:"not null;default:false;comment:是否完成" json:"completed"`
	CompletedAt   *time.Time `json:"completedAt"`
	Claimed       bool       `gorm:"not null;default:false;comment:是否已领取" json:"claimed"`
	ClaimedAt     *time.Time `json:"claimedAt"`
}

// TableName 表名。
func (UserAchievement) TableName() string { return "user_achievements" }

// AchievementView 成就 + 当前用户进度（列表/详情响应体）。
type AchievementView struct {
	Achievement
	Progress    int        `json:"progress"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Claimed     bool       `json:"claimed"`
	ClaimedAt   *time.Time `json:"claimedAt,omitempty"`
}

// AchievementInput 成就创建/更新入参。
type AchievementInput struct {
	Code               *string               `json:"code"`
	Name               *string               `json:"name"`
	Description        *string               `json:"description"`
	Icon               *string               `json:"icon"`
	Type               *string               `json:"type"`
	Rarity             *string               `json:"rarity"`
	Condition          *AchievementCondition `json:"condition"`
	RewardPoints       *int                  `json:"rewardPoints"`
	RewardExp          *int                  `json:"rewardExp"`
	RewardDecorationID *string               `json:"rewardDecorationId"`
	Hidden             *bool                 `json:"hidden"`
	Sort               *int                  `json:"sort"`
	Enabled            *bool                 `json:"enabled"`
}

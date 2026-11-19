// 积分域实体（对齐原 points 模块三张表：活动/任务记录/交易流水）。
//
// 设计差异（Go 侧）：
//   - 余额独立落 points_accounts 表，而不是写 users.points —— users 表归认证域，
//     跨域读写同一列会形成隐式耦合；全新库无存量数据，独立账户表更干净。
//   - code 等「业务唯一」字段用普通索引 + 应用层校验，不用 uniqueIndex：
//     BaseModel 带软删，唯一索引与软删共存会导致删除后无法复用同一 code。
package model

import "time"

// 积分活动类型（对齐原 enum）。
const (
	PointsActivityInstant = "INSTANT"
	PointsActivityDaily   = "DAILY"
	PointsActivityWeekly  = "WEEKLY"
	PointsActivityMonthly = "MONTHLY"
	PointsActivityOnce    = "ONCE"
)

// 积分交易类型（对齐原 enum）。
const (
	PointsTxEarn        = "EARN"
	PointsTxSpend       = "SPEND"
	PointsTxAdminAdjust = "ADMIN_ADJUST"
	PointsTxExpire      = "EXPIRE"
	PointsTxRefund      = "REFUND"
)

// PointsAccount 用户积分账户（余额权威来源）。
// 并发安全：所有变动都在事务内先对该行加行锁，或走带条件的原子 UPDATE。
type PointsAccount struct {
	BaseModel
	UserID  string `gorm:"type:char(36);uniqueIndex;comment:用户ID（一人一账户）" json:"userId"`
	Balance int    `gorm:"not null;default:0;comment:当前积分余额" json:"balance"`
	// TotalEarned / TotalSpent 累计口径，统计接口直接读，免去全表 SUM。
	TotalEarned int64 `gorm:"not null;default:0;comment:累计获取" json:"totalEarned"`
	TotalSpent  int64 `gorm:"not null;default:0;comment:累计消费" json:"totalSpent"`
}

// TableName 表名。
func (PointsAccount) TableName() string { return "points_accounts" }

// PointsActivity 积分活动（合并原「规则」与「任务」两个概念）。
type PointsActivity struct {
	BaseModel
	Code         string `gorm:"type:varchar(64);index;comment:活动代码" json:"code"`
	Name         string `gorm:"type:varchar(128);not null;comment:活动名称" json:"name"`
	Description  string `gorm:"type:text;comment:活动描述" json:"description"`
	Type         string `gorm:"type:varchar(20);default:INSTANT;comment:活动类型" json:"type"`
	RewardPoints int    `gorm:"not null;default:0;comment:奖励积分" json:"rewardPoints"`
	TargetCount  int    `gorm:"not null;default:1;comment:目标数量" json:"targetCount"`
	DailyLimit   int    `gorm:"not null;default:0;comment:每日限制次数（0 不限）" json:"dailyLimit"`
	TotalLimit   int    `gorm:"not null;default:0;comment:总限制次数（0 不限）" json:"totalLimit"`
	ValidDays    int    `gorm:"not null;default:0;comment:积分有效期天数（0 永久）" json:"validDays"`
	Icon         string `gorm:"type:varchar(512);comment:活动图标" json:"icon"`
	Link         string `gorm:"type:varchar(512);comment:跳转链接" json:"link"`
	IsActive     bool   `gorm:"not null;default:true;comment:是否启用" json:"isActive"`
	Sort         int    `gorm:"not null;default:0;comment:排序" json:"sort"`
}

// TableName 表名。
func (PointsActivity) TableName() string { return "points_activities" }

// PointsTaskRecord 任务完成记录（周期性任务用；INSTANT 类型不落此表）。
type PointsTaskRecord struct {
	BaseModel
	UserID       string     `gorm:"type:char(36);index:idx_points_task_user_activity,priority:1;comment:用户ID" json:"userId"`
	ActivityID   string     `gorm:"type:char(36);index:idx_points_task_user_activity,priority:2;comment:活动ID" json:"activityId"`
	CurrentCount int        `gorm:"not null;default:0;comment:当前完成数量" json:"currentCount"`
	TargetCount  int        `gorm:"not null;default:1;comment:目标数量" json:"targetCount"`
	IsCompleted  bool       `gorm:"not null;default:false;comment:是否完成" json:"isCompleted"`
	CompletedAt  *time.Time `json:"completedAt"`
	RewardPoints int        `gorm:"not null;default:0;comment:奖励积分" json:"rewardPoints"`
	IsRewarded   bool       `gorm:"not null;default:false;comment:是否已领取奖励" json:"isRewarded"`
	RewardedAt   *time.Time `json:"rewardedAt"`
}

// TableName 表名。
func (PointsTaskRecord) TableName() string { return "points_task_records" }

// PointsTransaction 积分流水（不可变账本，只追加）。
type PointsTransaction struct {
	BaseModel
	UserID      string     `gorm:"type:char(36);index:idx_points_tx_user,priority:1;comment:用户ID" json:"userId"`
	Amount      int        `gorm:"not null;comment:变动数量（正增负减）" json:"amount"`
	Balance     int        `gorm:"not null;comment:变动后余额" json:"balance"`
	Type        string     `gorm:"type:varchar(20);index:idx_points_tx_user,priority:2;comment:交易类型" json:"type"`
	Source      string     `gorm:"type:varchar(64);index;comment:来源/用途" json:"source"`
	RelatedType string     `gorm:"type:varchar(32);comment:关联业务类型" json:"relatedType"`
	RelatedID   string     `gorm:"type:char(36);comment:关联业务ID" json:"relatedId"`
	Description string     `gorm:"type:text;comment:描述" json:"description"`
	ExpiredAt   *time.Time `json:"expiredAt"`
	// Expired 过期积分是否已被回收（EXPIRE 清扫用）。
	Expired bool `gorm:"not null;default:false;comment:是否已过期回收" json:"expired"`
}

// TableName 表名。
func (PointsTransaction) TableName() string { return "points_transactions" }

// PointsTxQuery 积分流水查询条件（用户侧与管理员侧共用）。
type PointsTxQuery struct {
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
	Type   string `form:"type"`
	Source string `form:"source"`
	UserID string `form:"userId"`
}

// PointsActivityInput 活动创建/更新入参（PATCH 时指针字段表示「未传即不改」）。
type PointsActivityInput struct {
	Code         *string `json:"code"`
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Type         *string `json:"type"`
	RewardPoints *int    `json:"rewardPoints"`
	TargetCount  *int    `json:"targetCount"`
	DailyLimit   *int    `json:"dailyLimit"`
	TotalLimit   *int    `json:"totalLimit"`
	ValidDays    *int    `json:"validDays"`
	Icon         *string `json:"icon"`
	Link         *string `json:"link"`
	IsActive     *bool   `json:"isActive"`
	Sort         *int    `json:"sort"`
}

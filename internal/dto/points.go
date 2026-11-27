// 积分域（运营域 points 子域）对外契约：活动 / 流水 / 余额 / 统计 / 任务 / 变动结果。
//
// 字段来源严格取自 internal/model/points.go（四张表）与
// internal/service/points_service.go（PointsChangeResult / PointsStatistics），
// **不新增 model 里不存在的字段** —— 文档声明了前端拿不到的字段，比缺字段更糟。
// 任务书里提到的 activity.state/startAt/endAt 在原 NestJS 实体与本项目 model 中都不存在；
// 等价语义由 isActive（是否启用）与 validDays（积分有效期天数）表达，见 PointsActivityDto。
//
// 空值口径（docs/21，硬规则）：
//   - 字符串 → 值类型，空时 ""（不是 null）
//   - 数组   → 非 nil 切片，空时 []（nil 切片会被 json.Marshal 输出 null）
//   - 时间 / 嵌套对象 → *T + omitempty，空时整个字段不传
//
// 复用说明（任务书特别要求核对 dto/user.go）：
// dto/user.go 已有 WalletTransactionDto（钱包流水，wallet_transaction 表）。积分流水
// （points_transactions 表）与它**字段集不同**，因此**没有复用**，理由见
// PointsTransactionDto 的注释。
package dto

// ---------------------------------------------------------------------------
// 积分活动
// ---------------------------------------------------------------------------

// PointsActivityDto 积分活动（model.PointsActivity 的全部对外字段，17 个）。
//
// 活动在数据模型上合并了原仓库「规则」与「任务」两个概念：INSTANT 类活动是一次性奖励，
// DAILY/WEEKLY/MONTHLY 类活动按 targetCount 累计完成。
//
// 注意：本项目**没有** activity.status / startAt / endAt 字段（原 NestJS 实体也没有）。
// 「上线/下线」= isActive；「有效期」= validDays（0 表示永久）。不在此处虚构字段。
type PointsActivityDto struct {
	ID        string `json:"id" example:"5f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	Code        string `json:"code" example:"DAILY_LOGIN"`
	Name        string `json:"name" example:"每日登录"`
	Description string `json:"description" example:"每天首次登录获得积分"`

	// 活动类型，取值见 model.PointsActivity* 常量。
	Type string `json:"type" enums:"INSTANT,DAILY,WEEKLY,MONTHLY,ONCE" example:"DAILY"`

	// RewardPoints 奖励积分（整数，不是金额，无精度问题）。
	RewardPoints int `json:"reward_points" example:"10"`
	TargetCount  int `json:"target_count" example:"1"`
	// DailyLimit / TotalLimit 为 0 表示不限制。
	DailyLimit int `json:"daily_limit" example:"0"`
	TotalLimit int `json:"total_limit" example:"0"`
	// ValidDays 积分有效期天数，0 表示永久。
	ValidDays int `json:"valid_days" example:"0"`

	Icon string `json:"icon" example:"https://cdn.example.com/points/login.png"`
	Link string `json:"link" example:"/points"`
	// IsActive 是否启用（等价于「活动是否上线」）。
	IsActive bool `json:"is_active" example:"true"`
	Sort     int  `json:"sort" example:"1"`
}

// CreatePointsActivityRequest 创建积分活动请求体。
//
// 与 model.PointsActivityInput 一一对应：字段全是指针，语义是「未传即不改」——
// 同一结构同时服务 POST（创建）与 PATCH（局部更新），原仓库用 PartialType 表达同一件事。
// 请求体里的可选字段是 docs/21 §四 允许的例外（这是入参不是出参）。
type CreatePointsActivityRequest struct {
	Code         *string `json:"code" example:"DAILY_LOGIN"`
	Name         *string `json:"name" example:"每日登录"`
	Description  *string `json:"description" example:"每天首次登录获得积分"`
	Type         *string `json:"type" enums:"INSTANT,DAILY,WEEKLY,MONTHLY,ONCE" example:"DAILY"`
	RewardPoints *int    `json:"reward_points" example:"10"`
	TargetCount  *int    `json:"target_count" example:"1"`
	DailyLimit   *int    `json:"daily_limit" example:"0"`
	TotalLimit   *int    `json:"total_limit" example:"0"`
	ValidDays    *int    `json:"valid_days" example:"0"`
	Icon         *string `json:"icon"`
	Link         *string `json:"link"`
	IsActive     *bool   `json:"is_active" example:"true"`
	Sort         *int    `json:"sort" example:"1"`
}

// UpdatePointsActivityRequest 更新积分活动请求体（PATCH）。
//
// 字段与 CreatePointsActivityRequest 完全一致（全可选，未传即不改），故用别名而不是
// 复制一份字段 —— 复制会让「加字段只改一处」的保证失效。
type UpdatePointsActivityRequest = CreatePointsActivityRequest

// PointsActivityDeleteResultDto 删除积分活动的结果（data = {"deleted":true}）。
type PointsActivityDeleteResultDto struct {
	Deleted bool `json:"deleted" example:"true"`
}

// ---------------------------------------------------------------------------
// 积分流水
// ---------------------------------------------------------------------------

// PointsTransactionDto 积分流水（model.PointsTransaction 的对外视图，14 字段）。
//
// 为什么不复用 dto.WalletTransactionDto（任务书要求先核对）：两者是**不同表的不同语义**，
// 字段交集只有 id/userId/amount/description 四个，其余完全不同：
//
//	钱包流水 WalletTransaction     积分流水 PointsTransaction
//	amount        float64 元       amount   int 分（正增负减）
//	balanceBefore/balanceAfter     balance  变动后余额（单字段）
//	orderId / paymentId            source / relatedType / relatedId
//	remark                         type(EARN/SPEND/…)、expiredAt、expired
//
// 强行合并会把两套语义塞进一个结构，前端拿到的字段一半恒为空 —— 那不是复用是污染。
// 除此之外 WalletTransactionDto 属 user 域（/user/wallet/transactions），已冻结。
type PointsTransactionDto struct {
	ID        string `json:"id" example:"7a2b3c4d-5e6f-4a8b-9c0d-112233445566"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	UserID string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`

	// Amount 变动数量（正数增加、负数减少），单位是积分不是金额。
	Amount int `json:"amount" example:"10"`
	// Balance 变动后的积分余额。
	Balance int `json:"balance" example:"320"`

	Type        string `json:"type" enums:"EARN,SPEND,ADMIN_ADJUST,EXPIRE,REFUND" example:"EARN"`
	Source      string `json:"source" example:"DAILY_LOGIN"`
	RelatedType string `json:"related_type" example:"sign_in"`
	RelatedID   string `json:"related_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	// Description 中文业务描述（data 内容，不翻译）。
	Description string `json:"description" example:"每日登录奖励"`

	// ExpiredAt 积分过期时间；无过期语义时不传该字段（不是 null）。
	ExpiredAt *string `json:"expired_at,omitempty" format:"date-time"`

	// Expired 过期积分是否已被回收（EXPIRE 清扫任务用）。
	Expired bool `json:"expired" example:"false"`
}

// ---------------------------------------------------------------------------
// 余额 / 变动结果 / 任务
// ---------------------------------------------------------------------------

// PointsBalanceDto 积分余额（GET /points/balance 与 /points/users/{userId}/balance）。
//
// 不复用 dto.WalletBalanceDto：那个是钱包余额（float64 元），这里是积分（int），
// 且响应多一个 userId 字段（管理端按 userId 查任意用户余额时必需）。
type PointsBalanceDto struct {
	UserID string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Points int    `json:"points" example:"320"`
}

// PointsChangeResultDto 积分变动结果（发放 / 消费 / 领取奖励共用，3 字段）。
type PointsChangeResultDto struct {
	// Amount 本次变动数量（发放为正、消费为负）。
	Amount int `json:"amount" example:"10"`
	// CurrentPoints 变动后的余额。
	CurrentPoints int `json:"current_points" example:"320"`
	// Transaction 本次对应的流水；无流水时不传该字段（不是 null）。
	Transaction *PointsTransactionDto `json:"transaction,omitempty"`
}

// ClaimPointsResultDto 领取积分任务奖励的结果。
//
// 与 PointsChangeResultDto 是**同一个结构**（amount = 本次奖励积分、currentPoints = 领后余额、
// transaction = 对应流水），因此用类型别名而不是复制字段 —— 复制会出现两份定义，
// 将来改一处忘另一处。swag 解析时按被指向的实体类型生成 schema。
type ClaimPointsResultDto = PointsChangeResultDto

// PointsTaskDto 积分任务完成记录（model.PointsTaskRecord 的对外视图，12 字段）。
//
// 用于 GET /points/tasks/my（数组）与 GET /points/tasks/my/{activityId}（单条）。
type PointsTaskDto struct {
	ID        string `json:"id" example:"b1c2d3e4-5f60-4718-8293-0a1b2c3d4e5f"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:00Z"`

	UserID     string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ActivityID string `json:"activity_id" example:"5f1c2d3e-4a5b-6c7d-8e9f-0123456789ab"`

	CurrentCount int  `json:"current_count" example:"2"`
	TargetCount  int  `json:"target_count" example:"3"`
	IsCompleted  bool `json:"is_completed" example:"false"`
	// CompletedAt 完成时间；未完成时不传（不是 null）。
	CompletedAt *string `json:"completed_at,omitempty" format:"date-time"`

	RewardPoints int  `json:"reward_points" example:"10"`
	IsRewarded   bool `json:"is_rewarded" example:"false"`
	// RewardedAt 领取时间；未领取时不传（不是 null）。
	RewardedAt *string `json:"rewarded_at,omitempty" format:"date-time"`
}

// ---------------------------------------------------------------------------
// 统计
// ---------------------------------------------------------------------------

// PointsStatsTransactionsDto 流水计数（按类型）。
type PointsStatsTransactionsDto struct {
	Total  int64 `json:"total" example:"128"`
	Earned int64 `json:"earned" example:"96"`
	Spent  int64 `json:"spent" example:"32"`
}

// PointsStatsPointsDto 积分总量（单位：积分）。
type PointsStatsPointsDto struct {
	TotalEarned   int64 `json:"total_earned" example:"1280"`
	TotalSpent    int64 `json:"total_spent" example:"320"`
	InCirculation int64 `json:"in_circulation" example:"960"`
}

// PointsStatsUsersDto 用户维度计数。
type PointsStatsUsersDto struct {
	ActiveUsers int64 `json:"active_users" example:"42"`
}

// PointsStatsActivitiesDto 活动维度计数。
type PointsStatsActivitiesDto struct {
	Total  int64 `json:"total" example:"12"`
	Active int64 `json:"active" example:"10"`
}

// PointsStatsTasksDto 任务维度计数。
//
// CompletionRate / ClaimRate 是**字符串**（两位小数，如 "9.52"），
// 对齐原仓库 ((a/b)*100).toFixed(2) 的输出；不是数字，改动会破前端契约（P3 已知一致性问题）。
type PointsStatsTasksDto struct {
	TotalRecords   int64  `json:"total_records" example:"64"`
	Completed      int64  `json:"completed" example:"32"`
	Claimed        int64  `json:"claimed" example:"28"`
	CompletionRate string `json:"completion_rate" example:"50.00"`
	ClaimRate      string `json:"claim_rate" example:"87.50"`
}

// PointsSourceStatDto 按来源聚合的积分获取统计。
type PointsSourceStatDto struct {
	Source string `json:"source" example:"DAILY_LOGIN"`
	Count  int64  `json:"count" example:"32"`
	Total  int64  `json:"total" example:"320"`
}

// PointsStatisticsDto 积分系统统计（GET /points/statistics，管理端，6 段 + 来源数组）。
type PointsStatisticsDto struct {
	Transactions PointsStatsTransactionsDto `json:"transactions"`
	Points       PointsStatsPointsDto       `json:"points"`
	Users        PointsStatsUsersDto        `json:"users"`
	Activities   PointsStatsActivitiesDto   `json:"activities"`
	Tasks        PointsStatsTasksDto        `json:"tasks"`

	// BySource 按来源统计的获取明细；空时输出 []（不是 null）。
	BySource []PointsSourceStatDto `json:"by_source"`
}

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// AddPointsRequest 发放积分请求体（POST /points/add，管理端）。
//
// 与 service.AddPointsInput 一一对应（后者是该类型的别名，见 points_service.go），
// 保证「注解里的请求体」与「真正绑定的类型」不会漂移成两份。
type AddPointsRequest struct {
	Amount      int    `json:"amount" binding:"required" example:"100"`
	Source      string `json:"source" binding:"required" example:"ADMIN_ADJUST"`
	Description string `json:"description" example:"活动补发"`
	RelatedType string `json:"related_type" example:"order"`
	RelatedID   string `json:"related_id" example:"ORDER1234567890123456"`
	// ValidDays 积分有效期天数，0 表示永久。
	ValidDays int `json:"valid_days" example:"0"`
}

// SpendPointsRequest 消费积分请求体（POST /points/spend）。
//
// 与 service.SpendPointsInput 一一对应（同为别名关系）；与 AddPointsRequest 的差别
// 只在没有 validDays（消费不产生有效期）。
type SpendPointsRequest struct {
	Amount      int    `json:"amount" binding:"required" example:"50"`
	Source      string `json:"source" binding:"required" example:"EXCHANGE"`
	Description string `json:"description" example:"兑换表情包"`
	RelatedType string `json:"related_type" example:"emoji_pack"`
	RelatedID   string `json:"related_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
}

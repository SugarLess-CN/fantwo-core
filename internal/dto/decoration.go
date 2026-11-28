package dto

// 本文件覆盖 decoration 域（装饰品、用户持有、当前佩戴、活动、活动进度）的全部
// 对外请求与响应结构。
//
// 字段来源（不是想象，逐条可追）：
//   - DecorationDto / DecorationActivityDto / UserActivityProgressDto
//     ← internal/model/decoration.go 的 json tag
//   - UserDecorationDto ← model.UserDecoration + service 组装时补的 decoration 嵌套
//   - CurrentDecorationsDto ← internal/service/decoration_service.go 的 CurrentDecorations
//   - 请求体 ← model.DecorationInput / DecorationActivityInput（指针 = PATCH 语义）
//   - 动作类响应 ← internal/handler/decoration.go 里的 gin.H 字面量键
//
// 空值规则（docs/21-JSON序列化规范.md）：
//   - 字符串 → 值类型 string，空时 ""
//   - 数组   → 去掉 omitempty + 赋值时 make([]T, 0, n)，空时 []
//   - 时间   → *string + omitempty，空时**整个字段不传**
//     （expiresAt / completedAt / rewardedAt 在库里可为 NULL；传 null 或零值都不诚实）
//   - 对象   → *T + omitempty，空时整个字段不传（avatarFrame / commentBubble / decoration）

import "time"

// ---------------------------------------------------------------------------
// 装饰品定义（响应）
// ---------------------------------------------------------------------------

// DecorationDto 装饰品对外视图（model.Decoration，17 个业务字段 + id/时间）。
//
// 用于：GET /decoration（列表项）、GET /decoration/{id}、POST /decoration、
// PATCH /decoration/{id}，以及 UserDecorationDto 里的 decoration 嵌套。
type DecorationDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:05Z"`

	Name          string  `json:"name" example:"金色边框"`
	Type          string  `json:"type" example:"AVATAR_FRAME"` // AVATAR_FRAME / COMMENT_BUBBLE / ACHIEVEMENT_BADGE
	Description   string  `json:"description" example:"限时活动的头像框"`
	ImageURL      string  `json:"image_url" example:"https://cdn.example.com/deco/frame.png"`
	PreviewURL    string  `json:"preview_url" example:"https://cdn.example.com/deco/frame-preview.png"`
	Rarity        string  `json:"rarity" example:"COMMON"`          // COMMON / RARE / EPIC / LEGENDARY
	BubbleColor   string  `json:"bubble_color" example:"#FFD700"`   // 仅评论气泡用
	ObtainMethod  string  `json:"obtain_method" example:"PURCHASE"` // PURCHASE / ACTIVITY / GIFT / ACHIEVEMENT / DEFAULT
	IsPurchasable bool    `json:"is_purchasable" example:"true"`
	Price         float64 `json:"price" example:"9.9"`
	IsPermanent   bool    `json:"is_permanent" example:"false"`
	ValidDays     int     `json:"valid_days" example:"30"`
	Sort          int     `json:"sort" example:"0"`
	Status        string  `json:"status" example:"ACTIVE"` // ACTIVE / INACTIVE

	// 归属来源（反查唯一来源；非来源方向时为空串，不是 null）
	ActivityID    string `json:"activity_id" example:""`
	AchievementID string `json:"achievement_id" example:""`

	// ---- 归属 / 装备状态（前端交接单第三版 §二.5）----
	//
	// GET /decoration（商城列表）与 GET /decoration/{id} 会带上登录用户语境填充；
	// 管理端 CRUD、持有记录里的嵌套定义不计算归属，这两个字段**不传**。
	//
	// 为什么是 *bool + omitempty 而不是 bool：
	//   - nil = 「本次响应没有归属语境」，整个字段不传（docs/21 对象/未计算态）；
	//   - false = **明确未拥有**，必须出现 —— 前端要靠它显示「未解锁」。
	//   直接用 bool + omitempty 会把 false 吃掉，前端永远看不到「未拥有」，
	//   等于装饰页仍然全坏的（这正是本次要修的缺陷）。
	IsOwned *bool `json:"is_owned,omitempty" example:"false"`
	IsUsing *bool `json:"is_using,omitempty" example:"false"`

	// ---- 解锁门槛（同一交接单 §二.5 的「门槛文案」）----
	//
	// ⚠️ 事实核对：model.Decoration **没有** required_likes / required_comments 这两列
	//（`grep -n "RequiredLikes" internal/model/decoration.go` 只命中 DecorationActivity）。
	// 所以这里**不是**装饰品自身的列，而是它的**来源活动**（activity_id 指向的
	// decoration_activities 行）上声明的门槛 —— 对「活动获取」的装饰品这就是真实的
	// 解锁条件，不是编造。非活动来源（购买/赠送/成就）不传该字段（nil）。
	// 若产品需要「每个装饰品独立门槛」，必须给 decorations 表加列，那是另一个决策。
	RequiredLikes    *int `json:"required_likes,omitempty" example:"100"`
	RequiredComments *int `json:"required_comments,omitempty" example:"0"`
}

// ---------------------------------------------------------------------------
// 活动内嵌的引用结构（交接单第三版 §二.4）
// ---------------------------------------------------------------------------

// DecorationArticleBriefDto 活动内嵌的文章概要（5 个字段）。
//
// 为什么不嵌 dto.ArticleDto：那是 38 字段的完整视图，一个活动列表里每条都带一份
// 会让响应体爆掉（交接单明确要求「不要嵌完整 ArticleDto」）。
// 首页「活动」卡片只需要能渲染封面 + 点进文章。
type DecorationArticleBriefDto struct {
	ID      string   `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Title   string   `json:"title" example:"双十一摄影大赛"`
	Cover   string   `json:"cover" example:"https://cdn.example.com/cover.jpg"`
	Images  []string `json:"images"`
	Summary string   `json:"summary" example:"一句话摘要"`
}

// DecorationBriefDto 活动内嵌的装饰品概要（7 个字段）。
//
// 为什么不嵌 dto.DecorationDto：那是 20+ 字段的完整视图，活动卡片只需要图 + 名字。
// 字段名与 DecorationDto 完全一致（同样是 snake_case），前端可以复用同一个渲染函数。
type DecorationBriefDto struct {
	ID          string `json:"id" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	Name        string `json:"name" example:"金色边框"`
	Type        string `json:"type" example:"AVATAR_FRAME"`
	ImageURL    string `json:"image_url" example:"https://cdn.example.com/deco/frame.png"`
	PreviewURL  string `json:"preview_url" example:"https://cdn.example.com/deco/frame-preview.png"`
	Rarity      string `json:"rarity" example:"COMMON"`
	BubbleColor string `json:"bubble_color" example:"#FFD700"`
}

// ---------------------------------------------------------------------------
// 用户持有 / 当前佩戴（响应）
// ---------------------------------------------------------------------------

// UserDecorationDto 用户持有的装饰品对外视图（model.UserDecoration + decoration 嵌套）。
//
// isUsing 即「是否正在佩戴」（库里列名 is_using）。
//
// Decoration 是**对象字段**：service 批量补填（列表接口一次 IN 查询），
// 补不到时整个字段不传（不是在列表里塞 null）。这样前端画头像框不用二次请求。
type UserDecorationDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:05Z"`

	UserID         string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	DecorationID   string `json:"decoration_id" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	ObtainMethod   string `json:"obtain_method" example:"PURCHASE"`
	IsPermanent    bool   `json:"is_permanent" example:"false"`
	IsUsing        bool   `json:"is_using" example:"true"`
	GiftFromUserID string `json:"gift_from_user_id" example:""`
	OrderID        string `json:"order_id" example:""`
	ActivityID     string `json:"activity_id" example:""`
	Remark         string `json:"remark" example:""`

	// 时间：空时整个字段不传（永久持有 / 未领奖时库里是 NULL）
	ExpiresAt *string `json:"expires_at,omitempty" format:"date-time" example:"2026-12-25T10:00:00Z"`

	// 嵌套装饰品定义（service 批量补填；补不到时不传）
	Decoration *DecorationDto `json:"decoration,omitempty"`
}

// CurrentDecorationsDto 当前佩戴（头像框 + 评论气泡）。
//
// 两个字段都是**对象 + omitempty**：没佩戴时整个字段不传，
// 改前是 `*model.UserDecoration` 的 nil，序列化成 `"avatarFrame": null`。
type CurrentDecorationsDto struct {
	AvatarFrame   *UserDecorationDto `json:"avatar_frame,omitempty"`
	CommentBubble *UserDecorationDto `json:"comment_bubble,omitempty"`
}

// ---------------------------------------------------------------------------
// 装饰品活动（响应）
// ---------------------------------------------------------------------------

// DecorationActivityDto 装饰品活动对外视图（model.DecorationActivity，17 业务字段 + id/时间）。
//
// 用于：GET /decoration/activity（列表项）、GET /decoration/activity/{id}、
// POST /decoration/activity、PATCH /decoration/activity/{id}。
type DecorationActivityDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:05Z"`

	Name         string `json:"name" example:"双十一点赞活动"`
	Description  string `json:"description" example:"累计 100 赞得限定头像框"`
	Type         string `json:"type" example:"LIKE"` // LIKE / COMMENT / SHARE / RECHARGE / SIGN_IN / CUSTOM
	DecorationID string `json:"decoration_id" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	ArticleID    string `json:"article_id" example:""`

	RequiredLikes      int     `json:"required_likes" example:"100"`
	RequiredComments   int     `json:"required_comments" example:"0"`
	RequiredShares     int     `json:"required_shares" example:"0"`
	RequiredRecharge   float64 `json:"required_recharge" example:"0"`
	RequiredSignInDays int     `json:"required_sign_in_days" example:"0"`

	IsPermanent bool `json:"is_permanent" example:"false"`
	ValidDays   int  `json:"valid_days" example:"30"`

	// 活动起止时间：库里 NOT NULL（创建时必有值），故用 string 而非 omitempty 指针
	StartTime string `json:"start_time" format:"date-time" example:"2026-11-01T00:00:00Z"`
	EndTime   string `json:"end_time" format:"date-time" example:"2027-11-01T00:00:00Z"`

	Status           string `json:"status" example:"ACTIVE"` // ACTIVE / INACTIVE / ENDED
	ParticipantCount int64  `json:"participant_count" example:"128"`
	CompletedCount   int64  `json:"completed_count" example:"36"`

	// ---- 内嵌引用（前端交接单第三版 §二.4）----
	//
	// 活动卡片要能渲染文章封面并点进文章、要能显示奖励装扮的图，只给 id 前端做不到。
	// 两者都是**对象 + omitempty**：article_id 为空（不关联文章的纯门槛活动）或
	// 关联行已被删除时，整个字段不传；绝不下发 `null`（docs/21）。
	// 组装方式：列表接口对整页做 **IN 批量预取**（2 条额外查询，与页大小无关），
	// 不是逐条查询 —— 见 service.decorationService.activityViews。
	Article    *DecorationArticleBriefDto `json:"article,omitempty"`
	Decoration *DecorationBriefDto        `json:"decoration,omitempty"`
}

// UserActivityProgressDto 用户活动进度对外视图（model.UserActivityProgress，14 业务字段 + id/时间）。
//
// 用于：GET /decoration/activity/progress/my（数组元素）。
type UserActivityProgressDto struct {
	ID        string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-25T10:00:00Z"`
	UpdatedAt string `json:"updated_at" format:"date-time" example:"2026-11-25T10:00:05Z"`

	UserID     string `json:"user_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ActivityID string `json:"activity_id" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`

	CurrentLikes      int     `json:"current_likes" example:"88"`
	CurrentComments   int     `json:"current_comments" example:"12"`
	CurrentShares     int     `json:"current_shares" example:"0"`
	CurrentRecharge   float64 `json:"current_recharge" example:"0"`
	CurrentSignInDays int     `json:"current_sign_in_days" example:"0"`

	IsCompleted bool `json:"is_completed" example:"false"`
	IsRewarded  bool `json:"is_rewarded" example:"false"`

	// 时间：空时整个字段不传（未完成 / 未领奖时库里是 NULL）
	CompletedAt *string `json:"completed_at,omitempty" format:"date-time"`
	RewardedAt  *string `json:"rewarded_at,omitempty" format:"date-time"`
}

// ---------------------------------------------------------------------------
// 请求体
// ---------------------------------------------------------------------------

// DecorationInputRequest 装饰品创建 / 更新入参（model.DecorationInput，14 个可选字段）。
//
// POST /decoration 与 PATCH /decoration/{id} 共用；全指针 = PATCH 语义（不传 = 不改）。
// 请求体的可选字段保留指针是 docs/21 §四 的明确例外（入参不是出参）。
type DecorationInputRequest struct {
	Name          *string  `json:"name" example:"金色边框"`
	Type          *string  `json:"type" example:"AVATAR_FRAME"`
	Description   *string  `json:"description" example:"限时活动的头像框"`
	ImageURL      *string  `json:"image_url" example:"https://cdn.example.com/deco/frame.png"`
	PreviewURL    *string  `json:"preview_url" example:"https://cdn.example.com/deco/frame-preview.png"`
	Rarity        *string  `json:"rarity" example:"COMMON"`
	BubbleColor   *string  `json:"bubble_color" example:"#FFD700"`
	ObtainMethod  *string  `json:"obtain_method" example:"PURCHASE"`
	IsPurchasable *bool    `json:"is_purchasable" example:"true"`
	Price         *float64 `json:"price" example:"9.9"`
	IsPermanent   *bool    `json:"is_permanent" example:"false"`
	ValidDays     *int     `json:"valid_days" example:"30"`
	Sort          *int     `json:"sort" example:"0"`
	Status        *string  `json:"status" example:"ACTIVE"`
}

// DecorationActivityInputRequest 活动创建 / 更新入参（model.DecorationActivityInput，15 个可选字段）。
type DecorationActivityInputRequest struct {
	Name               *string    `json:"name" example:"双十一点赞活动"`
	Description        *string    `json:"description" example:"累计 100 赞得限定头像框"`
	Type               *string    `json:"type" example:"LIKE"`
	DecorationID       *string    `json:"decoration_id" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	ArticleID          *string    `json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	RequiredLikes      *int       `json:"required_likes" example:"100"`
	RequiredComments   *int       `json:"required_comments" example:"0"`
	RequiredShares     *int       `json:"required_shares" example:"0"`
	RequiredRecharge   *float64   `json:"required_recharge" example:"0"`
	RequiredSignInDays *int       `json:"required_sign_in_days" example:"0"`
	IsPermanent        *bool      `json:"is_permanent" example:"false"`
	ValidDays          *int       `json:"valid_days" example:"30"`
	StartTime          *time.Time `json:"start_time" format:"date-time" example:"2026-11-01T00:00:00Z"`
	EndTime            *time.Time `json:"end_time" format:"date-time" example:"2027-11-01T00:00:00Z"`
	Status             *string    `json:"status" example:"ACTIVE"`
}

// PurchaseDecorationRequest 购买装饰品（decorationId 必填）。
type PurchaseDecorationRequest struct {
	DecorationID string `json:"decoration_id" binding:"required" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
}

// GiftDecorationRequest 赠送装饰品（toUserId + decorationId 必填，message 可选）。
type GiftDecorationRequest struct {
	ToUserID     string `json:"to_user_id" binding:"required" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	DecorationID string `json:"decoration_id" binding:"required" example:"9f8c7b6a-5d4e-3f2a-1b0c-0123456789ab"`
	Message      string `json:"message" example:"生日快乐！"`
}

// ---------------------------------------------------------------------------
// 动作类响应
// ---------------------------------------------------------------------------

// CleanExpiredResultDto 清理过期装饰品的结果（管理触发）。
type CleanExpiredResultDto struct {
	Cleaned int64 `json:"cleaned" example:"3"`
}

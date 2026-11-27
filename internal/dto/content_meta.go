package dto

import "time"

// 本文件覆盖「内容元数据 + 运营小域」八个域的对外契约：
//
//	tag        标签（含关注/取消关注）
//	category   分类（含两级树）
//	banner     首页轮播
//	achievement 成就（定义 + 我的进度 + 统计 + 领取）
//	report     举报（提交/处理/统计）
//	invite     邀请码（创建/使用/统计/分成收益）
//	search     搜索管理（状态/同步/清空/热搜）+ 检索结果
//	statistics 站点统计（概览/趋势）
//
// 字段来源（逐条可追，不是想象）：
//
//	响应实体 ← internal/service/tag.go 的 TagView
//	响应实体 ← internal/service/category.go 的 CategoryView
//	响应实体 ← internal/model/{banner,achievement,report,invite}.go 的 json tag
//	响应实体 ← internal/service/{achievement,report,invite}_service.go 的 XxxResult/XxxStats
//	响应实体 ← internal/service/infra/{search_service,statistics_service,search_log}.go
//	请求体   ← internal/service/{tag,category}.go 与 internal/model/{banner,
//	           achievement,report,invite}.go 的 XxxInput
//
// 字段名一律 camelCase（AGENTS「API 字段命名口径」）；空值规则见
// docs/21-JSON序列化规范.md：
//   - 字符串：值类型 string，空时 ""
//   - 数组：无 omitempty，组装层保证 make([]T, 0, n)，空时 []
//   - 可选时间 / 可选嵌套对象：*T + omitempty，空时整个字段不传
//   - 必有时间（createdAt/updatedAt）：time.Time，随 BaseModel 恒有值
//
// 时间字段为什么用 time.Time 而不是 user.go 那种 string：本文件的响应结构
// 直接内嵌或镜像 model 的 BaseModel（CreatedAt/UpdatedAt 恒非零），保持
// time.Time 可以让 service 组装层零改动（不必为每个域加一次 Format），
// 序列化结果仍是 RFC3339 字符串。可空时间一律 *time.Time + omitempty，
// 语义与 docs/21 的「时间空时不传」一致。

// ---------------------------------------------------------------------------
// 标签（tag）
// ---------------------------------------------------------------------------

// TagDto 标签对外视图（service.TagView，13 字段）。
//
// 用于：GET /tag、/tag/recommended、/tag/hot、/tag/followed/list 的列表项，
// 以及 GET /tag/{id}、POST /tag、PATCH /tag/{id} 的单条响应。
type TagDto struct {
	ID   string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name string `json:"name" example:"Vue"`

	// 字符串字段：值类型，空时 ""（原 service.TagView 用 *string + omitempty，
	// 会让前端拿到 undefined；docs/21 §一 要求 ""）
	Description string `json:"description" example:"前端相关"`
	Avatar      string `json:"avatar" example:"https://cdn.example.com/tag.png"`
	Background  string `json:"background"`
	Cover       string `json:"cover" example:"https://cdn.example.com/cover.jpg"`

	Sort         int  `json:"sort" example:"9"`
	ArticleCount int  `json:"article_count" example:"42"`
	FollowCount  int  `json:"follow_count" example:"8"`
	IsFollowed   bool `json:"is_followed" example:"true"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// CreateTagRequest 创建标签（service.CreateTagRequest，6 字段）。
type CreateTagRequest struct {
	Name        string  `json:"name" binding:"required,max=50" example:"Vue"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Sort        *int    `json:"sort" example:"9"`
}

// UpdateTagRequest 更新标签（全字段可选，service.UpdateTagRequest）。
type UpdateTagRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Sort        *int    `json:"sort"`
}

// TagFollowDto 标签关注 / 取消关注结果（service 返回 {followed}）。
type TagFollowDto struct {
	Followed bool `json:"followed" example:"true"`
}

// ---------------------------------------------------------------------------
// 分类（category）
// ---------------------------------------------------------------------------

// CategoryDto 分类对外视图（service.CategoryView 迁移至此，14 字段）。
//
// 空值规则（docs/21，已在 service 层验证过，本次迁移结构不改语义）：
//   - 字符串：值类型，空时 ""（ParentID 空表示「一级分类」）
//   - Children：无 omitempty，组装层已 make([]CategoryDto, 0, n)，空时 []
//   - 时间：createdAt/updatedAt 恒有值
type CategoryDto struct {
	ID          string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Name        string `json:"name" example:"风景摄影"`
	Description string `json:"description"`
	ParentID    string `json:"parent_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Link        string `json:"link"`
	Avatar      string `json:"avatar"`
	Background  string `json:"background"`
	Cover       string `json:"cover"`
	Sort        int    `json:"sort" example:"1"`
	Status      string `json:"status" example:"ENABLED"`

	ArticleCount int `json:"article_count" example:"128"`
	FollowCount  int `json:"follow_count" example:"16"`

	// Children 子分类（tree=true 或详情时填充；叶节点是 [] 不是 null）。
	Children []CategoryDto `json:"children"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// CreateCategoryRequest 创建分类（service.CreateCategoryRequest，9 字段）。
type CreateCategoryRequest struct {
	Name        string  `json:"name" binding:"required,max=50" example:"风景摄影"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Sort        *int    `json:"sort" example:"1"`
	ParentID    *string `json:"parent_id"`
	Link        *string `json:"link" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Status      string  `json:"status" binding:"omitempty,oneof=ENABLED DISABLED" example:"ENABLED"`
}

// UpdateCategoryRequest 更新分类（全字段可选，service.UpdateCategoryRequest）。
type UpdateCategoryRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Sort        *int    `json:"sort"`
	ParentID    *string `json:"parent_id"`
	Link        *string `json:"link" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Status      string  `json:"status" binding:"omitempty,oneof=ENABLED DISABLED"`
}

// ---------------------------------------------------------------------------
// 轮播（banner）
// ---------------------------------------------------------------------------

// BannerDto 轮播位对外视图（model.Banner + BaseModel，10 字段）。
//
// 用于：GET /banners（分页项）、GET /banners/active（数组项）、GET /banners/{id}、
// POST /banners、PATCH /banners/{id}。
type BannerDto struct {
	ID          string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Title       string `json:"title" example:"双十一活动"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url" example:"https://cdn.example.com/banner.png"`
	LinkURL     string `json:"link_url" example:"https://example.com/activity"`
	SortOrder   int    `json:"sort_order" example:"1"`
	Status      string `json:"status" example:"active"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// CreateBannerRequest 创建轮播（model.BannerInput，6 字段；title/imageUrl 必填）。
type CreateBannerRequest struct {
	Title       *string `json:"title" example:"双十一活动"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url" example:"https://cdn.example.com/banner.png"`
	LinkURL     *string `json:"link_url"`
	SortOrder   *int    `json:"sort_order" example:"1"`
	Status      *string `json:"status" example:"active"`
}

// UpdateBannerRequest 更新轮播（与创建同构，model.BannerInput 全指针部分更新）。
type UpdateBannerRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url"`
	LinkURL     *string `json:"link_url"`
	SortOrder   *int    `json:"sort_order"`
	Status      *string `json:"status"`
}

// ---------------------------------------------------------------------------
// 成就（achievement）
// ---------------------------------------------------------------------------

// AchievementConditionDto 成就完成条件（model.AchievementCondition，jsonb 列）。
//
// 原仓库只约定 {type, target}，额外键保留在 extra 里做前向兼容；
// extra 为空时整个字段不传（任意结构透传，属 docs/21 §四 允许的例外）。
type AchievementConditionDto struct {
	Type   string                 `json:"type" example:"ARTICLE_COUNT"`
	Target int                    `json:"target" example:"10"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
}

// AchievementDto 成就定义 + 当前用户进度（model.AchievementView，19 字段）。
//
// 用于：GET /achievement（数组项）、GET /achievement/{id}、
// POST /achievement、PATCH /achievement/{id}。
//
// 进度三件套（progress/completed/claimed）在未登录时取零值 —— 这是
// service.List 的既有行为（userID 为空即不查 user_achievements）。
type AchievementDto struct {
	ID          string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Code        string `json:"code" example:"FIRST_ARTICLE"`
	Name        string `json:"name" example:"初次创作"`
	Description string `json:"description"`
	Icon        string `json:"icon" example:"https://cdn.example.com/badge.png"`
	Type        string `json:"type" example:"ARTICLE"`
	Rarity      string `json:"rarity" example:"COMMON"`

	// Condition 完成条件（必有，jsonb 默认零值 {type:"",target:0}）
	Condition AchievementConditionDto `json:"condition"`

	RewardPoints       int    `json:"reward_points" example:"10"`
	RewardExp          int    `json:"reward_exp" example:"5"`
	RewardDecorationID string `json:"reward_decoration_id"`
	Hidden             bool   `json:"hidden" example:"false"`
	Sort               int    `json:"sort" example:"1"`
	Enabled            bool   `json:"enabled" example:"true"`

	// 当前用户进度（未登录时 progress=0 / completed=false / claimed=false）
	Progress  int  `json:"progress" example:"3"`
	Completed bool `json:"completed" example:"false"`
	Claimed   bool `json:"claimed" example:"false"`
	// 时间：空时不传（未完成无 completedAt，未领取无 claimedAt）
	CompletedAt *time.Time `json:"completed_at,omitempty" format:"date-time"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty" format:"date-time"`

	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// CreateAchievementRequest 创建成就定义（model.AchievementInput，13 字段）。
type CreateAchievementRequest struct {
	Code               *string                  `json:"code" example:"FIRST_ARTICLE"`
	Name               *string                  `json:"name" example:"初次创作"`
	Description        *string                  `json:"description"`
	Icon               *string                  `json:"icon"`
	Type               *string                  `json:"type" example:"ARTICLE"`
	Rarity             *string                  `json:"rarity" example:"COMMON"`
	Condition          *AchievementConditionDto `json:"condition"`
	RewardPoints       *int                     `json:"reward_points" example:"10"`
	RewardExp          *int                     `json:"reward_exp" example:"5"`
	RewardDecorationID *string                  `json:"reward_decoration_id"`
	Hidden             *bool                    `json:"hidden"`
	Sort               *int                     `json:"sort"`
	Enabled            *bool                    `json:"enabled"`
}

// UpdateAchievementRequest 更新成就定义（model.AchievementInput 全指针部分更新）。
type UpdateAchievementRequest struct {
	Code               *string                  `json:"code"`
	Name               *string                  `json:"name"`
	Description        *string                  `json:"description"`
	Icon               *string                  `json:"icon"`
	Type               *string                  `json:"type"`
	Rarity             *string                  `json:"rarity"`
	Condition          *AchievementConditionDto `json:"condition"`
	RewardPoints       *int                     `json:"reward_points"`
	RewardExp          *int                     `json:"reward_exp"`
	RewardDecorationID *string                  `json:"reward_decoration_id"`
	Hidden             *bool                    `json:"hidden"`
	Sort               *int                     `json:"sort"`
	Enabled            *bool                    `json:"enabled"`
}

// AchievementStatsDto 我的成就进度统计（service.AchievementStats，4 字段）。
//
// completionRate 是**字符串**（对齐原 `((a/b)*100).toFixed(2)`，见
// service.formatFloat2），不是数字 —— 前端要按字符串解析。
type AchievementStatsDto struct {
	Total          int64  `json:"total" example:"20"`
	Completed      int64  `json:"completed" example:"3"`
	Claimed        int64  `json:"claimed" example:"1"`
	CompletionRate string `json:"completion_rate" example:"15.00"`
}

// AchievementClaimResultDto 单个成就领取结果（service.ClaimResult，3 字段）。
//
// rewardDecorationId 空时不传（多数成就只给积分/经验）。
type AchievementClaimResultDto struct {
	Points       int    `json:"points" example:"10"`
	Exp          int    `json:"exp" example:"5"`
	DecorationID string `json:"decoration_id,omitempty"`
}

// AchievementClaimAllResultDto 一键领取结果（service.ClaimAllResult，2 字段）。
type AchievementClaimAllResultDto struct {
	Claimed int `json:"claimed" example:"2"`
	Total   int `json:"total" example:"5"`
}

// ---------------------------------------------------------------------------
// 举报（report）
// ---------------------------------------------------------------------------

// ReportDto 举报记录对外视图（model.Report + BaseModel，17 字段）。
//
// 用于：POST /report、GET /report（分页项）、GET /report/{id}、PATCH /report/{id}。
type ReportDto struct {
	ID                string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Type              string `json:"type" example:"article"`
	Reason            string `json:"reason" example:"垃圾广告"`
	Category          string `json:"category" example:"spam"`
	Description       string `json:"description"`
	Status            string `json:"status" example:"PENDING"`
	Result            string `json:"result"`
	Action            string `json:"action" example:"NONE"`
	ReporterID        string `json:"reporter_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ReportedUserID    string `json:"reported_user_id"`
	ReportedArticleID string `json:"reported_article_id"`
	ReportedCommentID string `json:"reported_comment_id"`
	HandlerID         string `json:"handler_id"`

	// 时间：handledAt 未处理时不传
	HandledAt *time.Time `json:"handled_at,omitempty" format:"date-time"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	UpdatedAt time.Time  `json:"updated_at" format:"date-time"`
}

// CreateReportRequest 提交举报（model.ReportCreateInput，7 字段）。
type CreateReportRequest struct {
	Type              string `json:"type" binding:"required" example:"article"`
	Reason            string `json:"reason" binding:"required" example:"垃圾广告"`
	Category          string `json:"category" binding:"required" example:"spam"`
	Description       string `json:"description"`
	ReportedUserID    string `json:"reported_user_id"`
	ReportedArticleID string `json:"reported_article_id"`
	ReportedCommentID string `json:"reported_comment_id"`
}

// UpdateReportRequest 处理举报（model.ReportUpdateInput，3 字段）。
type UpdateReportRequest struct {
	Status string `json:"status" example:"RESOLVED"`
	Result string `json:"result"`
	Action string `json:"action" example:"DELETE_CONTENT"`
}

// GroupCountDto 分组计数（service.GroupCount，2 字段）。
type GroupCountDto struct {
	Key   string `json:"key" example:"article"`
	Count int64  `json:"count" example:"12"`
}

// ReportStatisticsDto 举报统计（service.ReportStatistics，4 字段）。
//
// byType / byCategory 是数组（不是 map），空时必须是 [] —— 组装层
// groupCount 返回 nil 时由 service 侧保证非 nil（见 report_service.go Statistics）。
type ReportStatisticsDto struct {
	Total      int64            `json:"total" example:"36"`
	ByStatus   map[string]int64 `json:"by_status"`
	ByType     []GroupCountDto  `json:"by_type"`
	ByCategory []GroupCountDto  `json:"by_category"`
}

// ---------------------------------------------------------------------------
// 邀请（invite）
// ---------------------------------------------------------------------------

// InviteDto 邀请码对外视图（model.Invite + BaseModel，14 字段）。
//
// 用于：POST /invite、POST /invite/use、GET /invite/my（分页项）、GET /invite/{id}。
type InviteDto struct {
	ID         string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	InviterID  string `json:"inviter_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	InviteeID  string `json:"invitee_id"`
	InviteCode string `json:"invite_code" example:"INVAB12CD34"`
	Status     string `json:"status" example:"PENDING"`
	Type       string `json:"type" example:"GENERAL"`
	// CommissionRate 分成比例（0-1 的小数，不是百分数）
	CommissionRate float64 `json:"commission_rate" example:"0.05"`
	InviteURL      string  `json:"invite_url" example:"http://localhost:3000/register?code=INVAB12CD34"`

	// 时间：usedAt / expiredAt 空时不传
	UsedAt    *time.Time `json:"used_at,omitempty" format:"date-time"`
	ExpiredAt *time.Time `json:"expired_at,omitempty" format:"date-time"`

	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// CreateInviteRequest 生成邀请码（model.InviteCreateInput，4 字段）。
type CreateInviteRequest struct {
	Type           *string    `json:"type" example:"GENERAL"`
	CommissionRate *float64   `json:"commission_rate" example:"0.05"`
	ExpiredAt      *time.Time `json:"expired_at" format:"date-time"`
	Remark         *string    `json:"remark"`
}

// UseInviteRequest 绑定邀请人（model.InviteUseInput，1 字段，必填）。
type UseInviteRequest struct {
	InviteCode string `json:"invite_code" binding:"required" example:"INVAB12CD34"`
}

// InviteCommissionDto 邀请分成记录（model.InviteCommission + BaseModel，16 字段）。
//
// 用于：GET /invite/earnings 的分页项。
type InviteCommissionDto struct {
	ID               string  `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	InviteID         string  `json:"invite_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	InviterID        string  `json:"inviter_id"`
	InviteeID        string  `json:"invitee_id"`
	OrderID          string  `json:"order_id"`
	OrderType        string  `json:"order_type" example:"membership"`
	OrderAmount      float64 `json:"order_amount" example:"99.00"`
	CommissionRate   float64 `json:"commission_rate" example:"0.05"`
	CommissionAmount float64 `json:"commission_amount" example:"4.95"`
	Status           string  `json:"status" example:"PENDING"`

	// 时间：paidAt 未结算时不传
	PaidAt *time.Time `json:"paid_at,omitempty" format:"date-time"`

	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
}

// InviteStatsDto 邀请统计（service.InviteStats，8 字段）。
type InviteStatsDto struct {
	Total           int64   `json:"total" example:"12"`
	Pending         int64   `json:"pending" example:"3"`
	Used            int64   `json:"used" example:"8"`
	Expired         int64   `json:"expired" example:"1"`
	TotalEarnings   float64 `json:"total_earnings" example:"59.40"`
	PendingEarnings float64 `json:"pending_earnings" example:"4.95"`
	PaidEarnings    float64 `json:"paid_earnings" example:"54.45"`
	CommissionCount int64   `json:"commission_count" example:"12"`
}

// ---------------------------------------------------------------------------
// 搜索（search）
// ---------------------------------------------------------------------------

// SearchStatusDto 搜索索引状态（infra.StatusResult，6 字段）。
//
// ES 未配置时 enabled=false + message（不返回错误，对齐原 getStatus 语义）；
// index / message / error 空时不传。
type SearchStatusDto struct {
	Enabled     bool   `json:"enabled" example:"true"`
	Index       string `json:"index,omitempty" example:"articles"`
	IndexExists bool   `json:"index_exists" example:"true"`
	Documents   int64  `json:"documents" example:"1280"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SearchSyncResultDto 全量同步文章到 ES 的结果（handler 组装的 4 字段）。
//
// 注意 data 里还套了一层 {totalSynced}（对齐原实现的 envelope 形状，前端按
// `data.data.totalSynced` 或顶层 `data.totalSynced` 取都行）—— 这是既有契约，
// 本次只做具名不改形状。
type SearchSyncResultDto struct {
	Success     bool               `json:"success" example:"true"`
	Message     string             `json:"message" example:"Synced 1280 articles"`
	Data        SearchSyncCountDto `json:"data"`
	TotalSynced int64              `json:"total_synced" example:"1280"`
}

// SearchSyncCountDto 同步计数（嵌在 SearchSyncResultDto.data 里）。
type SearchSyncCountDto struct {
	TotalSynced int64 `json:"total_synced" example:"1280"`
}

// HotKeywordDto 热搜词（model.HotKeyword，2 字段）。
//
// 注意与 article 域的 HotSearchItemDto 不同：热搜词只有 keyword + count，
// 没有 type（那个是热门标签/分类合成接口专用）。
type HotKeywordDto struct {
	Keyword string `json:"keyword" example:"风景"`
	Count   int64  `json:"count" example:"18"`
}

// HotKeywordsDto 热搜词列表（handler 组装的 3 字段）。
type HotKeywordsDto struct {
	Days  int             `json:"days" example:"7"`
	Total int             `json:"total" example:"10"`
	List  []HotKeywordDto `json:"list"`
}

// SearchArticleItemDto 检索命中的文章摘要（infra.articleRowToAPI，14 字段）。
//
// GET /search 的 data.list 元素。字段比完整 ArticleDto 少（只有列表页需要的
// 那些），刻意不复用 ArticleDto：复用会把详情页专属字段（content 等）声明成
// 运行时并不存在的字段。
type SearchArticleItemDto struct {
	ID            string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Title         string `json:"title" example:"我的第一篇作品"`
	Summary       string `json:"summary"`
	Cover         string `json:"cover" example:"https://cdn.example.com/c.png"`
	Type          string `json:"type" example:"article"`
	Status        string `json:"status" example:"PUBLISHED"`
	AuthorID      string `json:"author_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CategoryID    string `json:"category_id"`
	Views         int    `json:"views" example:"128"`
	Likes         int    `json:"likes" example:"12"`
	FavoriteCount int    `json:"favorite_count" example:"3"`
	CommentCount  int    `json:"comment_count" example:"5"`
	CreatedAt     string `json:"created_at" format:"date-time"`
	UpdatedAt     string `json:"updated_at" format:"date-time"`
}

// ---------------------------------------------------------------------------
// 统计（statistics）
// ---------------------------------------------------------------------------

// StatisticsUsersDto 用户组统计（infra.UsersStat，5 字段）。
type StatisticsUsersDto struct {
	Total         int64 `json:"total" example:"1024"`
	Active        int64 `json:"active" example:"860"`
	Banned        int64 `json:"banned" example:"12"`
	ActiveMembers int64 `json:"active_members" example:"96"`
	TodayNew      int64 `json:"today_new" example:"8"`
}

// StatisticsContentDto 内容组统计（infra.ContentStat，9 字段）。
type StatisticsContentDto struct {
	Articles          int64 `json:"articles" example:"3200"`
	PublishedArticles int64 `json:"published_articles" example:"2800"`
	PendingArticles   int64 `json:"pending_articles" example:"24"`
	Comments          int64 `json:"comments" example:"9800"`
	PublishedComments int64 `json:"published_comments" example:"9500"`
	Collections       int64 `json:"collections" example:"128"`
	PublicCollections int64 `json:"public_collections" example:"96"`
	TodayArticles     int64 `json:"today_articles" example:"12"`
	TodayComments     int64 `json:"today_comments" example:"86"`
}

// StatisticsOrdersDto 订单组统计（infra.OrdersStat，4 字段）。
type StatisticsOrdersDto struct {
	PaidCount        int64   `json:"paid_count" example:"320"`
	PaidRevenue      float64 `json:"paid_revenue" example:"12672.50"`
	TodayPaidCount   int64   `json:"today_paid_count" example:"5"`
	TodayPaidRevenue float64 `json:"today_paid_revenue" example:"396.00"`
}

// StatisticsModerationDto 审核组统计（infra.ModerationStat，3 字段）。
type StatisticsModerationDto struct {
	PendingReports    int64 `json:"pending_reports" example:"7"`
	ProcessingReports int64 `json:"processing_reports" example:"2"`
	PendingArticles   int64 `json:"pending_articles" example:"24"`
}

// StatisticsEngagementDto 互动组统计（infra.EngagementStat，9 字段）。
type StatisticsEngagementDto struct {
	ArticleViews     int64 `json:"article_views" example:"128000"`
	ArticleLikes     int64 `json:"article_likes" example:"8600"`
	ArticleFavorites int64 `json:"article_favorites" example:"2100"`
	ArticleShares    int64 `json:"article_shares" example:"320"`
	ArticleComments  int64 `json:"article_comments" example:"9800"`
	CommentLikes     int64 `json:"comment_likes" example:"1500"`
	CommentReplies   int64 `json:"comment_replies" example:"2400"`
	CollectionViews  int64 `json:"collection_views" example:"5600"`
	CollectionItems  int64 `json:"collection_items" example:"1280"`
}

// StatisticsOverviewDto 站点总览（infra.OverviewResult，5 个分组）。
//
// 用于：GET /statistics/overview。
type StatisticsOverviewDto struct {
	Users      StatisticsUsersDto      `json:"users"`
	Content    StatisticsContentDto    `json:"content"`
	Orders     StatisticsOrdersDto     `json:"orders"`
	Moderation StatisticsModerationDto `json:"moderation"`
	Engagement StatisticsEngagementDto `json:"engagement"`
}

// TrendPointDto 单日趋势点（infra.TrendPoint，6 字段）。
type TrendPointDto struct {
	Date        string  `json:"date" example:"2026-11-25"`
	Users       int64   `json:"users" example:"8"`
	Articles    int64   `json:"articles" example:"12"`
	Comments    int64   `json:"comments" example:"86"`
	PaidOrders  int64   `json:"paid_orders" example:"5"`
	PaidRevenue float64 `json:"paid_revenue" example:"396.00"`
}

// StatisticsTrendsDto 近 N 天趋势（infra.TrendsResult，2 字段）。
//
// series 空时必须是 []（service 按天补零，正常非空）。
type StatisticsTrendsDto struct {
	Days   int             `json:"days" example:"7"`
	Series []TrendPointDto `json:"series"`
}

// SearchSyncRequest 全量同步文章到 ES 的请求体（POST /search/sync/articles）。
//
// 两个字段都可缺省：不传 batch_size 用默认批量，不传 status 同步全部状态。
type SearchSyncRequest struct {
	BatchSize int    `json:"batch_size" example:"100"`
	Status    string `json:"status" example:"PUBLISHED"`
}

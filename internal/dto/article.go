// 文章域（content）对外契约：响应实体 + 请求体 + 动作结果。
//
// 空值口径（docs/21）：字符串一律值类型（空即 ""）、数组成员必须初始化（空即 []）、
// 对象与时间用指针 + omitempty（空即整个字段不传）。**任何字段都不输出 null。**
//
// 复用策略（docs/18 §五）：列表项与详情字段相同，**共用同一个 ArticleDto**，不拆
// ArticleBriefDto —— 列表只是不加载 content（withContent=false，序列化时省略该字段），
// 其余 37 个字段与详情完全一致；单独建一份 Brief 会让两处结构各改一遍、迟早漂移。
package dto

import "time"

// ---------------------------------------------------------------------------
// 嵌套子结构（文章 / 评论共用）
// ---------------------------------------------------------------------------

// AuthorBriefDto 作者简要信息（service.AuthorBrief 的对外形态，内容域与评论域共用）。
type AuthorBriefDto struct {
	ID         string `json:"id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Nickname   string `json:"nickname" example:"小明"`
	Username   string `json:"username" example:"xiaoming"`
	Avatar     string `json:"avatar" example:"https://cdn.example.com/a.png"`
	IsFollowed bool   `json:"is_followed" example:"false"`

	// EquippedDecorations 作者当前佩戴的装饰品，按 type 分组。
	//
	// 空时不传（不是 null、不是空对象）—— 见 docs/21 §一 规则 ③。
	// 对齐原 NestJS processUserDecorations()：Record<decorationType, Decoration>。
	EquippedDecorations map[string]EquippedDecorationDto `json:"equipped_decorations,omitempty"`

	// Certification 已通过的认证详情；未认证时不传。
	Certification *CertificationBriefDto `json:"certification,omitempty"`
}

// EquippedDecorationDto 佩戴中的装饰品（原 transformDecorationToFrontend 的 6 字段）。
type EquippedDecorationDto struct {
	ID          string `json:"id" example:"a1b2c3d4-0000-4e5f-8a9b-000000000001"`
	Name        string `json:"name" example:"星辰边框"`
	Type        string `json:"type" example:"AVATAR_FRAME"`
	ImageURL    string `json:"image_url" example:"https://cdn.example.com/frame.png"`
	Rarity      string `json:"rarity" example:"RARE"`
	BubbleColor string `json:"bubble_color" example:"#FFD700"`
}

// CertificationBriefDto 作者认证简要（挂在用户对象的 certification 上）。
//
// 与 CertificationDto 的区别：只带展示需要的 4 个字段，不含申请材料/审核记录。
type CertificationBriefDto struct {
	Type     string `json:"type" example:"OFFICIAL"`
	RealName string `json:"real_name" example:"张三"`
	Slogan   string `json:"slogan" example:"记录光"`
}

// TagBriefDto 标签简要（service.TagBrief）。
type TagBriefDto struct {
	ID    string `json:"id" example:"1a2b3c4d-0000-4e5f-8a9b-000000000001"`
	Name  string `json:"name" example:"风景"`
	Cover string `json:"cover" example:"https://cdn.example.com/tag.png"`
}

// CategoryBriefDto 分类简要（service.CategoryBrief）。
type CategoryBriefDto struct {
	ID     string `json:"id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	Name   string `json:"name" example:"摄影"`
	Link   string `json:"link" example:"/category/2b3c4d5e"`
	Avatar string `json:"avatar" example:"https://cdn.example.com/cat.png"`
	Status string `json:"status" example:"ACTIVE"`
}

// ---------------------------------------------------------------------------
// 文章响应实体
// ---------------------------------------------------------------------------

// ArticleDto 文章对外视图（service.ArticleView；列表与详情共用）。
//
// 列表（withContent=false）不加载正文 → content 省略；详情必有正文。
// 这里刻意用 omitempty 而不是输出 "content":"" —— 正文属于大字段，「未加载」与
// 「正文为空」是两种事实，省略能保住这个区分，且同样不产生 null。
type ArticleDto struct {
	ID string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`

	Title             string  `json:"title" example:"我的第一篇作品"`
	RequireLogin      bool    `json:"require_login" example:"false"`
	RequireFollow     bool    `json:"require_follow" example:"false"`
	RequirePayment    bool    `json:"require_payment" example:"false"`
	RequireMembership bool    `json:"require_membership" example:"false"`
	ListRequireLogin  bool    `json:"list_require_login" example:"false"`
	ViewPrice         float64 `json:"view_price" example:"0"`

	Type     string   `json:"type" example:"image"`
	Content  string   `json:"content,omitempty"`
	Images   []string `json:"images"`
	VideoURL string   `json:"video_url" example:"https://cdn.example.com/v.mp4"`
	Cover    string   `json:"cover" example:"https://cdn.example.com/cover.jpg"`
	Summary  string   `json:"summary" example:"一句话摘要"`

	Sort  int `json:"sort" example:"0"`
	Views int `json:"views" example:"12"`
	Likes int `json:"likes" example:"3"`

	// ReactionStats 各表情的计数（键固定为 like/love/haha/wow/sad/angry/dislike）。
	//
	// 对齐原 buildEmptyReactionStats()：无表态时是**全 0 对象**，不是 null。
	ReactionStats map[string]int `json:"reaction_stats"`

	// UserReaction 当前用户对该文章的表态；未表态或未登录时为 ""（不是 null）。
	UserReaction  string `json:"user_reaction" example:"like"`
	FavoriteCount int    `json:"favorite_count" example:"1"`
	CommentCount  int    `json:"comment_count" example:"0"`
	Shares        int    `json:"shares" example:"0"`
	DownloadCount int    `json:"download_count" example:"0"`

	// Downloads 下载资源列表（对齐原 article.downloads 一对多）。
	//
	// 空时输出 []（不是 null）—— 见 docs/21 §一 规则 ②。
	Downloads []ArticleDownloadRequest `json:"downloads"`

	IsFeatured        bool       `json:"is_featured" example:"false"`
	FeaturedAt        *time.Time `json:"featured_at,omitempty"`
	IsPinnedOnProfile bool       `json:"is_pinned_on_profile" example:"false"`
	PinnedAt          *time.Time `json:"pinned_at,omitempty"`

	Status string `json:"status" example:"PUBLISHED"`

	AuthorID   string            `json:"author_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Author     *AuthorBriefDto   `json:"author,omitempty"`
	CategoryID string            `json:"category_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	Category   *CategoryBriefDto `json:"category,omitempty"`
	Tags       []TagBriefDto     `json:"tags"`
	ActivityID string            `json:"activity_id" example:""`

	AllowReprint bool `json:"allow_reprint" example:"false"`

	IsLiked     bool `json:"is_liked" example:"false"`
	IsFavorited bool `json:"is_favorited" example:"false"`
	IsDisliked  bool `json:"is_disliked" example:"false"`

	CreatedAt time.Time `json:"created_at" example:"2026-11-25T10:00:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2026-11-25T10:00:00Z"`
}

// BrowseHistoryDto 浏览历史条目（service.BrowseHistoryView）。
//
// article 是文章概要（不含正文）；文章已被删除时整个字段不传。
type BrowseHistoryDto struct {
	ArticleID string      `json:"article_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ViewCount int         `json:"view_count" example:"2"`
	Progress  int         `json:"progress" example:"80"`
	Duration  int         `json:"duration" example:"120"`
	Article   *ArticleDto `json:"article,omitempty"`
	ViewedAt  time.Time   `json:"viewed_at" example:"2026-11-25T10:00:00Z"`
}

// HotSearchItemDto 热搜词条目（service.HotSearch 的列表元素）。
//
// type 取值：search（真实检索日志聚合）/ tag（热门标签）/ category（热门分类）。
type HotSearchItemDto struct {
	Keyword string `json:"keyword" example:"风景"`
	Type    string `json:"type" example:"tag"`
	Count   int64  `json:"count" example:"18"`
}

// ---------------------------------------------------------------------------
// 文章动作结果（各接口 data 的具名结构）
// ---------------------------------------------------------------------------

// CountDto 单值计数（点赞数 / 评论数）。
type CountDto struct {
	Count int64 `json:"count" example:"12"`
}

// ArticleLikeResultDto 点赞 / 表情回复结果。
type ArticleLikeResultDto struct {
	IsLiked      bool   `json:"is_liked" example:"true"`
	ReactionType string `json:"reaction_type" example:"like"`
	Likes        int64  `json:"likes" example:"13"`
}

// ArticleLikeStatusDto 点赞状态（未点赞时 reactionType 为 ""，不是 null）。
type ArticleLikeStatusDto struct {
	IsLiked      bool   `json:"is_liked" example:"false"`
	ReactionType string `json:"reaction_type" example:"like"`
}

// FavoriteResultDto 收藏 / 取消收藏结果。
type FavoriteResultDto struct {
	IsFavorited   bool  `json:"is_favorited" example:"true"`
	FavoriteCount int64 `json:"favorite_count" example:"6"`
}

// FavoriteStatusDto 收藏状态。
type FavoriteStatusDto struct {
	IsFavorited bool `json:"is_favorited" example:"true"`
}

// DislikeResultDto 不喜欢 / 取消不喜欢 / 状态查询（三处同形）。
type DislikeResultDto struct {
	IsDisliked bool `json:"is_disliked" example:"true"`
}

// ShareResultDto 分享上报结果。
type ShareResultDto struct {
	Channel string `json:"channel" example:"link"`
	Shared  bool   `json:"shared" example:"true"`
}

// SetFeaturedResultDto 设置 / 取消精华结果。
type SetFeaturedResultDto struct {
	ID         string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsFeatured bool   `json:"is_featured" example:"true"`
}

// ProfilePinResultDto 设置 / 取消主页置顶结果。
type ProfilePinResultDto struct {
	ID                string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsPinnedOnProfile bool   `json:"is_pinned_on_profile" example:"true"`
}

// DeleteResultDto 删除成功结果（文章 / 评论共用；id 为被删对象 ID）。
type DeleteResultDto struct {
	ID      string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Deleted bool   `json:"deleted" example:"true"`
}

// DeletedFlagDto 单条浏览记录删除结果（无 id，只有标记）。
type DeletedFlagDto struct {
	Deleted bool `json:"deleted" example:"true"`
}

// BatchDeleteResultDto 批量删除结果；deleted 是**条数**（不是布尔）。
type BatchDeleteResultDto struct {
	Deleted int `json:"deleted" example:"3"`
}

// ClearedResultDto 清空结果。
type ClearedResultDto struct {
	Cleared bool `json:"cleared" example:"true"`
}

// BrowseProgressDto 浏览进度上报结果。
type BrowseProgressDto struct {
	ArticleID string `json:"article_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Progress  int    `json:"progress" example:"80"`
	Duration  int    `json:"duration" example:"120"`
}

// BrowseStatsDto 浏览统计（均值来自 SQL AVG，无记录时为 0）。
type BrowseStatsDto struct {
	TotalArticles int64   `json:"total_articles" example:"12"`
	TotalViews    int64   `json:"total_views" example:"37"`
	AvgProgress   float64 `json:"avg_progress" example:"62.5"`
	AvgDuration   float64 `json:"avg_duration" example:"95.3"`
}

// ---------------------------------------------------------------------------
// 文章请求体
//
// 与 service 层的同名结构（article_dto.go）字段一一对应：handler 实际绑定的是
// service 那份（带 binding 校验），这里只为 swag 提供具名 schema。
// 改字段时**两处必须同步**（service.CreateArticleRequest ↔ dto.CreateArticleRequest）。
// ---------------------------------------------------------------------------

// ArticleDownloadRequest 下载资源项（对齐原 DownloadDto）。
type ArticleDownloadRequest struct {
	// Type 下载类型，取值固定 10 个（对齐原 DownloadType 枚举）。
	Type string `json:"type" binding:"required,oneof=baidu onedrive google quark aliyun dropbox direct lanzou mega other" example:"baidu" enums:"baidu,onedrive,google,quark,aliyun,dropbox,direct,lanzou,mega,other"`

	URL string `json:"url" binding:"required" example:"https://pan.baidu.com/s/1ABC123"`

	// Password 网盘分享密码（可空）。
	Password string `json:"password" example:"1234"`

	// ExtractionCode 下载提取码（可空，与 password 是两回事）。
	ExtractionCode string `json:"extraction_code" example:"abcd"`

	// VisibleWithoutPermission 文章设了门槛时该资源是否仍对无权限用户可见。
	VisibleWithoutPermission bool `json:"visible_without_permission" example:"false"`
}

// CreateArticleRequest 创建文章请求体（service.CreateArticleRequest）。
//
// images 兼容「单个字符串 / 字符串数组」两种入参（service.StringList），文档按数组声明。
type CreateArticleRequest struct {
	Title string `json:"title" binding:"required" example:"我的第一篇作品"`

	Content  string   `json:"content"`
	Summary  string   `json:"summary" example:"一句话摘要"`
	Images   []string `json:"images" example:"[\"https://cdn.example.com/1.jpg\"]"`
	VideoURL string   `json:"video_url"`
	Cover    string   `json:"cover"`
	Sort     int      `json:"sort"`

	CategoryID string   `json:"category_id" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	TagNames   []string `json:"tag_names" example:"[\"风景\"]"`
	TagIDs     []string `json:"tag_ids"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED" example:"PUBLISHED"`

	RequireLogin      bool    `json:"require_login"`
	RequireFollow     bool    `json:"require_follow"`
	RequirePayment    bool    `json:"require_payment"`
	RequireMembership bool    `json:"require_membership"`
	ListRequireLogin  bool    `json:"list_require_login"`
	ViewPrice         float64 `json:"view_price" example:"0"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video" example:"image"`

	ActivityID        string                   `json:"activity_id"`
	Downloads         []ArticleDownloadRequest `json:"downloads"`
	IsFeatured        bool                     `json:"is_featured"`
	IsPinnedOnProfile bool                     `json:"is_pinned_on_profile"`
	AllowReprint      bool                     `json:"allow_reprint"`
}

// UpdateArticleRequest 更新文章请求体（全字段可选，语义同 PartialType）。
type UpdateArticleRequest struct {
	Title *string `json:"title" example:"改后的标题"`

	Content  *string  `json:"content"`
	Summary  *string  `json:"summary"`
	Images   []string `json:"images"`
	VideoURL *string  `json:"video_url"`
	Cover    *string  `json:"cover"`
	Sort     *int     `json:"sort"`

	CategoryID *string  `json:"category_id"`
	TagNames   []string `json:"tag_names"`
	TagIDs     []string `json:"tag_ids"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED"`

	RequireLogin      *bool    `json:"require_login"`
	RequireFollow     *bool    `json:"require_follow"`
	RequirePayment    *bool    `json:"require_payment"`
	RequireMembership *bool    `json:"require_membership"`
	ListRequireLogin  *bool    `json:"list_require_login"`
	ViewPrice         *float64 `json:"view_price"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video"`

	ActivityID        *string                  `json:"activity_id"`
	Downloads         []ArticleDownloadRequest `json:"downloads"`
	IsFeatured        *bool                    `json:"is_featured"`
	IsPinnedOnProfile *bool                    `json:"is_pinned_on_profile"`
	AllowReprint      *bool                    `json:"allow_reprint"`
}

// ArticleReactionRequest 点赞 / 表情回复请求体（可空，缺省按 like）。
type ArticleReactionRequest struct {
	ReactionType string `json:"reaction_type" binding:"omitempty,oneof=like love haha wow sad angry dislike" example:"like"`
}

// ArticleDislikeRequest 不喜欢原因请求体（可空）。
type ArticleDislikeRequest struct {
	Reason string `json:"reason" example:"不感兴趣"`
}

// ArticleShareRequest 分享上报请求体（可空，缺省 channel=link）。
type ArticleShareRequest struct {
	Channel string `json:"channel" binding:"omitempty,oneof=link poster wechat moments weibo qq other" example:"link"`
}

// SetFeaturedRequest 设精请求体（必填 isFeatured）。
type SetFeaturedRequest struct {
	IsFeatured bool `json:"is_featured" binding:"required" example:"true"`
}

// SetProfilePinRequest 个人主页置顶请求体（必填 isPinned）。
type SetProfilePinRequest struct {
	IsPinned bool `json:"is_pinned" binding:"required" example:"true"`
}

// BrowseProgressRequest 浏览进度上报请求体（可空）。
type BrowseProgressRequest struct {
	Progress int `json:"progress" binding:"omitempty,gte=0,lte=100" example:"80"`
	Duration int `json:"duration" binding:"omitempty,gte=0" example:"120"`
}

// BatchDeleteBrowseRequest 批量删除浏览记录请求体（1~100 个 ID）。
type BatchDeleteBrowseRequest struct {
	ArticleIDs []string `json:"article_ids" binding:"required,min=1,max=100"`
}

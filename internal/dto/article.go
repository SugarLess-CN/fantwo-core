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
	IsFollowed bool   `json:"isFollowed" example:"false"`
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
	RequireLogin      bool    `json:"requireLogin" example:"false"`
	RequireFollow     bool    `json:"requireFollow" example:"false"`
	RequirePayment    bool    `json:"requirePayment" example:"false"`
	RequireMembership bool    `json:"requireMembership" example:"false"`
	ListRequireLogin  bool    `json:"listRequireLogin" example:"false"`
	ViewPrice         float64 `json:"viewPrice" example:"0"`

	Type     string   `json:"type" example:"image"`
	Content  string   `json:"content,omitempty"`
	Images   []string `json:"images"`
	VideoURL string   `json:"videoUrl" example:"https://cdn.example.com/v.mp4"`
	Cover    string   `json:"cover" example:"https://cdn.example.com/cover.jpg"`
	Summary  string   `json:"summary" example:"一句话摘要"`

	Sort          int `json:"sort" example:"0"`
	Views         int `json:"views" example:"12"`
	Likes         int `json:"likes" example:"3"`
	FavoriteCount int `json:"favoriteCount" example:"1"`
	CommentCount  int `json:"commentCount" example:"0"`
	Shares        int `json:"shares" example:"0"`
	DownloadCount int `json:"downloadCount" example:"0"`

	IsFeatured        bool       `json:"isFeatured" example:"false"`
	FeaturedAt        *time.Time `json:"featuredAt,omitempty"`
	IsPinnedOnProfile bool       `json:"isPinnedOnProfile" example:"false"`
	PinnedAt          *time.Time `json:"pinnedAt,omitempty"`

	Status string `json:"status" example:"PUBLISHED"`

	AuthorID   string            `json:"authorId" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Author     *AuthorBriefDto   `json:"author,omitempty"`
	CategoryID string            `json:"categoryId" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	Category   *CategoryBriefDto `json:"category,omitempty"`
	Tags       []TagBriefDto     `json:"tags"`
	ActivityID string            `json:"activityId" example:""`

	AllowReprint bool `json:"allowReprint" example:"false"`

	IsLiked     bool `json:"isLiked" example:"false"`
	IsFavorited bool `json:"isFavorited" example:"false"`
	IsDisliked  bool `json:"isDisliked" example:"false"`

	CreatedAt time.Time `json:"createdAt" example:"2026-11-25T10:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-11-25T10:00:00Z"`
}

// BrowseHistoryDto 浏览历史条目（service.BrowseHistoryView）。
//
// article 是文章概要（不含正文）；文章已被删除时整个字段不传。
type BrowseHistoryDto struct {
	ArticleID string      `json:"articleId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ViewCount int         `json:"viewCount" example:"2"`
	Progress  int         `json:"progress" example:"80"`
	Duration  int         `json:"duration" example:"120"`
	Article   *ArticleDto `json:"article,omitempty"`
	ViewedAt  time.Time   `json:"viewedAt" example:"2026-11-25T10:00:00Z"`
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
	IsLiked      bool   `json:"isLiked" example:"true"`
	ReactionType string `json:"reactionType" example:"like"`
	Likes        int64  `json:"likes" example:"13"`
}

// ArticleLikeStatusDto 点赞状态（未点赞时 reactionType 为 ""，不是 null）。
type ArticleLikeStatusDto struct {
	IsLiked      bool   `json:"isLiked" example:"false"`
	ReactionType string `json:"reactionType" example:"like"`
}

// FavoriteResultDto 收藏 / 取消收藏结果。
type FavoriteResultDto struct {
	IsFavorited   bool  `json:"isFavorited" example:"true"`
	FavoriteCount int64 `json:"favoriteCount" example:"6"`
}

// FavoriteStatusDto 收藏状态。
type FavoriteStatusDto struct {
	IsFavorited bool `json:"isFavorited" example:"true"`
}

// DislikeResultDto 不喜欢 / 取消不喜欢 / 状态查询（三处同形）。
type DislikeResultDto struct {
	IsDisliked bool `json:"isDisliked" example:"true"`
}

// ShareResultDto 分享上报结果。
type ShareResultDto struct {
	Channel string `json:"channel" example:"link"`
	Shared  bool   `json:"shared" example:"true"`
}

// SetFeaturedResultDto 设置 / 取消精华结果。
type SetFeaturedResultDto struct {
	ID         string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsFeatured bool   `json:"isFeatured" example:"true"`
}

// ProfilePinResultDto 设置 / 取消主页置顶结果。
type ProfilePinResultDto struct {
	ID                string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	IsPinnedOnProfile bool   `json:"isPinnedOnProfile" example:"true"`
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
	ArticleID string `json:"articleId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	Progress  int    `json:"progress" example:"80"`
	Duration  int    `json:"duration" example:"120"`
}

// BrowseStatsDto 浏览统计（均值来自 SQL AVG，无记录时为 0）。
type BrowseStatsDto struct {
	TotalArticles int64   `json:"totalArticles" example:"12"`
	TotalViews    int64   `json:"totalViews" example:"37"`
	AvgProgress   float64 `json:"avgProgress" example:"62.5"`
	AvgDuration   float64 `json:"avgDuration" example:"95.3"`
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
	Name string `json:"name" example:"原图.zip"`
	URL  string `json:"url" example:"https://cdn.example.com/a.zip"`
	Size string `json:"size" example:"12MB"`
	Type string `json:"type" example:"zip"`
	Code string `json:"code" example:"abcd"`
	Note string `json:"note" example:"解压密码见简介"`
}

// CreateArticleRequest 创建文章请求体（service.CreateArticleRequest）。
//
// images 兼容「单个字符串 / 字符串数组」两种入参（service.StringList），文档按数组声明。
type CreateArticleRequest struct {
	Title string `json:"title" binding:"required" example:"我的第一篇作品"`

	Content  string   `json:"content"`
	Summary  string   `json:"summary" example:"一句话摘要"`
	Images   []string `json:"images" example:"[\"https://cdn.example.com/1.jpg\"]"`
	VideoURL string   `json:"videoUrl"`
	Cover    string   `json:"cover"`
	Sort     int      `json:"sort"`

	CategoryID string   `json:"categoryId" example:"2b3c4d5e-0000-4f60-9bac-000000000002"`
	TagNames   []string `json:"tagNames" example:"[\"风景\"]"`
	TagIDs     []string `json:"tagIds"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED" example:"PUBLISHED"`

	RequireLogin      bool    `json:"requireLogin"`
	RequireFollow     bool    `json:"requireFollow"`
	RequirePayment    bool    `json:"requirePayment"`
	RequireMembership bool    `json:"requireMembership"`
	ListRequireLogin  bool    `json:"listRequireLogin"`
	ViewPrice         float64 `json:"viewPrice" example:"0"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video" example:"image"`

	ActivityID        string                   `json:"activityId"`
	Downloads         []ArticleDownloadRequest `json:"downloads"`
	IsFeatured        bool                     `json:"isFeatured"`
	IsPinnedOnProfile bool                     `json:"isPinnedOnProfile"`
	AllowReprint      bool                     `json:"allowReprint"`
}

// UpdateArticleRequest 更新文章请求体（全字段可选，语义同 PartialType）。
type UpdateArticleRequest struct {
	Title *string `json:"title" example:"改后的标题"`

	Content  *string  `json:"content"`
	Summary  *string  `json:"summary"`
	Images   []string `json:"images"`
	VideoURL *string  `json:"videoUrl"`
	Cover    *string  `json:"cover"`
	Sort     *int     `json:"sort"`

	CategoryID *string  `json:"categoryId"`
	TagNames   []string `json:"tagNames"`
	TagIDs     []string `json:"tagIds"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED"`

	RequireLogin      *bool    `json:"requireLogin"`
	RequireFollow     *bool    `json:"requireFollow"`
	RequirePayment    *bool    `json:"requirePayment"`
	RequireMembership *bool    `json:"requireMembership"`
	ListRequireLogin  *bool    `json:"listRequireLogin"`
	ViewPrice         *float64 `json:"viewPrice"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video"`

	ActivityID        *string                  `json:"activityId"`
	Downloads         []ArticleDownloadRequest `json:"downloads"`
	IsFeatured        *bool                    `json:"isFeatured"`
	IsPinnedOnProfile *bool                    `json:"isPinnedOnProfile"`
	AllowReprint      *bool                    `json:"allowReprint"`
}

// ArticleReactionRequest 点赞 / 表情回复请求体（可空，缺省按 like）。
type ArticleReactionRequest struct {
	ReactionType string `json:"reactionType" binding:"omitempty,oneof=like love haha wow sad angry dislike" example:"like"`
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
	IsFeatured bool `json:"isFeatured" binding:"required" example:"true"`
}

// SetProfilePinRequest 个人主页置顶请求体（必填 isPinned）。
type SetProfilePinRequest struct {
	IsPinned bool `json:"isPinned" binding:"required" example:"true"`
}

// BrowseProgressRequest 浏览进度上报请求体（可空）。
type BrowseProgressRequest struct {
	Progress int `json:"progress" binding:"omitempty,gte=0,lte=100" example:"80"`
	Duration int `json:"duration" binding:"omitempty,gte=0" example:"120"`
}

// BatchDeleteBrowseRequest 批量删除浏览记录请求体（1~100 个 ID）。
type BatchDeleteBrowseRequest struct {
	ArticleIDs []string `json:"articleIds" binding:"required,min=1,max=100"`
}

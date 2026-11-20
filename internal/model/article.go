package model

import (
	"time"
)

// 文章状态生命周期（对齐原 TypeORM Article.status enum）。
//
//	DRAFT     草稿（作者可见，未提交）
//	PENDING   待审核（提交后进入内容审核管线，见 internal/service 审核钩子）
//	PUBLISHED 已发布（列表可见）
//	REJECTED  审核驳回（作者可改后重投）
//	ARCHIVED  已下架（作者/管理员下架，详情可读、列表不可见）
//	BANNED    已封禁（违规，管理员操作）
//	DELETED   已删除（软删标记，与软删列区分：此值用于业务语义回收站）
const (
	ArticleStatusDraft     = "DRAFT"
	ArticleStatusPending   = "PENDING"
	ArticleStatusPublished = "PUBLISHED"
	ArticleStatusRejected  = "REJECTED"
	ArticleStatusArchived  = "ARCHIVED"
	ArticleStatusBanned    = "BANNED"
	ArticleStatusDeleted   = "DELETED"
)

// 文章类型（对齐原 enum：image 纯图 / mixed 图文混合 / video 视频）。
const (
	ArticleTypeImage = "image"
	ArticleTypeMixed = "mixed"
	ArticleTypeVideo = "video"
)

// Article 文章实体（表 articles）。
//
// 逻辑外键说明：AuthorID → users.id、CategoryID → categories.id、ActivityID → decoration_activities.id，
// 均由各自业务域维护；本域不声明 GORM 关联/物理外键，避免跨域迁移顺序耦合与循环依赖。
type Article struct {
	BaseModel

	Title string `gorm:"type:varchar(200);not null" json:"title"`

	// 可见性/付费门槛（对齐原 require* 五个开关）
	RequireLogin      bool    `gorm:"not null;default:false" json:"requireLogin"`
	RequireFollow     bool    `gorm:"not null;default:false" json:"requireFollow"`
	RequirePayment    bool    `gorm:"not null;default:false" json:"requirePayment"`
	RequireMembership bool    `gorm:"not null;default:false" json:"requireMembership"`
	ListRequireLogin  bool    `gorm:"not null;default:false;index:idx_articles_list_req_login" json:"listRequireLogin"`
	ViewPrice         float64 `gorm:"type:decimal(10,2);not null;default:0" json:"viewPrice"`

	Type    string  `gorm:"type:varchar(16);not null;default:mixed" json:"type"`
	Content *string `gorm:"type:text" json:"content"`
	// Images 图片列表，落库存 JSON 文本（原 TypeORM text 列存 JSON.stringify 结果）
	Images   *string `gorm:"type:text" json:"images"`
	VideoURL *string `gorm:"type:text" json:"videoUrl"`
	Cover    *string `gorm:"type:text" json:"cover"`
	Summary  *string `gorm:"type:varchar(500)" json:"summary"`

	Sort int `gorm:"not null;default:0" json:"sort"`

	// 计数冗余列（点赞/收藏/评论/分享/阅读），由 service 在同事务内增减
	Views         int `gorm:"not null;default:0" json:"views"`
	Likes         int `gorm:"not null;default:0" json:"likes"`
	FavoriteCount int `gorm:"not null;default:0" json:"favoriteCount"`
	CommentCount  int `gorm:"not null;default:0" json:"commentCount"`
	Shares        int `gorm:"not null;default:0" json:"shares"`

	// 精华（管理端置顶推荐位）
	IsFeatured bool       `gorm:"not null;default:false;index:idx_articles_featured_status,priority:1" json:"isFeatured"`
	FeaturedAt *time.Time `json:"featuredAt"`
	// 个人主页置顶（作者自主置顶）
	IsPinnedOnProfile bool       `gorm:"not null;default:false" json:"isPinnedOnProfile"`
	PinnedAt          *time.Time `json:"pinnedAt"`

	Status string `gorm:"type:varchar(16);not null;default:DRAFT;index:idx_articles_status_created,priority:1" json:"status"`

	AuthorID   *string `gorm:"type:varchar(36);index:idx_articles_author_status,priority:1" json:"authorId"`
	CategoryID *string `gorm:"type:varchar(36);index:idx_articles_category" json:"categoryId"`
	ActivityID *string `gorm:"type:varchar(36)" json:"activityId"`

	AllowReprint bool `gorm:"not null;default:false" json:"allowReprint"`
}

// TableName 表名。
func (Article) TableName() string { return "articles" }

// ArticleTag 文章-标签多对多关联（表 article_tags）；复合主键，不继承 BaseModel。
type ArticleTag struct {
	ArticleID string    `gorm:"type:varchar(36);primaryKey" json:"articleId"`
	TagID     string    `gorm:"type:varchar(36);primaryKey;index:idx_article_tags_tag" json:"tagId"`
	CreatedAt time.Time `json:"createdAt"`
}

// TableName 表名。
func (ArticleTag) TableName() string { return "article_tags" }

// 表情回复类型（对齐原 ArticleLike.reactionType enum）。
const (
	ReactionLike    = "like"
	ReactionLove    = "love"
	ReactionHaha    = "haha"
	ReactionWow     = "wow"
	ReactionSad     = "sad"
	ReactionAngry   = "angry"
	ReactionDislike = "dislike"
)

// ArticleLike 文章点赞/表情回复（表 article_likes）；同一用户对同一文章唯一。
type ArticleLike struct {
	BaseModel
	ArticleID    string `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_likes_user_article,priority:1" json:"articleId"`
	UserID       string `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_likes_user_article,priority:2" json:"userId"`
	ReactionType string `gorm:"type:varchar(16);not null;default:like" json:"reactionType"`
}

// TableName 表名。
func (ArticleLike) TableName() string { return "article_likes" }

// ArticleFavorite 文章收藏（表 article_favorites）；同一用户对同一文章唯一。
// 语义与「合集（collection）」互补：这里是轻量收藏，profile 域的「我的收藏」即读本表。
type ArticleFavorite struct {
	BaseModel
	UserID    string `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_favorites_user_article,priority:1" json:"userId"`
	ArticleID string `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_favorites_user_article,priority:2" json:"articleId"`
}

// TableName 表名。
func (ArticleFavorite) TableName() string { return "article_favorites" }

// ArticleDislike 「不感兴趣」记录（表 article_dislikes），
// 除文章/用户外额外快照 作者/分类/标签，供推荐流按维度过滤（对齐原实现）。
type ArticleDislike struct {
	BaseModel
	UserID     string  `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_dislikes_user_article,priority:1" json:"userId"`
	ArticleID  string  `gorm:"type:varchar(36);not null;uniqueIndex:uk_article_dislikes_user_article,priority:2" json:"articleId"`
	AuthorID   *string `gorm:"type:varchar(36)" json:"authorId"`
	CategoryID *string `gorm:"type:varchar(36)" json:"categoryId"`
	// TagIDs 标签 ID 快照，落库存 JSON 数组文本
	TagIDs *string `gorm:"type:text" json:"tagIds"`
	Reason *string `gorm:"type:varchar(255)" json:"reason"`
}

// TableName 表名。
func (ArticleDislike) TableName() string { return "article_dislikes" }

// BrowseHistory 浏览历史（表 browse_histories）；同一用户对同一文章唯一，重复浏览累加 viewCount。
type BrowseHistory struct {
	BaseModel
	UserID    string `gorm:"type:varchar(36);not null;uniqueIndex:uk_browse_histories_user_article,priority:1;index:idx_browse_histories_user_updated,priority:1" json:"userId"`
	ArticleID string `gorm:"type:varchar(36);not null;uniqueIndex:uk_browse_histories_user_article,priority:2" json:"articleId"`
	ViewCount int    `gorm:"not null;default:1" json:"viewCount"`
	Progress  int    `gorm:"not null;default:0" json:"progress"` // 浏览进度百分比 0-100
	Duration  int    `gorm:"not null;default:0" json:"duration"` // 停留时长（秒）
}

// TableName 表名。
func (BrowseHistory) TableName() string { return "browse_histories" }

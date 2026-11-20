package model

import "time"

// 评论状态（对齐原 Comment.status enum）。
const (
	CommentStatusPublished = "PUBLISHED"
	CommentStatusPending   = "PENDING"
	CommentStatusDraft     = "DRAFT"
	CommentStatusRejected  = "REJECTED"
	CommentStatusDeleted   = "DELETED"
)

// Comment 评论实体（表 comments）。
//
// 树形结构：ParentID 指向直接父评论，RootID 指向楼层根评论（一级评论自身 RootID 为空）。
// 列表按 rootId 聚合，二级及以下统一挂在根评论下（对齐原 rootId 索引设计）。
type Comment struct {
	BaseModel

	Content string  `gorm:"type:text;not null" json:"content"`
	Images  *string `gorm:"type:text" json:"images"` // JSON 数组文本，最多 9 张

	Likes      int `gorm:"not null;default:0" json:"likes"`
	ReplyCount int `gorm:"not null;default:0" json:"replyCount"`
	Floor      int `gorm:"not null;default:0" json:"floor"` // 楼层号，一级评论按文章内递增

	IsPinned bool       `gorm:"not null;default:false" json:"isPinned"`
	PinnedAt *time.Time `json:"pinnedAt"`

	Status string `gorm:"type:varchar(16);not null;default:PUBLISHED;index:idx_comments_root_status_created,priority:2" json:"status"`

	AuthorID  string  `gorm:"type:varchar(36);not null;index:idx_comments_author" json:"authorId"`
	ArticleID string  `gorm:"type:varchar(36);not null;index:idx_comments_article" json:"articleId"`
	ParentID  *string `gorm:"type:varchar(36);index:idx_comments_parent" json:"parentId"`
	RootID    *string `gorm:"type:varchar(36);index:idx_comments_root_status_created,priority:1" json:"rootId"`
}

// TableName 表名。
func (Comment) TableName() string { return "comments" }

// CommentLike 评论点赞（表 comment_likes）；同一用户对同一评论唯一。
type CommentLike struct {
	BaseModel
	CommentID string `gorm:"type:varchar(36);not null;uniqueIndex:uk_comment_likes_user_comment,priority:1" json:"commentId"`
	UserID    string `gorm:"type:varchar(36);not null;uniqueIndex:uk_comment_likes_user_comment,priority:2" json:"userId"`
}

// TableName 表名。
func (CommentLike) TableName() string { return "comment_likes" }

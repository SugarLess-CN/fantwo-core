// 评论域（content）对外契约：响应实体 + 请求体 + 动作结果。
//
// 空值口径同 docs/21：字符串值类型（空即 ""）、数组初始化（空即 []）、
// 对象与时间用指针 + omitempty（空即不传）。**不输出 null。**
package dto

import "time"

// CommentDto 评论对外视图（service.CommentView）。
//
// 一级评论带 replies（二级回复，已组树）；replies 恒为数组，无回复时是 [] 不是 null。
// 递归结构是**故意的** —— 原 NestJS 的评论树也是自引用（CommentDto.replies: CommentDto[]）。
type CommentDto struct {
	ID      string   `json:"id" example:"5c6d7e8f-0000-4a1b-9c2d-000000000003"`
	Content string   `json:"content" example:"拍得真好"`
	Images  []string `json:"images"`

	Likes      int    `json:"likes" example:"2"`
	ReplyCount int    `json:"replyCount" example:"0"`
	Floor      int    `json:"floor" example:"1"`
	IsPinned   bool   `json:"isPinned" example:"false"`
	Status     string `json:"status" example:"PUBLISHED"`

	AuthorID  string          `json:"authorId" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Author    *AuthorBriefDto `json:"author,omitempty"`
	ArticleID string          `json:"articleId" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`

	// 被回复的评论 ID / 楼层根评论 ID；一级评论两者均为 ""。
	ParentID string `json:"parentId" example:""`
	RootID   string `json:"rootId" example:""`

	IsLiked bool `json:"isLiked" example:"false"`

	Replies []CommentDto `json:"replies"`

	CreatedAt time.Time `json:"createdAt" example:"2026-11-25T10:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-11-25T10:00:00Z"`
}

// CommentPinResultDto 设置 / 取消评论置顶结果。
type CommentPinResultDto struct {
	ID       string `json:"id" example:"5c6d7e8f-0000-4a1b-9c2d-000000000003"`
	IsPinned bool   `json:"isPinned" example:"true"`
}

// CommentLikeResultDto 评论点赞结果（切换语义：已赞则取消）。
type CommentLikeResultDto struct {
	IsLiked bool `json:"isLiked" example:"true"`
}

// ---------------------------------------------------------------------------
// 评论请求体
//
// 与 service 层同名结构一一对应（handler 实际绑定 service 那份）；改字段两处同步。
// ---------------------------------------------------------------------------

// CreateCommentRequest 创建评论请求体（service.CreateCommentRequest）。
//
// images 兼容「单个字符串 / 字符串数组」两种入参（service.StringList），文档按数组声明。
type CreateCommentRequest struct {
	Content   string   `json:"content" binding:"required" example:"拍得真好"`
	ArticleID string   `json:"articleId" binding:"required" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ParentID  string   `json:"parentId" example:"5c6d7e8f-0000-4a1b-9c2d-000000000003"`
	Images    []string `json:"images"`
}

// UpdateCommentRequest 更新评论请求体（isPinned 仅文章作者或管理员可设）。
type UpdateCommentRequest struct {
	Content  *string  `json:"content" example:"改后的评论"`
	Images   []string `json:"images"`
	IsPinned *bool    `json:"isPinned" example:"false"`
}

// SetCommentPinRequest 置顶评论请求体（必填 isPinned）。
type SetCommentPinRequest struct {
	IsPinned bool `json:"isPinned" binding:"required" example:"true"`
}

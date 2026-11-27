// Package service 业务逻辑层。
//
// 本文件为内容域公共 DTO 与视图结构：请求侧带 json + binding 校验标签（供 gin 直接绑定），
// 视图侧统一 camelCase JSON（对齐原 NestJS 返回体），时间字段走 RFC3339。
package service

import (
	"encoding/json"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
)

// ---------- 权限点常量（对齐原 @Permissions 装饰器取值） ----------

const (
	PermArticleCreate   = "article:create"
	PermArticleUpdate   = "article:update"
	PermArticleDelete   = "article:delete"
	PermArticleFeatured = "article:featured" // 管理端设精/推荐位
	PermCommentCreate   = "comment:create"
	PermCommentUpdate   = "comment:update"
	PermCommentDelete   = "comment:delete"
	PermCommentManage   = "comment:manage"
	PermTagCreate       = "tag:create"
	PermTagUpdate       = "tag:update"
	PermTagDelete       = "tag:delete"
	PermCategoryCreate  = "category:create"
	PermCategoryUpdate  = "category:update"
	PermCategoryDelete  = "category:delete"
)

// Actor 当前操作者身份（handler 从 JWT claims 注入，service 不接触 HTTP 语义）。
type Actor struct {
	UserID      string
	Roles       []string
	Permissions []string
}

// Anonymous Actor 是否匿名访客。
func (a Actor) Anonymous() bool { return a.UserID == "" }

// Has 是否持有某权限点。
func (a Actor) Has(perm string) bool {
	for _, p := range a.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// HasAny 是否持有任一权限点。
func (a Actor) HasAny(perms ...string) bool {
	for _, p := range perms {
		if a.Has(p) {
			return true
		}
	}
	return false
}

// IsManager 是否管理端身份：持有内容域任一管理级权限点即视为管理员。
// 对齐原 PermissionGuard 不查库、直接读 JWT 权限的语义。
func (a Actor) IsManager() bool {
	return a.HasAny(PermArticleFeatured, PermCommentManage, PermTagUpdate, PermCategoryUpdate, "admin", "super_admin")
}

// ---------- 视图结构（camelCase，对齐原前端契约） ----------
//
// 对外结构统一收归 internal/dto（docs/18 §二）：下列类型都是 dto 的**别名**，
// 因此「swagger 文档里的字段」与「运行期序列化的字段」是同一份定义，不会漂移。
// 空值口径由 dto 保证（docs/21），组装层只负责把 model 的指针/可空值转成值类型。

// AuthorBrief 作者简要信息（由 user 域 resolver 填充；未接入时为空）。
//
// 注意：本结构仍是 user 域 resolver（author_resolver.go）的返回类型，保留 *string
// avatar（model.User.Avatar 是 *string）；对外输出前在组装层转成 dto.AuthorBriefDto。
type AuthorBrief struct {
	ID         string  `json:"id"`
	Nickname   string  `json:"nickname,omitempty"`
	Username   string  `json:"username,omitempty"`
	Avatar     *string `json:"avatar,omitempty"`
	IsFollowed bool    `json:"is_followed"`
}

// authorBriefDto 作者简要 → 对外 DTO（avatar 空指针转 ""，绝不输出 null）。
func authorBriefDto(a AuthorBrief) dto.AuthorBriefDto {
	return dto.AuthorBriefDto{
		ID:         a.ID,
		Nickname:   a.Nickname,
		Username:   a.Username,
		Avatar:     strOrEmpty(a.Avatar),
		IsFollowed: a.IsFollowed,
	}
}

// TagBrief 标签简要（dto.TagBriefDto 的别名）。
type TagBrief = dto.TagBriefDto

// CategoryBrief 分类简要（dto.CategoryBriefDto 的别名）。
type CategoryBrief = dto.CategoryBriefDto

// ArticleView 文章视图（dto.ArticleDto 的别名；列表与详情共用）。
type ArticleView = dto.ArticleDto

// BrowseHistoryView 浏览历史条目视图（dto.BrowseHistoryDto 的别名）。
type BrowseHistoryView = dto.BrowseHistoryDto

// ---------- 文章请求 DTO ----------

// ArticleDownloadRequest 下载资源项（对齐原 DownloadDto，5 字段）。
//
// 用 dto 别名而不是复制定义：字段与对外 JSON 必须物理同一份，
// 否则 service 加字段、dto 没加，会静默偏离（docs/18 §一）。
type ArticleDownloadRequest = dto.ArticleDownloadRequest

// CreateArticleRequest 创建文章。
type CreateArticleRequest struct {
	Title string `json:"title" binding:"required,max=200"`

	Content  *string    `json:"content"`
	Summary  *string    `json:"summary" binding:"omitempty,max=500"`
	Images   StringList `json:"images"`
	VideoURL *string    `json:"video_url"`
	Cover    *string    `json:"cover"`
	Sort     *int       `json:"sort"`

	CategoryID *string  `json:"category_id"`
	TagNames   []string `json:"tag_names"`
	TagIDs     []string `json:"tag_ids"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED"`

	RequireLogin      *bool    `json:"require_login"`
	RequireFollow     *bool    `json:"require_follow"`
	RequirePayment    *bool    `json:"require_payment"`
	RequireMembership *bool    `json:"require_membership"`
	ListRequireLogin  *bool    `json:"list_require_login"`
	ViewPrice         *float64 `json:"view_price" binding:"omitempty,gte=0"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video"`

	ActivityID *string                  `json:"activity_id"`
	Downloads  []ArticleDownloadRequest `json:"downloads"`

	IsFeatured        *bool `json:"is_featured"`
	IsPinnedOnProfile *bool `json:"is_pinned_on_profile"`
	AllowReprint      *bool `json:"allow_reprint"`
}

// UpdateArticleRequest 更新文章（全字段可选，语义同 PartialType）。
type UpdateArticleRequest struct {
	Title *string `json:"title" binding:"omitempty,max=200"`

	Content  *string    `json:"content"`
	Summary  *string    `json:"summary" binding:"omitempty,max=500"`
	Images   StringList `json:"images"`
	VideoURL *string    `json:"video_url"`
	Cover    *string    `json:"cover"`
	Sort     *int       `json:"sort"`

	CategoryID *string  `json:"category_id"`
	TagNames   []string `json:"tag_names"`
	TagIDs     []string `json:"tag_ids"`

	Status string `json:"status" binding:"omitempty,oneof=DRAFT PENDING PUBLISHED ARCHIVED"`

	RequireLogin      *bool    `json:"require_login"`
	RequireFollow     *bool    `json:"require_follow"`
	RequirePayment    *bool    `json:"require_payment"`
	RequireMembership *bool    `json:"require_membership"`
	ListRequireLogin  *bool    `json:"list_require_login"`
	ViewPrice         *float64 `json:"view_price" binding:"omitempty,gte=0"`

	Type string `json:"type" binding:"omitempty,oneof=image mixed video"`

	ActivityID *string                  `json:"activity_id"`
	Downloads  []ArticleDownloadRequest `json:"downloads"`

	IsFeatured        *bool `json:"is_featured"`
	IsPinnedOnProfile *bool `json:"is_pinned_on_profile"`
	AllowReprint      *bool `json:"allow_reprint"`
}

// ArticleReactionRequest 点赞/表情回复。
type ArticleReactionRequest struct {
	ReactionType string `json:"reaction_type" binding:"omitempty,oneof=like love haha wow sad angry dislike"`
}

// ArticleDislikeRequest 不喜欢原因。
type ArticleDislikeRequest struct {
	Reason *string `json:"reason" binding:"omitempty,max=255"`
}

// ArticleShareRequest 分享上报。
type ArticleShareRequest struct {
	Channel string `json:"channel" binding:"omitempty,oneof=link poster wechat moments weibo qq other"`
}

// SetFeaturedRequest 设精。
type SetFeaturedRequest struct {
	IsFeatured *bool `json:"is_featured" binding:"required"`
}

// SetProfilePinRequest 个人主页置顶。
type SetProfilePinRequest struct {
	IsPinned *bool `json:"is_pinned" binding:"required"`
}

// BrowseProgressRequest 浏览进度上报。
type BrowseProgressRequest struct {
	Progress *int `json:"progress" binding:"omitempty,gte=0,lte=100"`
	Duration *int `json:"duration" binding:"omitempty,gte=0"`
}

// BatchDeleteBrowseRequest 批量删除浏览记录。
type BatchDeleteBrowseRequest struct {
	ArticleIDs []string `json:"article_ids" binding:"required,min=1,max=100"`
}

// StringList 兼容「单个字符串 / 字符串数组」两种入参（对齐原 images 的 oneOf 定义）。
// 落库前统一序列化为 JSON 数组文本。
type StringList []string

// UnmarshalJSON 实现单值/数组双兼容反序列化。
func (s *StringList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = nil
		return nil
	}
	// 数组形式
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	// 单值形式
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		if one == "" {
			*s = nil
		} else {
			*s = []string{one}
		}
		return nil
	}
	return nil
}

// ToJSONText 序列化为 JSON 数组文本；空列表返回 nil（落库为 NULL）。
func (s StringList) ToJSONText() *string {
	if len(s) == 0 {
		return nil
	}
	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil
	}
	out := string(b)
	return &out
}

// ParseImages 把库内 JSON 文本还原为 []string；解析失败则原样返回字符串（兼容历史裸 URL）。
func ParseImages(raw *string) interface{} {
	if raw == nil || *raw == "" {
		return []string{}
	}
	var arr []string
	if err := json.Unmarshal([]byte(*raw), &arr); err == nil {
		return arr
	}
	return *raw
}

// ParseImagesList 把库内 JSON 文本还原为 []string（失败返回空切片）。
func ParseImagesList(raw *string) []string {
	if raw == nil || *raw == "" {
		return []string{}
	}
	var arr []string
	if err := json.Unmarshal([]byte(*raw), &arr); err == nil {
		return arr
	}
	return []string{*raw}
}

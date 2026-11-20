// 举报域实体（对齐原 report 模块 reports 表）。
package model

import "time"

// 举报类型（对齐原 enum）。
const (
	ReportTypeUser    = "USER"
	ReportTypeArticle = "ARTICLE"
	ReportTypeComment = "COMMENT"
)

// 举报分类（对齐原 enum）。
const (
	ReportCategorySpam          = "SPAM"
	ReportCategoryAbuse         = "ABUSE"
	ReportCategoryInappropriate = "INAPPROPRIATE"
	ReportCategoryCopyright     = "COPYRIGHT"
	ReportCategoryOther         = "OTHER"
)

// 举报处理状态（对齐原 enum）。
const (
	ReportStatusPending    = "PENDING"
	ReportStatusProcessing = "PROCESSING"
	ReportStatusResolved   = "RESOLVED"
	ReportStatusRejected   = "REJECTED"
)

// 举报处理动作（对齐原 enum）。
const (
	ReportActionDeleteContent = "DELETE_CONTENT"
	ReportActionBanUser       = "BAN_USER"
	ReportActionWarning       = "WARNING"
	ReportActionNone          = "NONE"
)

// Report 举报记录。
//
// 跨域边界：本表只记录举报事实与处理结论。DELETE_CONTENT / BAN_USER 的
// 实际执行由 content 域（文章/评论）与用户域（封禁）监听事件后完成，
// 运营域不直接写 articles / comments / users 表。
type Report struct {
	BaseModel
	Type              string     `gorm:"type:varchar(20);index;comment:举报类型" json:"type"`
	Reason            string     `gorm:"type:text;not null;comment:举报原因" json:"reason"`
	Category          string     `gorm:"type:varchar(20);index;comment:举报分类" json:"category"`
	Description       string     `gorm:"type:text;comment:详细描述" json:"description"`
	Status            string     `gorm:"type:varchar(20);default:PENDING;index;comment:处理状态" json:"status"`
	Result            string     `gorm:"type:text;comment:处理结果" json:"result"`
	Action            string     `gorm:"type:varchar(20);comment:处理动作" json:"action"`
	ReporterID        string     `gorm:"type:varchar(36);index;comment:举报人ID" json:"reporterId"`
	ReportedUserID    string     `gorm:"type:varchar(36);index;comment:被举报用户ID" json:"reportedUserId"`
	ReportedArticleID string     `gorm:"type:varchar(36);index;comment:被举报文章ID" json:"reportedArticleId"`
	ReportedCommentID string     `gorm:"type:varchar(36);index;comment:被举报评论ID" json:"reportedCommentId"`
	HandlerID         string     `gorm:"type:varchar(36);index;comment:处理人ID" json:"handlerId"`
	HandledAt         *time.Time `json:"handledAt"`
}

// TableName 表名。
func (Report) TableName() string { return "reports" }

// 跨域存在性校验用的轻量视图：只声明主键列，表由对应域 AutoMigrate 创建，
// 本结构不参与 AutoMigrate（与 order 域的 ArticleOrderView 同一做法）。
type ReportTargetUserView struct {
	ID string `gorm:"type:varchar(36);primaryKey;column:id"`
}

// TableName 用户表（用户主体表名是 user，单数；写成 users 会 relation does not exist）。
func (ReportTargetUserView) TableName() string { return "user" }

// ReportTargetArticleView 文章表局部视图。
type ReportTargetArticleView struct {
	ID string `gorm:"type:varchar(36);primaryKey;column:id"`
}

// TableName 文章表。
func (ReportTargetArticleView) TableName() string { return "articles" }

// ReportTargetCommentView 评论表局部视图。
type ReportTargetCommentView struct {
	ID string `gorm:"type:varchar(36);primaryKey;column:id"`
}

// TableName 评论表。
func (ReportTargetCommentView) TableName() string { return "comments" }

// ReportCreateInput 创建举报入参（对齐原 CreateReportDto）。
type ReportCreateInput struct {
	Type              string `json:"type" binding:"required"`
	Reason            string `json:"reason" binding:"required"`
	Category          string `json:"category" binding:"required"`
	Description       string `json:"description"`
	ReportedUserID    string `json:"reportedUserId"`
	ReportedArticleID string `json:"reportedArticleId"`
	ReportedCommentID string `json:"reportedCommentId"`
}

// ReportUpdateInput 更新举报入参（对齐原 UpdateReportDto）。
type ReportUpdateInput struct {
	Status string `json:"status"`
	Result string `json:"result"`
	Action string `json:"action"`
}

// ReportQuery 举报列表查询条件（对齐原 QueryReportDto）。
type ReportQuery struct {
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
	Type       string `form:"type"`
	Status     string `form:"status"`
	Category   string `form:"category"`
	ReporterID string `form:"reporterId"`
	Keyword    string `form:"keyword"`
	SortBy     string `form:"sortBy"`
	SortOrder  string `form:"sortOrder"`
}

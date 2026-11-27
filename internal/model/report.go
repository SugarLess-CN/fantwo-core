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
	Type              string     `gorm:"type:varchar(20);index;comment:举报类型" json:"type" example:"image"`
	Reason            string     `gorm:"type:text;not null;comment:举报原因" json:"reason" example:"违规内容"`
	Category          string     `gorm:"type:varchar(20);index;comment:举报分类" json:"category"`
	Description       string     `gorm:"type:text;comment:详细描述" json:"description" example:"一句话说明"`
	Status            string     `gorm:"type:varchar(20);default:PENDING;index;comment:处理状态" json:"status" example:"PUBLISHED"`
	Result            string     `gorm:"type:text;comment:处理结果" json:"result" example:"内容属违规"`
	Action            string     `gorm:"type:varchar(20);comment:处理动作" json:"action"`
	ReporterID        string     `gorm:"type:varchar(36);index;comment:举报人ID" json:"reporter_id"`
	ReportedUserID    string     `gorm:"type:varchar(36);index;comment:被举报用户ID" json:"reported_user_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReportedArticleID string     `gorm:"type:varchar(36);index;comment:被举报文章ID" json:"reported_article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReportedCommentID string     `gorm:"type:varchar(36);index;comment:被举报评论ID" json:"reported_comment_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	HandlerID         string     `gorm:"type:varchar(36);index;comment:处理人ID" json:"handler_id"`
	HandledAt         *time.Time `json:"handled_at"`
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
	Type              string `json:"type" binding:"required" example:"image"`
	Reason            string `json:"reason" binding:"required" example:"违规内容"`
	Category          string `json:"category" binding:"required"`
	Description       string `json:"description" example:"一句话说明"`
	ReportedUserID    string `json:"reported_user_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReportedArticleID string `json:"reported_article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	ReportedCommentID string `json:"reported_comment_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
}

// ReportUpdateInput 更新举报入参（对齐原 UpdateReportDto）。
type ReportUpdateInput struct {
	Status string `json:"status" example:"PUBLISHED"`
	Result string `json:"result" example:"内容属违规"`
	Action string `json:"action"`
}

// ReportQuery 举报列表查询条件（对齐原 QueryReportDto）。
type ReportQuery struct {
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
	Type       string `form:"type"`
	Status     string `form:"status"`
	Category   string `form:"category"`
	ReporterID string `form:"reporter_id"`
	Keyword    string `form:"keyword"`
	SortBy     string `form:"sort_by"`
	SortOrder  string `form:"sort_order"`
}

// 内容审核域实体：审核记录 + 队列消息去重表（asynq msg_id 幂等）。
package model

import (
	"time"
)

// 审核场景。
const (
	AuditSceneComment = "comment"
	AuditSceneAvatar  = "avatar"
	AuditSceneImage   = "image"
	AuditSceneArticle = "article"
)

// 审核动作。
const (
	AuditActionPass   = "pass"
	AuditActionReview = "review"
	AuditActionBlock  = "block"
)

// 审核来源。
const (
	AuditSourceAuto   = "auto"
	AuditSourceManual = "manual"
)

// AuditRecord 一次审核任务的执行记录（入队→结果回写全链路）。
type AuditRecord struct {
	BaseModel
	// MsgID asynq 任务 ID（msg_id），消费端以此幂等去重。
	MsgID string `gorm:"type:varchar(64);uniqueIndex;comment:asynq 任务ID" json:"msg_id"`
	Scene string `gorm:"type:varchar(20);index;comment:场景 comment/avatar/image/article" json:"scene"`
	// BizType 业务对象类型：article / comment / upload / user_avatar
	BizType string `gorm:"type:varchar(30);index" json:"biz_type"`
	// BizID 业务对象 ID（article id / comment id / upload id）。
	BizID string `gorm:"type:char(36);index;comment:业务对象ID" json:"biz_id"`
	// OperatorID 发起人（自动审核为空）。
	OperatorID string `gorm:"type:char(36)" json:"operator_id"`
	// Content 文本内容（text-audit）。
	Content string `gorm:"type:text" json:"content,omitempty"`
	// ImageURL 图片地址（image-audit）。
	ImageURL string `gorm:"type:varchar(512)" json:"image_url,omitempty"`
	// Provider aliyun / tencent / none
	Provider string `gorm:"type:varchar(20)" json:"provider"`
	// Status PENDING / PASSED / BLOCKED / REVIEW / FAILED
	Status string  `gorm:"type:varchar(20);index" json:"status"`
	Action string  `gorm:"type:varchar(20)" json:"action"`
	Label  string  `gorm:"type:varchar(128)" json:"label"`
	Reason string  `gorm:"type:text" json:"reason"`
	Score  float64 `gorm:"type:decimal(8,4)" json:"score"`
	// ResultDetail provider 原始返回摘要。
	ResultDetail map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"result_detail"`
	AuditedAt    *time.Time             `json:"audited_at"`
}

// TableName 表名。
func (AuditRecord) TableName() string { return "audit_records" }

// QueueMessage 队列消息去重表（审核 handler 幂等：msg_id 已存在则跳过）。
type QueueMessage struct {
	MsgID       string    `gorm:"type:varchar(64);primaryKey;column:msg_id" json:"msg_id"`
	Queue       string    `gorm:"type:varchar(64)" json:"queue"`
	ProcessedAt time.Time `gorm:"autoCreateTime" json:"processed_at"`
}

// TableName 表名。
func (QueueMessage) TableName() string { return "queue_messages" }

// AuditModels 审核域迁移实体（审核记录 + 队列幂等去重表）。
func AuditModels() []interface{} { return []interface{}{&AuditRecord{}, &QueueMessage{}} }

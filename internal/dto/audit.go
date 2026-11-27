package dto

import "time"

// 本文件覆盖 content-audit 域（内容审核）的对外请求与响应结构。
//
// 字段来源（逐条可追，不是想象）：
//   - AuditConfigDto        ← internal/service/audit_service.go 的 AuditConfig
//     （LoadAuditConfig 从 config 表聚合 16 个 key；T-34f 补的 Sensitivity 在其中）
//   - AuditResultDto        ← 同文件的 AuditResult（AuditText / AuditImage 的返回）
//   - PendingArticleItemDto ← 同文件的 PendingArticleItem（ListPendingArticles 扫描目标）
//   - ReviewArticleRequest  ← 同文件的 ReviewArticleInput（handler 直接绑它）
//   - AuditReviewResultDto  ← ReviewArticle 的返回 map 字面量（5 个键）
//   - AuditConfigReloadDto  ← handler/audit.go Reload 的 gin.H{"provider": ...}
//
// ⚠️ 审核状态机语义不许变（任务书要求 B）：
// 审核记录状态是 PENDING / APPROVED / REJECTED（ReviewArticleInput.Result 的 oneof），
// 复审通过后**向业务层发出的目标状态是 PUBLISHED**（article.status.changed 事件的 To 字段），
// 不是 APPROVED。DTO 只声明形状与枚举注释，不改任何取值。
//
// 空值规则（docs/21）：
//   - 字符串 → 值类型（categoryId 在 service 层是 *string，对外转成 ""）
//   - 时间   → reviewedAt 等可空时间用 *string + omitempty（不传），createdAt 必然有值用值类型

// AuditConfigDto 审核配置（GET /content-audit/config）。
//
// 两个 Secret 字段（tencentSecretKey / aliyunAccessKeySecret）在 service 层是 `json:"-"`，
// **永不下发** —— 本 DTO 刻意不声明这两个字段，避免有人误以为它们是响应的一部分。
// 敏感项凭据的读保护由 config 域的打码机制负责（/config 接口）。
//
// Sensitivity 是 T-34f 补的真实字段（原实现有、我们曾漏读），取值 low / medium / high，
// 默认 medium。**字段名与取值都不许改**。
type AuditConfigDto struct {
	// Provider 审核服务商：none（仅本地敏感词）/ aliyun / tencent。
	Provider string `json:"provider" enums:"none,aliyun,tencent" example:"none"`

	// 四类内容的审核开关（false = 该类型不送审，直接放行）。
	CommentEnabled bool `json:"comment_enabled" example:"true"`
	AvatarEnabled  bool `json:"avatar_enabled" example:"true"`
	ImageEnabled   bool `json:"image_enabled" example:"true"`
	ArticleEnabled bool `json:"article_enabled" example:"true"`

	// AutoBlock 命中后的处置：true = 硬拦截（action=block，passed=false）；
	// false = 放行但转人工（action=review，passed=true）。
	AutoBlock bool `json:"auto_block" example:"true"`

	// ReviewMode 审核模式：auto / manual。
	ReviewMode string `json:"review_mode" enums:"auto,manual" example:"auto"`
	// Sensitivity 敏感度：low / medium / high（默认 medium）。
	Sensitivity string `json:"sensitivity" enums:"low,medium,high" example:"medium"`

	// Tencent 腾讯云 CMS（凭据字段不下发，见类型注释）。
	TencentSecretID string `json:"tencent_secret_id" example:""`
	TencentRegion   string `json:"tencent_region" example:"ap-guangzhou"`
	TencentTextBiz  string `json:"tencent_text_biz" example:"text_zh"`
	TencentImageBiz string `json:"tencent_image_biz" example:"image_zh"`

	// Aliyun 阿里云 green（凭据字段不下发，见类型注释）。
	AliyunAccessKeyID string `json:"aliyun_access_key_id" example:""`
	AliyunRegion      string `json:"aliyun_region" example:"cn-shanghai"`
}

// AuditConfigReloadDto 重载审核配置的结果（POST /content-audit/config/reload）。
//
// 每次调用都现读 config 表（等价于重载），回显当前生效的 provider 供运维确认。
type AuditConfigReloadDto struct {
	Provider string `json:"provider" enums:"none,aliyun,tencent" example:"none"`
}

// AuditResultDto 送审结果（POST /content-audit/text、POST /content-audit/image）。
//
// Passed 与 Action 是两个独立维度，前端不要互相推导：
//
//	action=pass   → passed=true   正常放行
//	action=review → passed=true   放行但进人工复审队列（**不是拦截**）
//	action=block  → passed=false  直接拦下
//
// 把 review 当拦截是已知实现陷阱（曾被单测抓到），DTO 注释把它固定下来。
type AuditResultDto struct {
	Passed bool `json:"passed" example:"true"`
	// Action 处置动作，取值见上方说明。
	Action string `json:"action" enums:"pass,review,block" example:"pass"`
	// Label 命中的标签（如 sensitive_word）；无命中时为空串。
	Label string `json:"label" example:"sensitive_word"`
	// Reason 命中原因（**中文业务数据**，不是 message，按 AGENTS 不翻译）。
	Reason string `json:"reason" example:"命中本地敏感词：违禁词"`
	// Score 云服务商返回的风险分（0~100）；本地敏感词命中时为 0。
	Score float64 `json:"score" example:"0"`
	// DetailJSON 云服务商原始响应（jsonb 文本透传）；无则空串。
	DetailJSON string `json:"detail_json" example:""`
}

// PendingArticleItemDto 待人工复审的文章条目（GET /content-audit/pending-articles 的 list 元素）。
//
// CategoryID 在 service 层是 *string（文章可无分类，DB 是 NULL），对外必须是 "" ——
// 这是本域 null 的主要来源（曾返回 "categoryId": null）。
type PendingArticleItemDto struct {
	ID         string `json:"id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Title      string `json:"title" example:"我的第一篇画作"`
	AuthorID   string `json:"author_id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	CategoryID string `json:"category_id" example:""`
	// Status 文章当前状态；本接口只返回 PENDING 的文章。
	Status    string    `json:"status" enums:"PENDING" example:"PENDING"`
	CreatedAt time.Time `json:"created_at" example:"2026-11-25T10:00:00Z"`
}

// ReviewArticleRequest 人工复审请求体（POST /content-audit/articles/{id}/review）。
//
// Result 只接受 APPROVED / REJECTED 两个值（binding oneof，硬校验）：
//   - APPROVED → 向 content 域发 article.status.changed，To=PUBLISHED
//   - REJECTED → To=REJECTED
//
// ⚠️ 注意 APPROVED 对应的**业务落库状态是 PUBLISHED**，两者不是同一个词。
type ReviewArticleRequest struct {
	Result string `json:"result" binding:"required,oneof=APPROVED REJECTED" enums:"APPROVED,REJECTED" example:"APPROVED"`
	// Reason 驳回原因（中文业务数据，不翻译）；通过时可为空串。
	Reason string `json:"reason" example:"含违规内容"`
}

// AuditReviewResultDto 人工复审结果（POST /content-audit/articles/{id}/review 的 data）。
type AuditReviewResultDto struct {
	ArticleID  string `json:"article_id" example:"9f1d2e34-5678-4abc-9def-0123456789ab"`
	Result     string `json:"result" enums:"APPROVED,REJECTED" example:"APPROVED"`
	Reason     string `json:"reason" example:"含违规内容"`
	ReviewedBy string `json:"reviewed_by" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
	ReviewedAt string `json:"reviewed_at" format:"date-time" example:"2026-11-25T10:05:00Z"`
}

// AuditTextRequest 文本送审请求体（POST /content-audit/text，调试端点）。
//
// 原实现是 handler 内的匿名 struct，只取 content；此处具名以便 swag 复用。
// 注意入参中**没有** scene 字段：TestAuditText 调用时 scene 传空串，
// 实际场景由调用方（comment/article）在内部链路里指定。
type AuditTextRequest struct {
	Content string `json:"content" binding:"required" example:"这是一段待审核的文本"`
}

// AuditImageRequest 图片送审请求体（POST /content-audit/image，调试端点）。
type AuditImageRequest struct {
	URL string `json:"url" binding:"required" example:"https://cdn.example.com/a.png"`
}

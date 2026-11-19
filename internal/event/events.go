// 事件名约定：跨域事件契约（交易域 → 其它域），全仓库唯一事实来源。
//
// 契约规则：
//   - 事件名小写点分；payload 一律为本文件定义的结构体（JSON 友好）；
//   - 发布方只保证字段填充，订阅方按需取用；新增字段向后兼容；
//   - 文章状态回写：交易域（审核）只发 article.status.changed，
//     由 content 域监听并更新 article.status / 计数，交易域不直接写 articles 表。
package event

// 跨域事件名。
const (
	// EventOrderPaid 订单支付成功（支付回调/余额支付事务提交后发布）。
	EventOrderPaid = "order.paid"
	// EventArticleStatusChanged 文章状态变更（自动审核/人工复审结果回写）。
	EventArticleStatusChanged = "article.status.changed"
	// EventCommentStatusChanged 评论状态变更（自动审核结果回写）。
	EventCommentStatusChanged = "comment.status.changed"
	// EventUserAvatarAuditBlocked 头像审核拦截（通知用户域处理）。
	EventUserAvatarAuditBlocked = "user.avatar.audit_blocked"
	// EventConfigUpdated 配置更新（对齐原 config.updated）。
	EventConfigUpdated = "config.updated"
)

// OrderPaidPayload order.paid 事件载荷。
type OrderPaidPayload struct {
	OrderID   string  `json:"order_id"`
	OrderNo   string  `json:"order_no"`
	UserID    string  `json:"user_id"`    // 买家
	AuthorID  string  `json:"author_id"`  // 卖家/作者（平台单为 "1"）
	Type      string  `json:"type"`       // ARTICLE / MEMBERSHIP / PRODUCT / SERVICE
	Amount    float64 `json:"amount"`
	Method    string  `json:"method"` // ALIPAY / WECHAT / EPAY / BALANCE
	ArticleID string  `json:"article_id,omitempty"`
	// Commission 佣金信息（写入 payment_commissions 流水后随事件带出，成就/通知域直接消费）。
	Commission *CommissionInfo `json:"commission,omitempty"`
}

// CommissionInfo 佣金明细。
type CommissionInfo struct {
	InviterID         string  `json:"inviter_id,omitempty"`
	InviterAmount     float64 `json:"inviter_amount,omitempty"`
	InviterRate       float64 `json:"inviter_rate,omitempty"`
	AuthorAmount      float64 `json:"author_amount,omitempty"`
	AuthorRate        float64 `json:"author_rate,omitempty"`
	PlatformAmount    float64 `json:"platform_amount,omitempty"`
	PlatformRate      float64 `json:"platform_rate,omitempty"`
	CommissionRecordID string `json:"commission_record_id,omitempty"`
}

// ArticleStatusChangedPayload article.status.changed 事件载荷。
// 审核（自动/人工）结果由 content 域消费：更新 article.status 及计数。
type ArticleStatusChangedPayload struct {
	ArticleID string `json:"article_id"`
	// From/To 原/新状态：PENDING / PUBLISHED / REJECTED / FAILED...
	From string `json:"from"`
	To   string `json:"to"`
	// Reason 拦截/驳回原因（自动审核 label 或人工 reason）。
	Reason string `json:"reason,omitempty"`
	// Source 来源：auto（机审）/ manual（人工复审）。
	Source string `json:"source"`
	// OperatorID 人工复审操作员（auto 为空）。
	OperatorID string `json:"operator_id,omitempty"`
}

// CommentStatusChangedPayload comment.status.changed 事件载荷。
type CommentStatusChangedPayload struct {
	CommentID string `json:"comment_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason,omitempty"`
	Source    string `json:"source"` // auto / manual
}

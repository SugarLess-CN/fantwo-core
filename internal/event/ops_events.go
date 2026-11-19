// Package event 运营域事件：本域发布与消费的事件名及载荷。
//
// 约定（同 events.go）：运营域新增事件一律写在本文件，不修改 events.go，
// 避免与其它域并行冲突。订阅使用 internal/event.Bus（同步调用，
// 语义对齐 NestJS EventEmitter2 默认 emit）。
package event

// 运营域事件名（成就触发，对齐原 achievement-event.service.ts 的 @OnEvent 清单）。
const (
	// EventArticleCreated 文章创建（content 域发布，成就域消费）。
	EventArticleCreated = "article.created"
	// EventArticleReceivedLike 文章被点赞（content 域发布）。
	EventArticleReceivedLike = "article.receivedLike"
	// EventCommentCreated 评论创建（content 域发布）。
	EventCommentCreated = "comment.created"
	// EventUserFollowed 用户关注了别人（用户域发布）。
	EventUserFollowed = "user.followed"
	// EventUserReceivedFollow 用户被关注（用户域发布）。
	EventUserReceivedFollow = "user.receivedFollow"
	// EventUserDailyLogin 每日登录（用户域发布）。
	EventUserDailyLogin = "user.dailyLogin"
	// EventUserLevelUp 用户升级（用户域发布）。
	EventUserLevelUp = "user.levelUp"
	// EventMembershipPurchased 会员购买成功（交易域发布）。
	EventMembershipPurchased = "membership.purchased"
	// EventUserProfileCompleted 资料完善（用户域发布）。
	EventUserProfileCompleted = "user.profileCompleted"

	// EventAchievementCompleted 成就完成（成就域发布，通知/装饰品域可消费）。
	EventAchievementCompleted = "achievement.completed"
	// EventUserGainExp 用户获得经验（成就域领取奖励时发布）。
	EventUserGainExp = "user.gainExp"
	// EventDecorationGrant 授予装饰品（成就域领取奖励时发布；装饰品域消费落库）。
	EventDecorationGrant = "decoration.grant"
	// EventInviteUsed 邀请码被使用（邀请域发布，用户域可绑定邀请关系）。
	EventInviteUsed = "invite.used"
	// EventInviteCommissionPaid 邀请分成发放（邀请域发布，钱包/通知域可消费）。
	EventInviteCommissionPaid = "invite.commission.paid"
	// EventReportHandled 举报处理完成（举报域发布；DELETE_CONTENT/BAN_USER 由
	// content 域与用户域监听执行，运营域不直接写 articles/comments/users 表）。
	EventReportHandled = "report.handled"
)

// GenericPayload 通用事件载荷：仅携带领域对象与用户 ID。
// 原仓库多数成就事件 payload 只有 userId + 业务 ID，泛型结构即可覆盖；
// 需要更多字段的事件在本文件追加专用结构体。
type GenericPayload struct {
	UserID string                 `json:"user_id"`
	BizID  string                 `json:"biz_id,omitempty"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
}

// ArticleCreatedPayload article.created 载荷。
type ArticleCreatedPayload struct {
	UserID    string `json:"user_id"`
	ArticleID string `json:"article_id"`
}

// ArticleReceivedLikePayload article.receivedLike 载荷（对齐原字段）。
type ArticleReceivedLikePayload struct {
	AuthorID  string `json:"author_id"`
	ArticleID string `json:"article_id"`
	LikerID   string `json:"liker_id"`
}

// CommentCreatedPayload comment.created 载荷。
type CommentCreatedPayload struct {
	UserID    string `json:"user_id"`
	ArticleID string `json:"article_id"`
	CommentID string `json:"comment_id"`
}

// UserFollowedPayload user.followed 载荷。
type UserFollowedPayload struct {
	UserID       string `json:"user_id"`        // 关注者
	TargetUserID string `json:"target_user_id"` // 被关注者
}

// UserReceivedFollowPayload user.receivedFollow 载荷。
type UserReceivedFollowPayload struct {
	UserID     string `json:"user_id"`     // 被关注者
	FollowerID string `json:"follower_id"` // 关注者
}

// UserDailyLoginPayload user.dailyLogin 载荷。
type UserDailyLoginPayload struct {
	UserID          string `json:"user_id"`
	ConsecutiveDays int    `json:"consecutive_days,omitempty"`
}

// UserLevelUpPayload user.levelUp 载荷。
type UserLevelUpPayload struct {
	UserID string `json:"user_id"`
	Level  int    `json:"level"`
}

// MembershipPurchasedPayload membership.purchased 载荷。
type MembershipPurchasedPayload struct {
	UserID  string `json:"user_id"`
	OrderID string `json:"order_id"`
}

// AchievementCompletedPayload achievement.completed 载荷。
type AchievementCompletedPayload struct {
	UserID          string `json:"user_id"`
	AchievementID   string `json:"achievement_id"`
	AchievementCode string `json:"achievement_code"`
}

// UserGainExpPayload user.gainExp 载荷。
type UserGainExpPayload struct {
	UserID string `json:"user_id"`
	Exp    int    `json:"exp"`
	Reason string `json:"reason,omitempty"`
}

// DecorationGrantPayload decoration.grant 载荷。
type DecorationGrantPayload struct {
	UserID       string `json:"user_id"`
	DecorationID string `json:"decoration_id"`
	ObtainMethod string `json:"obtain_method"`
}

// InviteUsedPayload invite.used 载荷。
type InviteUsedPayload struct {
	InviteID   string `json:"invite_id"`
	InviterID  string `json:"inviter_id"`
	InviteeID  string `json:"invitee_id"`
	InviteCode string `json:"invite_code"`
}

// InviteCommissionPaidPayload invite.commission.paid 载荷。
type InviteCommissionPaidPayload struct {
	CommissionID     string  `json:"commission_id"`
	InviterID        string  `json:"inviter_id"`
	InviteeID        string  `json:"invitee_id"`
	OrderID          string  `json:"order_id"`
	CommissionAmount float64 `json:"commission_amount"`
}

// ReportHandledPayload report.handled 载荷（content/用户域据此执行删除/封禁）。
type ReportHandledPayload struct {
	ReportID          string `json:"report_id"`
	Type              string `json:"type"`   // USER / ARTICLE / COMMENT
	Action            string `json:"action"` // DELETE_CONTENT / BAN_USER / WARNING / NONE
	ReportedUserID    string `json:"reported_user_id,omitempty"`
	ReportedArticleID string `json:"reported_article_id,omitempty"`
	ReportedCommentID string `json:"reported_comment_id,omitempty"`
	HandlerID         string `json:"handler_id"`
}

// 成就触发映射：事件名 → 该事件推进的成就代码列表（对齐原
// achievement-event.service.ts 的 updateProgressBatch 调用清单）。
// 登录/升级类按 payload 条件过滤，不在此表（见 achievement service）。
var AchievementTriggers = map[string][]string{
	EventArticleCreated:       {"FIRST_ARTICLE", "ARTICLE_10", "ARTICLE_50", "ARTICLE_100"},
	EventArticleReceivedLike:  {"FIRST_LIKE", "LIKE_100", "LIKE_1000"},
	EventCommentCreated:       {"FIRST_COMMENT", "COMMENT_100"},
	EventUserFollowed:         {"FIRST_FOLLOW", "FOLLOW_10"},
	EventUserReceivedFollow:   {"FIRST_FOLLOWER", "FOLLOWER_100", "FOLLOWER_1000"},
	EventMembershipPurchased:  {"BECOME_MEMBER"},
	EventUserProfileCompleted: {"PROFILE_COMPLETED"},
}

package i18n

// 文案 key 常量表。
//
// 命名规范：`<domain>.<subject>.<result>`，全小写、点分。
//
// 分区：
//
//	common.*     通用（成功/失败/参数错误等）
//	auth.*       认证（登录/注册/令牌/验证码）
//	user.*       用户资料
//	upload.*     上传与文件
//	article.*    文章
//	comment.*    评论
//	collection.* 合集
//	interaction.*互动（点赞/收藏/关注）
//	points.*     积分与签到
//	order.*      订单与支付
//	decoration.* 装饰品
//	emoji.*      表情包
//	message.*    消息
//	config.*     系统配置
//	audit.*      内容审核
//	search.*     搜索
//	role.*       角色权限
//	report.*     举报
//	invite.*     邀请
//	category.*   分类与标签
//	banner.*     轮播
//	stats.*      统计
const (
	// ---------- common ----------
	CommonOK              Key = "common.ok"
	CommonCreated         Key = "common.created"
	CommonUpdated         Key = "common.updated"
	CommonDeleted         Key = "common.deleted"
	CommonBadRequest      Key = "common.bad_request"
	CommonUnauthorized    Key = "common.unauthorized"
	CommonForbidden       Key = "common.forbidden"
	CommonNotFound        Key = "common.not_found"
	CommonConflict        Key = "common.conflict"
	CommonTooManyRequests Key = "common.too_many_requests"
	CommonInternal        Key = "common.internal_error"
	CommonInvalidParam    Key = "common.invalid_param"
	CommonMissingField    Key = "common.missing_field"
	CommonInvalidID       Key = "common.invalid_id"
	CommonOperationFailed Key = "common.operation_failed"
	CommonAlreadyExists   Key = "common.already_exists"
	CommonNotAllowed      Key = "common.not_allowed"
	CommonExpired         Key = "common.expired"

	// ---------- auth ----------
	AuthLoginSuccess       Key = "auth.login_success"
	AuthLogoutSuccess      Key = "auth.logout_success"
	AuthLogoutAllSuccess   Key = "auth.logout_all_success"
	AuthRegisterSuccess    Key = "auth.register_success"
	AuthBadCredentials     Key = "auth.bad_credentials"
	AuthTokenExpired       Key = "auth.token_expired"
	AuthTokenInvalid       Key = "auth.token_invalid"
	AuthNeedLogin          Key = "auth.need_login"
	AuthAccountBanned      Key = "auth.account_banned"
	AuthAccountDisabled    Key = "auth.account_disabled"
	AuthCaptchaSent        Key = "auth.captcha_sent"
	AuthCaptchaInvalid     Key = "auth.captcha_invalid"
	AuthCaptchaTooFrequent Key = "auth.captcha_too_frequent"
	AuthPasswordReset      Key = "auth.password_reset"
	AuthPasswordChanged    Key = "auth.password_changed"
	AuthPasswordMismatch   Key = "auth.password_mismatch"
	AuthOldPasswordWrong   Key = "auth.old_password_wrong"
	AuthOAuthNotConfigured Key = "auth.oauth_not_configured"
	AuthOAuthFailed        Key = "auth.oauth_failed"
	AuthDeviceLimit        Key = "auth.device_limit"

	// ---------- user ----------
	UserNotFound            Key = "user.not_found"
	UserAlreadyExists       Key = "user.already_exists"
	UserEmailExists         Key = "user.email_exists"
	UserPhoneExists         Key = "user.phone_exists"
	UserUsernameExists      Key = "user.username_exists"
	UserUsernameInvalid     Key = "user.username_invalid"
	UserPasswordTooWeak     Key = "user.password_too_weak"
	UserProfileUpdated      Key = "user.profile_updated"
	UserAvatarUpdated       Key = "user.avatar_updated"
	UserFollowed            Key = "user.followed"
	UserUnfollowed          Key = "user.unfollowed"
	UserCannotFollowSelf    Key = "user.cannot_follow_self"
	UserAlreadyFollowed     Key = "user.already_followed"
	UserNotFollowing        Key = "user.not_following"
	UserCertificationSubmit Key = "user.certification_submit"
	UserCertificationPassed Key = "user.certification_passed"
	UserCertificationReject Key = "user.certification_rejected"

	// ---------- upload ----------
	UploadSuccess        Key = "upload.success"
	UploadNoFile         Key = "upload.no_file"
	UploadFileTooLarge   Key = "upload.file_too_large"
	UploadTooManyFiles   Key = "upload.too_many_files"
	UploadTypeNotAllowed Key = "upload.type_not_allowed"
	UploadMimeMismatch   Key = "upload.mime_mismatch"
	UploadInvalidContent Key = "upload.invalid_content"
	UploadNotFound       Key = "upload.not_found"
	UploadDeleted        Key = "upload.deleted"
	UploadPresignFailed  Key = "upload.presign_failed"
	UploadReadFailed     Key = "upload.read_failed"
	UploadSaveFailed     Key = "upload.save_failed"
	UploadSceneInvalid   Key = "upload.scene_invalid"
	UploadMigrationDone  Key = "upload.migration_done"

	// ---------- article ----------
	ArticleNotFound          Key = "article.not_found"
	ArticleCreated           Key = "article.created"
	ArticleUpdated           Key = "article.updated"
	ArticleDeleted           Key = "article.deleted"
	ArticleNoPermission      Key = "article.no_permission"
	ArticleNotPublished      Key = "article.not_published"
	ArticleRequireLogin      Key = "article.require_login"
	ArticleRequireMembership Key = "article.require_membership"
	ArticleRequirePayment    Key = "article.require_payment"
	ArticleRequireFollow     Key = "article.require_follow"
	ArticleTitleRequired     Key = "article.title_required"
	ArticleContentRequired   Key = "article.content_required"
	ArticleStatusInvalid     Key = "article.status_invalid"
	ArticlePinned            Key = "article.pinned"
	ArticleUnpinned          Key = "article.unpinned"
	ArticleFeatured          Key = "article.featured"

	// ---------- comment ----------
	CommentNotFound        Key = "comment.not_found"
	CommentCreated         Key = "comment.created"
	CommentDeleted         Key = "comment.deleted"
	CommentNoPermission    Key = "comment.no_permission"
	CommentContentRequired Key = "comment.content_required"
	CommentPinned          Key = "comment.pinned"
	CommentUnpinned        Key = "comment.unpinned"

	// ---------- collection ----------
	CollectionNotFound       Key = "collection.not_found"
	CollectionCreated        Key = "collection.created"
	CollectionUpdated        Key = "collection.updated"
	CollectionDeleted        Key = "collection.deleted"
	CollectionArticleAdded   Key = "collection.article_added"
	CollectionArticleRemoved Key = "collection.article_removed"
	CollectionArticleExists  Key = "collection.article_exists"
	CollectionNoPermission   Key = "collection.no_permission"

	// ---------- interaction ----------
	InteractionLiked       Key = "interaction.liked"
	InteractionUnliked     Key = "interaction.unliked"
	InteractionFavorited   Key = "interaction.favorited"
	InteractionUnfavorited Key = "interaction.unfavorited"
	InteractionAlreadyDone Key = "interaction.already_done"
	InteractionNotDone     Key = "interaction.not_done"

	// ---------- points ----------
	PointsInsufficient     Key = "points.insufficient"
	PointsSignInSuccess    Key = "points.sign_in_success"
	PointsAlreadySignedIn  Key = "points.already_signed_in"
	PointsRecordNotFound   Key = "points.record_not_found"
	PointsConfigNotFound   Key = "points.config_not_found"
	PointsActivityNotFound Key = "points.activity_not_found"

	// ---------- order / payment ----------
	OrderNotFound           Key = "order.not_found"
	OrderCreated            Key = "order.created"
	OrderCancelled          Key = "order.cancelled"
	OrderPaid               Key = "order.paid"
	OrderAlreadyPaid        Key = "order.already_paid"
	OrderStatusInvalid      Key = "order.status_invalid"
	OrderNoPermission       Key = "order.no_permission"
	PaymentChannelDisabled  Key = "payment.channel_disabled"
	PaymentConfigMissing    Key = "payment.config_missing"
	PaymentSignatureInvalid Key = "payment.signature_invalid"
	PaymentNotifySuccess    Key = "payment.notify_success"
	PaymentNotifyFailed     Key = "payment.notify_failed"
	PaymentRefunded         Key = "payment.refunded"
	PaymentNotSupported     Key = "payment.not_supported"

	// ---------- decoration ----------
	DecorationNotFound   Key = "decoration.not_found"
	DecorationEquipped   Key = "decoration.equipped"
	DecorationUnequipped Key = "decoration.unequipped"
	DecorationNotOwned   Key = "decoration.not_owned"
	DecorationExpired    Key = "decoration.expired"

	// ---------- emoji ----------
	EmojiNotFound     Key = "emoji.not_found"
	EmojiPackNotFound Key = "emoji.pack_not_found"
	EmojiCreated      Key = "emoji.created"
	EmojiDeleted      Key = "emoji.deleted"
	EmojiNotOwned     Key = "emoji.not_owned"

	// ---------- message ----------
	MessageSent             Key = "message.sent"
	MessageDeleted          Key = "message.deleted"
	MessageMarkedRead       Key = "message.marked_read"
	MessageNotFound         Key = "message.not_found"
	MessageReceiverNotFound Key = "message.receiver_not_found"
	MessageCannotSendSelf   Key = "message.cannot_send_self"
	MessageContentEmpty     Key = "message.content_empty"

	// ---------- config ----------
	ConfigNotFound              Key = "config.not_found"
	ConfigCreated               Key = "config.created"
	ConfigUpdated               Key = "config.updated"
	ConfigDeleted               Key = "config.deleted"
	ConfigKeyRequired           Key = "config.key_required"
	ConfigKeyExists             Key = "config.key_exists"
	ConfigStartupOnly           Key = "config.startup_only"
	ConfigGroupNotFound         Key = "config.group_not_found"
	ConfigListEmpty             Key = "config.list_empty"
	ConfigCacheCleared          Key = "config.cache_cleared"
	ConfigSensitiveAdded        Key = "config.sensitive_word_added"
	ConfigSensitiveRemoved      Key = "config.sensitive_word_removed"
	ConfigSensitiveNotFound     Key = "config.sensitive_word_not_found"
	ConfigSensitiveEmpty        Key = "config.sensitive_word_empty"
	ConfigSensitiveBatchAdded   Key = "config.sensitive_batch_added"
	ConfigSensitiveBatchRemoved Key = "config.sensitive_batch_removed"

	// ---------- audit ----------
	AuditConfigReloaded Key = "audit.config_reloaded"
	AuditProviderNone   Key = "audit.provider_none"
	AuditNotConfigured  Key = "audit.not_configured"
	AuditRejected       Key = "audit.rejected"
	AuditPendingReview  Key = "audit.pending_review"
	AuditPassed         Key = "audit.passed"
	AuditRecordNotFound Key = "audit.record_not_found"
	AuditHandleSuccess  Key = "audit.handle_success"

	// ---------- search ----------
	SearchDisabled        Key = "search.disabled"
	SearchNotConfigured   Key = "search.not_configured"
	SearchSynced          Key = "search.synced"
	SearchIndexCleared    Key = "search.index_cleared"
	SearchKeywordRequired Key = "search.keyword_required"
	SearchFailed          Key = "search.failed"
	SearchInitFailed      Key = "search.init_failed"

	// ---------- role ----------
	RoleNotFound       Key = "role.not_found"
	RoleCreated        Key = "role.created"
	RoleUpdated        Key = "role.updated"
	RoleDeleted        Key = "role.deleted"
	RoleNameExists     Key = "role.name_exists"
	RoleInUse          Key = "role.in_use"
	RoleNoPermission   Key = "role.no_permission"
	PermissionNotFound Key = "permission.not_found"
	PermissionAssigned Key = "permission.assigned"

	// ---------- report ----------
	ReportCreated        Key = "report.created"
	ReportNotFound       Key = "report.not_found"
	ReportHandled        Key = "report.handled"
	ReportAlreadyHandled Key = "report.already_handled"
	ReportTargetInvalid  Key = "report.target_invalid"
	ReportReasonRequired Key = "report.reason_required"

	// ---------- invite ----------
	InviteCodeInvalid   Key = "invite.code_invalid"
	InviteCodeUsed      Key = "invite.code_used"
	InviteCodeExpired   Key = "invite.code_expired"
	InviteCreated       Key = "invite.created"
	InviteRewardGranted Key = "invite.reward_granted"

	// ---------- category / tag ----------
	CategoryNotFound    Key = "category.not_found"
	CategoryCreated     Key = "category.created"
	CategoryUpdated     Key = "category.updated"
	CategoryDeleted     Key = "category.deleted"
	CategoryHasArticles Key = "category.has_articles"
	CategoryNameExists  Key = "category.name_exists"
	TagNotFound         Key = "tag.not_found"
	TagCreated          Key = "tag.created"
	TagDeleted          Key = "tag.deleted"
	TagNameExists       Key = "tag.name_exists"

	// ---------- banner ----------
	BannerNotFound Key = "banner.not_found"
	BannerCreated  Key = "banner.created"
	BannerUpdated  Key = "banner.updated"
	BannerDeleted  Key = "banner.deleted"

	// ---------- achievement ----------
	AchievementNotFound   Key = "achievement.not_found"
	AchievementCodeExists Key = "achievement.code_exists"
	AchievementCreated    Key = "achievement.created"
	AchievementUpdated    Key = "achievement.updated"
	AchievementDeleted    Key = "achievement.deleted"
	AchievementGranted    Key = "achievement.granted"

	// ---------- stats ----------
	StatsNotFound     Key = "stats.not_found"
	StatsInvalidRange Key = "stats.invalid_range"
)

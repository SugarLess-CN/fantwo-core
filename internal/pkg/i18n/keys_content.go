package i18n

// 补充 key：内容域（i18n-content 提取的 16 个）+ 用户运营域（i18n-user 提取的 15 个）。
const (
	// ---------- article（内容域）----------
	ArticleContentRequiredMixed Key = "article.content_required_mixed"
	ArticleVideoURLRequired     Key = "article.video_url_required"
	ArticleFeaturedAdminOnly    Key = "article.featured_admin_only"
	ArticleDeleteNoPermission   Key = "article.delete_no_permission"

	// ---------- interaction ----------
	InteractionViewNotFound Key = "interaction.view_not_found"

	// ---------- comment ----------
	CommentPinAuthorOrAdmin Key = "comment.pin_author_or_admin"

	// ---------- collection ----------
	CollectionPrivate      Key = "collection.is_private"
	CollectionNameRequired Key = "collection.name_required"

	// ---------- category ----------
	CategoryNameRequired  Key = "category.name_required"
	CategoryParentMissing Key = "category.parent_not_found"
	CategoryParentSelf    Key = "category.parent_self"
	CategoryHasChildren   Key = "category.has_children"

	// ---------- tag ----------
	TagNameRequired    Key = "tag.name_required"
	TagAlreadyFollowed Key = "tag.already_followed"
	TagNotFollowed     Key = "tag.not_followed"

	// ---------- banner ----------
	BannerTitleRequired Key = "banner.title_required"
	BannerImageRequired Key = "banner.image_required"

	// ---------- points（用户运营域）----------
	PointsAmountInvalid     Key = "points.amount_invalid"
	PointsTaskNotCompleted  Key = "points.task_not_completed"
	PointsDailyLimit        Key = "points.daily_limit_reached"
	PointsTotalLimit        Key = "points.total_limit_reached"
	PointsAccountCreateFail Key = "points.account_create_failed"

	// ---------- achievement ----------
	AchievementNotAvailable Key = "achievement.not_available"
	AchievementNotCompleted Key = "achievement.not_completed"

	// ---------- reward（points 与 achievement 共用）----------
	CommonRewardAlreadyClaimed Key = "common.reward_already_claimed"

	// ---------- report ----------
	ReportAlreadySubmitted Key = "report.already_submitted"
	ReportInvalidColumn    Key = "report.invalid_column"

	// ---------- invite ----------
	InviteNotFound          Key = "invite.not_found"
	InviteCommissionMissing Key = "invite.commission_not_found"
	InviteCodeGenerateFail  Key = "invite.code_generate_failed"

	// ---------- user ----------
	UserCommissionTypeUnknown Key = "user.commission_type_unknown"

	// ---------- auth ----------
	AuthCaptchaSendFailed Key = "auth.captcha_send_failed"
)

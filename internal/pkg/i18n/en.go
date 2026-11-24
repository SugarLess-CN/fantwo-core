package i18n

// 英文文案表（唯一权威）。
//
// 规则：
//   - 一律英文，首字母大写，句尾不加句号（与常见 API 提示风格一致）；
//   - 带参数的文案用 %s / %d / %.1f 占位，参数顺序与业务代码调用一致；
//   - 同一语义只保留一个 key，不要为不同域重复造词；
//   - 全局唯一性、完整性由 i18n_test.go 校验。
var enCatalog = map[Key]string{
	// ---------- common ----------
	CommonOK:              "Success",
	CommonCreated:         "Created successfully",
	CommonUpdated:         "Updated successfully",
	CommonDeleted:         "Deleted successfully",
	CommonBadRequest:      "Invalid request",
	CommonUnauthorized:    "Unauthorized",
	CommonForbidden:       "Permission denied",
	CommonNotFound:        "Resource not found",
	CommonConflict:        "Resource conflict",
	CommonTooManyRequests: "Too many requests, please try again later",
	CommonInternal:        "Internal server error",
	CommonInvalidParam:    "Invalid parameter: %s",
	CommonMissingField:    "Missing required field: %s",
	CommonInvalidID:       "Invalid ID: %s",
	CommonOperationFailed: "Operation failed: %s",
	CommonAlreadyExists:   "%s already exists",
	CommonNotAllowed:      "This operation is not allowed",
	CommonExpired:         "%s has expired",

	// ---------- auth ----------
	AuthLoginSuccess:        "Login successful",
	AuthLogoutSuccess:       "Logged out",
	AuthLogoutAllSuccess:    "Logged out from all devices",
	AuthRegisterSuccess:     "Registration successful",
	AuthBadCredentials:      "Incorrect username or password",
	AuthTokenExpired:        "Token expired, please log in again",
	AuthTokenInvalid:        "Invalid token",
	AuthNeedLogin:           "Please log in first",
	AuthAccountBanned:       "This account has been banned",
	AuthAccountDisabled:     "This account has been disabled",
	AuthCaptchaSent:         "Verification code sent",
	AuthCaptchaInvalid:      "Invalid or expired verification code",
	AuthCaptchaTooFrequent:  "Please wait before requesting another code",
	AuthPasswordReset:       "Password reset successful",
	AuthPasswordChanged:     "Password changed successfully",
	AuthPasswordMismatch:    "Passwords do not match",
	AuthOldPasswordWrong:    "Current password is incorrect",
	AuthOAuthNotConfigured:  "Third-party login is not configured",
	AuthOAuthFailed:         "Third-party login failed: %s",
	AuthOAuthReauthRequired: "Your third-party session has expired, please sign in again",
	AuthOAuthTokenForged:    "Invalid third-party credentials",
	AuthOAuthScopeMissing:   "Required permission was not granted, please authorize again and allow profile access",
	AuthDeviceLimit:         "Device limit reached, please log out from another device",

	// ---------- user ----------
	UserNotFound:            "User not found",
	UserAlreadyExists:       "User already exists",
	UserEmailExists:         "This email is already registered",
	UserPhoneExists:         "This phone number is already registered",
	UserUsernameExists:      "This username is already taken",
	UserUsernameInvalid:     "Username must be 3-32 characters, letters, digits or underscore",
	UserPasswordTooWeak:     "Password must be at least 8 characters",
	UserProfileUpdated:      "Profile updated",
	UserAvatarUpdated:       "Avatar updated",
	UserFollowed:            "Followed",
	UserUnfollowed:          "Unfollowed",
	UserCannotFollowSelf:    "You cannot follow yourself",
	UserAlreadyFollowed:     "You are already following this user",
	UserNotFollowing:        "You are not following this user",
	UserCertificationSubmit: "Certification submitted, pending review",
	UserCertificationPassed: "Certification approved",
	UserCertificationReject: "Certification rejected: %s",

	// ---------- upload ----------
	UploadSuccess:        "Upload successful",
	UploadNoFile:         "No file provided",
	UploadFileTooLarge:   "File too large: %.1fMB exceeds the %dMB limit",
	UploadTooManyFiles:   "Too many files: maximum %d per request",
	UploadTypeNotAllowed: "File type not allowed: %s",
	UploadMimeMismatch:   "File content does not match the declared type: %s",
	UploadInvalidContent: "File content is not a valid %s file",
	UploadNotFound:       "Upload not found",
	UploadDeleted:        "Upload deleted",
	UploadPresignFailed:  "Failed to generate upload URL",
	UploadReadFailed:     "Failed to read uploaded file",
	UploadSaveFailed:     "Failed to save file",
	UploadSceneInvalid:   "Invalid upload scene: %s",
	UploadMigrationDone:  "Migration finished: %d processed, %d skipped, %d failed",

	// ---------- article ----------
	ArticleNotFound:          "Article not found",
	ArticleCreated:           "Article created",
	ArticleUpdated:           "Article updated",
	ArticleDeleted:           "Article deleted",
	ArticleNoPermission:      "You do not have permission to modify this article",
	ArticleNotPublished:      "This article is not published yet",
	ArticleRequireLogin:      "Please log in to read this article",
	ArticleRequireMembership: "Membership required to read this article",
	ArticleRequirePayment:    "Purchase required to read this article",
	ArticleRequireFollow:     "Follow the author to read this article",
	ArticleTitleRequired:     "Article title is required",
	ArticleContentRequired:   "Article content is required",
	ArticleStatusInvalid:     "Invalid article status: %s",
	ArticlePinned:            "Article pinned",
	ArticleUnpinned:          "Article unpinned",
	ArticleFeatured:          "Article featured",

	// ---------- comment ----------
	CommentNotFound:        "Comment not found",
	CommentCreated:         "Comment posted",
	CommentDeleted:         "Comment deleted",
	CommentNoPermission:    "You do not have permission to modify this comment",
	CommentContentRequired: "Comment content is required",
	CommentPinned:          "Comment pinned",
	CommentUnpinned:        "Comment unpinned",

	// ---------- collection ----------
	CollectionNotFound:       "Collection not found",
	CollectionCreated:        "Collection created",
	CollectionUpdated:        "Collection updated",
	CollectionDeleted:        "Collection deleted",
	CollectionArticleAdded:   "Article added to collection",
	CollectionArticleRemoved: "Article removed from collection",
	CollectionArticleExists:  "Article is already in this collection",
	CollectionNoPermission:   "You do not have permission to modify this collection",

	// ---------- interaction ----------
	InteractionLiked:       "Liked",
	InteractionUnliked:     "Like removed",
	InteractionFavorited:   "Added to favorites",
	InteractionUnfavorited: "Removed from favorites",
	InteractionAlreadyDone: "Already done",
	InteractionNotDone:     "Not done yet",

	// ---------- points ----------
	PointsInsufficient:     "Insufficient points",
	PointsSignInSuccess:    "Signed in, +%d points",
	PointsAlreadySignedIn:  "Already signed in today",
	PointsRecordNotFound:   "Points record not found",
	PointsConfigNotFound:   "Points configuration not found",
	PointsActivityNotFound: "Points activity not found",

	// ---------- order / payment ----------
	OrderNotFound:           "Order not found",
	OrderCreated:            "Order created",
	OrderCancelled:          "Order cancelled",
	OrderPaid:               "Payment successful",
	OrderAlreadyPaid:        "Order has already been paid",
	OrderStatusInvalid:      "Invalid order status: %s",
	OrderNoPermission:       "You do not have permission to access this order",
	PaymentChannelDisabled:  "This payment channel is disabled",
	PaymentConfigMissing:    "Payment channel is not configured",
	PaymentSignatureInvalid: "Invalid payment signature",
	PaymentNotifySuccess:    "Success",
	PaymentNotifyFailed:     "Failed: %s",
	PaymentRefunded:         "Refunded",
	PaymentNotSupported:     "Payment method not supported: %s",

	// ---------- decoration ----------
	DecorationNotFound:   "Decoration not found",
	DecorationEquipped:   "Decoration equipped",
	DecorationUnequipped: "Decoration unequipped",
	DecorationNotOwned:   "You do not own this decoration",
	DecorationExpired:    "This decoration has expired",

	// ---------- emoji ----------
	EmojiNotFound:     "Emoji not found",
	EmojiPackNotFound: "Emoji pack not found",
	EmojiCreated:      "Emoji created",
	EmojiDeleted:      "Emoji deleted",
	EmojiNotOwned:     "You do not own this emoji pack",

	// ---------- message ----------
	MessageSent:             "Message sent",
	MessageDeleted:          "Message deleted",
	MessageMarkedRead:       "Marked as read",
	MessageNotFound:         "Message not found",
	MessageReceiverNotFound: "Recipient not found",
	MessageCannotSendSelf:   "You cannot send a message to yourself",
	MessageContentEmpty:     "Message content cannot be empty",

	// ---------- config ----------
	ConfigNotFound:              "Configuration not found",
	ConfigCreated:               "Configuration created",
	ConfigUpdated:               "Configuration updated",
	ConfigDeleted:               "Configuration deleted",
	ConfigKeyRequired:           "Configuration key is required",
	ConfigKeyExists:             "Configuration key %s already exists",
	ConfigStartupOnly:           "Configuration %s takes effect at startup only; change the environment variable and restart the service",
	ConfigGroupNotFound:         "Configuration group %s not found",
	ConfigListEmpty:             "Configuration list cannot be empty",
	ConfigCacheCleared:          "Cache cleared",
	ConfigSensitiveAdded:        "Sensitive word added",
	ConfigSensitiveRemoved:      "Sensitive word removed",
	ConfigSensitiveNotFound:     "Sensitive word %s not found",
	ConfigSensitiveEmpty:        "Sensitive word cannot be empty",
	ConfigSensitiveBatchAdded:   "Added %d sensitive words",
	ConfigSensitiveBatchRemoved: "Removed %d sensitive words",

	// ---------- audit ----------
	AuditConfigReloaded: "Audit configuration reloaded",
	AuditProviderNone:   "No audit provider configured",
	AuditNotConfigured:  "Content audit is not configured",
	AuditRejected:       "Content rejected by audit",
	AuditPendingReview:  "Content submitted for review",
	AuditPassed:         "Content passed audit",
	AuditRecordNotFound: "Audit record not found",
	AuditHandleSuccess:  "Audit handled",

	// ---------- search ----------
	SearchDisabled:        "Search is disabled",
	SearchNotConfigured:   "Search service is not configured",
	SearchSynced:          "Synced %d articles to Elasticsearch",
	SearchIndexCleared:    "Search index cleared",
	SearchKeywordRequired: "Search keyword is required",
	SearchFailed:          "Search failed: %s",
	SearchInitFailed:      "Failed to initialize Elasticsearch: %s",

	// ---------- role ----------
	RoleNotFound:       "Role not found",
	RoleCreated:        "Role created",
	RoleUpdated:        "Role updated",
	RoleDeleted:        "Role deleted",
	RoleNameExists:     "Role name %s already exists",
	RoleInUse:          "Role is assigned to users and cannot be deleted",
	RoleNoPermission:   "You do not have permission for this operation",
	PermissionNotFound: "Permission not found",
	PermissionAssigned: "Permissions assigned",

	// ---------- report ----------
	ReportCreated:        "Report submitted",
	ReportNotFound:       "Report not found",
	ReportHandled:        "Report handled",
	ReportAlreadyHandled: "Report has already been handled",
	ReportTargetInvalid:  "Invalid report target",
	ReportReasonRequired: "Report reason is required",

	// ---------- invite ----------
	InviteCodeInvalid:   "Invalid invitation code",
	InviteCodeUsed:      "Invitation code has already been used",
	InviteCodeExpired:   "Invitation code has expired",
	InviteCreated:       "Invitation code created",
	InviteRewardGranted: "Invitation reward granted",

	// ---------- category / tag ----------
	CategoryNotFound:    "Category not found",
	CategoryCreated:     "Category created",
	CategoryUpdated:     "Category updated",
	CategoryDeleted:     "Category deleted",
	CategoryHasArticles: "Category still has articles and cannot be deleted",
	CategoryNameExists:  "Category name %s already exists",
	TagNotFound:         "Tag not found",
	TagCreated:          "Tag created",
	TagDeleted:          "Tag deleted",
	TagNameExists:       "Tag name %s already exists",

	// ---------- banner ----------
	BannerNotFound: "Banner not found",
	BannerCreated:  "Banner created",
	BannerUpdated:  "Banner updated",
	BannerDeleted:  "Banner deleted",

	// ---------- achievement ----------
	AchievementNotFound:   "Achievement not found",
	AchievementCodeExists: "Achievement code %s already exists",
	AchievementCreated:    "Achievement created",
	AchievementUpdated:    "Achievement updated",
	AchievementDeleted:    "Achievement deleted",
	AchievementGranted:    "Achievement granted",

	// ---------- stats ----------
	StatsNotFound:     "Statistics not found",
	StatsInvalidRange: "Invalid date range",
}

// 补充文案（用户域 / RBAC / 依赖降级）。
var enCatalogExtra = map[Key]string{
	AuthPermVersionStale: "Permissions changed, please log in again",
	AuthRefreshInvalid:   "Invalid refresh token",
	AuthRefreshExpired:   "Refresh token expired",
	AuthDeviceIDRequired: "The device-id header is required",

	UserDeletionPending:    "Account deletion already requested and is in the cooling-off period",
	UserDeletionNotPending: "No pending account deletion request",
	UserAccountDeleted:     "This account has been deleted",

	UserCertificationExists:     "A certification is already pending or approved",
	UserCertificationNotFound:   "Certification record not found",
	UserCertificationNotPending: "This certification is not pending review",

	MembershipDuplicated: "Membership is still active, no need to renew",

	RoleIsSystem:         "Built-in system roles cannot be deleted",
	PermissionNameExists: "Permission name already exists",
	PermissionInUse:      "Permission is still referenced by roles and cannot be deleted",

	CommonDependencyUnavailable: "Dependency service unavailable, please try again later",

	CommonInvalidParameter:  "Invalid request parameter",
	CommonVerifyCodeExpired: "Verification code expired",
	CommonResourceInUse:     "Resource is still in use",
}

func init() {
	for k, v := range enCatalogExtra {
		enCatalog[k] = v
	}
}

// 补充文案（交易与装扮域，i18n-commerce 提取的存量文案）。
var enCatalogCommerce = map[Key]string{
	PaymentAlipaySDKInitFailed: "Failed to initialize Alipay SDK: %s",
	PaymentAlipayPubkeyFailed:  "Failed to load Alipay public key: %s",
	PaymentAlipayCreateFailed:  "Failed to create Alipay payment: %s",
	PaymentWechatCreateFailed:  "Failed to create WeChat payment: %s",
	PaymentRecordNotFound:      "Payment record not found",

	OrderNotPending: "Order is not in pending status",

	ArticleNotPayable:       "This article does not require payment",
	ArticlePriceInvalid:     "Invalid article price",
	ArticleAlreadyPurchased: "You have already purchased this article",

	MembershipLifetimeActive:  "You are already a lifetime member",
	MembershipPlanInvalid:     "Invalid membership plan",
	MembershipDurationInvalid: "Invalid membership duration (1-120 months)",

	DecorationNameRequired:      "Decoration name is required",
	DecorationTypeRequired:      "Decoration type is required",
	DecorationUnavailable:       "This decoration is unavailable",
	DecorationWalletUnavailable: "Wallet service is not available",
	DecorationNotPurchasable:    "This decoration cannot be purchased",
	DecorationAlreadyOwned:      "You already own this decoration",
	DecorationCannotGiftSelf:    "You cannot gift a decoration to yourself",
	DecorationRecipientOwned:    "The recipient already owns this decoration",
	DecorationActivityNameReq:   "Activity name is required",
	DecorationActivityRewardReq: "Reward decoration is required",
	DecorationActivityNotFound:  "Activity not found",
	DecorationActivityNotActive: "Activity is not active",
	DecorationActivityEnded:     "Activity has ended",
	DecorationActivityNotJoined: "You have not joined this activity",
	DecorationActivityRewardGot: "Activity reward has already been claimed",
	DecorationActivityReqNotMet: "Activity requirements are not met",

	EmojiNameRequired:       "Emoji name is required",
	EmojiURLRequired:        "Emoji image URL is required",
	EmojiCodeExists:         "Emoji code already exists",
	EmojiSystemOnlyAdmin:    "System emojis can only be managed by administrators",
	EmojiNoPermission:       "You can only manage your own emojis",
	EmojiPackNameRequired:   "Emoji pack name is required",
	EmojiPackNoPermission:   "You can only manage your own emoji packs",
	EmojiNotInPack:          "Emoji is not in this pack",
	EmojiUploadNotAvailable: "Emoji upload persistence is handled by the upload service and is not integrated yet; upload via POST /upload first, then create the emoji with the returned URL",
}

func init() {
	for k, v := range enCatalogCommerce {
		enCatalog[k] = v
	}
}

// 补充文案（内容域 + 用户运营域，i18n-content 与 i18n-user 提取的存量文案）。
var enCatalogContent = map[Key]string{
	// article
	ArticleContentRequiredMixed: "Content is required for mixed articles",
	ArticleVideoURLRequired:     "Video URL is required for video articles",
	ArticleFeaturedAdminOnly:    "Only administrators can set featured articles",
	ArticleDeleteNoPermission:   "You do not have permission to delete this article",

	// interaction
	InteractionViewNotFound: "View history record not found",

	// comment
	CommentPinAuthorOrAdmin: "Only the article author or an administrator can pin comments",

	// collection
	CollectionPrivate:      "This collection is private",
	CollectionNameRequired: "Collection name is required",

	// category
	CategoryNameRequired:  "Category name is required",
	CategoryParentMissing: "Parent category not found",
	CategoryParentSelf:    "A category cannot be its own parent",
	CategoryHasChildren:   "Category still has subcategories; delete or move them first",

	// tag
	TagNameRequired:    "Tag name is required",
	TagAlreadyFollowed: "You are already following this tag",
	TagNotFollowed:     "You are not following this tag",

	// banner
	BannerTitleRequired: "Banner title is required",
	BannerImageRequired: "Banner image is required",

	// points
	PointsAmountInvalid:     "Points amount must be greater than 0",
	PointsTaskNotCompleted:  "Task is not completed yet",
	PointsDailyLimit:        "Daily limit reached for this activity",
	PointsTotalLimit:        "Total limit reached for this activity",
	PointsAccountCreateFail: "Failed to create points account",

	// achievement
	AchievementNotAvailable: "Achievement not found or not started",
	AchievementNotCompleted: "Achievement is not completed yet",

	// reward
	CommonRewardAlreadyClaimed: "Reward has already been claimed",

	// report
	ReportAlreadySubmitted: "You have already reported this content",
	ReportInvalidColumn:    "Invalid statistic column",

	// invite
	InviteNotFound:          "Invitation not found",
	InviteCommissionMissing: "Commission record not found",
	InviteCodeGenerateFail:  "Failed to generate invitation code",

	// user
	UserCommissionTypeUnknown: "Unknown commission type",

	// auth
	AuthCaptchaSendFailed: "Failed to send verification code",
}

func init() {
	for k, v := range enCatalogContent {
		enCatalog[k] = v
	}
}

// 补充文案（消息域）。
var enCatalogMessage = map[Key]string{
	MessageTextRequired:       "Text message content cannot be empty",
	MessagePayloadRequired:    "Non-text messages must provide a payload",
	MessagePayloadImage:       "Image message payload must contain url or urls",
	MessagePayloadFile:        "File message payload must contain url and name",
	MessagePayloadCard:        "Card message payload must contain title and description",
	MessageKindInvalid:        "message_kind must be one of: text/image/file/card",
	MessageReceiverRequired:   "Receiver list cannot be empty",
	MessageBlacklisted:        "Cannot send: you or the recipient has blocked the other party",
	MessageSessionFailed:      "Failed to create conversation",
	MessageRecallForbidden:    "You do not have permission to recall this message",
	MessageBlockSelf:          "You cannot block yourself",
	MessageIDsRequired:        "message_ids cannot be empty",
	MessageBatchForbidden:     "You do not have permission to operate on some of the messages",
	MessageActionInvalid:      "action must be one of: read/delete",
	MessageBroadcastForbidden: "You do not have permission to send broadcast messages",
	MessageViewForbidden:      "You do not have permission to view this message",
	MessageUpdateForbidden:    "You do not have permission to modify this message",
	MessageDeleteForbidden:    "You do not have permission to delete this message",
}

func init() {
	for k, v := range enCatalogMessage {
		enCatalog[k] = v
	}
}

// 补充文案（审核域补充）。
var enCatalogAuditExtra = map[Key]string{
	AuditNotPending: "This article is not pending review",
}

func init() {
	for k, v := range enCatalogAuditExtra {
		enCatalog[k] = v
	}
}

// 补充文案（启动期配置拒写，运维可执行提示）。
var enCatalogStartupOnly = map[Key]string{
	ConfigStartupOnlyFix: "Configuration %s is startup-only (storage / search clients are initialized at service start); writing it to the database has no effect. Change the environment variable or config file and restart the service instead",
}

func init() {
	for k, v := range enCatalogStartupOnly {
		enCatalog[k] = v
	}
}

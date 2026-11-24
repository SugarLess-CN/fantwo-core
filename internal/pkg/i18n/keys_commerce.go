package i18n

// 补充 key：交易与装扮域（i18n-commerce 成员提取存量文案时缺的 key）。
//
// 命名规则：域前缀 + 蛇形（与 en.go 的 key 字符串一一对应）。
const (
	// payment 支付渠道初始化与创建
	PaymentAlipaySDKInitFailed Key = "payment.alipay_sdk_init_failed"
	PaymentAlipayPubkeyFailed  Key = "payment.alipay_pubkey_load_failed"
	PaymentAlipayCreateFailed  Key = "payment.alipay_create_failed"
	PaymentWechatCreateFailed  Key = "payment.wechat_create_failed"
	PaymentRecordNotFound      Key = "payment.record_not_found"

	// order 订单状态机
	OrderNotPending Key = "order.not_pending"

	// article 付费阅读
	ArticleNotPayable       Key = "article.not_payable"
	ArticlePriceInvalid     Key = "article.price_invalid"
	ArticleAlreadyPurchased Key = "article.already_purchased"

	// membership 会员
	MembershipLifetimeActive  Key = "membership.lifetime_active"
	MembershipPlanInvalid     Key = "membership.plan_invalid"
	MembershipDurationInvalid Key = "membership.duration_invalid"

	// decoration 装饰品
	DecorationNameRequired      Key = "decoration.name_required"
	DecorationTypeRequired      Key = "decoration.type_required"
	DecorationUnavailable       Key = "decoration.unavailable"
	DecorationWalletUnavailable Key = "decoration.wallet_unavailable"
	DecorationNotPurchasable    Key = "decoration.not_purchasable"
	DecorationAlreadyOwned      Key = "decoration.already_owned"
	DecorationCannotGiftSelf    Key = "decoration.cannot_gift_self"
	DecorationRecipientOwned    Key = "decoration.recipient_already_owned"
	DecorationActivityNameReq   Key = "decoration.activity_name_required"
	DecorationActivityRewardReq Key = "decoration.activity_reward_required"
	DecorationActivityNotFound  Key = "decoration.activity_not_found"
	DecorationActivityNotActive Key = "decoration.activity_not_active"
	DecorationActivityEnded     Key = "decoration.activity_ended"
	DecorationActivityNotJoined Key = "decoration.activity_not_joined"
	DecorationActivityRewardGot Key = "decoration.activity_reward_claimed"
	DecorationActivityReqNotMet Key = "decoration.activity_requirements_not_met"

	// emoji 表情包
	EmojiNameRequired       Key = "emoji.name_required"
	EmojiURLRequired        Key = "emoji.url_required"
	EmojiCodeExists         Key = "emoji.code_exists"
	EmojiSystemOnlyAdmin    Key = "emoji.system_only_admin"
	EmojiNoPermission       Key = "emoji.no_permission"
	EmojiPackNameRequired   Key = "emoji.pack_name_required"
	EmojiPackNoPermission   Key = "emoji.pack_no_permission"
	EmojiNotInPack          Key = "emoji.not_in_pack"
	EmojiUploadNotAvailable Key = "emoji.upload_not_available"
)

package i18n

// 补充 key：消息域（lead 负责，i18n-content 提到 message_private_service.go 仍缺）。
const (
	// ---- 私信内容校验 ----
	MessageTextRequired    Key = "message.text_required"
	MessagePayloadRequired Key = "message.payload_required"
	MessagePayloadImage    Key = "message.payload_image"
	MessagePayloadFile     Key = "message.payload_file"
	MessagePayloadCard     Key = "message.payload_card"
	MessageKindInvalid     Key = "message.kind_invalid"

	// ---- 私信发送 / 权限 ----
	MessageReceiverRequired Key = "message.receiver_required"
	MessageBlacklisted      Key = "message.blacklisted"
	MessageSessionFailed    Key = "message.session_failed"
	MessageRecallForbidden  Key = "message.recall_forbidden"
	MessageBlockSelf        Key = "message.block_self"

	// ---- 消息批量操作 ----
	MessageIDsRequired    Key = "message.ids_required"
	MessageBatchForbidden Key = "message.batch_forbidden"
	MessageActionInvalid  Key = "message.action_invalid"

	// ---- 广播 ----
	MessageBroadcastForbidden Key = "message.broadcast_forbidden"

	// ---- 消息操作权限（view / update / delete）----
	MessageViewForbidden   Key = "message.view_forbidden"
	MessageUpdateForbidden Key = "message.update_forbidden"
	MessageDeleteForbidden Key = "message.delete_forbidden"

	// ---- 消息类型（system / notification 复用 kind_invalid）----
)

// audit 审核补充
const (
	AuditNotPending Key = "audit.not_pending"
)

// config 启动期拒写（含变量名提示，运维可执行）。
const (
	ConfigStartupOnlyFix Key = "config.startup_only_howto"
)

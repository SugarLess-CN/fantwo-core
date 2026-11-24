package i18n

// 补充 key：用户域 / RBAC / 依赖降级（errcode_user.go 使用）。
const (
	// 权限与认证细分
	AuthPermVersionStale Key = "auth.perm_version_stale"
	AuthRefreshInvalid   Key = "auth.refresh_invalid"
	AuthRefreshExpired   Key = "auth.refresh_expired"
	AuthDeviceIDRequired Key = "auth.device_id_required"

	// 账号状态
	UserDeletionPending    Key = "user.deletion_pending"
	UserDeletionNotPending Key = "user.deletion_not_pending"
	UserAccountDeleted     Key = "user.account_deleted"

	// 认证申请
	UserCertificationExists     Key = "user.certification_exists"
	UserCertificationNotFound   Key = "user.certification_not_found"
	UserCertificationNotPending Key = "user.certification_not_pending"

	// 会员
	MembershipDuplicated Key = "membership.duplicated"

	// RBAC 补充
	RoleIsSystem         Key = "role.is_system"
	PermissionNameExists Key = "permission.name_exists"
	PermissionInUse      Key = "permission.in_use"

	// 依赖降级
	CommonDependencyUnavailable Key = "common.dependency_unavailable"

	// 通用（无参数版本，供 errcode 预定义使用）
	CommonInvalidParameter  Key = "common.invalid_parameter"
	CommonVerifyCodeExpired Key = "common.verify_code_expired"
	CommonResourceInUse     Key = "common.resource_in_use"
)

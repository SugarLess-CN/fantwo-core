// 用户域 / RBAC 错误码补段（T-05b）。
//
// 规则（任务书 §5）：新增文件，不改 errcode.go 既有段；
// 段位按任务书分配——2001 未认证 / 2003 无权限 / 4001 参数 /
// 4023 会员重复 / 4080-4086 橱窗 / 4096-4099 导入。
// 本文件只落地用户域用到的段位，其余段位由对应业务域自行补。
package errcode

import "net/http"

var (
	// ErrNeedAuth 未认证（细分码 2001；HTTP 401）。token 缺失/失效/被黑名单。
	ErrNeedAuth = &Err{Code: 2001, HTTP: http.StatusUnauthorized, Msg: "未登录或登录已过期"}
	// ErrPermVersionStale 权限版本过期（细分码 2002；HTTP 401）：权限被改动，
	// 客户端需重新登录/刷新以拿到最新 roles/perms。
	ErrPermVersionStale = &Err{Code: 2002, HTTP: http.StatusUnauthorized, Msg: "权限已变更，请重新登录"}
	// ErrNoPermission 无权限（细分码 2003；HTTP 403）。
	ErrNoPermission = &Err{Code: 2003, HTTP: http.StatusForbidden, Msg: "没有操作权限"}

	// ErrInvalidParam 参数错误（细分码 4001；HTTP 400）。
	ErrInvalidParam = &Err{Code: 4001, HTTP: http.StatusBadRequest, Msg: "请求参数错误"}

	// ErrMembershipDuplicated 会员重复开通（细分码 4023；HTTP 409）。
	ErrMembershipDuplicated = &Err{Code: 4023, HTTP: http.StatusConflict, Msg: "会员尚未到期，无需重复开通"}

	// ---- 用户域细分（4002-4022） ----
	// ErrUserNotExist 用户不存在（HTTP 404，对齐原 NotFoundException userNotExist）。
	ErrUserNotExist = &Err{Code: 4002, HTTP: http.StatusNotFound, Msg: "用户不存在"}
	// ErrPasswordError 密码错误（HTTP 400，对齐原 BadRequestException passwordError）。
	ErrPasswordError = &Err{Code: 4003, HTTP: http.StatusBadRequest, Msg: "密码错误"}
	// ErrUsernameExists 用户名已存在。
	ErrUsernameExists = &Err{Code: 4004, HTTP: http.StatusConflict, Msg: "用户名已存在"}
	// ErrEmailExists 邮箱已被使用。
	ErrEmailExists = &Err{Code: 4005, HTTP: http.StatusConflict, Msg: "邮箱已被使用"}
	// ErrPhoneExists 手机号已被使用。
	ErrPhoneExists = &Err{Code: 4006, HTTP: http.StatusConflict, Msg: "手机号已被绑定"}
	// ErrVerifyCodeInvalid 验证码错误。
	ErrVerifyCodeInvalid = &Err{Code: 4007, HTTP: http.StatusBadRequest, Msg: "验证码错误"}
	// ErrVerifyCodeExpired 验证码已过期（HTTP 410，对齐原 swagger 410）。
	ErrVerifyCodeExpired = &Err{Code: 4008, HTTP: http.StatusGone, Msg: "验证码已过期"}
	// ErrVerifyCodeTooOften 验证码请求过于频繁。
	ErrVerifyCodeTooOften = &Err{Code: 4009, HTTP: http.StatusTooManyRequests, Msg: "验证码发送过于频繁"}
	// ErrRefreshTokenInvalid refresh token 无效。
	ErrRefreshTokenInvalid = &Err{Code: 4010, HTTP: http.StatusUnauthorized, Msg: "refresh token 无效"}
	// ErrRefreshTokenExpired refresh token 过期。
	ErrRefreshTokenExpired = &Err{Code: 4011, HTTP: http.StatusUnauthorized, Msg: "refresh token 已过期"}
	// ErrDeviceIDRequired 缺少 device-id 头。
	ErrDeviceIDRequired = &Err{Code: 4012, HTTP: http.StatusBadRequest, Msg: "device-id header is required"}
	// ErrFollowSelf 不能关注自己。
	ErrFollowSelf = &Err{Code: 4013, HTTP: http.StatusForbidden, Msg: "不能关注自己"}
	// ErrSignInAlready 今天已签到。
	ErrSignInAlready = &Err{Code: 4014, HTTP: http.StatusBadRequest, Msg: "今天已经签到过了"}
	// ErrInsufficientBalance 余额不足（用户域钱包提现）。
	ErrInsufficientBalance = &Err{Code: 4015, HTTP: http.StatusBadRequest, Msg: "余额不足"}
	// ErrDeletionPending 账号已在注销冷静期。
	ErrDeletionPending = &Err{Code: 4016, HTTP: http.StatusConflict, Msg: "账号已提交注销申请，处于冷静期"}
	// ErrDeletionNotPending 账号不在注销冷静期。
	ErrDeletionNotPending = &Err{Code: 4017, HTTP: http.StatusBadRequest, Msg: "账号未提交注销申请"}
	// ErrAccountDeleted 账号已注销。
	ErrAccountDeleted = &Err{Code: 4018, HTTP: http.StatusForbidden, Msg: "账号已注销"}
	// ErrCertificationExists 已有待审核或已通过的认证。
	ErrCertificationExists = &Err{Code: 4019, HTTP: http.StatusConflict, Msg: "已有待审核或已通过的认证"}
	// ErrCertificationNotFound 认证记录不存在。
	ErrCertificationNotFound = &Err{Code: 4020, HTTP: http.StatusNotFound, Msg: "认证记录不存在"}
	// ErrCertificationNotPending 认证记录不在待审核状态。
	ErrCertificationNotPending = &Err{Code: 4021, HTTP: http.StatusConflict, Msg: "该认证申请不在待审核状态"}
	// ErrGithubOAuthNotConfigured GitHub OAuth 未配置。
	ErrGithubOAuthNotConfigured = &Err{Code: 4022, HTTP: http.StatusBadRequest, Msg: "GitHub OAuth 未配置"}

	// ---- RBAC 细分（4024-4039） ----
	// ErrRoleNameExists 角色名已存在。
	ErrRoleNameExists = &Err{Code: 4024, HTTP: http.StatusBadRequest, Msg: "角色名称已存在"}
	// ErrRoleNotExist 角色不存在。
	ErrRoleNotExist = &Err{Code: 4025, HTTP: http.StatusNotFound, Msg: "角色不存在"}
	// ErrRoleIsSystem 系统角色不允许删除。
	ErrRoleIsSystem = &Err{Code: 4026, HTTP: http.StatusForbidden, Msg: "系统内置角色不允许删除"}
	// ErrRoleInUse 角色仍被用户使用。
	ErrRoleInUse = &Err{Code: 4027, HTTP: http.StatusConflict, Msg: "角色仍在使用中，无法删除"}
	// ErrPermissionNotExist 权限不存在。
	ErrPermissionNotExist = &Err{Code: 4028, HTTP: http.StatusNotFound, Msg: "权限不存在"}
	// ErrPermissionNameExists 权限名已存在。
	ErrPermissionNameExists = &Err{Code: 4029, HTTP: http.StatusBadRequest, Msg: "权限名称已存在"}
	// ErrPermissionInUse 权限仍被角色引用。
	ErrPermissionInUse = &Err{Code: 4030, HTTP: http.StatusConflict, Msg: "权限仍被角色引用，无法删除"}

	// ---- 依赖不可用（503）：数据库/Redis 降级 ----
	// ErrDependencyUnavailable 依赖服务不可用（骨架懒连接语义下端点返回 5xx）。
	ErrDependencyUnavailable = &Err{Code: 5031, HTTP: http.StatusServiceUnavailable, Msg: "依赖服务不可用，请稍后重试"}
)

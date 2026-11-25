// 用户域 / RBAC 错误码（i18n 版）。
//
// 业务码段位：2 万段（认证用户）+ 7 万段（RBAC），5 位数字。
// 本文件不再含任何中文文案 —— 文案在 internal/pkg/i18n/en.go。
package errcode

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// 认证与用户域业务码（20000 段）。
const (
	BizNeedAuth            = 20001
	BizPermVersionStale    = 20002
	BizNoPermission        = 20003
	BizInvalidParamUser    = 20004
	BizMembershipDup       = 20005
	BizUserNotExist        = 20006
	BizPasswordError       = 20007
	BizUsernameExists      = 20008
	BizEmailExists         = 20009
	BizPhoneExists         = 20010
	BizVerifyCodeInvalid   = 20011
	BizVerifyCodeExpired   = 20012
	BizVerifyCodeTooOften  = 20013
	BizRefreshInvalid      = 20014
	BizRefreshExpired      = 20015
	BizDeviceIDRequired    = 20016
	BizFollowSelf          = 20017
	BizSignInAlready       = 20018
	BizInsufficientBalance = 20019
	BizDeletionPending     = 20020
	BizDeletionNotPending  = 20021
	BizAccountDeleted      = 20022
	BizCertificationExists = 20023
	BizCertNotFound        = 20024
	BizCertNotPending      = 20025
	BizOAuthNotConfigured  = 20026
	// 第三方账号绑定管理（T-37e）
	BizOAuthUnbindLastMethod = 20028 // 解绑后无任何登录方式
	BizOAuthBindingNotFound  = 20029 // 绑定不存在
	BizOAuthBindingConflict  = 20030 // openid 已被其他用户绑定
	BizOAuthStateInvalid     = 20031 // OAuth state 无效 / 已用过（链接过期或被重放）
	BizAccountBanned         = 20027
)

// RBAC 业务码（70000 段）。
const (
	BizRoleNameExists     = 70001
	BizRoleNotExist       = 70002
	BizRoleIsSystem       = 70003
	BizRoleInUse          = 70004
	BizPermissionNotExist = 70005
	BizPermissionNameUsed = 70006
	BizPermissionInUse    = 70007
)

var (
	// ErrNeedAuth 未认证。token 缺失/失效/被黑名单。
	ErrNeedAuth = &Err{BizCode: BizNeedAuth, HTTP: http.StatusUnauthorized, Key: i18n.AuthNeedLogin}
	// ErrPermVersionStale 权限版本过期：权限被改动，客户端需重新登录。
	ErrPermVersionStale = &Err{BizCode: BizPermVersionStale, HTTP: http.StatusUnauthorized, Key: i18n.AuthPermVersionStale}
	// ErrNoPermission 无权限。
	ErrNoPermission = &Err{BizCode: BizNoPermission, HTTP: http.StatusForbidden, Key: i18n.CommonForbidden}

	// ErrMembershipDuplicated 会员重复开通。
	ErrMembershipDuplicated = &Err{BizCode: BizMembershipDup, HTTP: http.StatusConflict, Key: i18n.MembershipDuplicated}

	// ---- 用户域细分 ----
	// ErrUserNotExist 用户不存在。
	ErrUserNotExist = &Err{BizCode: BizUserNotExist, HTTP: http.StatusNotFound, Key: i18n.UserNotFound}
	// ErrPasswordError 密码错误。
	ErrPasswordError = &Err{BizCode: BizPasswordError, HTTP: http.StatusBadRequest, Key: i18n.AuthBadCredentials}
	// ErrUsernameExists 用户名已存在。
	ErrUsernameExists = &Err{BizCode: BizUsernameExists, HTTP: http.StatusConflict, Key: i18n.UserUsernameExists}
	// ErrEmailExists 邮箱已被使用。
	ErrEmailExists = &Err{BizCode: BizEmailExists, HTTP: http.StatusConflict, Key: i18n.UserEmailExists}
	// ErrPhoneExists 手机号已被使用。
	ErrPhoneExists = &Err{BizCode: BizPhoneExists, HTTP: http.StatusConflict, Key: i18n.UserPhoneExists}
	// ErrVerifyCodeInvalid 验证码错误。
	ErrVerifyCodeInvalid = &Err{BizCode: BizVerifyCodeInvalid, HTTP: http.StatusBadRequest, Key: i18n.AuthCaptchaInvalid}
	// ErrVerifyCodeExpired 验证码已过期。
	ErrVerifyCodeExpired = &Err{BizCode: BizVerifyCodeExpired, HTTP: http.StatusGone, Key: i18n.CommonVerifyCodeExpired}
	// ErrVerifyCodeTooOften 验证码请求过于频繁。
	ErrVerifyCodeTooOften = &Err{BizCode: BizVerifyCodeTooOften, HTTP: http.StatusTooManyRequests, Key: i18n.AuthCaptchaTooFrequent}
	// ErrRefreshTokenInvalid refresh token 无效。
	ErrRefreshTokenInvalid = &Err{BizCode: BizRefreshInvalid, HTTP: http.StatusUnauthorized, Key: i18n.AuthRefreshInvalid}
	// ErrRefreshTokenExpired refresh token 过期。
	ErrRefreshTokenExpired = &Err{BizCode: BizRefreshExpired, HTTP: http.StatusUnauthorized, Key: i18n.AuthRefreshExpired}
	// ErrDeviceIDRequired 缺少 device-id 头。
	ErrDeviceIDRequired = &Err{BizCode: BizDeviceIDRequired, HTTP: http.StatusBadRequest, Key: i18n.AuthDeviceIDRequired}
	// ErrFollowSelf 不能关注自己。
	ErrFollowSelf = &Err{BizCode: BizFollowSelf, HTTP: http.StatusForbidden, Key: i18n.UserCannotFollowSelf}
	// ErrSignInAlready 今天已签到。
	ErrSignInAlready = &Err{BizCode: BizSignInAlready, HTTP: http.StatusBadRequest, Key: i18n.PointsAlreadySignedIn}
	// ErrInsufficientBalance 余额不足。
	ErrInsufficientBalance = &Err{BizCode: BizInsufficientBalance, HTTP: http.StatusBadRequest, Key: i18n.PointsInsufficient}
	// ErrDeletionPending 账号已在注销冷静期。
	ErrDeletionPending = &Err{BizCode: BizDeletionPending, HTTP: http.StatusConflict, Key: i18n.UserDeletionPending}
	// ErrDeletionNotPending 账号不在注销冷静期。
	ErrDeletionNotPending = &Err{BizCode: BizDeletionNotPending, HTTP: http.StatusBadRequest, Key: i18n.UserDeletionNotPending}
	// ErrAccountBanned 账号已被封禁。
	ErrAccountBanned = &Err{BizCode: BizAccountBanned, HTTP: http.StatusForbidden, Key: i18n.AuthAccountBanned}
	// ErrAccountDeleted 账号已注销。
	ErrAccountDeleted = &Err{BizCode: BizAccountDeleted, HTTP: http.StatusForbidden, Key: i18n.UserAccountDeleted}
	// ErrCertificationExists 已有待审核或已通过的认证。
	ErrCertificationExists = &Err{BizCode: BizCertificationExists, HTTP: http.StatusConflict, Key: i18n.UserCertificationExists}
	// ErrCertificationNotFound 认证记录不存在。
	ErrCertificationNotFound = &Err{BizCode: BizCertNotFound, HTTP: http.StatusNotFound, Key: i18n.UserCertificationNotFound}
	// ErrCertificationNotPending 认证记录不在待审核状态。
	ErrCertificationNotPending = &Err{BizCode: BizCertNotPending, HTTP: http.StatusConflict, Key: i18n.UserCertificationNotPending}
	// ErrOAuthNotConfigured 第三方登录未配置（任一 provider 的 appid / secret 缺失）。
	//
	// 语义一直是通用的，旧名 ErrGithubOAuthNotConfigured 只在 GitHub 是唯一渠道时
	// 才准确 —— 现在有微信 web/app、QQ web/app 四个渠道，故改名。
	ErrOAuthNotConfigured = &Err{BizCode: BizOAuthNotConfigured, HTTP: http.StatusBadRequest, Key: i18n.AuthOAuthNotConfigured}

	// ErrGithubOAuthNotConfigured 旧名，保留别名避免破坏既有引用。
	//
	// Deprecated: 用 ErrOAuthNotConfigured。
	ErrGithubOAuthNotConfigured = ErrOAuthNotConfigured

	// ErrOAuthUnbindLastMethod 解绑会移除最后一种登录方式 → 403。
	//
	// 判定口径（oauth_service.go Unbind）：手机号 OR 邮箱 OR 其它第三方绑定，
	// **不含 Password**（第三方建号是随机密码，用户不知道）。
	ErrOAuthUnbindLastMethod = &Err{BizCode: BizOAuthUnbindLastMethod, HTTP: http.StatusForbidden, Key: i18n.AuthOAuthUnbindLastMethod}
	// ErrOAuthBindingNotFound 绑定不存在 → 404。
	ErrOAuthBindingNotFound = &Err{BizCode: BizOAuthBindingNotFound, HTTP: http.StatusNotFound, Key: i18n.AuthOAuthBindingNotFound}
	// ErrOAuthBindingConflict 该 openid 已被其他用户绑定 → 409。
	ErrOAuthBindingConflict = &Err{BizCode: BizOAuthBindingConflict, HTTP: http.StatusConflict, Key: i18n.AuthOAuthBindingConflict}
	// ErrOAuthStateInvalid OAuth state 无效 / 已过期 / 已被使用（T-37f）→ 400。
	//
	// 为什么是 400 而不是 401：这不是「认证失败」，而是**这次登录请求的参数已失效**。
	// HTTP 401 会被前端拦截器当成「请重新登录本站」，而这里的正确动作是
	// 「重新点一次第三方登录按钮」，语义完全不同。
	//
	// 为什么必须与 ErrOAuthFailed 分开（2026-11-25 真机实测的缺陷）：
	// 两者同码时，「链接过期」「微信挂了」「有人在伪造 state」三种情况返回完全一样，
	// 前端与运维都无法判断该提示什么、该不该告警。
	ErrOAuthStateInvalid = &Err{BizCode: BizOAuthStateInvalid, HTTP: http.StatusBadRequest, Key: i18n.AuthOAuthStateInvalid}

	// ---- RBAC 细分 ----
	// ErrRoleNameExists 角色名已存在。
	ErrRoleNameExists = &Err{BizCode: BizRoleNameExists, HTTP: http.StatusBadRequest, Key: i18n.RoleNameExists}
	// ErrRoleNotExist 角色不存在。
	ErrRoleNotExist = &Err{BizCode: BizRoleNotExist, HTTP: http.StatusNotFound, Key: i18n.RoleNotFound}
	// ErrRoleIsSystem 系统角色不允许删除。
	ErrRoleIsSystem = &Err{BizCode: BizRoleIsSystem, HTTP: http.StatusForbidden, Key: i18n.RoleIsSystem}
	// ErrRoleInUse 角色仍被用户使用。
	ErrRoleInUse = &Err{BizCode: BizRoleInUse, HTTP: http.StatusConflict, Key: i18n.RoleInUse}
	// ErrPermissionNotExist 权限不存在。
	ErrPermissionNotExist = &Err{BizCode: BizPermissionNotExist, HTTP: http.StatusNotFound, Key: i18n.PermissionNotFound}
	// ErrPermissionNameExists 权限名已存在。
	ErrPermissionNameExists = &Err{BizCode: BizPermissionNameUsed, HTTP: http.StatusBadRequest, Key: i18n.PermissionNameExists}
	// ErrPermissionInUse 权限仍被角色引用。
	ErrPermissionInUse = &Err{BizCode: BizPermissionInUse, HTTP: http.StatusConflict, Key: i18n.PermissionInUse}
)

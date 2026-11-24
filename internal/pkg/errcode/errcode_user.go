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
	BizAccountBanned       = 20027
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
	// ErrGithubOAuthNotConfigured GitHub OAuth 未配置。
	ErrGithubOAuthNotConfigured = &Err{BizCode: BizOAuthNotConfigured, HTTP: http.StatusBadRequest, Key: i18n.AuthOAuthNotConfigured}

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

// Package errcode 业务错误码体系（国际化版）。
//
// 响应契约（本次重构，2026-11-24）：
//
//	{
//	  "data":        <object|null>,
//	  "message":     "<英文提示>",
//	  "status_code": <int>
//	}
//
// 与原 NestJS 仓库的差异（有意为之，用户明确要求）：
//   - 字段名 status_code（snake_case），不再是 code；
//   - 去掉 timestamp / path（原异常过滤器带的，前端用不到）；
//   - message 全英文，来自 i18n 文案表，不再硬编码中文；
//   - 业务细分码（1000+）保留在内部 Err.Code 上，但对外只暴露 status_code
//     （= HTTP 状态码），前端按 status_code 判断成败。
//
// 设计要点：
//   - Err 持 Key + 参数，不持文案 → 换语言只换文案表；
//   - Msg() 现场解析文案，避免在包级变量里固化语言；
//   - With()/Withf() 仍可用：覆盖为一段自定义英文提示（少数需要拼业务数据的场景），
//     此时 Key 置空、走 RawMsg。
package errcode

import (
	"fmt"
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// Err 业务错误描述。
type Err struct {
	Code int // 业务细分码（1000+；对外不暴露，仅日志/内部判断用）
	HTTP int // 实际返回的 HTTP 状态码，也是对外 status_code
	Key  i18n.Key
	Args []interface{}
	// RawMsg 非空时优先于 Key（With/Withf 覆盖的场景，含动态业务数据）。
	RawMsg string
}

// Error 实现 error 接口（供日志使用，取英文文案）。
func (e *Err) Error() string { return e.Msg() }

// Msg 取当前语言下的提示文案。
//
// 优先级：RawMsg > i18n 文案（带参数格式化）> key 原样返回（漏配时）。
func (e *Err) Msg() string {
	if e == nil {
		return i18n.T(i18n.CommonInternal)
	}
	if e.RawMsg != "" {
		return e.RawMsg
	}
	if e.Key != "" {
		return i18n.Tf(e.Key, e.Args...)
	}
	return i18n.T(i18n.CommonInternal)
}

// StatusCode 对外暴露的 status_code（= HTTP 状态码）。
func (e *Err) StatusCode() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	return e.HTTP
}

// 预定义错误码。
//
// 注意：这里只声明 Key，不声明文案 —— 文案在 i18n/en.go，全英文。
var (
	// Success 成功。
	Success = &Err{Code: http.StatusOK, HTTP: http.StatusOK, Key: i18n.CommonOK}

	// ErrBadRequest 参数错误。
	ErrBadRequest = &Err{Code: http.StatusBadRequest, HTTP: http.StatusBadRequest, Key: i18n.CommonBadRequest}
	// ErrUnauthorized 未登录 / token 失效。
	ErrUnauthorized = &Err{Code: http.StatusUnauthorized, HTTP: http.StatusUnauthorized, Key: i18n.CommonUnauthorized}
	// ErrForbidden 无权限（RBAC 校验失败）。
	ErrForbidden = &Err{Code: http.StatusForbidden, HTTP: http.StatusForbidden, Key: i18n.CommonForbidden}
	// ErrNotFound 资源不存在。
	ErrNotFound = &Err{Code: http.StatusNotFound, HTTP: http.StatusNotFound, Key: i18n.CommonNotFound}
	// ErrConflict 资源冲突。
	ErrConflict = &Err{Code: http.StatusConflict, HTTP: http.StatusConflict, Key: i18n.CommonConflict}
	// ErrTooManyRequests 限流。
	ErrTooManyRequests = &Err{Code: http.StatusTooManyRequests, HTTP: http.StatusTooManyRequests, Key: i18n.CommonTooManyRequests}
	// ErrInternal 服务内部错误。
	ErrInternal = &Err{Code: http.StatusInternalServerError, HTTP: http.StatusInternalServerError, Key: i18n.CommonInternal}

	// 业务细分码段（1000+）。
	ErrUserAlreadyExists  = &Err{Code: 1001, HTTP: http.StatusConflict, Key: i18n.UserAlreadyExists}
	ErrBadCredentials     = &Err{Code: 1002, HTTP: http.StatusUnauthorized, Key: i18n.AuthBadCredentials}
	ErrAccountBanned      = &Err{Code: 1003, HTTP: http.StatusForbidden, Key: i18n.AuthAccountBanned}
	ErrTokenExpired       = &Err{Code: 1004, HTTP: http.StatusUnauthorized, Key: i18n.AuthTokenExpired}
	ErrInsufficientWallet = &Err{Code: 1005, HTTP: http.StatusBadRequest, Key: i18n.PointsInsufficient}

	// 认证相关：ErrNeedAuth / ErrNoPermission 见 errcode_user.go（含细分码）。
	ErrTokenInvalid = &Err{Code: 1008, HTTP: http.StatusUnauthorized, Key: i18n.AuthTokenInvalid}
)

// With 用一段自定义英文提示覆盖（用于需要拼接业务数据的场景）。
//
// 优先推荐新增 i18n key + WithKeyf；With 留给确实无法预置文案的少数情况。
func (e *Err) With(msg string) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{Code: e.Code, HTTP: e.HTTP, RawMsg: msg}
}

// Withf 格式化覆盖。
func (e *Err) Withf(format string, args ...interface{}) *Err {
	return e.With(fmt.Sprintf(format, args...))
}

// WithKey 换成另一个 i18n key（保留原 Code/HTTP）。
func (e *Err) WithKey(k i18n.Key) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{Code: e.Code, HTTP: e.HTTP, Key: k}
}

// WithKeyf 换成 i18n key + 参数。
func (e *Err) WithKeyf(k i18n.Key, args ...interface{}) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{Code: e.Code, HTTP: e.HTTP, Key: k, Args: args}
}

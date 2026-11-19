// Package errcode 业务错误码体系。
//
// 设计对齐原 NestJS 仓库（docs/01 §1.2 / business.exception.ts）：
//   - code 直接复用 HTTP 状态码（400/401/403/404/409/429/500），
//     响应体 {code, message, data:null, timestamp, path}；
//   - BusinessException 默认 code=400（HTTP BAD_REQUEST）；
//   - 预留 1000+ 段作为业务细分码（映射到对应 HTTP 状态），
//     供后续批次（支付/审核）在需要前端区分细粒度错误时使用。
package errcode

import (
	"fmt"
	"net/http"
)

// Err 业务错误描述。
type Err struct {
	Code int    // 业务码（对齐原仓库：通常即 HTTP 状态码；1000+ 为业务细分码）
	HTTP int    // 实际返回的 HTTP 状态码
	Msg  string // 提示信息
}

// Error 实现 error 接口。
func (e *Err) Error() string { return e.Msg }

// 预定义错误码（消息沿用原仓库语义，中文提示对齐前端交互）。
var (
	// Success 成功（原仓库 TransformInterceptor 成功 code=200）。
	Success = &Err{Code: http.StatusOK, HTTP: http.StatusOK, Msg: "操作成功"}

	// ErrBadRequest 参数错误（原 BusinessException 默认）。
	ErrBadRequest = &Err{Code: http.StatusBadRequest, HTTP: http.StatusBadRequest, Msg: "请求参数错误"}
	// ErrUnauthorized 未登录 / token 失效。
	ErrUnauthorized = &Err{Code: http.StatusUnauthorized, HTTP: http.StatusUnauthorized, Msg: "未登录或登录已过期"}
	// ErrForbidden 无权限（RBAC 校验失败）。
	ErrForbidden = &Err{Code: http.StatusForbidden, HTTP: http.StatusForbidden, Msg: "没有操作权限"}
	// ErrNotFound 资源不存在。
	ErrNotFound = &Err{Code: http.StatusNotFound, HTTP: http.StatusNotFound, Msg: "资源不存在"}
	// ErrConflict 资源冲突（用户名/邮箱重复等）。
	ErrConflict = &Err{Code: http.StatusConflict, HTTP: http.StatusConflict, Msg: "资源冲突"}
	// ErrTooManyRequests 限流（原 too-many-request.exception.ts）。
	ErrTooManyRequests = &Err{Code: http.StatusTooManyRequests, HTTP: http.StatusTooManyRequests, Msg: "请求过于频繁"}
	// ErrInternal 服务内部错误。
	ErrInternal = &Err{Code: http.StatusInternalServerError, HTTP: http.StatusInternalServerError, Msg: "服务器内部错误"}

	// 业务细分码段（1000+，映射到 HTTP 状态码；按需扩充）。
	ErrUserAlreadyExists  = &Err{Code: 1001, HTTP: http.StatusConflict, Msg: "用户已存在"}
	ErrBadCredentials     = &Err{Code: 1002, HTTP: http.StatusUnauthorized, Msg: "用户名或密码错误"}
	ErrAccountBanned      = &Err{Code: 1003, HTTP: http.StatusForbidden, Msg: "账号已被封禁"}
	ErrTokenExpired       = &Err{Code: 1004, HTTP: http.StatusUnauthorized, Msg: "token 已过期"}
	ErrInsufficientWallet = &Err{Code: 1005, HTTP: http.StatusBadRequest, Msg: "余额不足"}
)

// With 覆盖默认提示信息，返回新错误（不修改预定义值）。
func (e *Err) With(msg string) *Err {
	return &Err{Code: e.Code, HTTP: e.HTTP, Msg: msg}
}

// Withf 格式化覆盖提示信息。
func (e *Err) Withf(format string, args ...interface{}) *Err {
	return e.With(fmt.Sprintf(format, args...))
}

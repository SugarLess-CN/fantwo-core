// Package errcode 业务错误码体系（i18n + 独立业务码契约，2026-11-24 定稿）。
//
// 响应契约：
//
//	{"message":"Article not found","status_code":40401}
//
// status_code 是【自定义业务错误码】，与 HTTP 状态码无关：
//   - 0 = 成功（所有成功响应默认值）
//   - 5 位 = 通用/无域前缀错误（10000~19999）
//   - 6 位 = 带域前缀：2 认证用户 / 3 内容 / 4 交易 / 5 运营 / 6 基础设施 / 7 RBAC
//   - HTTP 状态码仍然标准（404/401/403...），供网关/浏览器/监控使用
//
// Err 结构：
//   - BizCode 对外 status_code（5~6 位业务码）
//   - Key + Args 走 i18n 文案表（不持文案）
//   - RawMsg 是 With/Withf 的英文覆盖出口
package errcode

import (
	"fmt"
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// 业务错误码段位（5~6 位）。
const (
	BizOK          = 0     // 成功（默认）
	BizGenericBase = 10000 // 通用错误段起点
	BizAuthBase    = 20000 // 认证与用户域
	BizContentBase = 30000 // 内容域
	BizTradeBase   = 40000 // 交易域
	BizOpsBase     = 50000 // 运营域
	BizInfraBase   = 60000 // 基础设施域
	BizRBACBase    = 70000 // RBAC
)

// 通用错误码（5 位，10000 段）。
const (
	BizBadRequest      = 10001
	BizUnauthorized    = 10002
	BizForbidden       = 10003
	BizNotFound        = 10004
	BizConflict        = 10005
	BizTooManyRequests = 10006
	BizInternal        = 10007
	BizInvalidParam    = 10008
	BizDependencyDown  = 10009
	BizResourceInUse   = 10010
	BizExpired         = 10011
	BizNotAllow        = 10012
	BizRewardClaimed   = 10013
	BizOperationFailed = 10014
	BizAlreadyExists   = 10015
)

// Err 业务错误描述。
type Err struct {
	BizCode int // 对外 status_code（5~6 位业务码，与 HTTP 无关）
	HTTP    int // HTTP 状态码（404/401/403...，供网关与监控）
	Key     i18n.Key
	Args    []interface{}
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

// StatusCode 对外暴露的 status_code（= BizCode，业务错误码）。
func (e *Err) StatusCode() int {
	if e == nil {
		return BizInternal
	}
	return e.BizCode
}

// 预定义错误码。
//
// 注意：只声明 Key 与 BizCode，不声明文案 —— 文案在 i18n/en.go，全英文。
var (
	// Success 成功（status_code=0）。
	Success = &Err{BizCode: BizOK, HTTP: http.StatusOK, Key: i18n.CommonOK}

	// ErrBadRequest 参数错误（400 / 10001）。
	ErrBadRequest = &Err{BizCode: BizBadRequest, HTTP: http.StatusBadRequest, Key: i18n.CommonBadRequest}
	// ErrUnauthorized 未登录 / token 失效（401 / 10002）。
	ErrUnauthorized = &Err{BizCode: BizUnauthorized, HTTP: http.StatusUnauthorized, Key: i18n.CommonUnauthorized}
	// ErrForbidden 无权限（403 / 10003）。
	ErrForbidden = &Err{BizCode: BizForbidden, HTTP: http.StatusForbidden, Key: i18n.CommonForbidden}
	// ErrNotFound 资源不存在（404 / 10004）。
	ErrNotFound = &Err{BizCode: BizNotFound, HTTP: http.StatusNotFound, Key: i18n.CommonNotFound}
	// ErrConflict 资源冲突（409 / 10005）。
	ErrConflict = &Err{BizCode: BizConflict, HTTP: http.StatusConflict, Key: i18n.CommonConflict}
	// ErrTooManyRequests 限流（429 / 10006）。
	ErrTooManyRequests = &Err{BizCode: BizTooManyRequests, HTTP: http.StatusTooManyRequests, Key: i18n.CommonTooManyRequests}
	// ErrInternal 服务内部错误（500 / 10007）。
	ErrInternal = &Err{BizCode: BizInternal, HTTP: http.StatusInternalServerError, Key: i18n.CommonInternal}
	// ErrInvalidParam 参数格式错误（400 / 10008）。
	ErrInvalidParam = &Err{BizCode: BizInvalidParam, HTTP: http.StatusBadRequest, Key: i18n.CommonInvalidParameter}
	// ErrDependencyUnavailable 依赖服务不可用（503 / 10009）。
	ErrDependencyUnavailable = &Err{BizCode: BizDependencyDown, HTTP: http.StatusServiceUnavailable, Key: i18n.CommonDependencyUnavailable}
	// ErrResourceInUse 资源仍被引用（409 / 10010）。
	ErrResourceInUse = &Err{BizCode: BizResourceInUse, HTTP: http.StatusConflict, Key: i18n.CommonResourceInUse}
	// ErrExpired 已过期（410 / 10011）。
	ErrExpired = &Err{BizCode: BizExpired, HTTP: http.StatusGone, Key: i18n.CommonExpired}
	// ErrNotAllow 不允许的操作（403 / 10012）。
	ErrNotAllow = &Err{BizCode: BizNotAllow, HTTP: http.StatusForbidden, Key: i18n.CommonNotAllowed}
	// ErrRewardClaimed 奖励已领取（409 / 10013）。
	ErrRewardClaimed = &Err{BizCode: BizRewardClaimed, HTTP: http.StatusConflict, Key: i18n.CommonRewardAlreadyClaimed}
	// ErrOperationFailed 操作失败（500 / 10014）。
	ErrOperationFailed = &Err{BizCode: BizOperationFailed, HTTP: http.StatusInternalServerError, Key: i18n.CommonOperationFailed}
	// ErrAlreadyExists 已存在（409 / 10015）。
	ErrAlreadyExists = &Err{BizCode: BizAlreadyExists, HTTP: http.StatusConflict, Key: i18n.CommonAlreadyExists}
)

// 基础设施域业务码（60000 段）。
const (
	BizInfraUnavailable = 60001
	BizSearchInit       = 60002
)

// With 用一段自定义英文提示覆盖（用于需要拼接业务数据的场景）。
//
// 优先推荐 WithKeyf；With 留给确实无法预置文案的少数情况。
func (e *Err) With(msg string) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{BizCode: e.BizCode, HTTP: e.HTTP, RawMsg: msg}
}

// Withf 格式化覆盖。
func (e *Err) Withf(format string, args ...interface{}) *Err {
	return e.With(fmt.Sprintf(format, args...))
}

// WithKey 换成另一个 i18n key（保留原 BizCode/HTTP）。
func (e *Err) WithKey(k i18n.Key) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{BizCode: e.BizCode, HTTP: e.HTTP, Key: k}
}

// WithKeyf 换成 i18n key + 参数。
func (e *Err) WithKeyf(k i18n.Key, args ...interface{}) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{BizCode: e.BizCode, HTTP: e.HTTP, Key: k, Args: args}
}

// WithBizCode 覆盖业务码（域专属细分码场景）。
func (e *Err) WithBizCode(code int) *Err {
	if e == nil {
		e = ErrInternal
	}
	return &Err{BizCode: code, HTTP: e.HTTP, Key: e.Key, Args: e.Args, RawMsg: e.RawMsg}
}

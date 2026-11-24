// Package response 统一响应包装（国际化契约，2026-11-24 重构）。
//
// 响应契约（全部接口一致，成功与失败同构）：
//
//	{
//	  "data":        <object|null>,
//	  "message":     "Success",
//	  "status_code": 200
//	}
//
// 设计变更（相对原 NestJS 契约）：
//   - 字段名 status_code（snake_case），替代原 code；
//   - 去掉 timestamp / path —— 前端不需要，日志中间件已记录；
//   - message 一律英文，来自 internal/pkg/i18n 文案表；
//   - **原先塞在 data 里的 message 一律提到顶层**：
//     旧：{"code":200,"message":"Success","data":{"success":true,"message":"Logged out"}}
//     新：{"data":{"success":true},"message":"Logged out","status_code":200}
//     成功提示走顶层 message，data 只放业务数据。
//   - HTTP 状态码与 status_code 保持一致（不出现 HTTP 200 + status_code 500 的分裂）。
package response

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/gin-gonic/gin"
)

// Body 统一响应体。
//
// 字段顺序固定为 data / message / status_code，便于日志与前端解析。
type Body struct {
	Data       interface{} `json:"data"`
	Message    string      `json:"message"`
	StatusCode int         `json:"status_code"`
}

// Meta 分页元信息。
type Meta struct {
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int64 `json:"totalPages"`
}

// PageData 分页数据载荷。
type PageData struct {
	List interface{} `json:"list"`
	Meta Meta        `json:"meta"`
}

// Success 成功响应：{data, message:"Success", status_code:200}。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    i18n.T(i18n.CommonOK),
		StatusCode: http.StatusOK,
	})
}

// SuccessMsg 成功响应，并指定顶层提示（业务动作类接口用）。
//
// 用在「操作成功但需要告知具体结果」的场景，例如登出、发送验证码。
// 原先这类提示塞在 data 里（{"success":true,"message":"Logged out"}），
// 现在统一提到顶层 message。
func SuccessMsg(c *gin.Context, key i18n.Key, data interface{}, args ...interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    i18n.Tf(key, args...),
		StatusCode: http.StatusOK,
	})
}

// SuccessPage 分页成功响应。
func SuccessPage(c *gin.Context, list interface{}, total int64, page, limit int) {
	if limit <= 0 {
		limit = 20 // 防除零
	}
	tp := total / int64(limit)
	if total%int64(limit) > 0 {
		tp++
	}
	Success(c, PageData{
		List: list,
		Meta: Meta{Total: total, Page: page, Limit: limit, TotalPages: tp},
	})
}

// Health 健康检查响应：{data, message:"OK", status_code:0}。
//
// 保留 status_code:0 的骨架约定 —— /health 是运维探针端点，
// 与业务接口的 200 语义不同（0 = 探针专用成功），前端不会调用它。
func Health(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    "OK",
		StatusCode: 0,
	})
}

// Fail 失败响应：{data:null, message, status_code}。
//
// HTTP 状态取 err.HTTP，与 status_code 一致；Abort 阻止后续 handler 继续写响应。
func Fail(c *gin.Context, e *errcode.Err) {
	if e == nil {
		e = errcode.ErrInternal
	}
	c.AbortWithStatusJSON(e.HTTP, Body{
		Data:       nil,
		Message:    e.Msg(),
		StatusCode: e.HTTP,
	})
}

// FailKey 用 i18n key 构造失败响应（推荐，业务代码不必持有文案）。
func FailKey(c *gin.Context, e *errcode.Err, key i18n.Key, args ...interface{}) {
	Fail(c, e.WithKeyf(key, args...))
}

// FailWith 覆盖提示信息的 Fail。
//
// 注意：msg 必须是英文。优先用 FailKey；这里留给确实无法预置文案的场景。
func FailWith(c *gin.Context, e *errcode.Err, msg string) {
	Fail(c, e.With(msg))
}

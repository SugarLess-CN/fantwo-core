// Package response 统一响应包装，对齐原 NestJS 仓库：
//
//   - 成功（TransformInterceptor）：{code:200, message:"操作成功", data}
//   - 失败（HttpExceptionFilter）：{code, message, data:null, timestamp, path}
//   - 分页（PaginatedResponseDto）：data 为 {list, meta:{total,page,limit,totalPages}}
//
// code 体系见 internal/pkg/errcode（成功 200；异常码即 HTTP 状态码；
// health/ping 端点按骨架约定返回 code:0）。
package response

import (
	"net/http"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/gin-gonic/gin"
)

// Body 统一响应体。
type Body struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp,omitempty"` // 仅错误体携带（对齐原异常过滤器）
	Path      string      `json:"path,omitempty"`
}

// Meta 分页元信息（对齐原 PaginatedResponseDto.data.meta）。
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

// Success 成功响应：{code:200, message:"操作成功", data}。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 200, Message: "操作成功", Data: data})
}

// SuccessPage 分页成功响应。
func SuccessPage(c *gin.Context, list interface{}, total int64, page, limit int) {
	tp := total / int64(limit)
	if total%int64(limit) > 0 {
		tp++
	}
	Success(c, PageData{
		List: list,
		Meta: Meta{Total: total, Page: page, Limit: limit, TotalPages: tp},
	})
}

// Health 成功响应（code:0）：骨架约定的 health/ping 专用。
func Health(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

// Fail 业务错误响应：HTTP 状态取 err.HTTP，体 {code, message, data:null, timestamp, path}。
func Fail(c *gin.Context, e *errcode.Err) {
	if e == nil {
		e = errcode.ErrInternal
	}
	c.AbortWithStatusJSON(e.HTTP, Body{
		Code:      e.Code,
		Message:   e.Msg,
		Data:      nil,
		Timestamp: time.Now().UnixMilli(),
		Path:      c.Request.URL.Path,
	})
}

// FailWith 覆盖提示信息的 Fail。
func FailWith(c *gin.Context, e *errcode.Err, msg string) {
	Fail(c, e.With(msg))
}

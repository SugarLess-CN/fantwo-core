// Package response 统一响应包装（i18n + 业务错误码 + 链路追踪，2026-11-24 定稿）。
//
// 响应契约：
//
//	成功：{"message":"Success","status_code":0,"data":{...},"trace_id":"...","timestamp":...}
//	失败：{"message":"Article not found","status_code":40401,"trace_id":"...","timestamp":...}
//	健康：{"status_code":0}
//
// 规则（用户 2026-11-24 定稿，写进 AGENTS.md）：
//  1. status_code 是【自定义业务错误码】，与 HTTP 状态码无关；
//     5~6 位数字；0 = 成功（默认）。
//  2. **字段缺省**：data 为空（nil）时整个 data 字段不出现在 JSON 里；
//     message 为空串时整个 message 字段也不出现。
//  3. message 一律英文，来自 internal/pkg/i18n 文案表。
//  4. HTTP 状态码仍然是标准的（404/401/403...），供网关/浏览器/监控使用；
//     status_code 才是前端判断业务成败的依据。
//  5. **trace_id + timestamp 恒在**：每个响应都带，便于日志关联与前端上报定位。
//     trace_id 从 gin.Context 取（中间件生成/透传），无则自动生成 UUID。
//
// 错误码段位规划（5~6 位）：
//
//	    0                    成功
//	10000 ~ 19999            通用错误（参数/认证/权限/限流/内部）
//	20000 ~ 29999            认证与用户域
//	30000 ~ 39999            内容域（文章/评论/合集/分类标签）
//	40000 ~ 49999            交易域（订单/支付/钱包/会员）
//	50000 ~ 59999            运营域（积分/成就/装饰品/邀请/举报）
//	60000 ~ 69999            基础设施域（上传/消息/配置/搜索/审核）
//	70000 ~ 79999            RBAC（角色/权限）
//	90000+                   预留
package response

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TraceIDKey gin.Context 里存 trace_id 的 key（中间件生成 / response 读取）。
const TraceIDKey = "X-Trace-Id"

// Body 统一响应体。
//
// 零值省略：Data / Message 为空时省略；TraceID / Timestamp / StatusCode 恒在。
type Body struct {
	Data       interface{} `json:"data,omitempty"`
	Message    string      `json:"message,omitempty"`
	StatusCode int         `json:"status_code"`
	TraceID    string      `json:"trace_id"`
	Timestamp  int64       `json:"timestamp"`
}

// MarshalJSON 自定义序列化：data 为 nil 或空对象时省略整个字段；
// trace_id / timestamp 恒在。
func (b Body) MarshalJSON() ([]byte, error) {
	type alias struct {
		Message    string      `json:"message,omitempty"`
		StatusCode int         `json:"status_code"`
		Data       interface{} `json:"data,omitempty"`
		TraceID    string      `json:"trace_id"`
		Timestamp  int64       `json:"timestamp"`
	}
	a := alias{Message: b.Message, StatusCode: b.StatusCode, Data: b.Data,
		TraceID: b.TraceID, Timestamp: b.Timestamp}
	if isEmptyData(b.Data) {
		a.Data = nil // 触发 omitempty
	}
	return json.Marshal(a)
}

// isEmptyData 判断 data 是否「空到不值得返回」。
func isEmptyData(v interface{}) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

// traceOf 从 gin.Context 取 trace_id；无则自动生成 UUID。
//
// 中间件（request-logger / trace）应在请求入口生成并 Set 到 Context；
// 这里兜底保证响应必有 trace_id。
func traceOf(c *gin.Context) string {
	if v, ok := c.Get(TraceIDKey); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// 兜底：从请求头取（上游网关透传场景）
	if h := c.GetHeader(TraceIDKey); h != "" {
		return h
	}
	return uuid.NewString()
}

// Meta 分页元信息。
type Meta struct {
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int64 `json:"total_pages"`
}

// PageData 分页数据载荷。
type PageData struct {
	List interface{} `json:"list"`
	Meta Meta        `json:"meta"`
}

// Success 成功响应：data 非空时返回 {"data":...,"message":"Success",...}，
// data 为空时 {"message":"Success",...}。status_code 恒 0。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    string(i18n.CommonOK),
		StatusCode: 0,
		TraceID:    traceOf(c),
		Timestamp:  time.Now().UnixMilli(),
	})
}

// SuccessMsg 成功响应，并指定顶层提示（业务动作类接口用）。
//
// message 返回 **key 本身**（用户 2026-11-27 指定），翻译由前端按语言包处理。
func SuccessMsg(c *gin.Context, key i18n.Key, data interface{}, args ...interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    string(key),
		StatusCode: 0,
		TraceID:    traceOf(c),
		Timestamp:  time.Now().UnixMilli(),
	})
}

// SuccessCode 成功响应，带自定义业务码（极少用；默认成功一律 0）。
func SuccessCode(c *gin.Context, code int, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Data:       data,
		Message:    string(i18n.CommonOK),
		StatusCode: code,
		TraceID:    traceOf(c),
		Timestamp:  time.Now().UnixMilli(),
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

// Health 健康检查响应：{"status_code":0}（探针端点，data/message/trace_id 全省略）。
func Health(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{StatusCode: 0})
}

// Fail 失败响应：{"message","status_code","trace_id","timestamp"}，data 省略。
//
// HTTP 状态码独立于 status_code（HTTP 供网关/监控，status_code 供前端判断业务）；
// Abort 阻止后续 handler 继续写响应。
func Fail(c *gin.Context, e *errcode.Err) {
	if e == nil {
		e = errcode.ErrInternal
	}
	c.AbortWithStatusJSON(e.HTTP, Body{
		Message:    e.Msg(),
		StatusCode: e.BizCode,
		TraceID:    traceOf(c),
		Timestamp:  time.Now().UnixMilli(),
	})
}

// FailKey 用 i18n key 构造失败响应（推荐，业务代码不必持有文案）。
func FailKey(c *gin.Context, e *errcode.Err, key i18n.Key, args ...interface{}) {
	Fail(c, e.WithKeyf(key, args...))
}

// FailWith 覆盖提示信息的 Fail。msg 必须是英文。
func FailWith(c *gin.Context, e *errcode.Err, msg string) {
	Fail(c, e.With(msg))
}

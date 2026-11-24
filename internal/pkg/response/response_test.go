// Package response 统一响应包装测试（i18n 契约版）。
//
// 防的 bug（前端契约，改错即全员白屏）：
//   - 响应体字段名不对（必须是 data / message / status_code）；
//   - message 不是英文 / 是空串；
//   - status_code 与 HTTP 状态不一致 → 浏览器/网关按 HTTP 判断，
//     前端按 status_code 判断，两边打架；
//   - Fail 没有 Abort → 后续 handler 继续写响应，产生 superfluous WriteHeader；
//   - 分页 totalPages 向下取整（total=11,limit=10 → 1 页，最后一页永远取不到）；
//   - limit=0 时除零 panic；
//   - data 里残留 message 字段（本次重构要求提到顶层）。
package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newCtx(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应体不是合法 JSON: %v（原文：%s）", err, w.Body.String())
	}
	return m
}

// TestContract_FieldNames 契约核心：只有 data / message / status_code 三个字段。
//
// 旧的 code / timestamp / path 一律不得出现。
func TestContract_FieldNames(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article/a-1")
	Success(c, map[string]string{"id": "a-1"})
	m := decode(t, w)

	for _, k := range []string{"data", "message", "status_code", "trace_id", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Errorf("响应体缺少字段 %q：%s", k, w.Body.String())
		}
	}
	for _, k := range []string{"code", "path"} {
		if _, ok := m[k]; ok {
			t.Errorf("响应体不应再有字段 %q（已重构）：%s", k, w.Body.String())
		}
	}
	if len(m) != 5 {
		t.Errorf("响应体应恰好 5 个字段，实际 %d 个：%s", len(m), w.Body.String())
	}
}

// TestSuccess 成功响应：status_code=200，message=Success，data 原样。
func TestSuccess(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article/a-1")
	Success(c, map[string]string{"id": "a-1"})

	if w.Code != http.StatusOK {
		t.Errorf("HTTP：得到 %d，期望 200", w.Code)
	}
	m := decode(t, w)
	if got := m["status_code"].(float64); got != 0 {
		t.Errorf("status_code：得到 %v，期望 0（成功默认 0）", got)
	}
	if got := m["message"].(string); got != "Success" {
		t.Errorf("message：得到 %q，期望 \"Success\"", got)
	}
	data, ok := m["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 不是对象：%v", m["data"])
	}
	if data["id"] != "a-1" {
		t.Errorf("data.id：得到 %v，期望 a-1", data["id"])
	}
}

// TestSuccessMsg 业务提示走顶层 message，不再塞进 data。
func TestSuccessMsg(t *testing.T) {
	c, w := newCtx(http.MethodPost, "/api/v1/user/logout")
	SuccessMsg(c, i18n.AuthLogoutSuccess, gin.H{"success": true})

	m := decode(t, w)
	if got := m["message"].(string); got != "Logged out" {
		t.Errorf("message：得到 %q，期望 \"Logged out\"", got)
	}
	data := m["data"].(map[string]interface{})
	if _, leaked := data["message"]; leaked {
		t.Errorf("data 里不应再有 message 字段：%v", data)
	}
	if data["success"] != true {
		t.Errorf("data.success 应保留：%v", data)
	}
}

// TestHealth health 端点保留 status_code:0 的骨架约定。
func TestHealth(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/health")
	Health(c, gin.H{"status": "up"})
	m := decode(t, w)
	if got := m["status_code"].(float64); got != 0 {
		t.Errorf("status_code：得到 %v，期望 0", got)
	}
	// Health 契约：data/message 省略；trace_id/timestamp 恒在（非测试 Context 无请求，为空/0 是正常的）
	if _, ok := m["trace_id"]; !ok {
		t.Errorf("Health 响应缺少 trace_id：%s", w.Body.String())
	}
	if _, ok := m["timestamp"]; !ok {
		t.Errorf("Health 响应缺少 timestamp：%s", w.Body.String())
	}
}

// TestFail 失败响应：HTTP=401，status_code=业务码（10002），data 省略。
func TestFail(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/user/profile")
	Fail(c, errcode.ErrUnauthorized)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("HTTP：得到 %d，期望 401", w.Code)
	}
	m := decode(t, w)
	if got := m["status_code"].(float64); got != float64(errcode.BizUnauthorized) {
		t.Errorf("status_code：得到 %v，期望 %d（业务码）", got, errcode.BizUnauthorized)
	}
	if _, exists := m["data"]; exists {
		t.Errorf("失败响应 data 应省略（空数据不返回），body=%s", w.Body.String())
	}
	if got := m["message"].(string); got != "Unauthorized" {
		t.Errorf("message：得到 %q，期望 \"Unauthorized\"", got)
	}
	if !c.IsAborted() {
		t.Error("Fail 应调用 Abort，阻止后续 handler 继续写响应")
	}
}

// TestFailKey 用 i18n key 构造失败响应。
func TestFailKey(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article/x")
	FailKey(c, errcode.ErrNotFound, i18n.ArticleNotFound)
	m := decode(t, w)
	if got := m["message"].(string); got != "Article not found" {
		t.Errorf("message：得到 %q，期望 \"Article not found\"", got)
	}
}

// TestFailWith 自定义提示覆盖。
func TestFailWith(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article/x")
	FailWith(c, errcode.ErrNotFound, "Article not found")
	m := decode(t, w)
	if got := m["message"].(string); got != "Article not found" {
		t.Errorf("message：得到 %q", got)
	}
}

// TestFail_NilErr 传 nil 不应 panic，回退 500。
func TestFail_NilErr(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/x")
	Fail(c, nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("HTTP：得到 %d，期望 500", w.Code)
	}
}

// TestSuccessPage totalPages 必须向上取整。
func TestSuccessPage(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article")
	SuccessPage(c, []int{1, 2}, 11, 2, 10)

	m := decode(t, w)
	data := m["data"].(map[string]interface{})
	meta := data["meta"].(map[string]interface{})
	if got := meta["totalPages"].(float64); got != 2 {
		t.Errorf("totalPages：得到 %v，期望 2（11 条 / 每页 10 = 2 页）", got)
	}
}

// TestSuccessPage_ZeroLimit limit=0 不得除零 panic。
func TestSuccessPage_ZeroLimit(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article")
	SuccessPage(c, []int{}, 0, 1, 0) // 不 panic 即通过
	if w.Code != http.StatusOK {
		t.Errorf("HTTP：得到 %d", w.Code)
	}
}

// TestAllMessagesEnglish 所有预定义错误码的 message 必须是英文（无中文）。
func TestAllMessagesEnglish(t *testing.T) {
	errs := []*errcode.Err{
		errcode.ErrBadRequest, errcode.ErrUnauthorized, errcode.ErrForbidden,
		errcode.ErrNotFound, errcode.ErrConflict, errcode.ErrTooManyRequests,
		errcode.ErrInternal, errcode.ErrAlreadyExists, errcode.ErrPasswordError,
	}
	for _, e := range errs {
		msg := e.Msg()
		if msg == "" {
			t.Errorf("错误码 %d 的 message 是空串", e.BizCode)
		}
		if strings.ContainsFunc(msg, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff }) {
			t.Errorf("错误码 %d 的 message 含中文：%q", e.BizCode, msg)
		}
	}
}

// TestPresetNotPolluted With 不得污染预定义错误码。
func TestPresetNotPolluted(t *testing.T) {
	before := errcode.ErrNotFound.Msg()
	_ = errcode.ErrNotFound.With("custom message")
	if after := errcode.ErrNotFound.Msg(); after != before {
		t.Errorf("With 污染了预定义错误码：%q → %q", before, after)
	}
}

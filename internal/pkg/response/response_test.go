// Package response 统一响应包装测试。
//
// 防的 bug（前端契约，改错即全员白屏）：
//   - 成功体少了 code/message 字段，或 code 不是 200 → 前端 axios 拦截器判失败；
//   - 失败体缺 timestamp/path → 前端上报无法定位；
//   - 失败体 HTTP 状态与 code 不一致 → 浏览器/网关按 HTTP 判断，前端按 code 判断，两边打架；
//   - Fail 没有 Abort → 后续 handler 继续写响应，产生 "superfluous WriteHeader" 或数据串包；
//   - 分页 totalPages 向下取整（total=11,limit=10 → 1 页，最后一页永远取不到）；
//   - limit=0 时除零 panic。
package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newCtx 构造测试用 gin 上下文（路径可通过 query 影响断言）。
func newCtx(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

// decode 解出响应体为 map，便于逐字段断言（不依赖结构体 tag）。
func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应体不是合法 JSON: %v（原文：%s）", err, w.Body.String())
	}
	return m
}

// TestSuccess_Contract 成功响应契约：{code:200, message:"操作成功", data}。
// 不带 timestamp/path（对齐原 TransformInterceptor）。
func TestSuccess_Contract(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/article/a-1")
	Success(c, map[string]string{"id": "a-1"})

	if w.Code != http.StatusOK {
		t.Errorf("HTTP：得到 %d，期望 200", w.Code)
	}
	m := decode(t, w)

	if got := m["code"].(float64); got != 200 {
		t.Errorf("code：得到 %v，期望 200（前端拦截器按此判断成功）", got)
	}
	if got := m["message"].(string); got != "操作成功" {
		t.Errorf("message：得到 %q，期望 \"操作成功\"", got)
	}
	data, ok := m["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 不是对象：%T", m["data"])
	}
	if data["id"] != "a-1" {
		t.Errorf("data.id：得到 %v", data["id"])
	}
	// 成功体不应携带错误专用字段
	if _, exists := m["timestamp"]; exists {
		t.Error("成功体不应带 timestamp（对齐原 TransformInterceptor）")
	}
	if _, exists := m["path"]; exists {
		t.Error("成功体不应带 path")
	}
}

// TestSuccess_NilData data 为 nil 时仍输出 "data": null（不是省略字段）。
// 防的是：前端读 data.xxx 时遇到 undefined 报错。
func TestSuccess_NilData(t *testing.T) {
	c, w := newCtx(http.MethodDelete, "/api/v1/article/a-1")
	Success(c, nil)

	m := decode(t, w)
	data, exists := m["data"]
	if !exists {
		t.Fatal("data 字段被省略（前端解构会崩）")
	}
	if data != nil {
		t.Errorf("data 应为 null，得到 %v", data)
	}
}

// TestHealth_CodeZero health/ping 专用 code:0。
// 防的是：探针拿 200 判成功，但业务前端把 code:0 当异常（约定与业务码分离）。
func TestHealth_CodeZero(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/health")
	Health(c, map[string]string{"status": "up"})

	if w.Code != http.StatusOK {
		t.Errorf("HTTP：得到 %d，期望 200（探针语义：依赖抖动也不返回 5xx）", w.Code)
	}
	m := decode(t, w)
	if got := m["code"].(float64); got != 0 {
		t.Errorf("code：得到 %v，期望 0", got)
	}
	if got := m["message"].(string); got != "ok" {
		t.Errorf("message：得到 %q，期望 \"ok\"", got)
	}
}

// TestFail_Contract 失败响应契约：HTTP = err.HTTP，体 {code,message,data:null,timestamp,path}。
func TestFail_Contract(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/api/v1/user/profile")
	Fail(c, errcode.ErrUnauthorized)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("HTTP：得到 %d，期望 401", w.Code)
	}
	m := decode(t, w)

	if got := m["code"].(float64); got != 401 {
		t.Errorf("code：得到 %v，期望 401", got)
	}
	if got := m["message"].(string); got != errcode.ErrUnauthorized.Msg {
		t.Errorf("message：得到 %q，期望 %q", got, errcode.ErrUnauthorized.Msg)
	}
	if d, exists := m["data"]; !exists || d != nil {
		t.Errorf("data 应为 null 且字段存在，得到 %v (exists=%v)", d, exists)
	}
	if _, exists := m["timestamp"]; !exists {
		t.Error("失败体缺 timestamp（前端上报无法定位时间）")
	}
	if got := m["path"].(string); got != "/api/v1/user/profile" {
		t.Errorf("path：得到 %q，期望请求路径", got)
	}
}

// TestFail_NilErrFallsBackToInternal nil 错误回退 500，不能 panic。
// 防的是：service 返回 (nil, nil) 或错误被吞后进入 Fail，空指针 panic 变 500 白页。
func TestFail_NilErrFallsBackToInternal(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/x")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Fail(nil) panic 了: %v", r)
		}
	}()
	Fail(c, nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("nil 错误应回退 500，得到 %d", w.Code)
	}
	m := decode(t, w)
	if got := m["code"].(float64); got != 500 {
		t.Errorf("code：得到 %v，期望 500", got)
	}
}

// TestFail_BusinessCodeKeepsItsHTTP 1000+ 业务码：code 用业务码，HTTP 用映射值。
// 这是两条并行契约，混淆会让前端分段判断失效。
func TestFail_BusinessCodeKeepsItsHTTP(t *testing.T) {
	c, w := newCtx(http.MethodPost, "/api/v1/user/register")
	Fail(c, errcode.ErrUserAlreadyExists)

	if w.Code != http.StatusConflict {
		t.Errorf("HTTP：得到 %d，期望 409", w.Code)
	}
	m := decode(t, w)
	if got := m["code"].(float64); got != float64(errcode.ErrUserAlreadyExists.Code) {
		t.Errorf("code：得到 %v，期望 %d（业务码不能被 HTTP 码覆盖）",
			got, errcode.ErrUserAlreadyExists.Code)
	}
}

// TestFail_AbortsChain Fail 必须中断后续 handler。
//
// 防的是：Fail 后 handler 继续执行并写第二份响应，
// gin 会报 "superfluous response.WriteHeader call"，前端收到拼接的坏 JSON。
func TestFail_AbortsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var afterCalled bool

	r.GET("/t", func(c *gin.Context) {
		Fail(c, errcode.ErrForbidden)
		// 此处若控制器继续往下走就会被记录；Abort 应让后续 handler 不执行
	}, func(c *gin.Context) {
		afterCalled = true
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))

	if afterCalled {
		t.Error("Fail 未中断链路：后续 handler 仍执行了（会产生 superfluous WriteHeader）")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("HTTP：得到 %d，期望 403", w.Code)
	}
}

// TestFailWith 覆盖提示语，code/HTTP 保持原样。
func TestFailWith(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/x")
	FailWith(c, errcode.ErrNotFound, "文章不存在")

	if w.Code != http.StatusNotFound {
		t.Errorf("HTTP：得到 %d，期望 404", w.Code)
	}
	m := decode(t, w)
	if got := m["message"].(string); got != "文章不存在" {
		t.Errorf("message：得到 %q，期望覆盖值", got)
	}
	if got := m["code"].(float64); got != 404 {
		t.Errorf("code：得到 %v，期望 404（FailWith 不应改 code）", got)
	}
	if errcode.ErrNotFound.Msg != "资源不存在" {
		t.Errorf("FailWith 污染了预定义错误码：%q", errcode.ErrNotFound.Msg)
	}
}

// TestSuccessPage_TotalPagesCeiling 分页 totalPages 必须向上取整。
//
// 防的是：total=11, limit=10 算出 1 页 → 最后一页（第 2 页的 1 条）永远取不到。
func TestSuccessPage_TotalPagesCeiling(t *testing.T) {
	cases := []struct {
		name        string
		total       int64
		page, limit int
		wantPages   int64
	}{
		{"整除", 20, 1, 10, 2},
		{"有余数必须进位", 11, 1, 10, 2},
		{"总量小于一页", 3, 1, 10, 1},
		{"总量为零", 0, 1, 10, 0},
		{"恰好一页", 10, 1, 10, 1},
		{"单页 limit=1", 5, 1, 1, 5},
		{"大数", 1000, 1, 100, 10},
		{"1001 条", 1001, 1, 100, 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newCtx(http.MethodGet, "/list")
			SuccessPage(c, []string{"item"}, tc.total, tc.page, tc.limit)

			m := decode(t, w)
			data, ok := m["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("data 不是对象：%T", m["data"])
			}
			meta, ok := data["meta"].(map[string]interface{})
			if !ok {
				t.Fatalf("data.meta 不是对象：%T", data["meta"])
			}
			if got := int64(meta["totalPages"].(float64)); got != tc.wantPages {
				t.Errorf("totalPages：得到 %d，期望 %d（total=%d limit=%d）",
					got, tc.wantPages, tc.total, tc.limit)
			}
			if got := int64(meta["total"].(float64)); got != tc.total {
				t.Errorf("total：得到 %d，期望 %d", got, tc.total)
			}
			if got := int(meta["page"].(float64)); got != tc.page {
				t.Errorf("page：得到 %d，期望 %d", got, tc.page)
			}
			if got := int(meta["limit"].(float64)); got != tc.limit {
				t.Errorf("limit：得到 %d，期望 %d", got, tc.limit)
			}
			// list 字段必须存在
			if _, exists := data["list"]; !exists {
				t.Error("data.list 缺失")
			}
		})
	}
}

// TestSuccessPage_MetaFieldNamesCamelCase meta 字段名必须是 camelCase。
// 防的是：写成 total_pages，前端读到 undefined（T-13 口径：对外 JSON 一律 camelCase）。
func TestSuccessPage_MetaFieldNamesCamelCase(t *testing.T) {
	c, w := newCtx(http.MethodGet, "/list")
	SuccessPage(c, []int{1, 2}, 2, 1, 10)

	raw := w.Body.String()
	for _, want := range []string{`"totalPages"`, `"total"`, `"page"`, `"limit"`, `"list"`} {
		if !contains(raw, want) {
			t.Errorf("响应体缺少 %s（对外 JSON 必须 camelCase）：%s", want, raw)
		}
	}
	if contains(raw, "total_pages") {
		t.Errorf("响应体出现 snake_case total_pages（违反 camelCase 口径）：%s", raw)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

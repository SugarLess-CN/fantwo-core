package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/gin-gonic/gin"
)

// userDomainRoutes 用户域路由勾销表（对齐 docs/02 四个 tag）。
//
//	用户管理 46 + 用户认证 7（含在 certification 组）+ 角色管理 10 + 权限管理 5
//
// 说明：docs/02 里用户管理 46 条已包含 certification 的 7 条（apply/status/
// identity/identityOf/admin 列表/admin 审核/admin 撤销），此处按 swagger 路径
// 全量列出，测试逐条比对是否注册成功。
var userDomainRoutes = [][2]string{
	// ---- 用户管理 46 ----
	{"GET", "/api/v1/user"},
	{"POST", "/api/v1/user"},
	{"GET", "/api/v1/user/certification/admin/applications"},
	{"PATCH", "/api/v1/user/certification/admin/applications/:id/audit"},
	{"POST", "/api/v1/user/certification/admin/applications/:id/revoke"},
	{"POST", "/api/v1/user/certification/apply"},
	{"GET", "/api/v1/user/certification/identity"},
	{"GET", "/api/v1/user/certification/identity/:user_id"},
	{"GET", "/api/v1/user/certification/status"},
	{"POST", "/api/v1/user/commission/calculate"},
	{"GET", "/api/v1/user/commission/config"},
	{"POST", "/api/v1/user/commission/config"},
	{"GET", "/api/v1/user/config"},
	{"PATCH", "/api/v1/user/config"},
	{"PATCH", "/api/v1/user/config/commission"},
	{"PATCH", "/api/v1/user/config/notifications"},
	{"POST", "/api/v1/user/email/verification"},
	{"POST", "/api/v1/user/login"},
	{"POST", "/api/v1/user/logout"},
	{"POST", "/api/v1/user/membership/batch-check"},
	{"GET", "/api/v1/user/oauth/github/callback"},
	{"POST", "/api/v1/user/oauth/github/callback"},
	{"GET", "/api/v1/user/oauth/github/url"},
	{"POST", "/api/v1/user/password/change"},
	{"POST", "/api/v1/user/password/reset"},
	{"GET", "/api/v1/user/profile"},
	{"PATCH", "/api/v1/user/profile/contact"},
	{"POST", "/api/v1/user/refresh-token"},
	{"POST", "/api/v1/user/register"},
	{"POST", "/api/v1/user/sign-in"},
	{"GET", "/api/v1/user/sign-in/records"},
	{"GET", "/api/v1/user/sign-in/stats"},
	{"GET", "/api/v1/user/wallet/balance"},
	{"GET", "/api/v1/user/wallet/statistics"},
	{"GET", "/api/v1/user/wallet/transactions"},
	{"POST", "/api/v1/user/wallet/withdraw"},
	{"DELETE", "/api/v1/user/:id"},
	{"GET", "/api/v1/user/:id"},
	{"PATCH", "/api/v1/user/:id"},
	{"POST", "/api/v1/user/:id/follow"},
	{"GET", "/api/v1/user/:id/followers"},
	{"GET", "/api/v1/user/:id/followers/count"},
	{"GET", "/api/v1/user/:id/followings"},
	{"GET", "/api/v1/user/:id/followings/count"},
	{"GET", "/api/v1/user/:id/membership/check"},
	{"POST", "/api/v1/user/:id/unfollow"},

	// ---- 角色管理 10 ----
	{"GET", "/api/v1/role"},
	{"POST", "/api/v1/role"},
	{"GET", "/api/v1/role/list"},
	{"GET", "/api/v1/role/active"},
	{"GET", "/api/v1/role/:id"},
	{"PATCH", "/api/v1/role/:id"},
	{"DELETE", "/api/v1/role/:id"},
	{"POST", "/api/v1/role/:id/copy"},
	{"POST", "/api/v1/role/:id/permissions"},
	{"PATCH", "/api/v1/role/:id/status"},

	// ---- 权限管理 5 ----
	{"GET", "/api/v1/permission"},
	{"POST", "/api/v1/permission"},
	{"GET", "/api/v1/permission/:id"},
	{"PATCH", "/api/v1/permission/:id"},
	{"DELETE", "/api/v1/permission/:id"},

	// ---- 任务书别名（/auth/*、/admin/roles*、/admin/permissions/all、/profile*、/account*） ----
	{"POST", "/api/v1/auth/login"},
	{"POST", "/api/v1/auth/register"},
	{"POST", "/api/v1/auth/refresh"},
	{"POST", "/api/v1/auth/logout"},
	{"POST", "/api/v1/auth/logout-all"},
	{"POST", "/api/v1/auth/sms/send"},
	{"POST", "/api/v1/auth/sms/login"},
	{"GET", "/api/v1/profile"},
	{"PATCH", "/api/v1/profile"},
	{"POST", "/api/v1/profile/phone"},
	{"POST", "/api/v1/profile/phone/code"},
	{"GET", "/api/v1/profile/favorites"},
	{"GET", "/api/v1/profile/favorites/:product_id"},
	{"POST", "/api/v1/account/deletion"},
	{"GET", "/api/v1/account/status"},
	{"POST", "/api/v1/account/deletion/cancel"},
	{"GET", "/api/v1/admin/roles"},
	{"POST", "/api/v1/admin/roles"},
	{"GET", "/api/v1/admin/roles/:id"},
	{"PATCH", "/api/v1/admin/roles/:id"},
	{"DELETE", "/api/v1/admin/roles/:id"},
	{"PUT", "/api/v1/admin/roles/:id/permissions"},
	{"PUT", "/api/v1/admin/roles/:id/menus"},
	{"GET", "/api/v1/admin/permissions/all"},
}

// newTestRouter 构建仅含用户域路由的测试引擎（DB/Redis 均为 nil，走降级分支）。
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	signer := jwtx.NewSigner("test-secret", "test", 0, 0)
	r := gin.New()
	v1 := r.Group("/api/v1")
	// 注册过程若出现 gin 路由树冲突会 panic，测试即失败——这正是要验证的点。
	RegisterUserRoutes(v1, Option{Signer: signer})
	return r
}

// TestUserDomainRoutesRegistered 逐条校验四个 tag 的路由都已注册（勾销表）。
func TestUserDomainRoutesRegistered(t *testing.T) {
	r := newTestRouter(t)
	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	missing := 0
	for _, want := range userDomainRoutes {
		key := want[0] + " " + want[1]
		if !got[key] {
			t.Errorf("路由缺失: %s", key)
			missing++
		}
	}
	if missing == 0 {
		t.Logf("用户域路由勾销通过：%d 条全部注册", len(userDomainRoutes))
	}
}

// TestProtectedRouteRequiresAuth 未带 token 访问受保护端点 → 401 + code=2001。
func TestProtectedRouteRequiresAuth(t *testing.T) {
	r := newTestRouter(t)
	for _, path := range []string{"/api/v1/user/profile", "/api/v1/profile", "/api/v1/role", "/api/v1/user"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s 期望 401，实际 %d（body=%s）", path, w.Code, w.Body.String())
			continue
		}
		if !contains(w.Body.String(), `"message":"auth.need_login"`) {
			t.Errorf("%s 期望 message 返回 key auth.need_login，实际 body=%s", path, w.Body.String())
		}
	}
}

// TestPublicRouteNoAuth 公开端点不带 token 不应被 401 拦住。
func TestPublicRouteNoAuth(t *testing.T) {
	r := newTestRouter(t)
	for _, path := range []string{"/api/v1/user/login", "/api/v1/user/register", "/api/v1/user/refresh-token"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s 是公开端点，不应返回 401（body=%s）", path, w.Body.String())
		}
	}
}

// TestOAuthNotConfiguredGitHub OAuth 占位端点返回 4022（未配置语义）。
func TestOAuthNotConfigured(t *testing.T) {
	r := newTestRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/oauth/github/url", nil)
	// did 必填（2026-11-28），且校验顺序在「配置齐全性」之前 —— 不带头会先吃 20016，
	// 这条用例要验的是「未配置」，所以必须先把 did 补上。
	req.Header.Set("did", "test-device")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !contains(w.Body.String(), "auth.oauth_not_configured") {
		t.Errorf("期望 400 + message 返回 key auth.oauth_not_configured，实际 %d body=%s", w.Code, w.Body.String())
	}
}

// TestDegradedDBReturns503 DB 为 nil（降级启动）时业务端点返回 503。
func TestDegradedDBReturns503(t *testing.T) {
	r := newTestRouter(t)
	signer := jwtx.NewSigner("test-secret", "test", 0, 0)
	token, err := signer.Generate("u-1", []string{"user"}, []string{"user:read"}, "dev-1", "access")
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("DB 降级时期望 503，实际 %d body=%s", w.Code, w.Body.String())
	}
}

// contains 子串判定。
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

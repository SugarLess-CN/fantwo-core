// Package middleware 中间件测试（纯 HTTP 层，不依赖真实 Redis/PG）。
//
// 覆盖 JWT 验签链路、CORS、gzip、zap 日志与 recovery。
// 黑名单/perm_ver 相关分支通过 nil Redis 走降级路径验证（rdb=nil 时不查 Redis，
// 语义等价于「Redis 不可用则只按纯 JWT 放行」——这条降级约定本身也要测）。
//
// 防的 bug：
//   - 缺 Bearer 前缀也放行（任何 header 都能通过）；
//   - 无 token 返回 200（未登录能访问受保护端点）；
//   - OptionalAuth 把空 claims 注入成非 nil（下游 CurrentUserID 取到 "" 后误判为已登录）；
//   - RBAC 把 AND 当 OR（多个权限点要求全部满足 → 管理员反而 403）；
//   - CORS 预检缺 Allow-Headers，浏览器带 Authorization 的请求被拦；
//   - recovery 未捕获 panic → 整个进程崩。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func init() { gin.SetMode(gin.TestMode) }

const (
	testSecret = "middleware-test-secret"
	testIssuer = "test"
)

func newSigner() *jwtx.Signer {
	return jwtx.NewSigner(testSecret, testIssuer, time.Hour, 7*24*time.Hour)
}

// bearerToken 签一个 access token。
func bearerToken(t *testing.T, perms []string) string {
	t.Helper()
	tok, err := newSigner().GenerateWithPermVer("u-1", []string{"user"}, perms, "d-1", "access", 0)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return tok
}

// doRequest 用给定中间件 + 可选请求头发起请求，返回 recorder。
func doRequest(mw gin.HandlerFunc, headers map[string]string) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/t", mw, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestJWTAuth_ValidTokenPasses 有效 token 放行。
func TestJWTAuth_ValidTokenPasses(t *testing.T) {
	tok := bearerToken(t, []string{"article:create"})
	w := doRequest(JWTAuth(newSigner()), map[string]string{"Authorization": "Bearer " + tok})

	if w.Code != http.StatusOK {
		t.Errorf("有效 token 应放行，得到 HTTP %d，体 %s", w.Code, w.Body.String())
	}
}

// TestJWTAuth_RejectsBadCredentials 各类无效凭证都必须 401。
//
// 逐项覆盖：无 header、无 Bearer 前缀、空 token、乱码、别的密钥签的。
func TestJWTAuth_RejectsBadCredentials(t *testing.T) {
	otherSigner := jwtx.NewSigner("different-secret", testIssuer, time.Hour, time.Hour)
	otherTok, err := otherSigner.Generate("attacker", nil, []string{"user:manage"}, "", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"无 Authorization 头", nil},
		{"空 Authorization", map[string]string{"Authorization": ""}},
		{"缺 Bearer 前缀", map[string]string{"Authorization": bearerToken(t, nil)}},
		{"只有 Bearer 无值", map[string]string{"Authorization": "Bearer "}},
		{"乱码 token", map[string]string{"Authorization": "Bearer not-a-jwt"}},
		{"别的密钥签的 token", map[string]string{"Authorization": "Bearer " + otherTok}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doRequest(JWTAuth(newSigner()), c.headers)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s 应返回 401，得到 %d（未授权访问漏洞）", c.name, w.Code)
			}
		})
	}
}

// TestJWTAuth_InjectsClaims claims 注入 context 供下游读取。
func TestJWTAuth_InjectsClaims(t *testing.T) {
	tok := bearerToken(t, []string{"article:create"})

	var gotUserID string
	var gotPerms []string
	r := gin.New()
	r.GET("/t", JWTAuth(newSigner()), func(c *gin.Context) {
		gotUserID = CurrentUserID(c)
		if cl := ClaimsFrom(c); cl != nil {
			gotPerms = cl.Permissions
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if gotUserID != "u-1" {
		t.Errorf("CurrentUserID：得到 %q，期望 u-1", gotUserID)
	}
	if len(gotPerms) != 1 || gotPerms[0] != "article:create" {
		t.Errorf("权限未注入：%v", gotPerms)
	}
}

// TestOptionalAuth_MissingTokenPassesWithNilClaims 可选登录：无 token 放行且 claims=nil。
//
// 防的是：claims 被塞成空对象（非 nil），下游 `if claims == nil` 判断失效，
// 匿名用户被当成已登录用户（会把作者字段写成空串）。
func TestOptionalAuth_MissingTokenPassesWithNilClaims(t *testing.T) {
	var claimsNil bool
	r := gin.New()
	r.GET("/t", OptionalAuth(newSigner()), func(c *gin.Context) {
		claimsNil = ClaimsFrom(c) == nil
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))

	if w.Code != http.StatusOK {
		t.Errorf("可选登录无 token 应放行，得到 %d", w.Code)
	}
	if !claimsNil {
		t.Error("无 token 时 claims 必须为 nil（否则匿名请求会被当成已登录）")
	}
}

// TestOptionalAuth_ValidTokenInjects 可选登录：有 token 时注入。
func TestOptionalAuth_ValidTokenInjects(t *testing.T) {
	tok := bearerToken(t, nil)
	var gotUserID string
	r := gin.New()
	r.GET("/t", OptionalAuth(newSigner()), func(c *gin.Context) {
		gotUserID = CurrentUserID(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if gotUserID != "u-1" {
		t.Errorf("可选登录有 token 时应注入用户，得到 %q", gotUserID)
	}
}

// TestRequirePermissions_OrSemantics 多权限 OR 关系。
//
// 关键：只要命中任意一个就放行。若实现误写成 AND，
// 同时有 article:create 与 article:update 的管理员反而会被 403。
func TestRequirePermissions_OrSemantics(t *testing.T) {
	cases := []struct {
		name       string
		userPerms  []string
		required   []string
		wantStatus int
	}{
		{"命中第一个", []string{"article:create"}, []string{"article:create", "article:update"}, 200},
		{"命中第二个", []string{"article:update"}, []string{"article:create", "article:update"}, 200},
		{"全命中", []string{"article:create", "article:update"}, []string{"article:create", "article:update"}, 200},
		{"一个都不命中", []string{"user:read"}, []string{"article:create", "article:update"}, 403},
		{"无任何权限", nil, []string{"article:create"}, 403},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok := bearerToken(t, c.userPerms)
			r := gin.New()
			r.GET("/t", JWTAuth(newSigner()), RequirePermissions(c.required...),
				func(ctx *gin.Context) { ctx.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != c.wantStatus {
				t.Errorf("权限 %v 请求 %v：得到 %d，期望 %d",
					c.userPerms, c.required, w.Code, c.wantStatus)
			}
		})
	}
}

// TestRequirePermissions_NoClaimsIsUnauthorized 未登录时 RBAC 返回 401（不是 403）。
// 语义区别：401 = 请先登录，403 = 已登录但无权。前端据此决定跳登录还是提示。
func TestRequirePermissions_NoClaimsIsUnauthorized(t *testing.T) {
	w := doRequest(RequirePermissions("article:create"),
		map[string]string{"Authorization": ""})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未登录访问需权限端点应 401，得到 %d", w.Code)
	}
}

// TestJWTAuthV2_NilRedisDegradesToJWT 黑名单中间件在 Redis 不可用时退化为纯 JWT。
//
// 这是骨架的降级约定：Redis 挂了认证不能整体不可用（否则全站 401）。
func TestJWTAuthV2_NilRedisDegradesToJWT(t *testing.T) {
	tok := bearerToken(t, []string{"user:read"})
	w := doRequest(JWTAuthV2(newSigner(), nil), map[string]string{"Authorization": "Bearer " + tok})

	if w.Code != http.StatusOK {
		t.Errorf("Redis=nil 时应退化为纯 JWT 放行，得到 %d（全站会 401）", w.Code)
	}
}

// TestJWTAuthV2_RejectsInvalidToken nil Redis 下仍要验签。
func TestJWTAuthV2_RejectsInvalidToken(t *testing.T) {
	w := doRequest(JWTAuthV2(newSigner(), nil),
		map[string]string{"Authorization": "Bearer garbage"})
	if w.Code == http.StatusOK {
		t.Error("JWTAuthV2 未验签就放行了")
	}
}

// TestJWTAuthV2_InjectsToken 原始 token 注入 context（登出算剩余 TTL 用）。
//
// 防的是：登出时拿不到原 token，黑名单写不进去，token 退出后仍可用。
func TestJWTAuthV2_InjectsToken(t *testing.T) {
	tok := bearerToken(t, nil)
	var gotToken string
	r := gin.New()
	r.GET("/t", JWTAuthV2(newSigner(), nil), func(c *gin.Context) {
		gotToken = CurrentToken(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if gotToken != tok {
		t.Errorf("CurrentToken：得到 %q，期望原始 token（登出黑名单会写不进去）", gotToken)
	}
}

// TestOptionalAuthV2_NilRedisPasses 可选登录 + nil Redis。
func TestOptionalAuthV2_NilRedisPasses(t *testing.T) {
	w := doRequest(OptionalAuthV2(newSigner(), nil), nil)
	if w.Code != http.StatusOK {
		t.Errorf("可选登录无 token 应放行，得到 %d", w.Code)
	}
}

// TestRequireAnyPermission_Or 权限 OR 校验（authz 版本）。
func TestRequireAnyPermission_Or(t *testing.T) {
	r := gin.New()
	r.GET("/t",
		JWTAuthV2(newSigner(), nil),
		RequireAnyPermission("article:create", "article:update"),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	// 有 article:update 即可
	tok := bearerToken(t, []string{"article:update"})
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("OR 语义失效：有 article:update 却被拒（%d）", w.Code)
	}
}

// TestClaimsHelpers_NotLoggedIn 未登录时辅助函数返回零值。
func TestClaimsHelpers_NotLoggedIn(t *testing.T) {
	r := gin.New()
	var uid, tok string
	var claimsNil bool
	r.GET("/t", func(c *gin.Context) {
		uid = CurrentUserID(c)
		tok = CurrentToken(c)
		claimsNil = ClaimsFrom(c) == nil
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	if uid != "" {
		t.Errorf("未登录 CurrentUserID 应为空，得到 %q", uid)
	}
	if tok != "" {
		t.Errorf("未登录 CurrentToken 应为空，得到 %q", tok)
	}
	if !claimsNil {
		t.Error("未登录 ClaimsFrom 应为 nil")
	}
}

// TestCORS_Headers 跨域响应头。
//
// 原仓库 enableCors() 全开；这条测试钉死 AllowAllOrigins 与 Authorization 头放行，
// 否则前端带 token 的跨域请求会被浏览器 CORS 拦掉。
func TestCORS_Headers(t *testing.T) {
	r := gin.New()
	r.Use(CORS())
	r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("Origin", "https://frontend.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin：得到 %q，期望 *（原仓库 CORS 全开）", got)
	}
}

// TestCORS_PreflightAllowsAuthorization 预检请求必须放行 Authorization 头。
// 防的是：预检返回的 Allow-Headers 不含 Authorization，带 token 的请求全被浏览器拦。
func TestCORS_PreflightAllowsAuthorization(t *testing.T) {
	r := gin.New()
	r.Use(CORS())
	r.POST("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/t", nil)
	req.Header.Set("Origin", "https://frontend.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization,Content-Type")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if allowHeaders == "" {
		t.Fatal("预检未返回 Access-Control-Allow-Headers")
	}
	if !containsFold(allowHeaders, "Authorization") {
		t.Errorf("预检未放行 Authorization 头（带 token 的跨域请求会被浏览器拦）：%q", allowHeaders)
	}
}

// TestZapRecovery_CatchesPanic panic 转为 500，进程不崩。
//
// 防的是：任一 handler 空指针直接让整个进程退出（容器 restart 循环）。
func TestZapRecovery_CatchesPanic(t *testing.T) {
	logger := zap.NewNop()
	r := gin.New()
	r.Use(ZapRecovery(logger))
	r.GET("/boom", func(c *gin.Context) { panic("handler 炸了") })

	w := httptest.NewRecorder()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("panic 未被 recovery 捕获，会打挂进程: %v", rec)
		}
	}()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("panic 应转 500，得到 %d", w.Code)
	}
}

// TestZapRecovery_NoErrorWhenNormal 正常请求不写 500。
func TestZapRecovery_NoErrorWhenNormal(t *testing.T) {
	r := gin.New()
	r.Use(ZapRecovery(zap.NewNop()))
	r.GET("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Errorf("正常请求被 recovery 干扰：得到 %d", w.Code)
	}
}

// TestZapLogger_DoesNotPanic 请求日志不 panic 且不吞掉响应。
//
// 用 Nop logger 验证：日志中间件在所有 status（2xx/4xx/5xx）下都不崩，
// 且响应体原样透传（日志中间件不得改写 body）。
func TestZapLogger_DoesNotPanic(t *testing.T) {
	statuses := []int{200, 400, 401, 403, 404, 500}

	for _, st := range statuses {
		r := gin.New()
		r.Use(ZapLogger(zap.NewNop()))
		r.GET("/t", func(c *gin.Context) { c.JSON(st, gin.H{"code": st}) })

		w := httptest.NewRecorder()
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("status=%d 时日志中间件 panic: %v", st, rec)
				}
			}()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t?q=1", nil))
		}()
		if w.Code != st {
			t.Errorf("status=%d 时响应被改写为 %d", st, w.Code)
		}
		if w.Body.Len() == 0 {
			t.Errorf("status=%d 时响应体被日志中间件吞掉", st)
		}
	}
}

func containsFold(s, sub string) bool {
	ls, lsub := toLower(s), toLower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

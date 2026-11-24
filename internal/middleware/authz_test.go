package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/gin-gonic/gin"
)

// memStore 认证态存储的内存替身（无需真实 Redis）。
type memStore struct {
	mu   sync.Mutex
	data map[string]string
	ttl  map[string]time.Time
	fail bool
}

func newMemStore() *memStore {
	return &memStore{data: map[string]string{}, ttl: map[string]time.Time{}}
}

func (m *memStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return fmt.Errorf("redis down")
	}
	m.data[key] = fmt.Sprint(value)
	if ttl > 0 {
		m.ttl[key] = time.Now().Add(ttl)
	}
	return nil
}

func (m *memStore) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return "", fmt.Errorf("redis down")
	}
	v, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("nil")
	}
	if exp, has := m.ttl[key]; has && time.Now().After(exp) {
		delete(m.data, key)
		delete(m.ttl, key)
		return "", fmt.Errorf("expired")
	}
	return v, nil
}

func (m *memStore) Exists(_ context.Context, key string) (int64, error) {
	if _, err := m.Get(context.Background(), key); err != nil {
		return 0, nil
	}
	return 1, nil
}

func (m *memStore) Incr(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return 0, fmt.Errorf("redis down")
	}
	n, _ := strconv.ParseInt(m.data[key], 10, 64)
	n++
	m.data[key] = strconv.FormatInt(n, 10)
	return n, nil
}

// Del 删除键（authtoken.Store 接口要求）。
func (m *memStore) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return fmt.Errorf("redis down")
	}
	delete(m.data, key)
	return nil
}

const authzSecret = "authz-test-secret"

// authzFixture 构建测试路由器：/protected 走强制鉴权，/perm 走权限点，/optional 走可选鉴权。
type authzFixture struct {
	router *gin.Engine
	signer *jwtx.Signer
	store  *memStore
}

func newAuthzFixture(t *testing.T) *authzFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	signer := jwtx.NewSigner(authzSecret, "test", time.Hour, 24*time.Hour)
	store := newMemStore()
	r := gin.New()

	r.GET("/protected", JWTAuthV2(signer, store), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"uid": CurrentUserID(c), "token": CurrentToken(c)})
	})
	r.GET("/perm", JWTAuthV2(signer, store),
		RequireAnyPermission("user:read", "user:manage"), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	r.GET("/optional", OptionalAuthV2(signer, store), func(c *gin.Context) {
		if ClaimsFrom(c) == nil {
			c.JSON(http.StatusOK, gin.H{"user": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user": CurrentUserID(c)})
	})
	return &authzFixture{router: r, signer: signer, store: store}
}

// do 发起带可选 token/头的请求。
func (f *authzFixture) do(t *testing.T, path, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	f.router.ServeHTTP(w, req)
	return w
}

// TestJWTAuthV2RejectsNoToken 无 Authorization 头 → 401 / code=2001。
//
// 防的 bug：路由组漏挂鉴权中间件，未登录即可访问用户资料（越权读）。
func TestJWTAuthV2RejectsNoToken(t *testing.T) {
	f := newAuthzFixture(t)
	w := f.do(t, "/protected", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %d", w.Code)
	}
	if !bodyHas(w.Body.String(), `"message":"Please log in first"`) {
		t.Errorf("期望 status_code=401，body=%s", w.Body.String())
	}
	if !bodyHas(w.Body.String(), i18n.T(i18n.AuthNeedLogin)) {
		t.Errorf("期望 message=%q，body=%s", i18n.T(i18n.AuthNeedLogin), w.Body.String())
	}
}

// TestJWTAuthV2RejectsMalformedHeader 缺少 "Bearer " 前缀视为无凭证。
//
// 防的 bug：用 TrimPrefix 后未判断「是否真的去掉」，裸 token 被当作有效凭证。
func TestJWTAuthV2RejectsMalformedHeader(t *testing.T) {
	f := newAuthzFixture(t)
	token, err := f.signer.Generate("u-1", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w := f.do(t, "/protected", token) // 没有 Bearer 前缀
	if w.Code != http.StatusUnauthorized {
		t.Errorf("缺少 Bearer 前缀应 401，实际 %d", w.Code)
	}
}

// TestJWTAuthV2RejectsBadSignature 签名错误/伪造 token → 401。
func TestJWTAuthV2RejectsBadSignature(t *testing.T) {
	f := newAuthzFixture(t)
	other := jwtx.NewSigner("other-secret", "test", time.Hour, time.Hour)
	token, err := other.Generate("u-1", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+token); w.Code != http.StatusUnauthorized {
		t.Errorf("伪造签名应 401，实际 %d", w.Code)
	}
	if w := f.do(t, "/protected", "Bearer garbage"); w.Code != http.StatusUnauthorized {
		t.Errorf("垃圾 token 应 401，实际 %d", w.Code)
	}
}

// TestJWTAuthV2AcceptsValidToken 有效 token 放行并注入 claims/token。
func TestJWTAuthV2AcceptsValidToken(t *testing.T) {
	f := newAuthzFixture(t)
	token, err := f.signer.Generate("u-42", nil, []string{"user:read"}, "dev-x", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w := f.do(t, "/protected", "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if !bodyHas(w.Body.String(), "u-42") {
		t.Errorf("handler 未拿到 userID: %s", w.Body.String())
	}
	if !bodyHas(w.Body.String(), token) {
		t.Errorf("handler 未拿到原始 token（登出黑名单需要）: %s", w.Body.String())
	}
}

// TestJWTAuthV2RejectsBlacklisted 已登出（黑名单）token → 401。
//
// 防的 bug：登出只删设备不写黑名单，被窃取的 access token 在有效期内继续可用。
func TestJWTAuthV2RejectsBlacklisted(t *testing.T) {
	f := newAuthzFixture(t)
	token, err := f.signer.Generate("u-1", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+token); w.Code != http.StatusOK {
		t.Fatalf("黑名单前应放行，实际 %d", w.Code)
	}
	if err := authtoken.BlacklistAdd(context.Background(), f.store, token, time.Minute); err != nil {
		t.Fatalf("写黑名单失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+token); w.Code != http.StatusUnauthorized {
		t.Errorf("登出后应 401，实际 %d", w.Code)
	}
}

// TestJWTAuthV2PermVersionMismatch 权限版本落后 → 401 / code=2002（要求刷新）。
//
// 防的 bug：角色权限被收回后，旧 token 内仍带着旧 perms，越权一直有效到过期。
func TestJWTAuthV2PermVersionMismatch(t *testing.T) {
	f := newAuthzFixture(t)
	token, err := f.signer.GenerateWithPermVer("u-1", []string{"user"}, []string{"user:read"}, "d", "access", 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+token); w.Code != http.StatusOK {
		t.Fatalf("版本一致时应放行，实际 %d", w.Code)
	}
	// 模拟权限变更：bump 版本
	authtoken.BumpPermVer(context.Background(), f.store, "u-1")
	w := f.do(t, "/protected", "Bearer "+token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("版本不一致应 401，实际 %d", w.Code)
	}
	if !bodyHas(w.Body.String(), i18n.T(i18n.AuthPermVersionStale)) {
		t.Errorf("期望 message=%q，body=%s", i18n.T(i18n.AuthPermVersionStale), w.Body.String())
	}
	if !bodyHas(w.Body.String(), "20002") {
		t.Errorf("期望业务码 20002，body=%s", w.Body.String())
	}
}

// TestJWTAuthV2LogoutAllInvalidatesOlderTokens 全端登出后，签发时间更早的 token 失效。
//
// 防的 bug：多点登录只清当前设备，其它设备 token 仍然有效（「全端登出」名不副实）。
func TestJWTAuthV2LogoutAllInvalidatesOlderTokens(t *testing.T) {
	f := newAuthzFixture(t)
	old, err := f.signer.Generate("u-9", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	// 全端登出时间设为 2 秒后（确保 old 的 iat 更早）
	if err := authtoken.SetLogoutAll(context.Background(), f.store, "u-9", time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("写全端登出失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+old); w.Code != http.StatusUnauthorized {
		t.Errorf("全端登出后旧 token 应 401，实际 %d", w.Code)
	}
	// 其他用户不受影响
	other, err := f.signer.Generate("u-10", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if w := f.do(t, "/protected", "Bearer "+other); w.Code != http.StatusOK {
		t.Errorf("其他用户不应被牵连，实际 %d", w.Code)
	}
}

// TestRequireAnyPermission 权限点 OR 语义：命中放行、缺失 403 / code=2003。
//
// 防的 bug：权限校验写成 AND（任一不满足即拒）或完全跳过，导致管理端点越权。
func TestRequireAnyPermission(t *testing.T) {
	f := newAuthzFixture(t)
	cases := []struct {
		name  string
		perms []string
		want  int
		code  string
	}{
		{"命中第一个权限", []string{"user:read"}, http.StatusOK, ""},
		{"命中第二个权限", []string{"user:manage"}, http.StatusOK, ""},
		{"无相关权限", []string{"article:read"}, http.StatusForbidden, "20003"},
		{"空权限集", nil, http.StatusForbidden, "20003"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := f.signer.Generate("u-perm", nil, tc.perms, "d", "access")
			if err != nil {
				t.Fatalf("签发失败: %v", err)
			}
			w := f.do(t, "/perm", "Bearer "+token)
			if w.Code != tc.want {
				t.Fatalf("期望 %d，实际 %d body=%s", tc.want, w.Code, w.Body.String())
			}
			if tc.code != "" && !bodyHas(w.Body.String(), tc.code) {
				t.Errorf("期望错误码 %s，body=%s", tc.code, w.Body.String())
			}
		})
	}
}

// TestRequireAnyPermissionWithoutLogin 未登录直接访问权限端点 → 401（不是 403）。
//
// 防的 bug：未登录被判定为「无权限」返回 403，前端误以为「权限不足」而非去登录。
func TestRequireAnyPermissionWithoutLogin(t *testing.T) {
	f := newAuthzFixture(t)
	w := f.do(t, "/perm", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401，实际 %d", w.Code)
	}
	if !bodyHas(w.Body.String(), `"message":"Please log in first"`) {
		t.Errorf("期望 status_code=401，body=%s", w.Body.String())
	}
}

// TestOptionalAuthV2 可选登录：无 token / 无效 token 都放行（claims=nil），有效 token 注入。
//
// 防的 bug：@NoAuth 端点（文章列表等）因可选鉴权误判为强制，匿名用户 401。
func TestOptionalAuthV2(t *testing.T) {
	f := newAuthzFixture(t)

	w := f.do(t, "/optional", "")
	if w.Code != http.StatusOK || !bodyHas(w.Body.String(), `"user":null`) {
		t.Errorf("无 token 应放行且 user=nil，实际 %d body=%s", w.Code, w.Body.String())
	}

	w = f.do(t, "/optional", "Bearer garbage")
	if w.Code != http.StatusOK || !bodyHas(w.Body.String(), `"user":null`) {
		t.Errorf("无效 token 应放行且 user=nil，实际 %d body=%s", w.Code, w.Body.String())
	}

	token, err := f.signer.Generate("u-opt", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w = f.do(t, "/optional", "Bearer "+token)
	if w.Code != http.StatusOK || !bodyHas(w.Body.String(), "u-opt") {
		t.Errorf("有效 token 应注入 user，实际 %d body=%s", w.Code, w.Body.String())
	}
}

// TestAuthzDegradesWithoutStore Redis 不可用（store=nil）时退化为纯 JWT 校验。
//
// 防的 bug：无 Redis 环境启动后所有请求 500 / 全站 401。
func TestAuthzDegradesWithoutStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signer := jwtx.NewSigner(authzSecret, "test", time.Hour, time.Hour)
	r := gin.New()
	r.GET("/p", JWTAuthV2(signer, nil), func(c *gin.Context) { c.Status(http.StatusOK) })

	token, err := signer.Generate("u-1", nil, nil, "d", "access")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("无 Redis 时有效 token 应放行，实际 %d", w.Code)
	}
}

// TestClaimsFromAndCurrentUserIDNil 未注入 claims 时不 panic（防盗链/匿名路径）。
func TestClaimsFromAndCurrentUserIDNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/raw", func(c *gin.Context) {
		if ClaimsFrom(c) != nil {
			t.Error("未注入时 ClaimsFrom 应为 nil")
		}
		if CurrentUserID(c) != "" {
			t.Error("未注入时 CurrentUserID 应为空")
		}
		if CurrentToken(c) != "" {
			t.Error("未注入时 CurrentToken 应为空")
		}
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/raw", nil))
	if w.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d", w.Code)
	}
}

// bodyHas 响应体包含子串。
func bodyHas(body, sub string) bool {
	for i := 0; i+len(sub) <= len(body); i++ {
		if body[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

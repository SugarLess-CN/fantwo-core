// 认证增强中间件：黑名单 / 全端登出 / 权限版本校验。
//
// 与骨架 middleware.JWTAuth / OptionalAuth 并存（不改其签名）；
// 需要黑名单与 perm_ver 校验的路由组用 JWTAuthV2 / OptionalAuthV2。
package middleware

import (
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// CtxTokenKey gin context 中原始 token 的键（登出时取剩余有效期用）。
const CtxTokenKey = "auth_token"

// JWTAuthV2 强制鉴权：验签 → 黑名单 → 全端登出时间 → perm_ver 比对。
// rdb 为 nil 时退化为纯 JWT 校验（骨架降级语义）。
func JWTAuthV2(signer *jwtx.Signer, st authtoken.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, token, authErr := parseBearerV2(c, signer, st)
		if authErr != nil {
			response.Fail(c, authErr)
			return
		}
		c.Set(CtxUserKey, claims)
		c.Set(CtxTokenKey, token)
		c.Next()
	}
}

// OptionalAuthV2 可选登录：失败放行（claims=nil），对齐原 @NoAuth 可选语义。
func OptionalAuthV2(signer *jwtx.Signer, st authtoken.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, token, authErr := parseBearerV2(c, signer, st)
		if authErr == nil {
			c.Set(CtxUserKey, claims)
			c.Set(CtxTokenKey, token)
		} else {
			c.Set(CtxUserKey, nil)
		}
		c.Next()
	}
}

// RequireAnyPermission 权限点校验（OR 关系，对齐原 PermissionGuard）。
//
// **权限来源是数据库实时值**（`ctxPermSource`），见 RequirePermissions 的说明。
func RequireAnyPermission(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			response.Fail(c, errcode.ErrNeedAuth)
			return
		}
		perms, _ := currentPermissions(c, claims)
		for _, req := range required {
			for _, p := range perms {
				if p == req {
					c.Next()
					return
				}
			}
		}
		response.Fail(c, errcode.ErrNoPermission)
	}
}

// ClaimsFrom 从 gin context 取当前用户 claims（未登录返回 nil）。
func ClaimsFrom(c *gin.Context) *jwtx.Claims {
	raw, exists := c.Get(CtxUserKey)
	if !exists {
		return nil
	}
	claims, _ := raw.(*jwtx.Claims)
	return claims
}

// CurrentUserID 当前用户 ID（未登录返回空串）。
func CurrentUserID(c *gin.Context) string {
	if claims := ClaimsFrom(c); claims != nil {
		return claims.UserID
	}
	return ""
}

// CurrentToken 当前请求携带的原始 token（登出用）。
func CurrentToken(c *gin.Context) string {
	if v, ok := c.Get(CtxTokenKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// parseBearerV2 解析并完成全部认证态校验。
//
// 错误码区分（客户端据此决定动作）：
//   - 2001 ErrNeedAuth：缺 token / 验签失败 / 已登出 / 全端登出后失效 → 去登录；
//   - 2002 ErrPermVersionStale：token 有效但权限版本落后 → 刷新 token 或重新登录。
func parseBearerV2(c *gin.Context, signer *jwtx.Signer, st authtoken.Store) (*jwtx.Claims, string, *errcode.Err) {
	h := c.GetHeader("Authorization")
	if h == "" {
		return nil, "", errcode.ErrNeedAuth
	}
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h {
		return nil, "", errcode.ErrNeedAuth
	}
	claims, err := signer.Parse(token)
	if err != nil {
		return nil, "", errcode.ErrNeedAuth
	}
	ctx := c.Request.Context()
	// 1) 单端登出黑名单
	if authtoken.Blacklisted(ctx, st, token) {
		return nil, "", errcode.ErrNeedAuth
	}
	// 2) 全端登出：签发时间早于登出时刻的 token 全部失效
	if at := authtoken.LogoutAllAt(ctx, st, claims.UserID); !at.IsZero() {
		if claims.IssuedAt != nil && claims.IssuedAt.Time.Before(at.Add(time.Second)) {
			return nil, "", errcode.ErrNeedAuth
		}
	}
	// 3) 权限版本比对：perm_ver 单调递增，**0 = 未设置或查询失败 → 跳过比对**。
	// 与上面黑名单 / 全端登出同向 fail-open，避免 Redis 故障把已登录用户全站打死：
	// 从未 bump 过的用户库里就没有 perm_ver key（读到 0），那种场景 token 里携带的也是 0，
	// 所以「cur == 0 跳过」不会漏判任何真实发生过的权限变更 —— 变更必然伴随 bump，
	// key 一定存在且 > 0，此时才走严格比对。
	if cur := authtoken.PermVer(ctx, st, claims.UserID); cur != 0 && cur != claims.PermVer {
		return nil, "", errcode.ErrPermVersionStale
	}
	return claims, token, nil
}

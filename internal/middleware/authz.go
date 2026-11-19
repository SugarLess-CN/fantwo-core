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
	"github.com/redis/go-redis/v9"
)

// CtxTokenKey gin context 中原始 token 的键（登出时取剩余有效期用）。
const CtxTokenKey = "auth_token"

// JWTAuthV2 强制鉴权：验签 → 黑名单 → 全端登出时间 → perm_ver 比对。
// rdb 为 nil 时退化为纯 JWT 校验（骨架降级语义）。
func JWTAuthV2(signer *jwtx.Signer, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, token, ok := parseBearerV2(c, signer, rdb)
		if !ok {
			response.Fail(c, errcode.ErrNeedAuth)
			return
		}
		c.Set(CtxUserKey, claims)
		c.Set(CtxTokenKey, token)
		c.Next()
	}
}

// OptionalAuthV2 可选登录：失败放行（claims=nil），对齐原 @NoAuth 可选语义。
func OptionalAuthV2(signer *jwtx.Signer, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, token, ok := parseBearerV2(c, signer, rdb)
		if ok {
			c.Set(CtxUserKey, claims)
			c.Set(CtxTokenKey, token)
		} else {
			c.Set(CtxUserKey, nil)
		}
		c.Next()
	}
}

// RequireAnyPermission 权限点校验（OR 关系，对齐原 PermissionGuard）。
// claims 内权限来自 JWT；perm_ver 已在 JWTAuthV2 校验，这里不再查库。
func RequireAnyPermission(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			response.Fail(c, errcode.ErrNeedAuth)
			return
		}
		for _, req := range required {
			for _, p := range claims.Permissions {
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
func parseBearerV2(c *gin.Context, signer *jwtx.Signer, rdb *redis.Client) (*jwtx.Claims, string, bool) {
	h := c.GetHeader("Authorization")
	if h == "" {
		return nil, "", false
	}
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h {
		return nil, "", false
	}
	claims, err := signer.Parse(token)
	if err != nil {
		return nil, "", false
	}
	ctx := c.Request.Context()
	// 1) 单端登出黑名单
	if authtoken.Blacklisted(ctx, rdb, token) {
		return nil, "", false
	}
	// 2) 全端登出：签发时间早于登出时刻的 token 全部失效
	if at := authtoken.LogoutAllAt(ctx, rdb, claims.UserID); !at.IsZero() {
		if claims.IssuedAt != nil && claims.IssuedAt.Time.Before(at.Add(time.Second)) {
			return nil, "", false
		}
	}
	// 3) 权限版本比对
	if cur := authtoken.PermVer(ctx, rdb, claims.UserID); cur != claims.PermVer {
		return nil, "", false
	}
	return claims, token, true
}

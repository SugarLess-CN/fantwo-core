// Package middleware Gin 中间件：JWT 鉴权、CORS、zap 请求日志、panic recovery。
package middleware

import (
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// CtxUserKey gin context 中当前用户 claims 的键。
const CtxUserKey = "auth_user"

// JWTAuth 强制鉴权中间件：无有效 token 直接 401。
// 对齐原 JwtAuthGuard（全局默认需要登录）。
func JWTAuth(signer *jwtx.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseBearer(c, signer)
		if !ok {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		c.Set(CtxUserKey, claims)
		c.Next()
	}
}

// OptionalAuth 可选登录中间件：token 有效则注入用户，无效/缺失放行（claims 为 nil）。
// 对齐原 @NoAuth() 装饰器语义（「可选登录」：验证失败也放行，request.user 为 null）。
func OptionalAuth(signer *jwtx.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims, ok := parseBearer(c, signer); ok {
			c.Set(CtxUserKey, claims)
		} else {
			c.Set(CtxUserKey, nil)
		}
		c.Next()
	}
}

// RequirePermissions 权限点校验（RBAC，多个权限 OR 关系，对齐原 PermissionGuard）。
// 依赖 JWTAuth 已注入 claims；claims 内 permissions 来自 JWT（guard 不查库）。
func RequirePermissions(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(CtxUserKey)
		claims, ok := raw.(*jwtx.Claims)
		if !exists || !ok || claims == nil {
			response.Fail(c, errcode.ErrUnauthorized)
			return
		}
		// OR 关系：任一命中即放行
		for _, req := range required {
			for _, p := range claims.Permissions {
				if p == req {
					c.Next()
					return
				}
			}
		}
		response.Fail(c, errcode.ErrForbidden)
	}
}

// parseBearer 从 Authorization: Bearer <token> 解析 claims。
func parseBearer(c *gin.Context, signer *jwtx.Signer) (*jwtx.Claims, bool) {
	h := c.GetHeader("Authorization")
	if h == "" {
		return nil, false
	}
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h { // 没有 Bearer 前缀
		return nil, false
	}
	claims, err := signer.Parse(token)
	if err != nil {
		return nil, false
	}
	return claims, true
}

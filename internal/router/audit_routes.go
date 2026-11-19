// 内容审核域路由注册（swagger 内容审核 6 操作）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterAuditRoutes 注册 /api/v1/content-audit*。
func RegisterAuditRoutes(v1 *gin.RouterGroup, opt Option) {
	if opt.DB == nil {
		return
	}
	h := &handler.AuditHandler{Svc: service.NewAuditService(opt.DB, queue.FromRedisClient(opt.Redis))}
	auth := middleware.JWTAuth(opt.Signer)
	perm := middleware.RequirePermissions

	g := v1.Group("/content-audit")
	{
		g.GET("/config", auth, perm("audit:read"), h.Config)                        // 获取审核配置
		g.POST("/config/reload", auth, perm("audit:update"), h.Reload)              // 重载配置
		g.GET("/pending-articles", auth, perm("audit:read"), h.PendingArticles)     // 待审核文章
		g.POST("/articles/:id/review", auth, perm("audit:update"), h.ReviewArticle) // 人工复审
		g.POST("/text", auth, perm("audit:read"), h.AuditText)                      // 测试文本审核
		g.POST("/image", auth, perm("audit:read"), h.AuditImage)                    // 测试图片审核
	}
}

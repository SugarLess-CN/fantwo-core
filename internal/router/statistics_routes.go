// 基础设施域路由：统计模块（2 操作）。
//
// 权限点对齐原 statistics.controller.ts：@Permissions("statistics:read", "statistics:read")
// ——RequirePermissions 为 OR 语义（permission.guard 原实现），任一权限命中即放行。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// RegisterStatisticsRoutes 注册统计路由（挂载在 /api/v1 下）。
func RegisterStatisticsRoutes(v1 *gin.RouterGroup, opt Option) {
	h := handler.NewStatisticsHandler(infra.NewStatistics(opt.DB))
	g := v1.Group("/statistics")
	perms := []string{"statistics:read", "statistics:read"}
	g.GET("/overview", append(infraChain(opt, perms...), h.Overview)...)
	g.GET("/trends", append(infraChain(opt, perms...), h.Trends)...)
}

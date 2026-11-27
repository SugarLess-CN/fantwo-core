// 订单域路由注册（对齐原 order.controller.ts / swagger 订单管理 10 操作）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterOrderRoutes 注册 /api/v1/order*。
func RegisterOrderRoutes(v1 *gin.RouterGroup, opt Option) {
	if opt.DB == nil {
		return // 降级启动：无 DB 不注册业务路由
	}
	h := &handler.OrderHandler{Svc: service.NewOrderService(opt.DB)}
	auth := middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
	perm := middleware.RequirePermissions

	g := v1.Group("/order")
	{
		g.GET("", auth, perm("order:manage"), h.ListAll)                      // 管理员：所有订单
		g.GET("/user", auth, perm("order:read"), h.ListMine)                  // 我的订单分页
		g.GET("/pending", auth, perm("order:read"), h.ListPending)            // 待支付订单
		g.GET("/wallet/balance", auth, perm("user:read"), h.WalletBalance)    // 钱包余额
		g.GET("/no/:order_no", auth, perm("order:read"), h.GetByNo)           // 按订单号查
		g.GET("/:id", auth, perm("order:read"), h.Get)                        // 订单详情
		g.PUT("/:id/cancel", auth, perm("order:cancel"), h.Cancel)            // 取消订单
		g.POST("/:id/refund", auth, perm("order:refund"), h.Refund)           // 申请退款
		g.POST("/article", auth, perm("order:create"), h.CreateArticle)       // 创建文章订单
		g.POST("/membership", auth, perm("order:create"), h.CreateMembership) // 会员充值订单
	}
}

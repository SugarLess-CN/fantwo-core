// 支付域路由注册（swagger 支付管理 7 操作 + epay 签名自测）。
//
// 三个回调端点 NoAuth：支付网关直连，无 Authorization 头；
// 验签由 SDK（alipay.GetTradeNotification / wechatpay notify.Handler / 易支付 MD5）自行完成。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterPaymentRoutes 注册 /api/v1/payment*。
func RegisterPaymentRoutes(v1 *gin.RouterGroup, opt Option) {
	if opt.DB == nil {
		return
	}
	h := &handler.PaymentHandler{Svc: service.NewPaymentService(opt.DB, queue.FromRedisClient(opt.Redis))}
	auth := middleware.JWTAuth(opt.Signer)

	g := v1.Group("/payment")
	{
		// 用户侧（需登录）
		g.POST("/create", auth, h.Create)         // 创建支付
		g.GET("/record/:id", auth, h.Record)      // 查询支付记录
		g.GET("/order/:orderId", auth, h.ByOrder) // 查询订单支付记录
		g.GET("/user", auth, h.Mine)              // 查询用户支付记录

		// 回调（NoAuth，路径与 swagger 完全一致）
		g.POST("/notify/alipay", h.NotifyAlipay)
		g.POST("/notify/wechat", h.NotifyWechat)
		g.GET("/notify/epay", h.NotifyEpay)

		// 管理端自测：易支付签名计算（不真实下单）
		g.POST("/test/epay-signature", auth, h.EpaySignature)
	}
}

// QQ 机器人域路由注册（T-41b，12 个端点）。
//
// # 三条注册约定
//
//  1. `/qqbot/webhook` **不挂 JWT**：平台回调不带本站 token，靠 ed25519 签名校验。
//     它同时**不套统一响应包装**（裸 JSON），因此 handler 里直接 c.JSON/c.Status。
//  2. 权限点用 `qqbot:read` / `qqbot:manage`。这两个名字**还没进 RBAC 权限种子**
//     （role_service.go 的 DefaultPermissionSeeds 不归本域改），所以这里额外 OR 上
//     `user:manage` 作为过渡：否则连 super-admin 都会 403（它的权限来自权限表，
//     表里没有 qqbot:* 就拿不到）。种子补上后可以删掉 OR 的那一半。
//  3. 路由按「静态段 → 参数段」顺序写，避免 `/applications/batch-approve` 被
//     `/applications/:id` 吞掉（gin 同一方法下会冲突 panic）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterQqbotRoutes 注册 /api/v1/qqbot*。
//
// DB 为 nil（无库降级启动）时整域跳过：webhook 落库与池子投递都依赖数据库，
// 注册出来也只会一路 503，不如直接不暴露。
func RegisterQqbotRoutes(v1 *gin.RouterGroup, opt Option) {
	if opt.DB == nil {
		return
	}
	qqbotSvc := service.NewQqbotService(service.NewDeps(
		opt.DB, opt.Redis, opt.Signer, queue.FromRedisClient(opt.Redis), opt.Logger,
	))
	h := &handler.QqbotHandler{Svc: qqbotSvc}

	auth := middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
	canRead := middleware.RequireAnyPermission("qqbot:read", "user:manage")
	canManage := middleware.RequireAnyPermission("qqbot:manage", "user:manage")

	g := v1.Group("/qqbot")

	// ---------- 平台回调（NoAuth，裸 JSON） ----------
	g.POST("/webhook", h.Webhook)

	// ---------- 申请单 ----------
	g.GET("/applications", auth, canRead, h.ListApplications)
	// 注意顺序：静态段 batch-approve 与参数段 :id/approve 段数不同，gin 不会冲突；
	// 但为可读性仍按「集合 → 静态动作 → 单条动作」排列。
	g.POST("/applications/batch-approve", auth, canManage, h.BatchApprove)
	g.GET("/applications/:id", auth, canRead, h.GetApplication)
	g.POST("/applications/:id/approve", auth, canManage, h.ApproveApplication)
	g.POST("/applications/:id/reject", auth, canManage, h.RejectApplication)

	// ---------- 会话与消息 ----------
	g.GET("/conversations", auth, canRead, h.ListConversations)
	g.GET("/conversations/:openid/messages", auth, canRead, h.ListMessages)
	g.POST("/messages/send", auth, canManage, h.SendMessage)

	// ---------- 发件池 ----------
	g.GET("/outbox", auth, canRead, h.ListOutbox)
	g.POST("/outbox/:id/retry", auth, canManage, h.RetryOutbox)

	// ---------- 健康检查 ----------
	g.GET("/status", auth, canRead, h.Status)
}

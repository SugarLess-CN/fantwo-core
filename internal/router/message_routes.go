// Package router 消息域路由注册（站内消息 / 私信 / 拉黑 / WS）。
//
// 挂载约定：全部在 /api/v1 之下（骨架全局前缀），与源端 @Controller("message")
// 的路径一一对应；WS 端点 /api/v1/ws-message 对齐原 Socket.io namespace /ws-message。
//
// 路由顺序注意：/message/search、/message/unread/count、/message/private/* 等静态段
// 必须先于 /message/:id 注册，避免被参数段吞掉（gin 允许静态+参数同层共存，
// 但显式按「静态优先」排列更稳妥）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
	"github.com/gin-gonic/gin"
)

// RegisterMessageRoutes 注册消息域全部路由（19 个 REST 操作 + 1 个 WS 端点）。
//
// DB 为 nil 时仍注册（handler 内 ready() 返回 503），保证路由表稳定、前端可探测。
func RegisterMessageRoutes(v1 *gin.RouterGroup, opt Option) {
	// 1) WS 网关：先建 hub 与业务回调，再交给 service 作推送通道
	hub := ws.NewHub()
	go hub.Run()

	publisher := &handler.MsgPublisher{}
	msgSvc := service.NewMessageService(opt.DB, publisher)
	privSvc := service.NewPrivateMessageService(opt.DB, publisher)
	presenceSvc := service.NewMessagePresenceService(opt.DB, opt.Redis)

	gateway := ws.NewMsgGateway(
		hub,
		handler.MsgWSAuth(opt.Signer, msgSvc),
		handler.MsgDispatch(msgSvc, privSvc),
		presenceAdapter{presenceSvc},
	)
	publisher.Gateway = gateway

	h := &handler.MessageHandler{Svc: msgSvc, Priv: privSvc, Presence: presenceSvc}

	// 2) WS 端点（token 走 query/header，源端同语义；不走 JWT 中间件）
	v1.GET("/ws-message", gateway.Handler)

	// 3) REST：登录态统一由中间件保证（DB 不可用时 handler 返回 503）
	auth := v1.Group("", middleware.JWTAuth(opt.Signer))
	{
		msg := auth.Group("/message")
		{
			// 创建/更新需 message:manage，删除需 message:delete（对齐源端 @Permissions）
			msg.POST("", middleware.RequirePermissions("message:manage"), h.Create)

			// 静态段优先于 :id
			msg.GET("/search", h.Search)
			msg.GET("/unread/count", h.UnreadCount)
			msg.POST("/read-all", h.MarkAllRead)
			msg.POST("/batch", h.Batch)

			// 私信（全部静态段，放在 :id 之前）
			priv := msg.Group("/private")
			{
				priv.GET("/conversations", h.PrivateConversations)
				priv.GET("/conversations/:userId/messages", h.PrivateHistory)
				priv.POST("/conversations/:userId/read", h.PrivateConversationRead)
				priv.POST("/read-batch", h.PrivateReadBatch)
				priv.POST("/recall/:id", h.RecallPrivate)
				priv.GET("/blocks", h.Blocks)
				priv.POST("/block/:userId", h.BlockUser)
				priv.DELETE("/block/:userId", h.UnblockUser)
				priv.POST("/:userId", h.SendPrivate)
			}

			// 列表（空路径 = /message）
			msg.GET("", h.List)

			// 参数段最后
			msg.GET("/:id", h.Detail)
			msg.PATCH("/:id", middleware.RequirePermissions("message:manage"), h.Update)
			msg.DELETE("/:id", middleware.RequirePermissions("message:manage", "message:manage"), h.Remove)
			msg.POST("/:id/read", h.MarkRead)
		}
	}
}

// presenceAdapter 把 service 的在线状态服务适配成 ws.MsgPresenceHook。
//
// 这层适配的存在意义：ws 包不 import service（避免与 service→ws 的常量依赖成环），
// 只依赖自己的 MsgPresenceHook 接口。
type presenceAdapter struct {
	svc *service.MessagePresenceService
}

// Connected 见 ws.MsgPresenceHook。
func (a presenceAdapter) Connected(userID, socketID, deviceID string) (bool, ws.UserStatusPayload) {
	if a.svc == nil {
		return false, ws.UserStatusPayload{UserID: userID, IsOnline: true}
	}
	return a.svc.HandleConnected(userID, socketID, deviceID)
}

// Disconnected 见 ws.MsgPresenceHook。
func (a presenceAdapter) Disconnected(socketID string) (bool, string, ws.UserStatusPayload) {
	if a.svc == nil {
		return false, "", ws.UserStatusPayload{}
	}
	return a.svc.HandleDisconnected(socketID)
}

// Touch 见 ws.MsgPresenceHook。
func (a presenceAdapter) Touch(userID, deviceID string) {
	if a.svc == nil {
		return
	}
	a.svc.Touch(userID, deviceID)
}

// Presence 见 ws.MsgPresenceHook。
func (a presenceAdapter) Presence(userID string) ws.UserStatusPayload {
	if a.svc == nil {
		return ws.UserStatusPayload{UserID: userID}
	}
	return a.svc.Presence(userID)
}

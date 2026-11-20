// 基础设施域路由：系统配置（13 操作 + 按 id/key 直取扩展 + 敏感词管理）。
//
// 权限点统一使用权限表实际存在的 config:read / config:update
// （此前引用的 setting:* 与 config:manage 在 RBAC 种子中不存在，导致端点对所有用户 403）。
//
// 路由顺序注意：/config/public、/config/advertisement、/config/key/:key、
// /config/group/:group 等静态段必须先于 /config/:id 注册，避免被参数段吞掉。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// infraConfigSvc 取进程级配置服务单例（含一级缓存与敏感词前缀树）。
//
// 走 infra.Default 而非 NewConfigService：router 与 asynq worker 必须共用同一个实例，
// 否则敏感词树会有两份，改词后两边生效时间不一致（详见 infra/singleton.go）。
func infraConfigSvc(opt Option) *infra.ConfigService {
	return infra.Default(opt.DB, opt.Redis)
}

// RegisterConfigRoutes 注册系统配置路由（挂载在 /api/v1 下）。
func RegisterConfigRoutes(v1 *gin.RouterGroup, opt Option) {
	h := handler.NewConfigHandler(infraConfigSvc(opt))
	g := v1.Group("/config")

	read := []string{"config:read", "config:update"}
	write := []string{"config:update"}

	// 公开端点（对齐原 @NoAuth）
	g.GET("/public", h.Public)
	g.GET("/advertisement", h.Advertisement)

	// 敏感词（静态段，先于 /:id）
	swRead := append(infraChain(opt, read...), h.ListSensitiveWords)
	g.GET("/sensitive-words", swRead...)
	g.POST("/sensitive-words", append(infraChain(opt, write...), h.CreateSensitiveWord)...)
	g.DELETE("/sensitive-words", append(infraChain(opt, write...), h.RemoveSensitiveWord)...)
	g.POST("/sensitive-words/batch", append(infraChain(opt, write...), h.BatchCreateSensitiveWords)...)
	g.DELETE("/sensitive-words/batch", append(infraChain(opt, write...), h.BatchRemoveSensitiveWords)...)
	g.POST("/sensitive-words/clear-cache", append(infraChain(opt, write...), h.ClearSensitiveWordsCache)...)

	// 分组与 key
	g.GET("/group/:group", append(infraChain(opt, read...), h.ByGroup)...)
	g.PATCH("/group/:group", append(infraChain(opt, write...), h.UpdateGroup)...)
	g.GET("/key/:key", append(infraChain(opt, read...), h.ByKey)...)

	// 根集合
	g.GET("", append(infraChain(opt, read...), h.List)...)
	g.POST("", append(infraChain(opt, write...), h.Create)...)
	g.PATCH("", append(infraChain(opt, write...), h.UpdateAll)...)

	// 按 id（参数段，最后注册）
	g.GET("/:id", append(infraChain(opt, read...), h.ByID)...)
	g.PATCH("/:id", append(infraChain(opt, write...), h.UpdateByID)...)
	g.DELETE("/:id", append(infraChain(opt, write...), h.Remove)...)
}

// 基础设施域路由：系统配置（13 操作 + 按 id/key 直取扩展 + 敏感词管理）。
//
// 权限点同时接受原实现的 setting:read / setting:update / setting:create
// 与任务书约定的 config:manage 命名（RequirePermissions 为 OR 语义）。
//
// 路由顺序注意：/config/public、/config/advertisement、/config/key/:key、
// /config/group/:group 等静态段必须先于 /config/:id 注册，避免被参数段吞掉。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// infraConfigSingleton 配置服务单例（内部含一级缓存与敏感词前缀树）。
var infraConfigSingleton *infra.ConfigService

// infraConfigSvc 取配置服务单例；首次构造时初始化默认配置种子与敏感词树。
func infraConfigSvc(opt Option) *infra.ConfigService {
	if infraConfigSingleton == nil {
		infraConfigSingleton = infra.NewConfigService(opt.DB, opt.Redis)
		// 幂等：已存在的 key 不覆盖；DB 不可用时返回 nil 错误，服务降级可用
		_ = infraConfigSingleton.EnsureSeeds()
	}
	return infraConfigSingleton
}

// RegisterConfigRoutes 注册系统配置路由（挂载在 /api/v1 下）。
func RegisterConfigRoutes(v1 *gin.RouterGroup, opt Option) {
	h := handler.NewConfigHandler(infraConfigSvc(opt))
	g := v1.Group("/config")

	read := []string{"setting:read", "config:manage"}
	write := []string{"setting:update", "setting:create", "config:manage"}

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

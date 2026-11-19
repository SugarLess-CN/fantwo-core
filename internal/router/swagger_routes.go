// Swagger UI 路由注册（swaggo/gin-swagger）。
//
// 为什么要单独一个文件、并接受 gin.IRouter：AGENTS.md 约定各域只在自己文件里
// 注册路由、不改 router.go 的 New()。这里用接口类型 gin.IRouter，
// 使同一函数既能挂根路径（传 *gin.Engine → /swagger/*any），
// 也能挂在业务组下（传 *gin.RouterGroup → /api/v1/swagger/*any），
// 调用点因此可以放在任意一侧而无需改动 router.go。
//
// 当前调用点：registry.go 的 RegisterAll 末尾（传 v1，故 UI 路径为
// /api/v1/swagger/index.html）。若希望暴露在根路径，把该调用换成
// router.New 里的 RegisterSwaggerRoutes(r, opt) 即可，函数无需改动。
//
// 生成命令（改了注解后重跑）：
//
//	swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// 生成的 swagger 包通过 init() 把文档注册进 swaggo 全局注册表，
	// ginSwagger.WrapHandler 依赖它，故此处必须空导入。
	_ "github.com/SugarLess-CN/fantwo-core/docs/swagger"
)

// RegisterSwaggerRoutes 挂载 Swagger UI。
//
// 开关：swagger.enabled（默认 false，生产勿开）。关闭时本函数直接返回，
// 不注册任何路由，避免文档在生产暴露。
func RegisterSwaggerRoutes(r gin.IRouter, cfg *config.Config) {
	if r == nil || cfg == nil || !cfg.Swagger.Enabled {
		return
	}
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

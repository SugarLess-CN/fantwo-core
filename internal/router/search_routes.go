// 基础设施域路由：搜索管理（3 个管理操作 + 文章检索）。
//
// 权限点对齐原 search.controller.ts 的 @Permissions("article:manage")；
// 检索端点 GET /search 用可选登录（游客可搜，登录用户可将来按权限过滤）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// infraSearchSingleton 搜索服务单例（ES 客户端懒初始化，进程内复用）。
var infraSearchSingleton *infra.Searcher

// infraSearchLogsSingleton 检索日志服务单例（热门搜索词来源）。
var infraSearchLogsSingleton *infra.SearchLogService

// infraSearchSvc 取搜索服务单例（同时注入检索日志服务）。
func infraSearchSvc(opt Option) *infra.Searcher {
	if infraSearchSingleton == nil {
		cfg := config.LoadInfra()
		infraSearchLogsSingleton = infra.NewSearchLogService(opt.DB, opt.Redis)
		infraSearchSingleton = infra.NewSearcher(cfg.Search).WithLogs(infraSearchLogsSingleton)
	}
	return infraSearchSingleton
}

// infraSearchLogsSvc 取检索日志服务单例（热门搜索词聚合用）。
func infraSearchLogsSvc(opt Option) *infra.SearchLogService {
	if infraSearchLogsSingleton == nil {
		infraSearchLogsSingleton = infra.NewSearchLogService(opt.DB, opt.Redis)
	}
	return infraSearchLogsSingleton
}

// RegisterSearchRoutes 注册搜索管理路由（挂载在 /api/v1 下）。
func RegisterSearchRoutes(v1 *gin.RouterGroup, opt Option) {
	h := handler.NewSearchHandler(infraSearchSvc(opt), opt.DB, infraSearchLogsSvc(opt))
	g := v1.Group("/search")

	// 管理端点（article:manage）
	g.POST("/sync/articles", append(infraChain(opt, "article:manage"), h.SyncArticles)...)
	g.GET("/status", append(infraChain(opt, "article:manage"), h.Status)...)
	g.POST("/clear/articles", append(infraChain(opt, "article:manage"), h.ClearArticles)...)

	// 检索端点（可选登录）：/search 检索 + /search/hot 热门词（公开聚合）
	searchChain := make([]gin.HandlerFunc, 0, 1)
	if opt.Signer != nil {
		searchChain = append(searchChain, middleware.OptionalAuth(opt.Signer))
	}
	g.GET("", append(searchChain, h.SearchArticles)...)
	g.GET("/hot", h.HotKeywords)
}

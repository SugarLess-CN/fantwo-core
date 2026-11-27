// 基础设施域路由：搜索管理（3 个管理操作 + 文章检索）。
//
// 权限点对齐原 search.controller.ts 的 @Permissions("article:manage")；
// 检索端点 GET /search 用可选登录（游客可搜，登录用户可将来按权限过滤）。
package router

import (
	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// infraSearchSingleton 搜索服务单例（ES 客户端懒初始化，进程内复用）。
var infraSearchSingleton *infra.Searcher

// infraSearchLogsSingleton 检索日志服务单例（热门搜索词来源）。
var infraSearchLogsSingleton *infra.SearchLogService

// infraSearchSvc 取搜索服务单例（同时注入检索日志服务与进程级配置服务）。
//
// 注入 infraConfigSvc（= infra.Default 单例）后，search_enabled / search_addresses /
// search_username / search_password / search_articles_index 全部 DB 优先、env 兜底：
// 管理端改配置 → 下一次请求在读到时按签名惰性重建 ES client，无需重启。
func infraSearchSvc(opt Option) *infra.Searcher {
	if infraSearchSingleton == nil {
		cfg := config.LoadInfra()
		infraSearchLogsSingleton = infra.NewSearchLogService(opt.DB, opt.Redis)
		infraSearchSingleton = infra.NewSearcher(cfg.Search).
			WithLogs(infraSearchLogsSingleton).
			WithConfigService(infraConfigSvc(opt))
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
	h := handler.NewSearchHandler(infraSearchSvc(opt), opt.DB, infraSearchLogsSvc(opt), opt.Logger)
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

	// 搜索结果的「分类维度」：标签 / 用户。
	//
	// 为什么单独建而不让前端调 /tag 与 /user：
	//  · /tag?keyword= 等价可用，但前端要的是「搜索页的一个 tab」，路径语义应归属于 /search；
	//  · /user 需要 user:manage（管理端分页），普通用户搜人用不了；
	//  · /search/users 返回 UserSearchItemDto（**不含 email/phone/wallet**），公开安全。
	// 两条都是可选登录：游客能搜，登录后额外拿到 is_followed。
	userH := handler.NewUserHandler(service.NewUserService(buildUserDeps(opt)))
	g.GET("/tags", append(searchChain, contentHandlers(opt).Tag.SearchTags)...)
	g.GET("/users", append(searchChain, userH.SearchUsers)...)
}

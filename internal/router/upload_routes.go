// 基础设施域路由：上传管理（10 操作 + 删除）。
//
// 约定（见 internal/router/registry.go）：
//   - 本文件只注册上传域，不触碰其它域；
//   - 中间件链由 infraChain 统一构造：JWT 强制鉴权 + RBAC 权限点（OR 语义）；
//   - NoAuth 端点（/upload/config、/upload/info/:id）不加鉴权中间件，对齐原 @NoAuth。
package router

import (
	"os"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// infraUploadService 进程内单例：避免每个请求重复加载配置/初始化 S3 客户端。
var infraUploadSingleton *infra.Infra

// infraUploadSvc 取上传服务单例（首次调用时按配置构造）。
func infraUploadSvc(opt Option) *infra.Infra {
	if infraUploadSingleton == nil {
		cfg := config.LoadInfra()
		infraUploadSingleton = infra.New(opt.DB, cfg)
		if opt.Redis != nil {
			infraUploadSingleton = infraUploadSingleton.WithRedis(opt.Redis)
		}
		// 水印参数来自 config 表（与系统配置域共用同一服务实例）
		infraUploadSingleton = infraUploadSingleton.WithConfigService(infraConfigSvc(opt))
	}
	return infraUploadSingleton
}

// infraChain 构造「强制鉴权 + 权限点」中间件链；perms 为空表示仅需登录。
// opt.Signer 为 nil（测试/降级）时跳过 JWT，但仍保留权限点校验（无 claims 即 401）。
func infraChain(opt Option, perms ...string) []gin.HandlerFunc {
	chain := make([]gin.HandlerFunc, 0, 2)
	if opt.Signer != nil {
		chain = append(chain, middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis)))
	}
	if len(perms) > 0 {
		chain = append(chain, middleware.RequirePermissions(perms...))
	}
	return chain
}

// RegisterUploadRoutes 注册上传管理路由（挂载在 /api/v1 下）。
func RegisterUploadRoutes(v1 *gin.RouterGroup, opt Option) {
	svc := infraUploadSvc(opt)
	h := handler.NewUploadHandler(svc)

	// 本地存储：暴露静态文件路由 /api/v1/uploads/*（CDN 模式由 cdn.base_url 直出，不走这里）
	if dir := svc.LocalDir(); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			v1.Static("/uploads", dir)
		}
	}

	g := v1.Group("/upload")

	// 公开端点（对齐原 @NoAuth）
	g.GET("/config", h.Config)
	g.GET("/info/:id", h.Info)

	// 需登录 + 权限点
	// 列表：仅要求登录；有 upload:list 者看全站，其余只看自己（handler 内降级）
	g.GET("", append(infraChain(opt), h.List)...)
	g.POST("/file", append(infraChain(opt, "upload:create"), h.UploadFile)...)
	g.POST("/avatar", append(infraChain(opt, "upload:create"), h.UploadAvatar)...)
	g.POST("/presign", append(infraChain(opt, "upload:create"), h.Presign)...)
	g.POST("/confirm", append(infraChain(opt, "upload:create"), h.Confirm)...)

	// 管理端点
	g.GET("/migration/stats", append(infraChain(opt, "upload:delete"), h.MigrationStats)...)
	g.POST("/migration/process", append(infraChain(opt, "upload:delete"), h.MigrationProcess)...)
	g.POST("/migration/metadata", append(infraChain(opt, "upload:delete"), h.MigrationMetadata)...)
	g.DELETE("/:id", append(infraChain(opt, "upload:delete"), h.Remove)...)
}

// Package router 路由注册：全局前缀 /api/v1 + 健康检查端点。
//
// 对齐原仓库：全局路由前缀 api/v1（NestJS setGlobalPrefix），
// CORS 全开、gzip、请求日志、recovery 全局挂载；
// JWT 鉴权后续在业务路由组按原 guard 语义挂载（默认鉴权 + @NoAuth 例外）。
package router

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
)

// Option 路由依赖注入。
// DB 可为 nil（无数据库降级启动），各域路由需自行判空。
type Option struct {
	Signer *jwtx.Signer
	Logger *zap.Logger
	DB     *gorm.DB
	Cfg    *config.Config
	Redis  *redis.Client
}

// New 构建 gin 引擎并注册全部路由。
func New(opt Option) *gin.Engine {
	gin.SetMode(gin.ReleaseMode) // 默认 release；debug 由环境变量 GIN_MODE 覆盖
	r := gin.New()

	// 全局中间件（顺序对齐原仓库：日志 → recovery → gzip → CORS）
	r.Use(middleware.ZapLogger(opt.Logger))
	r.Use(middleware.ZapRecovery(opt.Logger))
	r.Use(gzip.Gzip(gzip.DefaultCompression))
	r.Use(middleware.CORS())

	// 健康检查（不走 /api/v1 前缀，便于探针直连）
	r.GET("/health", health)
	r.GET("/ping", ping)

	// 业务 API：/api/v1
	v1 := r.Group("/api/v1")
	{
		// 根探测（对齐原 AppController.getHello，swagger 路径 "/"）
		v1.GET("", appHello)

		// 各业务域在此统一注册（见 registry.go；每域一行，勿交叉改动）
		RegisterAll(v1, opt)
	}

	return r
}

// appHello 根探测接口（对齐原 AppController_getHello）。
func appHello(c *gin.Context) {
	response.Success(c, gin.H{"hello": "PicArt API"})
}

// health 存活探针：{code:0, message:"ok", data:{...}}。
func health(c *gin.Context) {
	response.Health(c, gin.H{
		"status":    "up",
		"timestamp": time.Now().UnixMilli(),
	})
}

// ping 同 health，供简单探活。
func ping(c *gin.Context) {
	response.Health(c, gin.H{"pong": time.Now().UnixMilli()})
}

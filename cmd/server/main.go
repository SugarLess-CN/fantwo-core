// fantwo-core 服务入口。
//
// 启动流程：加载配置 → 初始化 zap → 懒连接 PostgreSQL/Redis（缺失只 warning）
// → AutoMigrate（DB 可用时）→ 注册路由 → HTTP 服务 → 优雅退出。

// Swagger 全局元信息。swag init 以 -g cmd/server/main.go 指向本文件；
// 注解口径对齐原仓库 swagger.json 的 info.title / info.description 与全局前缀 api/v1。
//
//	@title			PicArt API
//	@version		1.0
//	@description	PicArt 图片社区 API 文档（Go + Gin 重构版，与原 NestJS swagger.json 语义对齐）
//	@termsOfService	https://github.com/SugarLess-CN/fantwo-core
//
//	@contact.name	fantwo-core
//	@contact.url	https://github.com/SugarLess-CN/fantwo-core
//
//	@host		localhost:3000
//	@BasePath	/api/v1
//	@schemes	http https
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				先调 POST /user/login 取 token，再填 "Bearer {token}"
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/database"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/router"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/SugarLess-CN/fantwo-core/migrations"
	"github.com/hibiken/asynq"
	"github.com/spf13/cast"
	"go.uber.org/zap"
)

func main() {
	// 1. 配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	// 2. 日志
	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()
	if cfg.Log.Format == "console" {
		logger, _ = zap.NewDevelopment()
	}
	zap.ReplaceGlobals(logger)

	// 3. 数据层（懒连接：失败降级，不 panic）
	db := database.MustInitPostgres(&cfg.Postgres)
	rdb := database.MustInitRedis(&cfg.Redis)
	_ = rdb // 队列/缓存批次接入

	// 4. 迁移（DB 可用时）
	if err := migrations.Run(db); err != nil {
		logger.Warn("AutoMigrate 失败", zap.Error(err))
	}

	// 5. JWT 签发器
	signer := jwtx.NewSigner(
		cfg.JWT.Secret,
		cfg.JWT.Issuer,
		cast.ToDuration(cfg.JWT.ExpiresIn),
		cast.ToDuration(cfg.JWT.RefreshExpiresIn),
	)

	// 6. 异步队列 worker（asynq；无 Redis 时跳过，不阻塞启动）
	var asynqSrv *asynq.Server
	if rdb != nil {
		auditSvc := service.NewAuditService(db, queue.FromRedisClient(rdb))
		// 关键：worker 侧的审核实例同样要注入本地敏感词检查。
		// 否则 provider=none（自检模式）时 worker 的 sensitive==nil，
		// 敏感词被完全跳过 → 含违禁词的内容照常 PASSED 放行
		// （2026-11-21 实测发现：该实例与路由层是两个对象，只给路由层注入不够）。
		// 与 router 层共用同一实例（infra.Default 是进程级单例），
		// 避免出现两份敏感词树导致「改词后审核用旧表」。
		if cfgSvc := infra.Default(db, rdb); cfgSvc != nil {
			_ = cfgSvc.ReloadSensitiveWords()
			auditSvc.SetSensitiveChecker(cfgSvc.CheckText)
			logger.Info("审核 worker 已注入本地敏感词检查", zap.Int("trieSize", cfgSvc.TrieSize()))
		} else {
			logger.Warn("审核 worker 未能注入敏感词检查（cfgSvc 为 nil）")
		}
		mux := asynq.NewServeMux()
		auditSvc.RegisterQueueHandlers(mux) // 图片/文本审核 worker（并发 5/10，重试 5 次指数退避）
		asynqSrv = queue.NewServer(cfg.Redis.Addr, cfg.Redis.Password)
		go func() {
			if err := asynqSrv.Run(mux); err != nil {
				logger.Error("队列 worker 退出", zap.Error(err))
			}
		}()
		logger.Info("队列 worker 已启动",
			zap.Strings("queues", queue.QueueNames),
			zap.Ints("concurrency", []int{5, 10, 2}))
	} else {
		logger.Warn("Redis 不可用，跳过队列 worker 启动")
	}

	// 7. 路由
	// 注意：WebSocket 端点 /api/v1/ws-message 由消息域（message_routes.go）注册，
	// 此处不得重复注册，否则 gin 启动即 panic（handlers are already registered）。
	// /health 的依赖摘要（PG/Redis 状态、队列统计）需先注入。
	router.SetHealthDeps(db, rdb)

	engine := router.New(router.Option{
		Signer: signer,
		Logger: logger,
		DB:     db,
		Cfg:    cfg,
		Redis:  rdb,
	})

	srv := &http.Server{
		Addr:    ":" + cast.ToString(cfg.Server.Port),
		Handler: engine,
	}

	// 8. 启动
	go func() {
		logger.Info("服务启动", zap.Int("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP 服务异常退出", zap.Error(err))
		}
	}()

	// 9. 优雅退出（对齐原 enableShutdownHooks）
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("收到退出信号，开始优雅关闭")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("优雅关闭超时", zap.Error(err))
	}
	if asynqSrv != nil {
		asynqSrv.Shutdown()
		logger.Info("队列 worker 已停止")
	}
	logger.Info("服务已退出")
}

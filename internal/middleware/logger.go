package middleware

import (
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// stack 返回当前 goroutine 堆栈。
func stack() []byte { return debug.Stack() }

// ZapLogger 请求日志中间件（对齐原 LoggingInterceptor）。
func ZapLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		fields := []zap.Field{
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", latency),
			zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()),
		}
		if len(c.Errors) > 0 {
			logger.Error("request", fields...)
			return
		}
		if c.Writer.Status() >= 500 {
			logger.Error("request", fields...)
			return
		}
		// 健康探针的成功日志跳过。
		//
		// Dockerfile 的 HEALTHCHECK 每 30 秒探一次 /health，一天 2880 条，
		// 而它恒返回 200、没有任何诊断价值 —— 实测它占了生产日志量的 99% 以上，
		// 把真正的业务日志淹没了。
		//
		// 健康探针的成功日志**降到 DEBUG**（不是丢弃）。
		//
		// 语义依据（internal/pkg/logger 包注释）：
		//   info  只打"必要信息" —— 30s 一次的恒 200 探针不属于必要信息
		//   debug 打"调试信息"   —— 排查健康检查本身时才需要看
		//
		// 为什么不是直接 return：`LOG_LEVEL=debug` 时应该能复盘探针频率与耗时，
		// 直接丢弃会让"为什么健康检查变慢/变频繁"无从查起。
		//
		// **失败仍然记 ERROR**：5xx 与带 error 的情况在上面分支已经处理，
		// 探针失败（依赖挂了）必须看得见。
		if isHealthProbe(path) {
			logger.Debug("request", fields...)
			return
		}
		logger.Info("request", fields...)
	}
}

// isHealthProbe 健康探针路径（成功时不记访问日志）。
//
// 同时覆盖根路径与 api/v1 前缀两种形态，避免将来加别名时漏掉。
func isHealthProbe(path string) bool {
	return path == "/health" || path == "/api/v1/health"
}

// ZapRecovery panic 恢复中间件：捕获 panic 记入 zap 并返回 500。
// 对齐原 HttpExceptionFilter 的兜底行为（未知异常也返回统一结构）。
func ZapRecovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error("panic recovered",
					zap.Any("error", err),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.ByteString("stack", stack()),
				)
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}

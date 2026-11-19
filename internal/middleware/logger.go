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
		logger.Info("request", fields...)
	}
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

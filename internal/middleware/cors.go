package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件。
// 原仓库 enableCors() 无配置（全开），保持一致；后续收紧白名单时改 AllowOrigins。
func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-Device-Id"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false, // AllowAllOrigins 时必须为 false（gin-contrib/cors 约束）
	})
}

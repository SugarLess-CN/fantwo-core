package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件。
// 原仓库 enableCors() 无配置（全开），保持一致；后续收紧白名单时改 AllowOrigins。
func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		// 设备标识三种写法都要放行 —— 前端习惯各不相同，漏一个就会在**浏览器**层被拦
		// （请求根本到不了后端，日志里连记录都没有，极难排查）：
		//   · `Device-Id`   —— 我们文档/curl 示例用的写法，也是 deviceIDOf 优先读的
		//   · `device-id`   —— HTTP header 大小写不敏感，浏览器发的实际是规范化的形式
		//   · `X-Device-Id` —— 历史遗留（原 NestJS 客户端用过 X- 前缀）
		// 2026-11-28 实测：AllowHeaders 里只有 X-Device-Id 时，前端发 Device-Id
		// 的预检会被拒绝，而 deviceIDOf 又读不到 X-Device-Id → 跨域登录直接死锁。
		AllowHeaders: []string{
			"Origin", "Content-Length", "Content-Type", "Authorization",
			"Device-Id", "device-id", "X-Device-Id",
		},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false, // AllowAllOrigins 时必须为 false（gin-contrib/cors 约束）
	})
}

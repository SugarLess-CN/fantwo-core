// Package router 运营域路由注册（积分/成就/表情包/装饰品/轮播/举报/邀请）。
//
// 本文件只做路由挂接：构建运营域 handler，再按原 NestJS controller 的路径与
// 守卫语义注册端点。不改 router.go，只在 registry.go 里调用自己那一行
// RegisterOpsRoutes（并行纪律）。
//
// 鉴权语义（对齐原 controller 装饰器）：
//   - 默认 JWT（middleware.JWTAuth）；
//   - @NoAuth() → middleware.OptionalAuth；
//   - @Permissions(...) → middleware.RequirePermissions(...)。
//
// 中间件以完整链的形式随每条路由注册（gin 的 variadic handlers），
// 不用 group 级中间件 —— 同一前缀下公开/受保护端点混排，group 级会误伤。
package router

import (
	"net/http"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RegisterOpsRoutes 注册运营域全部路由（对齐原 NestJS 7 个 controller）。
func RegisterOpsRoutes(v1 *gin.RouterGroup, opt Option) {
	h := handler.NewOps(opt.DB, opt.Signer)
	auth := func(perms ...string) []gin.HandlerFunc { return guard(opt, perms...) }
	noAuth := optAuth(opt)

	// ---- 积分管理 /points（原 PointsController，16 操作）----
	points := v1.Group("/points")
	{
		points.POST("/add", with(auth(), h.PointsAdd)...)
		points.POST("/spend", with(auth(), h.PointsSpend)...)
		points.GET("/balance", with(auth(), h.PointsBalance)...)
		points.GET("/transactions", with(auth(), h.PointsTransactions)...)
		points.GET("/transactions/all", with(auth("points:manage"), h.PointsTxAll)...)
		points.GET("/statistics", with(auth("points:manage"), h.PointsStatistics)...)
		points.GET("/users/:userId/balance", with(auth("points:manage"), h.PointsUserBalance)...)
		points.GET("/users/:userId/transactions", with(auth("points:manage"), h.PointsUserTransactions)...)
		points.GET("/tasks/my", with(auth(), h.PointsMyTasks)...)
		points.GET("/tasks/my/:activityId", with(auth(), h.PointsMyTaskProgress)...)
		points.POST("/activities", with(auth("points:manage"), h.PointsActivityCreate)...)
		points.GET("/activities", with(noAuth, h.PointsActivityList)...)
		points.GET("/activities/:id", with(noAuth, h.PointsActivityGet)...)
		points.PATCH("/activities/:id", with(auth("points:manage"), h.PointsActivityUpdate)...)
		points.DELETE("/activities/:id", with(auth("points:manage"), h.PointsActivityDelete)...)
		points.POST("/activities/:id/claim", with(auth(), h.PointsActivityClaim)...)
	}

	// ---- 成就管理 /achievement（原 AchievementController，8 操作）----
	achievement := v1.Group("/achievement")
	{
		achievement.POST("", with(auth("achievement:manage"), h.AchievementCreate)...)
		achievement.GET("", with(noAuth, h.AchievementList)...)
		achievement.GET("/stats", with(auth(), h.AchievementStats)...)
		achievement.GET("/:id", with(noAuth, h.AchievementGet)...)
		achievement.PATCH("/:id", with(auth("achievement:manage"), h.AchievementUpdate)...)
		achievement.DELETE("/:id", with(auth("achievement:manage"), h.AchievementDelete)...)
		achievement.POST("/:id/claim", with(auth(), h.AchievementClaim)...)
		achievement.POST("/claim-all", with(auth(), h.AchievementClaimAll)...)
	}

	// ---- 表情包管理 /emoji（原 EmojiController 13 + EmojiPackController 8）----
	emoji := v1.Group("/emoji")
	{
		emoji.POST("", with(auth("emoji:manage"), h.EmojiCreate)...)
		emoji.POST("/upload", with(auth(), h.EmojiUpload)...)
		emoji.GET("", with(noAuth, h.EmojiList)...)
		emoji.GET("/categories/list", with(noAuth, h.EmojiCategories)...)
		emoji.GET("/popular/list", with(noAuth, h.EmojiPopular)...)
		emoji.GET("/favorites/list", with(auth(), h.EmojiFavorites)...)
		emoji.GET("/recent/list", with(auth(), h.EmojiRecent)...)
		emoji.GET("/:id", with(noAuth, h.EmojiGet)...)
		emoji.PATCH("/:id", with(auth(), h.EmojiUpdate)...)
		emoji.DELETE("/:id", with(auth(), h.EmojiDelete)...)
		emoji.POST("/:id/favorite", with(auth(), h.EmojiFavoriteAdd)...)
		emoji.DELETE("/:id/favorite", with(auth(), h.EmojiFavoriteRemove)...)
		emoji.POST("/:id/use", with(auth(), h.EmojiUse)...)

		// 表情包子树（固定前缀 /packs，与 /:id 通配区分）
		emoji.POST("/packs", with(auth(), h.EmojiPackCreate)...)
		emoji.GET("/packs", with(noAuth, h.EmojiPackList)...)
		emoji.GET("/packs/:id", with(noAuth, h.EmojiPackGet)...)
		emoji.PATCH("/packs/:id", with(auth(), h.EmojiPackUpdate)...)
		emoji.DELETE("/packs/:id", with(auth(), h.EmojiPackDelete)...)
		emoji.POST("/packs/:id/emojis", with(auth(), h.EmojiPackAddEmoji)...)
		emoji.PATCH("/packs/:id/emojis/:emojiId", with(auth(), h.EmojiPackUpdateEmoji)...)
		emoji.DELETE("/packs/:id/emojis/:emojiId", with(auth(), h.EmojiPackRemoveEmoji)...)
	}

	// ---- 装饰品管理 /decoration（原 DecorationController，21 操作）----
	decoration := v1.Group("/decoration")
	{
		decoration.POST("", with(auth("decoration:manage"), h.DecorationCreate)...)
		decoration.GET("", with(auth(), h.DecorationList)...)
		decoration.GET("/achievement-badges/my", with(auth(), h.DecorationMyBadges)...)
		decoration.GET("/activity", with(auth(), h.DecorationActivityList)...)
		decoration.POST("/activity", with(auth("decoration:manage"), h.DecorationActivityCreate)...)
		decoration.GET("/activity/progress/my", with(auth(), h.DecorationMyActivityProgress)...)
		decoration.POST("/activity/claim/:activityId", with(auth(), h.DecorationActivityClaim)...)
		decoration.GET("/activity/:id", with(auth(), h.DecorationActivityGet)...)
		decoration.PATCH("/activity/:id", with(auth("decoration:manage"), h.DecorationActivityUpdate)...)
		decoration.DELETE("/activity/:id", with(auth("decoration:manage"), h.DecorationActivityDelete)...)
		decoration.POST("/clean-expired", with(auth("decoration:manage"), h.DecorationCleanExpired)...)
		decoration.POST("/gift", with(auth(), h.DecorationGift)...)
		decoration.POST("/purchase", with(auth(), h.DecorationPurchase)...)
		decoration.POST("/unuse/:decorationId", with(auth(), h.DecorationUnuse)...)
		decoration.POST("/use/:decorationId", with(auth(), h.DecorationUse)...)
		decoration.GET("/user/current/decorations", with(auth(), h.DecorationMyCurrent)...)
		decoration.GET("/user/my", with(auth(), h.DecorationMyList)...)
		decoration.GET("/user/:userId", with(auth(), h.DecorationUserList)...)
		decoration.DELETE("/:id", with(auth("decoration:manage"), h.DecorationDelete)...)
		decoration.GET("/:id", with(auth(), h.DecorationGet)...)
		decoration.PATCH("/:id", with(auth("decoration:manage"), h.DecorationUpdate)...)
	}

	// ---- 轮播管理 /banners（原 BannerController，6 操作）----
	banners := v1.Group("/banners")
	{
		banners.POST("", with(auth("banner:create"), h.BannerCreate)...)
		banners.GET("", with(auth("banner:list"), h.BannerList)...)
		banners.GET("/active", with(noAuth, h.BannerActive)...)
		banners.GET("/:id", with(noAuth, h.BannerGet)...)
		banners.PATCH("/:id", with(auth("banner:update"), h.BannerUpdate)...)
		banners.DELETE("/:id", with(auth("banner:delete"), h.BannerDelete)...)
	}

	// ---- 举报管理 /report（原 ReportController，6 操作）----
	report := v1.Group("/report")
	{
		report.POST("", with(auth(), h.ReportCreate)...)
		report.GET("", with(auth("report:manage"), h.ReportList)...)
		report.GET("/statistics", with(auth("report:manage"), h.ReportStatistics)...)
		report.GET("/:id", with(auth("report:manage"), h.ReportGet)...)
		report.PATCH("/:id", with(auth("report:manage"), h.ReportUpdate)...)
		report.DELETE("/:id", with(auth("report:manage"), h.ReportDelete)...)
	}

	// ---- 邀请管理 /invite（原 InviteController，6 操作）----
	invite := v1.Group("/invite")
	{
		invite.POST("", with(auth(), h.InviteCreate)...)
		invite.POST("/use", with(auth(), h.InviteUse)...)
		invite.GET("/my", with(auth(), h.InviteMyList)...)
		invite.GET("/stats", with(auth(), h.InviteStats)...)
		invite.GET("/earnings", with(auth(), h.InviteEarnings)...)
		invite.GET("/:id", with(auth(), h.InviteGet)...)
	}
}

// with 把「中间件链 + 业务 handler」拼成 gin 需要的 handler 列表。
func with(mws []gin.HandlerFunc, h gin.HandlerFunc) []gin.HandlerFunc {
	out := make([]gin.HandlerFunc, 0, len(mws)+1)
	out = append(out, mws...)
	out = append(out, h)
	return out
}

// guard 强制 JWT + 可选权限点（对齐 @UseGuards(JwtAuthGuard[, PermissionGuard])）。
// opt.DB 为 nil（降级启动）时返回 503 占位，保证路由表完整、服务可启动。
func guard(opt Option, perms ...string) []gin.HandlerFunc {
	if opt.DB == nil {
		return []gin.HandlerFunc{degraded()}
	}
	if len(perms) == 0 {
		return []gin.HandlerFunc{middleware.JWTAuth(opt.Signer)}
	}
	return []gin.HandlerFunc{middleware.JWTAuth(opt.Signer), middleware.RequirePermissions(perms...)}
}

// optAuth 可选登录（对齐 @NoAuth()）。
func optAuth(opt Option) []gin.HandlerFunc {
	if opt.DB == nil {
		return []gin.HandlerFunc{degraded()}
	}
	return []gin.HandlerFunc{middleware.OptionalAuth(opt.Signer)}
}

// degraded 降级占位：无数据库时返回 503。
func degraded() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, response.Body{
			Code:      http.StatusServiceUnavailable,
			Message:   "数据库不可用，运营域端点降级",
			Data:      nil,
			Timestamp: time.Now().UnixMilli(),
		})
	}
}

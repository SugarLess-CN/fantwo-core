// 内容域路由：文章管理（含点赞/收藏/不喜欢/浏览历史/分享）。
//
// 路由前缀严格对齐原仓库 swagger（全局前缀 /api/v1 + controller path "article"）。
// 鉴权语义对齐原 guard：默认 JwtAuthGuard + @NoAuth() 可选登录 + @Permissions 权限点。
package router

import (
	"sync"

	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// contentOnce / contentSet 保证五个域共享同一套 handler（懒构造，只建一次）。
var (
	contentOnce sync.Once
	contentSet  *handler.ContentHandlers
)

// contentHandlers 取内容域 handler 集合（懒构造）。
func contentHandlers(opt Option) *handler.ContentHandlers {
	contentOnce.Do(func() {
		contentSet = handler.NewContentHandlers(opt.DB, opt.Logger)
	})
	return contentSet
}

// authMid 强制鉴权中间件；Signer 未配置时直接 401（避免 nil 解引用）。
func authMid(opt Option) gin.HandlerFunc {
	if opt.Signer == nil {
		return func(c *gin.Context) {
			response.Fail(c, errcode.ErrUnauthorized)
		}
	}
	return middleware.JWTAuth(opt.Signer)
}

// optionalMid 可选登录中间件；Signer 未配置时注入空身份放行。
func optionalMid(opt Option) gin.HandlerFunc {
	if opt.Signer == nil {
		return func(c *gin.Context) {
			c.Set(middleware.CtxUserKey, nil)
			c.Next()
		}
	}
	return middleware.OptionalAuth(opt.Signer)
}

// noAuthMid 免登录直通（对应原 @NoAuth() 且无需用户身份的端点）。
func noAuthMid(opt Option) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.CtxUserKey, nil)
		c.Next()
	}
}

// permMid 权限点校验（OR 关系，对齐原 PermissionGuard）。
func permMid(perms ...string) gin.HandlerFunc {
	return middleware.RequirePermissions(perms...)
}

// RegisterArticleRoutes 注册文章管理路由（33 操作）。
func RegisterArticleRoutes(v1 *gin.RouterGroup, opt Option) {
	h := contentHandlers(opt).Article
	auth := authMid(opt)
	opt1 := optionalMid(opt)
	free := noAuthMid(opt)

	g := v1.Group("/article")

	// ---------- 集合与检索（静态路径优先注册） ----------
	g.GET("", opt1, h.List)
	g.POST("", auth, permMid("article:create"), h.Create)
	g.GET("/drafts", auth, h.Drafts)
	g.GET("/search", opt1, h.Search)
	g.GET("/hot-search", opt1, h.HotSearch)
	g.GET("/published/ids", free, h.PublishedIDs)
	g.GET("/recommend/:id", opt1, h.Recommendations)
	g.GET("/author/:id", opt1, h.ByAuthor)

	// ---------- 我的互动列表 ----------
	g.GET("/liked", auth, h.Liked)
	g.GET("/favorited/list", auth, h.FavoritedList)
	g.GET("/disliked/list", auth, h.DislikedList)

	// ---------- 浏览历史 ----------
	g.DELETE("/browse", auth, h.BrowseClear)
	g.GET("/browse/history", auth, h.BrowseHistory)
	g.GET("/browse/stats", auth, h.BrowseStats)
	g.GET("/browse/recent", auth, h.BrowseRecent)
	g.POST("/browse/batch-delete", auth, h.BrowseBatchDelete)
	g.GET("/browse/:articleId", auth, h.BrowseOne)
	g.DELETE("/browse/:articleId", auth, h.BrowseDelete)

	// ---------- 单篇文章：互动 ----------
	g.GET("/:id/like/count", free, h.LikeCount)
	g.GET("/:id/like/status", opt1, h.LikeStatus)
	g.POST("/:id/like", auth, h.Like)
	g.POST("/:id/favorite", auth, h.Favorite)
	g.DELETE("/:id/favorite", auth, h.Unfavorite)
	g.GET("/:id/favorite/status", auth, h.FavoriteStatus)
	g.POST("/:id/dislike", auth, h.Dislike)
	g.DELETE("/:id/dislike", auth, h.CancelDislike)
	g.GET("/:id/dislike/status", auth, h.DislikeStatus)
	g.POST("/:id/share", opt1, h.Share)
	g.POST("/:id/browse/progress", auth, h.BrowseProgress)

	// ---------- 单篇文章：管理端 ----------
	g.PATCH("/:id/featured", auth, h.SetFeatured)
	g.PATCH("/:id/profile-pin", auth, h.SetProfilePin)
	g.GET("/:id", opt1, h.FindOne)
	g.PATCH("/:id", auth, permMid("article:update"), h.Update)
	g.DELETE("/:id", auth, permMid("article:delete"), h.Remove)
}

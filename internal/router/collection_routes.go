// 内容域路由：合集管理（10 操作 + 合集内重排 1）。
package router

import "github.com/gin-gonic/gin"

// RegisterCollectionRoutes 注册合集管理路由。
func RegisterCollectionRoutes(v1 *gin.RouterGroup, opt Option) {
	h := contentHandlers(opt).Collection
	auth := authMid(opt)

	g := v1.Group("/collection", auth)

	g.POST("", h.Create)
	g.GET("", h.List)

	// 静态段优先于 /:id
	g.GET("/check/:articleId", h.CheckArticle)
	g.GET("/article/:articleId/info", h.ArticleCollectionInfo)

	g.GET("/:id", h.FindOne)
	g.PATCH("/:id", h.Update)
	g.DELETE("/:id", h.Remove)
	g.GET("/:id/items", h.ListItems)
	g.PUT("/:id/items/order", h.ReorderItems)
	g.POST("/:id/article/:articleId", h.AddArticle)
	g.DELETE("/:id/article/:articleId", h.RemoveArticle)
}

// 内容域路由：评论管理（10 操作）。
package router

import "github.com/gin-gonic/gin"

// RegisterCommentRoutes 注册评论管理路由。
func RegisterCommentRoutes(v1 *gin.RouterGroup, opt Option) {
	h := contentHandlers(opt).Comment
	auth := authMid(opt)
	opt1 := optionalMid(opt)

	g := v1.Group("/comment")

	g.POST("", auth, permMid("comment:create"), h.Create)
	g.GET("", auth, permMid("comment:manage"), h.FindAll)

	// 文章维度（静态段优先于 /:id）
	g.GET("/article/:id", opt1, h.FindByArticle)
	g.GET("/article/:id/count", opt1, h.Count)
	g.GET("/user/:userId", opt1, h.UserComments)

	g.GET("/:id", opt1, h.FindOne)
	g.PATCH("/:id", auth, permMid("comment:update"), h.Update)
	g.PATCH("/:id/pin", auth, h.SetPin)
	g.DELETE("/:id", auth, permMid("comment:delete"), h.Remove)
	g.POST("/:id/like", auth, h.Like)
}

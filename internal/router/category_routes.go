// 内容域路由：分类管理（5 操作）。
package router

import "github.com/gin-gonic/gin"

// RegisterCategoryRoutes 注册分类管理路由。
func RegisterCategoryRoutes(v1 *gin.RouterGroup, opt Option) {
	h := contentHandlers(opt).Category
	auth := authMid(opt)
	opt1 := optionalMid(opt)

	g := v1.Group("/category")

	g.POST("", auth, permMid("category:create"), h.Create)
	g.GET("", opt1, h.List)
	g.GET("/:id", opt1, h.FindOne)
	g.PATCH("/:id", auth, permMid("category:update"), h.Update)
	g.DELETE("/:id", auth, permMid("category:delete"), h.Remove)
}

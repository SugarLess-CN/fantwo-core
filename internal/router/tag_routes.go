// 内容域路由：标签管理（9 操作 + 热门标签统计 1）。
package router

import "github.com/gin-gonic/gin"

// RegisterTagRoutes 注册标签管理路由。
func RegisterTagRoutes(v1 *gin.RouterGroup, opt Option) {
	h := contentHandlers(opt).Tag
	auth := authMid(opt)
	opt1 := optionalMid(opt)

	g := v1.Group("/tag")

	g.POST("", auth, permMid("tag:create"), h.Create)
	g.GET("", opt1, h.List)
	g.GET("/recommended", opt1, h.Recommended)
	g.GET("/hot", opt1, h.Hot)
	g.GET("/followed/list", opt1, h.FollowedList)

	g.GET("/:id", opt1, h.FindOne)
	g.PATCH("/:id", auth, permMid("tag:update"), h.Update)
	g.DELETE("/:id", auth, permMid("tag:delete"), h.Remove)
	g.POST("/:id/follow", auth, h.Follow)
	g.DELETE("/:id/follow", auth, h.Unfollow)
}

// Package router 业务路由注册表。
//
// 多成员并行开发时，为避免同时修改 router.go 的 New() 造成冲突，
// 各业务域在自己成员手里创建 <域>_routes.go，实现签名统一的注册函数：
//
//	func RegisterXxxRoutes(v1 *gin.RouterGroup, opt Option)
//
// 然后在下面的 RegisterAll 中追加一行调用。
// 追加时只在「你的那一行」新增，不要改动其它域的行。
package router

import "github.com/gin-gonic/gin"

// RegisterAll 汇总注册各业务域路由。
// 每接一个域，在此追加一行。当前尚未接入任何域（等待各成员产出）。
func RegisterAll(v1 *gin.RouterGroup, opt Option) {
	// 用户/认证/RBAC/message 域
	RegisterUserRoutes(v1, opt)
	// RegisterRoleRoutes(v1, opt)
	// RegisterPermissionRoutes(v1, opt)
	RegisterMessageRoutes(v1, opt)

	// 内容域
	RegisterArticleRoutes(v1, opt)
	RegisterCommentRoutes(v1, opt)
	RegisterTagRoutes(v1, opt)
	RegisterCategoryRoutes(v1, opt)
	RegisterCollectionRoutes(v1, opt)

	// 运营域（积分/成就/表情包/装饰品/轮播/举报/邀请，实现见 ops_routes.go）
	RegisterOpsRoutes(v1, opt)

	// 基础设施域
	RegisterUploadRoutes(v1, opt)
	RegisterSearchRoutes(v1, opt)
	RegisterStatisticsRoutes(v1, opt)
	RegisterConfigRoutes(v1, opt)

	// 交易域
	RegisterOrderRoutes(v1, opt)
	RegisterPaymentRoutes(v1, opt)
	RegisterAuditRoutes(v1, opt)
}

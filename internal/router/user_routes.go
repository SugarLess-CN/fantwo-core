// Package router 用户域路由注册（认证 / 用户管理 / 角色 / 权限）。
//
// 路径严格对齐 docs/02（swagger 权威）：
//
//	用户管理 46 操作 → /user**
//	角色管理 10 操作 → /role**
//	权限管理 5 操作  → /permission**
//	用户认证 7 操作  → /user/certification/**
//
// 任务书里点名的 /auth/*、/admin/roles*、/admin/permissions/all 在 swagger 中
// 不存在，作为**别名**一并挂载（同一 handler，不引入第二套实现）。
package router

import (
	"context"
	"sync"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/handler"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// registerUserSubscribersOnce 保证跨域事件订阅只注册一次（多次构造引擎不重复订阅）。
var registerUserSubscribersOnce sync.Once

// RegisterUserRoutes 注册用户域全部路由（认证 / 用户 / RBAC）。
func RegisterUserRoutes(v1 *gin.RouterGroup, opt Option) {
	deps := buildUserDeps(opt)

	authH := handler.NewAuthHandler(service.NewAuthService(deps))
	userH := handler.NewUserHandler(service.NewUserService(deps))
	actionH := handler.NewUserActionHandler(service.NewUserService(deps))
	roleSvc := service.NewRoleService(deps)
	roleH := handler.NewRoleHandler(roleSvc)
	permH := handler.NewPermissionHandler(roleSvc)

	auth := middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
	optAuth := middleware.OptionalAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
	canUserRead := middleware.RequireAnyPermission("user:read", "user:manage")
	canUserWrite := middleware.RequireAnyPermission("user:update", "user:manage")
	canUserCreate := middleware.RequireAnyPermission("user:create")
	canUserDelete := middleware.RequireAnyPermission("user:delete", "user:manage")
	canUserManage := middleware.RequireAnyPermission("user:manage")
	canRoleRead := middleware.RequireAnyPermission("role:read")
	canRoleCreate := middleware.RequireAnyPermission("role:create")
	canRoleUpdate := middleware.RequireAnyPermission("role:update")
	canRoleDelete := middleware.RequireAnyPermission("role:delete")
	canPermRead := middleware.RequireAnyPermission("permission:read")
	canPermCreate := middleware.RequireAnyPermission("permission:create")
	canPermUpdate := middleware.RequireAnyPermission("permission:update")
	canPermDelete := middleware.RequireAnyPermission("permission:delete")
	canCertAdmin := middleware.RequireAnyPermission("certification:audit", "user:manage")

	// ---------- 认证（公开） ----------
	v1.POST("/user/login", authH.Login)
	v1.POST("/auth/login", authH.Login) // 任务书别名
	v1.POST("/user/register", authH.Register)
	v1.POST("/auth/register", authH.Register) // 任务书别名
	v1.POST("/user/refresh-token", authH.RefreshToken)
	v1.POST("/auth/refresh", authH.RefreshToken) // 任务书别名
	v1.POST("/user/email/verification", authH.SendEmailCode)
	v1.POST("/user/password/reset", authH.ResetPassword)
	v1.POST("/auth/sms/send", authH.SendSmsCode)
	v1.POST("/auth/sms/login", authH.SmsLogin)
	v1.GET("/user/oauth/github/url", authH.GithubURL)
	v1.POST("/user/oauth/github/callback", authH.GithubCallback)
	v1.GET("/user/oauth/github/callback", authH.GithubRedirect)

	// ---------- 认证（需登录） ----------
	authGroup := v1.Group("")
	authGroup.Use(auth)
	{
		authGroup.POST("/user/logout", authH.Logout)
		authGroup.POST("/user/logout-all", authH.LogoutAll)
		authGroup.POST("/auth/logout", authH.Logout)        // 任务书别名
		authGroup.POST("/auth/logout-all", authH.LogoutAll) // 任务书别名
		authGroup.POST("/user/password/change", authH.ChangePassword)
	}

	// ---------- 用户资料 / 账号 ----------
	me := v1.Group("")
	me.Use(auth)
	{
		me.GET("/user/profile", userH.Profile)
		me.GET("/profile", userH.Profile) // 任务书别名
		me.PATCH("/user/profile", userH.UpdateProfile)
		me.PATCH("/profile", userH.UpdateProfile) // 任务书别名
		me.PATCH("/user/profile/contact", userH.UpdateContact)
		me.POST("/user/profile/phone", userH.BindPhone)
		me.POST("/profile/phone", userH.BindPhone) // 任务书别名
		me.POST("/user/profile/phone/code", userH.SendBindPhoneCode)
		me.POST("/profile/phone/code", userH.SendBindPhoneCode) // 任务书别名
		me.GET("/user/profile/favorites", userH.Favorites)
		me.GET("/profile/favorites", userH.Favorites) // 任务书别名
		// 我的菜单树（swagger /profile/menus；此前未注册）
		me.GET("/profile/menus", roleH.MyMenus)
		me.GET("/user/profile/menus", roleH.MyMenus)
		me.GET("/user/profile/favorites/:productId", userH.FavoriteDetail)
		me.GET("/profile/favorites/:productId", userH.FavoriteDetail) // 任务书别名

		// 账号注销三态
		me.POST("/user/account/deletion", userH.DeletionRequest)
		me.GET("/user/account/status", userH.DeletionStatus)
		me.POST("/user/account/deletion/cancel", userH.DeletionCancel)
		me.POST("/account/deletion", userH.DeletionRequest)       // 任务书别名
		me.GET("/account/status", userH.DeletionStatus)           // 任务书别名
		me.POST("/account/deletion/cancel", userH.DeletionCancel) // 任务书别名

		// 用户配置
		me.GET("/user/config", userH.GetConfig)
		me.PATCH("/user/config", userH.UpdateConfig)
		me.PATCH("/user/config/notifications", userH.UpdateNotifications)
		me.PATCH("/user/config/commission", userH.UpdateCommissionSettings)
		me.GET("/user/commission/config", userH.GetCommissionConfig)
		me.POST("/user/commission/config", userH.SetCommissionConfig)
		me.POST("/user/commission/calculate", userH.CalculateCommission)

		// 签到
		me.POST("/user/sign-in", actionH.SignIn)
		me.GET("/user/sign-in/records", actionH.SignInRecords)
		me.GET("/user/sign-in/stats", actionH.SignInStats)

		// 钱包
		me.GET("/user/wallet/balance", actionH.WalletBalance)
		me.GET("/user/wallet/transactions", actionH.WalletTransactions)
		me.GET("/user/wallet/statistics", actionH.WalletStatistics)
		me.POST("/user/wallet/withdraw", actionH.WalletWithdraw)

		// 认证（本人）
		me.POST("/user/certification/apply", actionH.CertApply)
		me.GET("/user/certification/me", actionH.CertMe) // 我的认证记录（列表）
		me.GET("/user/certification/status", actionH.CertStatus)
		me.GET("/user/certification/identity", actionH.CertIdentity)

		// 关注体系（写操作需登录）
		me.POST("/user/:id/follow", actionH.Follow)
		me.POST("/user/:id/unfollow", actionH.Unfollow)
	}

	// 关注体系（读操作，可选登录：用于标注 isFollowing）
	rel := v1.Group("")
	rel.Use(optAuth)
	{
		rel.GET("/user/:id/followers", actionH.Followers)
		rel.GET("/user/:id/followings", actionH.Followings)
		rel.GET("/user/:id/followers/count", actionH.FollowerCount)
		rel.GET("/user/:id/followings/count", actionH.FollowingCount)
		rel.GET("/user/certification/identity/:userId", actionH.CertIdentityOf)
	}

	// ---------- 用户管理（管理员） ----------
	admin := v1.Group("/user")
	admin.Use(auth)
	{
		admin.POST("", canUserCreate, userH.Create)
		// 全站用户列表属管理能力：收紧为 user:manage。
		// 原项目此处是 @NoAuth() 公开列表（会泄露全部用户），我方按安全加固处理；
		// 普通用户查看他人主页仍可走 GET /user/:id（user:read）。
		admin.GET("", canUserManage, userH.FindAll)
		admin.PATCH("/:id", canUserWrite, userH.Update)
		admin.DELETE("/:id", canUserDelete, userH.Remove)
		admin.GET("/:id", canUserRead, userH.FindOne)
		admin.GET("/:id/membership/check", canUserManage, userH.CheckMembership)
		admin.POST("/membership/batch-check", canUserManage, userH.BatchCheckMembership)
		// 认证审核（管理员）
		admin.GET("/certification/admin/applications", auth, canCertAdmin, actionH.CertApplications)
		admin.PATCH("/certification/admin/applications/:id/audit", canCertAdmin, actionH.CertAudit)
		admin.POST("/certification/admin/applications/:id/revoke", canCertAdmin, actionH.CertRevoke)
	}

	// ---------- 角色管理（10 操作） ----------
	role := v1.Group("/role")
	role.Use(auth)
	{
		role.GET("", canRoleRead, roleH.List)
		role.POST("", canRoleCreate, roleH.Create)
		role.GET("/list", canRoleRead, roleH.ListPaged)
		role.GET("/active", canRoleRead, roleH.Active)
		role.GET("/:id", canRoleRead, roleH.FindOne)
		role.PATCH("/:id", canRoleUpdate, roleH.Update)
		role.DELETE("/:id", canRoleDelete, roleH.Remove)
		role.POST("/:id/copy", canRoleCreate, roleH.Copy)
		role.POST("/:id/permissions", canRoleUpdate, roleH.AssignPermissions)
		role.PATCH("/:id/status", canRoleUpdate, roleH.ToggleStatus)
		role.GET("/:id/menus", canRoleRead, roleH.Menus)
		role.PUT("/:id/menus", canRoleUpdate, roleH.AssignMenus)
	}

	// ---------- 角色管理（任务书别名 /admin/roles**） ----------
	adminRole := v1.Group("/admin/roles")
	adminRole.Use(auth)
	{
		adminRole.GET("", canRoleRead, roleH.List)
		adminRole.POST("", canRoleCreate, roleH.Create)
		adminRole.GET("/list", canRoleRead, roleH.ListPaged)
		adminRole.GET("/active", canRoleRead, roleH.Active)
		adminRole.GET("/:id", canRoleRead, roleH.FindOne)
		adminRole.PATCH("/:id", canRoleUpdate, roleH.Update)
		adminRole.DELETE("/:id", canRoleDelete, roleH.Remove)
		adminRole.POST("/:id/copy", canRoleCreate, roleH.Copy)
		adminRole.PUT("/:id/permissions", canRoleUpdate, roleH.AssignPermissions)
		adminRole.PATCH("/:id/status", canRoleUpdate, roleH.ToggleStatus)
		adminRole.GET("/:id/menus", canRoleRead, roleH.Menus)
		adminRole.PUT("/:id/menus", canRoleUpdate, roleH.AssignMenus)
	}

	// ---------- 权限管理（5 操作） ----------
	perm := v1.Group("/permission")
	perm.Use(auth)
	{
		perm.GET("", canPermRead, permH.List)
		perm.POST("", canPermCreate, permH.Create)
		perm.GET("/tree", canPermRead, permH.Tree)
		perm.GET("/:id", canPermRead, permH.FindOne)
		perm.PATCH("/:id", canPermUpdate, permH.Update)
		perm.DELETE("/:id", canPermDelete, permH.Remove)
	}

	// 权限列表别名 GET /admin/permissions/all
	v1.GET("/admin/permissions/all", auth, canPermRead, permH.List)

	// ------------------- 跨域事件订阅（report.handled 封禁 / invite.used 回写） -------------------
	registerUserSubscribersOnce.Do(func() { service.RegisterUserEventSubscribers(deps) })

	// ------------------- 初始化：RBAC 种子（幂等；DB 不可用则跳过） -------------------
	if opt.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := roleSvc.InitRBAC(ctx); e != nil {
			if opt.Logger != nil {
				opt.Logger.Warn("RBAC 初始化失败（可稍后重试）: " + e.Msg)
			}
		}
	}
}

// RegisterRoleRoutes / RegisterPermissionRoutes 说明：
// 角色与权限路由已由 RegisterUserRoutes 一并注册（同一 RBAC 依赖，拆开注册会
// 因共享中间件与 service 重复构造而不划算）。registry.go 里**只取消**
// RegisterUserRoutes 一行；这两行保持注释，否则 gin 会因重复路由 panic。

// buildUserDeps 由路由 Option 组装 service 依赖。
func buildUserDeps(opt Option) *service.Deps {
	return service.NewDeps(opt.DB, opt.Redis, opt.Signer, userQueueClient(opt), opt.Logger)
}

// userQueueClient 构造 asynq 客户端（Redis 配置缺失时返回 nil，队列功能降级）。
func userQueueClient(opt Option) *asynq.Client {
	if opt.Cfg == nil || opt.Cfg.Redis.Addr == "" {
		return nil
	}
	return queue.NewClient(opt.Cfg.Redis.Addr, opt.Cfg.Redis.Password)
}

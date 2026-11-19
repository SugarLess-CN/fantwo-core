// Package handler 运营域聚合入口：把七个域（积分/成就/表情包/装饰品/轮播/
// 举报/邀请）的 service 装进一个 OpsHandler，供 internal/router/ops_routes.go
// 统一注册；方法实现按域拆在同包的 points.go / achievement.go / emoji.go /
// decoration.go / banner.go / report.go / invite.go。
package handler

import (
	"strconv"
	"strings"
	"sync"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// OpsHandler 运营域处理器集合。
type OpsHandler struct {
	Points      *service.PointsService
	Achievement *service.AchievementService
	Emoji       *service.EmojiService
	Decoration  *service.DecorationService
	Banner      *service.BannerService
	Report      *service.ReportService
	Invite      *service.InviteService
}

// opsSubscribeOnce 保证成就事件订阅只注册一次（多次构造 handler 不会重复推进）。
var opsSubscribeOnce sync.Once

// NewOps 构造运营域处理器。
// db 为 nil（降级启动）时返回可用但不可调用的实例 —— 路由层已用降级中间件拦截。
func NewOps(db *gorm.DB, signer *jwtx.Signer) *OpsHandler {
	bus := event.Default()

	points := service.NewPointsService(db)
	decorations := service.NewDecorationService(db, nil)
	achievements := service.NewAchievementService(db, points, decorations, bus)
	opsSubscribeOnce.Do(func() { achievements.Subscribe(bus) })

	return &OpsHandler{
		Points:      points,
		Achievement: achievements,
		Emoji:       service.NewEmojiService(db),
		Decoration:  decorations,
		Banner:      service.NewBannerService(db),
		Report:      service.NewReportService(db, bus),
		Invite:      service.NewInviteService(db, bus, "", 0.05),
	}
}

// ---- 公共辅助 ----

// currentUser 取当前登录用户（未登录返回 nil）。
func currentUser(c *gin.Context) *jwtx.Claims {
	raw, ok := c.Get("auth_user")
	if !ok {
		return nil
	}
	claims, _ := raw.(*jwtx.Claims)
	return claims
}

// currentUserID 取当前用户 ID；未登录返回空串。
func currentUserID(c *gin.Context) string {
	if u := currentUser(c); u != nil {
		return u.UserID
	}
	return ""
}

// requireUser 强制登录：未登录直接写 401 并返回空串。
func requireUser(c *gin.Context) string {
	uid := currentUserID(c)
	if uid == "" {
		response.Fail(c, errcode.ErrUnauthorized)
	}
	return uid
}

// isAdmin 管理端判定：JWT 权限里含 admin / * / 任意 `:manage` 权限点。
// 对齐原 PermissionUtil.hasPermission 的粗粒度用法（细粒度差异见接口文档）。
func isAdmin(c *gin.Context) bool {
	u := currentUser(c)
	if u == nil {
		return false
	}
	for _, p := range u.Permissions {
		if p == "*" || p == "admin" {
			return true
		}
		if strings.HasSuffix(p, ":manage") {
			return true
		}
	}
	for _, r := range u.Roles {
		if r == "admin" || r == "super_admin" || r == "ADMIN" {
			return true
		}
	}
	return false
}

// fail 统一错误响应：errcode 走业务错误体，其它按 500。
func fail(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if e, ok := err.(*errcode.Err); ok {
		response.Fail(c, e)
		return
	}
	response.Fail(c, errcode.ErrInternal)
}

// ok 成功响应。
func ok(c *gin.Context, data interface{}) { response.Success(c, data) }

// okPage 分页成功响应（service 返回的 ListResult.PageData 即 response.PageData）。
func okPage(c *gin.Context, lr *service.ListResult) {
	if lr == nil {
		response.SuccessPage(c, []interface{}{}, 0, 1, service.DefaultPageSize)
		return
	}
	response.Success(c, lr.PageData)
}

// bindingMsg 取绑定错误的首条消息（对齐原异常过滤器的 message[0] 语义）。
func bindingMsg(err error) string {
	if err == nil {
		return errcode.ErrBadRequest.Msg
	}
	msg := err.Error()
	// validator 的报错形如 "Key: 'X.Field' Error:Field validation for 'Field' failed on the 'required' tag"
	if i := strings.Index(msg, "failed on the '"); i >= 0 {
		if j := strings.Index(msg[i+len("failed on the '"):], "'"); j >= 0 {
			tag := msg[i+len("failed on '") : i+len("failed on '")+j]
			return "参数校验失败：" + tag
		}
	}
	return "参数校验失败"
}

// normQuery 分页参数收敛（与 service 层 normPage 同口径，handler 侧独立计算
// 只为把 page/limit 回填进响应 meta）。
func normQuery(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = service.DefaultPageSize
	}
	if limit > service.MaxPageSize {
		limit = service.MaxPageSize
	}
	return page, limit
}

// boolPtr 把 query 字符串转 *bool：空串返回 nil（不过滤），true/1 为 true。
func boolPtr(s string) *bool {
	if s == "" {
		return nil
	}
	v := s == "true" || s == "1"
	return &v
}

// atoiDefault 字符串转 int（空串/非法返回默认值）。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

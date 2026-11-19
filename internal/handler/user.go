// Package handler 用户管理端点（用户管理 tag 46 操作）。
//
// 路径严格对齐 docs/02（swagger 权威）：/user**、/profile**、/account**。
package handler

import (
	"strconv"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// UserHandler 用户管理端点。
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler 构造。
func NewUserHandler(svc *service.UserService) *UserHandler { return &UserHandler{svc: svc} }

// Profile GET /user/profile（另挂 GET /api/v1/profile）。
func (h *UserHandler) Profile(c *gin.Context) {
	out, e := h.svc.Profile(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// UpdateProfile PATCH /user/profile（另挂 PATCH /profile）。
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	var in service.UpdateProfileInput
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.UpdateProfile(c.Request.Context(), middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// UpdateContact PATCH /user/profile/contact：修改邮箱/手机号（带验证码校验走 BindPhone）。
func (h *UserHandler) UpdateContact(c *gin.Context) {
	var in service.UpdateProfileInput
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.UpdateProfile(c.Request.Context(), middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// SendBindPhoneCode POST /user/profile/phone/code：发送绑定手机验证码。
func (h *UserHandler) SendBindPhoneCode(c *gin.Context) {
	var req struct {
		Phone string `json:"phone"`
	}
	if !bindJSON(c, &req) {
		return
	}
	auth := service.NewAuthService(h.svc.Deps())
	if e := auth.SendCode(c.Request.Context(), service.SendCodeInput{Target: req.Phone, Scene: "bind_phone"}); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "message": "验证码已发送"})
}

// BindPhone POST /user/profile/phone：绑定手机号。
func (h *UserHandler) BindPhone(c *gin.Context) {
	var in service.BindPhoneInput
	if !bindJSON(c, &in) {
		return
	}
	if e := h.svc.BindPhone(c.Request.Context(), middleware.CurrentUserID(c), in); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "phone": in.Phone})
}

// DeletionRequest POST /user/account/deletion：提交注销（status→1，7 天冷静期）。
func (h *UserHandler) DeletionRequest(c *gin.Context) {
	var in service.RequestDeletionInput
	_ = c.ShouldBindJSON(&in) // 允许空体
	out, e := h.svc.RequestDeletion(c.Request.Context(), middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// DeletionStatus GET /user/account/status：注销状态。
func (h *UserHandler) DeletionStatus(c *gin.Context) {
	out, e := h.svc.DeletionStatus(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// DeletionCancel POST /user/account/deletion/cancel：撤销注销。
func (h *UserHandler) DeletionCancel(c *gin.Context) {
	out, e := h.svc.CancelDeletion(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Favorites GET /user/profile/favorites：我的收藏分页。
func (h *UserHandler) Favorites(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.ListFavorites(c.Request.Context(), middleware.CurrentUserID(c), page, limit)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// FavoriteDetail GET /user/profile/favorites/{productId}：收藏详情（productId 即 articleId）。
func (h *UserHandler) FavoriteDetail(c *gin.Context) {
	out, e := h.svc.FavoriteDetail(c.Request.Context(), middleware.CurrentUserID(c), c.Param("productId"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FindAll GET /user：用户列表。
func (h *UserHandler) FindAll(c *gin.Context) {
	page, limit := queryPage(c)
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("username")
	}
	list, total, e := h.svc.ListUsers(c.Request.Context(), service.ListUsersInput{
		Page: page, Limit: limit, Keyword: keyword,
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// Create POST /user：管理员创建用户。
func (h *UserHandler) Create(c *gin.Context) {
	var in service.CreateUserInput
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.CreateUser(c.Request.Context(), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// FindOne GET /user/{id}：用户详情。
func (h *UserHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindUser(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// Update PATCH /user/{id}：管理员更新用户。
func (h *UserHandler) Update(c *gin.Context) {
	var in service.UpdateUserInput
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.UpdateUser(c.Request.Context(), c.Param("id"), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// Remove DELETE /user/{id}：删除用户。
func (h *UserHandler) Remove(c *gin.Context) {
	if e := h.svc.RemoveUser(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// GetConfig GET /user/config：当前用户配置。
func (h *UserHandler) GetConfig(c *gin.Context) {
	p, e := h.svc.GetConfig(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, p)
}

// UpdateConfig PATCH /user/config：更新配置。
func (h *UserHandler) UpdateConfig(c *gin.Context) {
	var in service.UpdateConfigInput
	if !bindJSON(c, &in) {
		return
	}
	if e := h.svc.UpdateConfig(c.Request.Context(), middleware.CurrentUserID(c), in); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// UpdateNotifications PATCH /user/config/notifications：通知设置。
func (h *UserHandler) UpdateNotifications(c *gin.Context) {
	var in service.NotificationInput
	if !bindJSON(c, &in) {
		return
	}
	if e := h.svc.UpdateNotifications(c.Request.Context(), middleware.CurrentUserID(c), in); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// UpdateCommissionSettings PATCH /user/config/commission：抽成设置。
func (h *UserHandler) UpdateCommissionSettings(c *gin.Context) {
	var in service.UpdateConfigInput
	if !bindJSON(c, &in) {
		return
	}
	if e := h.svc.UpdateConfig(c.Request.Context(), middleware.CurrentUserID(c), in); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// GetCommissionConfig GET /user/commission/config。
func (h *UserHandler) GetCommissionConfig(c *gin.Context) {
	out, e := h.svc.CommissionConfig(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// SetCommissionConfig POST /user/commission/config。
func (h *UserHandler) SetCommissionConfig(c *gin.Context) {
	var in service.SetCommissionInput
	if !bindJSON(c, &in) {
		return
	}
	if e := h.svc.SetCommission(c.Request.Context(), middleware.CurrentUserID(c), in); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// CalculateCommission POST /user/commission/calculate。
func (h *UserHandler) CalculateCommission(c *gin.Context) {
	var req struct {
		Amount float64 `json:"amount"`
		Type   string  `json:"type"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, e := h.svc.CalculateCommission(c.Request.Context(), middleware.CurrentUserID(c), req.Amount, req.Type)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CheckMembership GET /user/{id}/membership/check。
func (h *UserHandler) CheckMembership(c *gin.Context) {
	out, e := h.svc.CheckMembership(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// BatchCheckMembership POST /user/membership/batch-check。
func (h *UserHandler) BatchCheckMembership(c *gin.Context) {
	out, e := h.svc.BatchCheckMembership(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// queryPage 读取 page/limit 查询参数。
func queryPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	return page, limit
}

// queryBool 读取布尔查询参数（缺省 nil）。
func queryBool(c *gin.Context, key string) *bool {
	v := c.Query(key)
	if v == "" {
		return nil
	}
	b := v == "true" || v == "1"
	return &b
}

// Package handler 用户管理端点（用户管理 tag 46 操作）。
//
// 路径严格对齐 docs/02（swagger 权威）：/user**、/profile**、/account**。
package handler

import (
	"strconv"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
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
//
//	@Summary	当前用户资料
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserProfileDto}
//	@Failure	401	{object}	dto.ResponseBody
//	@ID			UserController_getProfile
//	@Router		/user/profile [get]
func (h *UserHandler) Profile(c *gin.Context) {
	out, e := h.svc.Profile(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// UpdateProfile PATCH /user/profile（另挂 PATCH /profile）。
//
//	@Summary		更新当前用户资料
//	@Description	昵称/头像/背景/简介等字段，请求体见 service.UpdateProfileInput
//	@Tags			用户管理
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body body dto.UpdateProfileRequest	true	"待更新字段（部分更新）"
//	@Success		200		{object}	dto.ResponseBody{data=dto.UserDto}
//	@Failure		400		{object}	dto.ResponseBody
//	@ID			UserController_updateProfile
//	@Router			/user/profile [patch]
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

// UpdateContact PATCH /user/profile/contact：修改邮箱/手机号（**必须带验证码**）。
//
// 与 PATCH /user/profile 的分工：那边改昵称/头像等资料，这边改「能用来登录的标识」。
// 后者才是账号接管的入口，所以独立端点 + 独立 DTO + 强制验证码。
//
//	@Summary	修改联系方式（需验证码）
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.UpdateContactRequest true "email 或 phone（二选一）+ verification_code"
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDto}
//	@ID			UserController_updateContact
//	@Router		/user/profile/contact [patch]
func (h *UserHandler) UpdateContact(c *gin.Context) {
	var in dto.UpdateContactRequest
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.UpdateContact(c.Request.Context(), middleware.CurrentUserID(c), service.UpdateContactInput{
		Email: in.Email, Phone: in.Phone, VerificationCode: in.VerificationCode,
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// SendBindPhoneCode POST /user/profile/phone/code：发送绑定手机验证码。
//
//	@Summary	发送绑定手机验证码
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.BindPhoneCodeRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserProfileController_sendBindPhoneCode
//	@Router		/user/profile/phone/code [post]
func (h *UserHandler) SendBindPhoneCode(c *gin.Context) {
	var req dto.BindPhoneCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	auth := service.NewAuthService(h.svc.Deps())
	if e := auth.SendCode(c.Request.Context(), service.SendCodeInput{Target: req.Phone, Scene: "bind_phone"}); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthCaptchaSent, gin.H{"success": true})
}

// BindPhone POST /user/profile/phone：绑定手机号。
//
//	@Summary	绑定手机号
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.BindPhoneRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.BindPhoneResultDto}
//	@ID			UserProfileController_bindPhone
//	@Router		/user/profile/phone [post]
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
//
//	@Summary	申请注销账号
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.RequestDeletionRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletionRequestResultDto}
//	@ID			UserController_requestAccountDeletion
//	@Router		/user/account/deletion [post]
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
//
//	@Summary	注销申请状态
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletionStatusDto}
//	@ID			UserController_getDeletionStatus
//	@Router		/user/account/status [get]
func (h *UserHandler) DeletionStatus(c *gin.Context) {
	out, e := h.svc.DeletionStatus(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// DeletionCancel POST /user/account/deletion/cancel：撤销注销。
//
//	@Summary	取消注销申请
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletionStatusDto}
//	@ID			UserController_cancelAccountDeletion
//	@Router		/user/account/deletion/cancel [post]
func (h *UserHandler) DeletionCancel(c *gin.Context) {
	out, e := h.svc.CancelDeletion(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Favorites GET /user/profile/favorites：我的收藏分页。
//
//	@Summary	我的收藏夹
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.FavoriteItemDto}}
//	@ID			UserProfileController_findFavorites
//	@Router		/user/profile/favorites [get]
func (h *UserHandler) Favorites(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.ListFavorites(c.Request.Context(), middleware.CurrentUserID(c), page, limit)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// FavoriteDetail GET /user/profile/favorites/{product_id}：收藏详情（productId 即 articleId）。
//
//	@Summary	收藏项详情
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		product_id path string true "productId" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.FavoriteDetailDto}
//	@ID			UserProfileController_findFavoriteDetail
//	@Router		/user/profile/favorites/{product_id} [get]
func (h *UserHandler) FavoriteDetail(c *gin.Context) {
	out, e := h.svc.FavoriteDetail(c.Request.Context(), middleware.CurrentUserID(c), c.Param("product_id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FindAll GET /user：用户列表。
//
//	@Summary		用户列表
//	@Description	分页查询用户，支持 keyword（或 username）模糊搜索
//	@Tags			用户管理
//	@Produce		json
//	@Security		BearerAuth
//	@Param		page query int false "页码，默认 1" example(1)
//	@Param		limit query int false "每页条数，默认 10" example(10)
//	@Param		keyword query string false "用户名关键字" example(风景)
//	@Success		200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserDto}}
//	@ID			UserController_findAll
//	@Router			/user [get]
func (h *UserHandler) FindAll(c *gin.Context) {
	page, limit := queryPage(c)
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("username")
	}
	list, total, e := h.svc.ListUsers(c.Request.Context(), service.ListUsersInput{
		Page: page, Limit: limit, Keyword: keyword, Actor: actorOf(c),
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// Create POST /user：管理员创建用户。
//
//	@Summary	管理员创建用户
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.CreateUserRequest	true	"CreateUserInput"
//	@Success	200		{object}	dto.ResponseBody{data=dto.UserDto}
//	@Failure	403		{object}	dto.ResponseBody
//	@ID			UserController_create
//	@Router		/user [post]
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
//
//	@Summary	用户详情
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "用户 ID" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDto}
//	@Failure	404	{object}	dto.ResponseBody
//	@ID			UserController_findOne
//	@Router		/user/{id} [get]
func (h *UserHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindUser(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// Update PATCH /user/{id}：管理员更新用户。
//
//	@Summary	更新用户（管理端）
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Param		body body dto.UpdateUserRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDto}
//	@ID			UserController_update
//	@Router		/user/{id} [patch]
func (h *UserHandler) Update(c *gin.Context) {
	var in service.UpdateUserInput
	if !bindJSON(c, &in) {
		return
	}
	view, e := h.svc.UpdateUser(c.Request.Context(), c.Param("id"), in, actorOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, view)
}

// Remove DELETE /user/{id}：删除用户。
//
//	@Summary	删除用户
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "用户 ID" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@Failure	403	{object}	dto.ResponseBody
//	@ID			UserController_remove
//	@Router		/user/{id} [delete]
func (h *UserHandler) Remove(c *gin.Context) {
	if e := h.svc.RemoveUser(c.Request.Context(), c.Param("id")); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true})
}

// GetConfig GET /user/config：当前用户配置。
//
//	@Summary	我的用户配置
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserConfigDto}
//	@ID			UserController_getUserConfig
//	@Router		/user/config [get]
func (h *UserHandler) GetConfig(c *gin.Context) {
	p, e := h.svc.GetConfig(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, p)
}

// UpdateConfig PATCH /user/config：更新配置。
//
//	@Summary	更新用户配置
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.UpdateUserConfigRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_updateUserConfig
//	@Router		/user/config [patch]
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
//
//	@Summary	更新通知偏好
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.UpdateNotificationRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_updateNotificationSettings
//	@Router		/user/config/notifications [patch]
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
//
//	@Summary	更新全局佣金配置
//	@Tags		用户管理
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.UpdateUserConfigRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_updateCommissionSettings
//	@Router		/user/config/commission [patch]
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
//
//	@Summary	读取佣金配置
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommissionConfigDto}
//	@ID			UserController_getUserCommissionConfig
//	@Router		/user/commission/config [get]
func (h *UserHandler) GetCommissionConfig(c *gin.Context) {
	out, e := h.svc.CommissionConfig(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// SetCommissionConfig POST /user/commission/config。
//
//	@Summary	设置佣金配置
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SetCommissionRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_setUserCommissionConfig
//	@Router		/user/commission/config [post]
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
//
//	@Summary	佣金试算
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.CalculateCommissionRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.CommissionResultDto}
//	@ID			UserController_calculateCommission
//	@Router		/user/commission/calculate [post]
func (h *UserHandler) CalculateCommission(c *gin.Context) {
	var req dto.CalculateCommissionRequest
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
//
//	@Summary	会员状态检查
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id" example(9f1d2e34-5678-4abc-9def-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.MembershipCheckDto}
//	@ID			UserController_checkMembershipStatus
//	@Router		/user/{id}/membership/check [get]
func (h *UserHandler) CheckMembership(c *gin.Context) {
	out, e := h.svc.CheckMembership(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// BatchCheckMembership POST /user/membership/batch-check。
//
//	@Summary	批量会员状态检查
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.BatchMembershipCheckDto}
//	@ID			UserController_batchCheckMembershipStatus
//	@Router		/user/membership/batch-check [post]
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

// 让 dto 包成为本文件的真实 import：swag 按「注释所在文件的 import」解析跨包类型，
// 本文件的注解写了 dto.Xxx，这里必须引用一次（不参与运行逻辑）。
var _ = dto.ResponseBody{}

// SearchUsers GET /search/users：用户搜索（公开端点，可选登录）。
//
//	@Summary	搜索用户
//	@Tags		搜索
//	@Produce	json
//	@Param		keyword query string false "用户名/昵称关键字 example(alice)"
//	@Param		page query int false "页码 example(1)"
//	@Param		limit query int false "每页条数 example(10)"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserSearchItemDto}}
//	@ID			SearchController_searchUsers
//	@Router		/search/users [get]
//
// 响应**不含** email / phone / wallet —— 搜索是公开端点，见 dto.UserSearchItemDto 注释。
func (h *UserHandler) SearchUsers(c *gin.Context) {
	page, limit := queryPage(c)
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("username")
	}
	list, total, e := h.svc.ListUsersForSearch(c.Request.Context(), actorOf(c), page, limit, keyword)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

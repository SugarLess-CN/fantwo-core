// Package handler 用户域扩展端点：关注 / 签到 / 钱包 / 认证。
package handler

import (
	"strconv"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// UserActionHandler 关注/签到/钱包/认证端点。
type UserActionHandler struct {
	svc *service.UserService
}

// NewUserActionHandler 构造。
func NewUserActionHandler(svc *service.UserService) *UserActionHandler {
	return &UserActionHandler{svc: svc}
}

// Follow POST /user/{id}/follow。
//
//	@Summary	关注用户
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_follow
//	@Router		/user/{id}/follow [post]
func (h *UserActionHandler) Follow(c *gin.Context) {
	out, e := h.svc.Follow(c.Request.Context(), middleware.CurrentUserID(c), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Unfollow POST /user/{id}/unfollow。
//
//	@Summary	取消关注
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_unfollow
//	@Router		/user/{id}/unfollow [post]
func (h *UserActionHandler) Unfollow(c *gin.Context) {
	out, e := h.svc.Unfollow(c.Request.Context(), middleware.CurrentUserID(c), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FollowerCount GET /user/{id}/followers/count。
//
//	@Summary	粉丝数
//	@Tags		用户管理
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_getFollowerCount
//	@Router		/user/{id}/followers/count [get]
func (h *UserActionHandler) FollowerCount(c *gin.Context) {
	out, e := h.svc.FollowerCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FollowingCount GET /user/{id}/followings/count。
//
//	@Summary	关注数
//	@Tags		用户管理
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_getFollowingCount
//	@Router		/user/{id}/followings/count [get]
func (h *UserActionHandler) FollowingCount(c *gin.Context) {
	out, e := h.svc.FollowingCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Followers GET /user/{id}/followers。
//
//	@Summary	粉丝列表
//	@Tags		用户管理
//	@Produce	json
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				UserController_getFollowers
//	@Router		/user/{id}/followers [get]
func (h *UserActionHandler) Followers(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.ListFollowers(c.Request.Context(), c.Param("id"), service.FollowListInput{
		Page: page, Limit: limit, Keyword: c.Query("keyword"),
		ViewerID: middleware.CurrentUserID(c),
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// Followings GET /user/{id}/followings。
//
//	@Summary	关注列表
//	@Tags		用户管理
//	@Produce	json
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				UserController_getFollowings
//	@Router		/user/{id}/followings [get]
func (h *UserActionHandler) Followings(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.ListFollowings(c.Request.Context(), c.Param("id"), service.FollowListInput{
		Page: page, Limit: limit, Keyword: c.Query("keyword"),
		ViewerID: middleware.CurrentUserID(c),
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// SignIn POST /user/sign-in：手动签到。
//
//	@Summary	每日签到
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_signIn
//	@Router		/user/sign-in [post]
func (h *UserActionHandler) SignIn(c *gin.Context) {
	out, e := h.svc.Sign(c.Request.Context(), middleware.CurrentUserID(c), false)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// SignInRecords GET /user/sign-in/records。
//
//	@Summary	签到记录
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				UserController_getSignInRecords
//	@Router		/user/sign-in/records [get]
func (h *UserActionHandler) SignInRecords(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	list, e := h.svc.SignRecords(c.Request.Context(), middleware.CurrentUserID(c), days)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, list)
}

// SignInStats GET /user/sign-in/stats。
//
//	@Summary	签到统计
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_getSignInStats
//	@Router		/user/sign-in/stats [get]
func (h *UserActionHandler) SignInStats(c *gin.Context) {
	out, e := h.svc.SignStats(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletBalance GET /user/wallet/balance。
//
//	@Summary	钱包余额
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_getWalletBalance
//	@Router		/user/wallet/balance [get]
func (h *UserActionHandler) WalletBalance(c *gin.Context) {
	out, e := h.svc.WalletBalance(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletTransactions GET /user/wallet/transactions。
//
//	@Summary	钱包流水
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				UserController_getWalletTransactions
//	@Router		/user/wallet/transactions [get]
func (h *UserActionHandler) WalletTransactions(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.WalletTransactions(c.Request.Context(),
		middleware.CurrentUserID(c), c.Query("type"), page, limit)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// WalletStatistics GET /user/wallet/statistics。
//
//	@Summary	钱包统计
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_getWalletStatistics
//	@Router		/user/wallet/statistics [get]
func (h *UserActionHandler) WalletStatistics(c *gin.Context) {
	out, e := h.svc.WalletStatistics(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletWithdraw POST /user/wallet/withdraw。
//
//	@Summary	钱包提现
//	@Tags		用户管理
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				UserController_withdrawWallet
//	@Router		/user/wallet/withdraw [post]
func (h *UserActionHandler) WalletWithdraw(c *gin.Context) {
	var in service.WithdrawInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.Withdraw(c.Request.Context(), middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertApply POST /user/certification/apply。
//
//	@Summary	提交认证申请
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "certApplyRequest{type,realName,idCard,attachments}"
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_apply
//	@Router		/user/certification/apply [post]
func (h *UserActionHandler) CertApply(c *gin.Context) {
	var in service.ApplyCertInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.ApplyCertification(c.Request.Context(), middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertStatus GET /user/certification/status。
//
//	@Summary	我的认证状态
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_getMyCertificationStatus
//	@Router		/user/certification/status [get]
func (h *UserActionHandler) CertStatus(c *gin.Context) {
	out, e := h.svc.LatestCertification(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertMe GET /user/certification/me（我的全部认证记录）。
//
//	@Summary	我的认证记录
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_getMyCertifications
//	@Router		/user/certification/me [get]
func (h *UserActionHandler) CertMe(c *gin.Context) {
	out, e := h.svc.MyCertifications(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertIdentity GET /user/certification/identity。
//
//	@Summary	我的身份标识
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_getMyIdentity
//	@Router		/user/certification/identity [get]
func (h *UserActionHandler) CertIdentity(c *gin.Context) {
	out, e := h.svc.Identity(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertIdentityOf GET /user/certification/identity/{userId}。
//
//	@Summary	指定用户身份标识
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_getIdentityByUserId
//	@Router		/user/certification/identity/{userId} [get]
func (h *UserActionHandler) CertIdentityOf(c *gin.Context) {
	out, e := h.svc.IdentityOf(c.Request.Context(), c.Param("userId"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertApplications GET /user/certification/admin/applications。
//
//	@Summary	认证申请列表（管理端）
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Param		status query string false "申请状态"
//	@Success	200	{object}	ResponsePageBody
//	@ID				UserCertificationController_findApplications
//	@Router		/user/certification/admin/applications [get]
func (h *UserActionHandler) CertApplications(c *gin.Context) {
	page, limit := queryPage(c)
	list, total, e := h.svc.AdminListCerts(c.Request.Context(), page, limit,
		c.Query("status"), c.Query("type"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// CertAudit PATCH /user/certification/admin/applications/{id}/audit。
//
//	@Summary	认证审核
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "申请 ID"
//	@Param		body body object true "auditRequest{pass,reason}"
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_audit
//	@Router		/user/certification/admin/applications/{id}/audit [patch]
func (h *UserActionHandler) CertAudit(c *gin.Context) {
	var in service.AuditCertInput
	if !bindJSON(c, &in) {
		return
	}
	out, e := h.svc.AuditCertification(c.Request.Context(), c.Param("id"),
		middleware.CurrentUserID(c), in)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertRevoke POST /user/certification/admin/applications/{id}/revoke。
//
//	@Summary	撤销认证
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "申请 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				UserCertificationController_revoke
//	@Router		/user/certification/admin/applications/{id}/revoke [post]
func (h *UserActionHandler) CertRevoke(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	out, e := h.svc.RevokeCertification(c.Request.Context(), c.Param("id"),
		middleware.CurrentUserID(c), body.Reason)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

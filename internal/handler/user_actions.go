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
func (h *UserActionHandler) Follow(c *gin.Context) {
	out, e := h.svc.Follow(c.Request.Context(), middleware.CurrentUserID(c), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Unfollow POST /user/{id}/unfollow。
func (h *UserActionHandler) Unfollow(c *gin.Context) {
	out, e := h.svc.Unfollow(c.Request.Context(), middleware.CurrentUserID(c), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FollowerCount GET /user/{id}/followers/count。
func (h *UserActionHandler) FollowerCount(c *gin.Context) {
	out, e := h.svc.FollowerCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// FollowingCount GET /user/{id}/followings/count。
func (h *UserActionHandler) FollowingCount(c *gin.Context) {
	out, e := h.svc.FollowingCount(c.Request.Context(), c.Param("id"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// Followers GET /user/{id}/followers。
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
func (h *UserActionHandler) SignIn(c *gin.Context) {
	out, e := h.svc.Sign(c.Request.Context(), middleware.CurrentUserID(c), false)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// SignInRecords GET /user/sign-in/records。
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
func (h *UserActionHandler) SignInStats(c *gin.Context) {
	out, e := h.svc.SignStats(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletBalance GET /user/wallet/balance。
func (h *UserActionHandler) WalletBalance(c *gin.Context) {
	out, e := h.svc.WalletBalance(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletTransactions GET /user/wallet/transactions。
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
func (h *UserActionHandler) WalletStatistics(c *gin.Context) {
	out, e := h.svc.WalletStatistics(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WalletWithdraw POST /user/wallet/withdraw。
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
func (h *UserActionHandler) CertStatus(c *gin.Context) {
	out, e := h.svc.LatestCertification(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertIdentity GET /user/certification/identity。
func (h *UserActionHandler) CertIdentity(c *gin.Context) {
	out, e := h.svc.Identity(c.Request.Context(), middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertIdentityOf GET /user/certification/identity/{userId}。
func (h *UserActionHandler) CertIdentityOf(c *gin.Context) {
	out, e := h.svc.IdentityOf(c.Request.Context(), c.Param("userId"))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// CertApplications GET /user/certification/admin/applications。
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

// 邀请域 HTTP 层（原 InviteController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// InviteCreate POST /invite。
//
//	@Summary	生成邀请码
//	@Tags		邀请
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.InviteDto}
//	@Router		/invite [post]
func (h *OpsHandler) InviteCreate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.InviteCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	inv, err := h.Invite.Create(uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, inv)
}

// InviteUse POST /invite/use。
//
//	@Summary	绑定邀请人
//	@Tags		邀请
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "useInviteRequest{code}"
//	@Success	200	{object}	dto.ResponseBody{data=dto.InviteDto}
//	@Router		/invite/use [post]
func (h *OpsHandler) InviteUse(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.InviteUseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	inv, err := h.Invite.Use(uid, in.InviteCode)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, inv)
}

// InviteMyList GET /invite/my。
//
//	@Summary	我的邀请码列表
//	@Tags		邀请
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.InviteDto}}
//	@Router		/invite/my [get]
func (h *OpsHandler) InviteMyList(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	lr, err := h.Invite.MyList(uid, c.Query("keyword"), c.Query("status"),
		atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// InviteStats GET /invite/stats。
//
//	@Summary	我的邀请统计
//	@Tags		邀请
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.InviteStatsDto}
//	@Router		/invite/stats [get]
func (h *OpsHandler) InviteStats(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	st, err := h.Invite.Stats(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, st)
}

// InviteEarnings GET /invite/earnings。
//
//	@Summary	我的邀请收益
//	@Tags		邀请
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.InviteStatsDto}
//	@Router		/invite/earnings [get]
func (h *OpsHandler) InviteEarnings(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	lr, err := h.Invite.Earnings(uid, atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// InviteGet GET /invite/:id。
//
//	@Summary	邀请码详情
//	@Tags		邀请
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "邀请码 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.InviteDto}
//	@Router		/invite/{id} [get]
func (h *OpsHandler) InviteGet(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	inv, err := h.Invite.Get(c.Param("id"), uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, inv)
}

// 让 dto 包成为真实 import（swag 只按注解所在文件的 import 解析跨包类型）。
var _ = dto.ResponseBody{}

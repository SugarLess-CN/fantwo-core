// 邀请域 HTTP 层（原 InviteController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// InviteCreate POST /invite。
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

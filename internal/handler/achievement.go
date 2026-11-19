// 成就域 HTTP 层（原 AchievementController 8 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// AchievementCreate POST /achievement（管理端）。
func (h *OpsHandler) AchievementCreate(c *gin.Context) {
	var in model.AchievementInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Achievement.Create(in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// AchievementList GET /achievement（可选登录：登录后带进度）。
func (h *OpsHandler) AchievementList(c *gin.Context) {
	list, err := h.Achievement.List(
		currentUserID(c),
		c.Query("keyword"),
		c.Query("sortBy"),
		c.Query("sortOrder"),
	)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// AchievementStats GET /achievement/stats。
func (h *OpsHandler) AchievementStats(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	st, err := h.Achievement.Stats(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, st)
}

// AchievementGet GET /achievement/:id（可选登录）。
func (h *OpsHandler) AchievementGet(c *gin.Context) {
	view, err := h.Achievement.Get(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, view)
}

// AchievementUpdate PATCH /achievement/:id（管理端）。
func (h *OpsHandler) AchievementUpdate(c *gin.Context) {
	var in model.AchievementInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Achievement.Update(c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// AchievementDelete DELETE /achievement/:id（管理端）。
func (h *OpsHandler) AchievementDelete(c *gin.Context) {
	if err := h.Achievement.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// AchievementClaim POST /achievement/:id/claim。
func (h *OpsHandler) AchievementClaim(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	res, err := h.Achievement.ClaimReward(uid, c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, res)
}

// AchievementClaimAll POST /achievement/claim-all。
func (h *OpsHandler) AchievementClaimAll(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	res, err := h.Achievement.ClaimAll(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, res)
}

// 成就域 HTTP 层（原 AchievementController 8 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// AchievementCreate POST /achievement（管理端）。
//
//	@Summary	创建成就定义
//	@Tags		成就
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementDto}
//	@ID			AchievementController_create
//	@Param		body body dto.CreateAchievementRequest true "创建成就定义"
//	@Router		/achievement [post]
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
//
//	@Summary	成就列表
//	@Tags		成就
//	@Produce	json
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.AchievementDto}}
//	@ID			AchievementController_findAll
//	@Router		/achievement [get]
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
//
//	@Summary	我的成就进度统计
//	@Tags		成就
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementStatsDto}
//	@ID			AchievementController_getUserStats
//	@Router		/achievement/stats [get]
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
//
//	@Summary	成就详情
//	@Tags		成就
//	@Produce	json
//	@Param		id path string true "成就 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementDto}
//	@ID			AchievementController_findOne
//	@Router		/achievement/{id} [get]
func (h *OpsHandler) AchievementGet(c *gin.Context) {
	view, err := h.Achievement.Get(c.Param("id"), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, view)
}

// AchievementUpdate PATCH /achievement/:id（管理端）。
//
//	@Summary	更新成就定义
//	@Tags		成就
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body dto.UpdateAchievementRequest true "请求体"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementDto}
//	@ID			AchievementController_update
//	@Router		/achievement/{id} [patch]
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
//
//	@Summary	删除成就定义
//	@Tags		成就
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@ID			AchievementController_remove
//	@Router		/achievement/{id} [delete]
func (h *OpsHandler) AchievementDelete(c *gin.Context) {
	if err := h.Achievement.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// AchievementClaim POST /achievement/:id/claim。
//
//	@Summary	领取成就奖励
//	@Tags		成就
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "成就 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementClaimResultDto}
//	@ID			AchievementController_claimReward
//	@Router		/achievement/{id}/claim [post]
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
//
//	@Summary	一键领取全部可领成就
//	@Tags		成就
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementClaimAllResultDto}
//	@ID			AchievementController_claimAllRewards
//	@Router		/achievement/claim-all [post]
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

// 让 dto 包成为真实 import（swag 只按注解所在文件的 import 解析跨包类型）。
var _ = dto.ResponseBody{}

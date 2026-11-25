// 积分域 HTTP 层（原 PointsController 16 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// PointsAdd POST /points/add（管理端：给当前登录用户加积分）。
//
//	@Summary	加积分
//	@Tags		积分
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "addPointsRequest{userId,amount,reason}"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_addPoints
//	@Router		/points/add [post]
func (h *OpsHandler) PointsAdd(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in service.AddPointsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	res, err := h.Points.Add(uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, res)
}

// PointsSpend POST /points/spend。
//
//	@Summary	花积分
//	@Tags		积分
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "spendPointsRequest{amount,reason}"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_spendPoints
//	@Router		/points/spend [post]
func (h *OpsHandler) PointsSpend(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in service.SpendPointsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	res, err := h.Points.Spend(uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, res)
}

// PointsBalance GET /points/balance。
//
//	@Summary	我的积分余额
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_getBalance
//	@Router		/points/balance [get]
func (h *OpsHandler) PointsBalance(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	balance, err := h.Points.Balance(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"userId": uid, "points": balance})
}

// PointsTransactions GET /points/transactions（我的流水）。
//
//	@Summary	我的积分流水
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				PointsController_getTransactions
//	@Router		/points/transactions [get]
func (h *OpsHandler) PointsTransactions(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	q := model.PointsTxQuery{
		Page:   atoiDefault(c.Query("page"), 1),
		Limit:  atoiDefault(c.Query("limit"), 0),
		Type:   c.Query("type"),
		Source: c.Query("source"),
		UserID: uid,
	}
	list, total, err := h.Points.Transactions(q)
	if err != nil {
		fail(c, err)
		return
	}
	page, limit := normQuery(q.Page, q.Limit)
	response.SuccessPage(c, list, total, page, limit)
}

// PointsTxAll GET /points/transactions/all（管理端：全部流水）。
//
//	@Summary	全部积分流水（管理端）
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				PointsController_getAllTransactions
//	@Router		/points/transactions/all [get]
func (h *OpsHandler) PointsTxAll(c *gin.Context) {
	q := model.PointsTxQuery{
		Page:   atoiDefault(c.Query("page"), 1),
		Limit:  atoiDefault(c.Query("limit"), 0),
		Type:   c.Query("type"),
		Source: c.Query("source"),
		UserID: c.Query("userId"),
	}
	list, total, err := h.Points.Transactions(q)
	if err != nil {
		fail(c, err)
		return
	}
	page, limit := normQuery(q.Page, q.Limit)
	response.SuccessPage(c, list, total, page, limit)
}

// PointsUserTransactions GET /points/users/:userId/transactions（管理端）。
//
//	@Summary	指定用户积分流水
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				PointsController_getUserTransactions
//	@Router		/points/users/{userId}/transactions [get]
func (h *OpsHandler) PointsUserTransactions(c *gin.Context) {
	q := model.PointsTxQuery{
		Page:   atoiDefault(c.Query("page"), 1),
		Limit:  atoiDefault(c.Query("limit"), 0),
		Type:   c.Query("type"),
		Source: c.Query("source"),
		UserID: c.Param("userId"),
	}
	list, total, err := h.Points.Transactions(q)
	if err != nil {
		fail(c, err)
		return
	}
	page, limit := normQuery(q.Page, q.Limit)
	response.SuccessPage(c, list, total, page, limit)
}

// PointsUserBalance GET /points/users/:userId/balance（管理端）。
//
//	@Summary	指定用户积分余额
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_getUserBalance
//	@Router		/points/users/{userId}/balance [get]
func (h *OpsHandler) PointsUserBalance(c *gin.Context) {
	uid := c.Param("userId")
	balance, err := h.Points.Balance(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"userId": uid, "points": balance})
}

// PointsStatistics GET /points/statistics（管理端）。
//
//	@Summary	积分统计
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_getStatistics
//	@Router		/points/statistics [get]
func (h *OpsHandler) PointsStatistics(c *gin.Context) {
	st, err := h.Points.Statistics()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, st)
}

// PointsMyTasks GET /points/tasks/my。
//
//	@Summary	我的积分任务
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_getMyTasks
//	@Router		/points/tasks/my [get]
func (h *OpsHandler) PointsMyTasks(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	list, err := h.Points.MyTasks(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// PointsMyTaskProgress GET /points/tasks/my/:activityId。
//
//	@Summary	我的任务进度
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		activityId path string true "activityId"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_getMyTaskProgress
//	@Router		/points/tasks/my/{activityId} [get]
func (h *OpsHandler) PointsMyTaskProgress(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	r, err := h.Points.MyTaskProgress(uid, c.Param("activityId"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, r)
}

// PointsActivityCreate POST /points/activities（管理端）。
//
//	@Summary	创建积分活动
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_createActivity
//	@Router		/points/activities [post]
func (h *OpsHandler) PointsActivityCreate(c *gin.Context) {
	var in model.PointsActivityInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Points.CreateActivity(in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// PointsActivityList GET /points/activities（公开）。
//
//	@Summary	积分活动列表
//	@Tags		积分
//	@Produce	json
//	@Success	200	{object}	ResponsePageBody
//	@ID				PointsController_findAllActivities
//	@Router		/points/activities [get]
func (h *OpsHandler) PointsActivityList(c *gin.Context) {
	list, err := h.Points.Activities(c.Query("type"), c.Query("keyword"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// PointsActivityGet GET /points/activities/:id（公开）。
//
//	@Summary	积分活动详情
//	@Tags		积分
//	@Produce	json
//	@Param		id path string true "活动 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_findOneActivity
//	@Router		/points/activities/{id} [get]
func (h *OpsHandler) PointsActivityGet(c *gin.Context) {
	a, err := h.Points.Activity(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// PointsActivityUpdate PATCH /points/activities/:id（管理端）。
//
//	@Summary	更新积分活动
//	@Tags		积分
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_updateActivity
//	@Router		/points/activities/{id} [patch]
func (h *OpsHandler) PointsActivityUpdate(c *gin.Context) {
	var in model.PointsActivityInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Points.UpdateActivity(c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// PointsActivityDelete DELETE /points/activities/:id（管理端）。
//
//	@Summary	删除积分活动
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_removeActivity
//	@Router		/points/activities/{id} [delete]
func (h *OpsHandler) PointsActivityDelete(c *gin.Context) {
	if err := h.Points.DeleteActivity(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// PointsActivityClaim POST /points/activities/:id/claim。
//
//	@Summary	领取积分活动奖励
//	@Tags		积分
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "活动 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				PointsController_claimTaskReward
//	@Router		/points/activities/{id}/claim [post]
func (h *OpsHandler) PointsActivityClaim(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	res, err := h.Points.ClaimTaskReward(uid, c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, res)
}

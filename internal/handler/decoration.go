// 装饰品域 HTTP 层（原 DecorationController 21 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// DecorationCreate POST /decoration（管理端）。
//
//	@Summary	创建装饰品
//	@Tags		装饰品
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "decorationRequest"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_create
//	@Router		/decoration [post]
func (h *OpsHandler) DecorationCreate(c *gin.Context) {
	var in model.DecorationInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	d, err := h.Decoration.Create(in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, d)
}

// DecorationList GET /decoration。
//
//	@Summary	装饰品商城列表
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponsePageBody
//	@ID				DecorationController_findAll
//	@Router		/decoration [get]
func (h *OpsHandler) DecorationList(c *gin.Context) {
	q := model.DecorationQuery{
		Page:      atoiDefault(c.Query("page"), 1),
		Limit:     atoiDefault(c.Query("limit"), 0),
		Type:      c.Query("type"),
		Status:    c.Query("status"),
		Keyword:   c.Query("keyword"),
		SortBy:    c.Query("sortBy"),
		SortOrder: c.Query("sortOrder"),
	}
	lr, err := h.Decoration.List(q)
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// DecorationGet GET /decoration/:id。
//
//	@Summary	装饰品详情
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "装饰品 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_findOne
//	@Router		/decoration/{id} [get]
func (h *OpsHandler) DecorationGet(c *gin.Context) {
	d, err := h.Decoration.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, d)
}

// DecorationUpdate PATCH /decoration/:id（管理端）。
//
//	@Summary	更新装饰品
//	@Tags		装饰品
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_update
//	@Router		/decoration/{id} [patch]
func (h *OpsHandler) DecorationUpdate(c *gin.Context) {
	var in model.DecorationInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	d, err := h.Decoration.Update(c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, d)
}

// DecorationDelete DELETE /decoration/:id（管理端）。
//
//	@Summary	删除装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "装饰品 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_remove
//	@Router		/decoration/{id} [delete]
func (h *OpsHandler) DecorationDelete(c *gin.Context) {
	if err := h.Decoration.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// DecorationMyList GET /decoration/user/my。
//
//	@Summary	我的装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_getMyDecorations
//	@Router		/decoration/user/my [get]
func (h *OpsHandler) DecorationMyList(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	lr, err := h.Decoration.MyDecorations(uid, c.Query("type"),
		atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// DecorationUserList GET /decoration/user/:userId。
//
//	@Summary	指定用户装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_getUserDecorations
//	@Router		/decoration/user/{userId} [get]
func (h *OpsHandler) DecorationUserList(c *gin.Context) {
	lr, err := h.Decoration.UserDecorations(c.Param("userId"), c.Query("type"),
		atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// DecorationMyBadges GET /decoration/achievement-badges/my。
//
//	@Summary	我的成就徽章
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_getMyAchievementBadges
//	@Router		/decoration/achievement-badges/my [get]
func (h *OpsHandler) DecorationMyBadges(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	lr, err := h.Decoration.MyAchievementBadges(uid,
		atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// DecorationMyCurrent GET /decoration/user/current/decorations。
//
//	@Summary	我当前佩戴的装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_getCurrentDecorations
//	@Router		/decoration/user/current/decorations [get]
func (h *OpsHandler) DecorationMyCurrent(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	cur, err := h.Decoration.MyCurrent(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, cur)
}

// DecorationUse POST /decoration/use/:decorationId。
//
//	@Summary	佩戴装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		decorationId path string true "装饰品 ID"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_useDecoration
//	@Router		/decoration/use/{decorationId} [post]
func (h *OpsHandler) DecorationUse(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	ud, err := h.Decoration.Use(uid, c.Param("decorationId"))
	if err != nil {
		fail(c, err)
		return
	}
	// 成功提示提到顶层 message（统一响应契约：data 只放业务数据）
	response.SuccessMsg(c, i18n.DecorationEquipped, ud)
}

// DecorationUnuse POST /decoration/unuse/:decorationId。
//
//	@Summary	卸下装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		decorationId path string true "decorationId"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_unuseDecoration
//	@Router		/decoration/unuse/{decorationId} [post]
func (h *OpsHandler) DecorationUnuse(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	ud, err := h.Decoration.Unuse(uid, c.Param("decorationId"))
	if err != nil {
		fail(c, err)
		return
	}
	// 成功提示提到顶层 message（统一响应契约：data 只放业务数据）
	response.SuccessMsg(c, i18n.DecorationUnequipped, ud)
}

// DecorationPurchase POST /decoration/purchase。
//
//	@Summary	购买装饰品
//	@Tags		装饰品
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "purchaseRequest{decorationId}"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_purchase
//	@Router		/decoration/purchase [post]
func (h *OpsHandler) DecorationPurchase(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body struct {
		DecorationID string `json:"decorationId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	ud, err := h.Decoration.Purchase(uid, body.DecorationID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, ud)
}

// DecorationGift POST /decoration/gift。
//
//	@Summary	赠送装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_gift
//	@Router		/decoration/gift [post]
func (h *OpsHandler) DecorationGift(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body struct {
		ToUserID     string `json:"toUserId" binding:"required"`
		DecorationID string `json:"decorationId" binding:"required"`
		Message      string `json:"message"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	ud, err := h.Decoration.Gift(uid, body.ToUserID, body.DecorationID, body.Message)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, ud)
}

// DecorationCleanExpired POST /decoration/clean-expired。
//
//	@Summary	清理过期装饰品（管理触发）
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_cleanExpired
//	@Router		/decoration/clean-expired [post]
func (h *OpsHandler) DecorationCleanExpired(c *gin.Context) {
	n, err := h.Decoration.CleanExpired()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"cleaned": n})
}

// ---- 装饰品活动 ----

// DecorationActivityCreate POST /decoration/activity（管理端）。
//
//	@Summary	创建装饰品限时活动
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_createActivity
//	@Router		/decoration/activity [post]
func (h *OpsHandler) DecorationActivityCreate(c *gin.Context) {
	var in model.DecorationActivityInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Decoration.CreateActivity(in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// DecorationActivityList GET /decoration/activity。
//
//	@Summary	装饰品活动列表
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				DecorationController_findAllActivities
//	@Router		/decoration/activity [get]
func (h *OpsHandler) DecorationActivityList(c *gin.Context) {
	lr, err := h.Decoration.Activities(c.Query("status"), c.Query("type"),
		atoiDefault(c.Query("page"), 1), atoiDefault(c.Query("limit"), 0))
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// DecorationActivityGet GET /decoration/activity/:id。
//
//	@Summary	活动详情
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_findOneActivity
//	@Router		/decoration/activity/{id} [get]
func (h *OpsHandler) DecorationActivityGet(c *gin.Context) {
	a, err := h.Decoration.GetActivity(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// DecorationActivityUpdate PATCH /decoration/activity/:id（管理端）。
//
//	@Summary	更新装饰品活动
//	@Tags		装饰品
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body object true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_updateActivity
//	@Router		/decoration/activity/{id} [patch]
func (h *OpsHandler) DecorationActivityUpdate(c *gin.Context) {
	var in model.DecorationActivityInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Decoration.UpdateActivity(c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// DecorationActivityDelete DELETE /decoration/activity/:id（管理端）。
//
//	@Summary	删除装饰品活动
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_removeActivity
//	@Router		/decoration/activity/{id} [delete]
func (h *OpsHandler) DecorationActivityDelete(c *gin.Context) {
	if err := h.Decoration.DeleteActivity(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// DecorationMyActivityProgress GET /decoration/activity/progress/my。
//
//	@Summary	我的活动进度
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_getMyActivityProgress
//	@Router		/decoration/activity/progress/my [get]
func (h *OpsHandler) DecorationMyActivityProgress(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	list, err := h.Decoration.MyActivityProgress(uid)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// DecorationActivityClaim POST /decoration/activity/claim/:activityId。
//
//	@Summary	领取活动奖励
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		activityId path string true "activityId"
//	@Success	200	{object}	ResponseBody
//	@ID				DecorationController_claimActivityReward
//	@Router		/decoration/activity/claim/{activityId} [post]
func (h *OpsHandler) DecorationActivityClaim(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	ud, err := h.Decoration.ClaimActivityReward(uid, c.Param("activityId"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, ud)
}

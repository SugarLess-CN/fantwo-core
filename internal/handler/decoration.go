// 装饰品域 HTTP 层（原 DecorationController 21 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
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
//	@Param		body body dto.DecorationInputRequest true "装饰品字段（全部可选，不传=不改）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationDto}
//	@ID			DecorationController_create
//	@Param		body body dto.DecorationInputRequest true "请求体"
//	@Router		/decoration [post]
func (h *OpsHandler) DecorationCreate(c *gin.Context) {
	// 绑定 dto 请求结构：文档声明与实际解析必须同源（docs/18-DTO复用规范.md）。
	var body dto.DecorationInputRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	d, err := h.Decoration.Create(decorationInputOf(body))
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
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.DecorationDto}}
//	@ID			DecorationController_findAll
//	@Router		/decoration [get]
func (h *OpsHandler) DecorationList(c *gin.Context) {
	q := model.DecorationQuery{
		Page:      atoiDefault(c.Query("page"), 1),
		Limit:     atoiDefault(c.Query("limit"), 0),
		Type:      c.Query("type"),
		Status:    c.Query("status"),
		Keyword:   c.Query("keyword"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationDto}
//	@ID			DecorationController_findOne
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
//	@Param		body body dto.DecorationInputRequest true "要改的字段（全部可选）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationDto}
//	@ID			DecorationController_update
//	@Router		/decoration/{id} [patch]
func (h *OpsHandler) DecorationUpdate(c *gin.Context) {
	var body dto.DecorationInputRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	d, err := h.Decoration.Update(c.Param("id"), decorationInputOf(body))
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletedDto}
//	@ID			DecorationController_remove
//	@Router		/decoration/{id} [delete]
func (h *OpsHandler) DecorationDelete(c *gin.Context) {
	if err := h.Decoration.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, dto.DeletedDto{Deleted: true})
}

// DecorationMyList GET /decoration/user/my。
//
//	@Summary	我的装饰品
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserDecorationDto}}
//	@ID			DecorationController_getMyDecorations
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
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserDecorationDto}}
//	@ID			DecorationController_getUserDecorations
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
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserDecorationDto}}
//	@ID			DecorationController_getMyAchievementBadges
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.CurrentDecorationsDto}
//	@ID			DecorationController_getCurrentDecorations
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDecorationDto}
//	@ID			DecorationController_useDecoration
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDecorationDto}
//	@ID			DecorationController_unuseDecoration
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
//	@Param		body body dto.PurchaseDecorationRequest true "decorationId"
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDecorationDto}
//	@ID			DecorationController_purchase
//	@Router		/decoration/purchase [post]
func (h *OpsHandler) DecorationPurchase(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body dto.PurchaseDecorationRequest
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
//	@Param		body body dto.GiftDecorationRequest true "toUserId/decorationId/message"
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDecorationDto}
//	@ID			DecorationController_gift
//	@Router		/decoration/gift [post]
func (h *OpsHandler) DecorationGift(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var body dto.GiftDecorationRequest
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.CleanExpiredResultDto}
//	@ID			DecorationController_cleanExpired
//	@Router		/decoration/clean-expired [post]
func (h *OpsHandler) DecorationCleanExpired(c *gin.Context) {
	n, err := h.Decoration.CleanExpired()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, dto.CleanExpiredResultDto{Cleaned: n})
}

// ---- 装饰品活动 ----

// DecorationActivityCreate POST /decoration/activity（管理端）。
//
//	@Summary	创建装饰品限时活动
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.DecorationActivityInputRequest true "活动字段（name/decorationId 必填）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationActivityDto}
//	@ID			DecorationController_createActivity
//	@Router		/decoration/activity [post]
func (h *OpsHandler) DecorationActivityCreate(c *gin.Context) {
	var body dto.DecorationActivityInputRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Decoration.CreateActivity(decorationActivityInputOf(body))
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
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.DecorationActivityDto}}
//	@ID			DecorationController_findAllActivities
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationActivityDto}
//	@ID			DecorationController_findOneActivity
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
//	@Param		body body dto.DecorationActivityInputRequest true "要改的字段（全部可选）"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DecorationActivityDto}
//	@ID			DecorationController_updateActivity
//	@Router		/decoration/activity/{id} [patch]
func (h *OpsHandler) DecorationActivityUpdate(c *gin.Context) {
	var body dto.DecorationActivityInputRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	a, err := h.Decoration.UpdateActivity(c.Param("id"), decorationActivityInputOf(body))
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeletedDto}
//	@ID			DecorationController_removeActivity
//	@Router		/decoration/activity/{id} [delete]
func (h *OpsHandler) DecorationActivityDelete(c *gin.Context) {
	if err := h.Decoration.DeleteActivity(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, dto.DeletedDto{Deleted: true})
}

// DecorationMyActivityProgress GET /decoration/activity/progress/my。
//
//	@Summary	我的活动进度
//	@Tags		装饰品
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.UserActivityProgressDto}
//	@ID			DecorationController_getMyActivityProgress
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
//	@Success	200	{object}	dto.ResponseBody{data=dto.UserDecorationDto}
//	@ID			DecorationController_claimActivityReward
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

// 让 dto 包成为本文件的真实 import：swag 按「注释所在文件的 import」解析跨包类型，
// 本文件的注解写了 dto.Xxx，这里必须引用一次（不参与运行逻辑）。
var _ = dto.ResponseBody{}

// ---------- dto 请求体 → service 入参 ----------

// decorationInputOf dto 装饰品入参 → service 入参。
//
// 这一层转换的意义是「字段一旦对不上就在编译期报错」，而不是等运行期
// 发现某个请求字段被静默丢弃。
func decorationInputOf(in dto.DecorationInputRequest) model.DecorationInput {
	return model.DecorationInput{
		Name:          in.Name,
		Type:          in.Type,
		Description:   in.Description,
		ImageURL:      in.ImageURL,
		PreviewURL:    in.PreviewURL,
		Rarity:        in.Rarity,
		BubbleColor:   in.BubbleColor,
		ObtainMethod:  in.ObtainMethod,
		IsPurchasable: in.IsPurchasable,
		Price:         in.Price,
		IsPermanent:   in.IsPermanent,
		ValidDays:     in.ValidDays,
		Sort:          in.Sort,
		Status:        in.Status,
	}
}

// decorationActivityInputOf dto 活动入参 → service 入参。
func decorationActivityInputOf(in dto.DecorationActivityInputRequest) model.DecorationActivityInput {
	return model.DecorationActivityInput{
		Name:               in.Name,
		Description:        in.Description,
		Type:               in.Type,
		DecorationID:       in.DecorationID,
		ArticleID:          in.ArticleID,
		RequiredLikes:      in.RequiredLikes,
		RequiredComments:   in.RequiredComments,
		RequiredShares:     in.RequiredShares,
		RequiredRecharge:   in.RequiredRecharge,
		RequiredSignInDays: in.RequiredSignInDays,
		IsPermanent:        in.IsPermanent,
		ValidDays:          in.ValidDays,
		StartTime:          in.StartTime,
		EndTime:            in.EndTime,
		Status:             in.Status,
	}
}

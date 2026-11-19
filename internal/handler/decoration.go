// 装饰品域 HTTP 层（原 DecorationController 21 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// DecorationCreate POST /decoration（管理端）。
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
func (h *OpsHandler) DecorationGet(c *gin.Context) {
	d, err := h.Decoration.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, d)
}

// DecorationUpdate PATCH /decoration/:id（管理端）。
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
func (h *OpsHandler) DecorationDelete(c *gin.Context) {
	if err := h.Decoration.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// DecorationMyList GET /decoration/user/my。
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
	ok(c, ud)
}

// DecorationUnuse POST /decoration/unuse/:decorationId。
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
	ok(c, ud)
}

// DecorationPurchase POST /decoration/purchase。
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
func (h *OpsHandler) DecorationActivityGet(c *gin.Context) {
	a, err := h.Decoration.GetActivity(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, a)
}

// DecorationActivityUpdate PATCH /decoration/activity/:id（管理端）。
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
func (h *OpsHandler) DecorationActivityDelete(c *gin.Context) {
	if err := h.Decoration.DeleteActivity(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// DecorationMyActivityProgress GET /decoration/activity/progress/my。
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

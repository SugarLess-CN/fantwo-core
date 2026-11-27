// 轮播域 HTTP 层（原 BannerController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// BannerCreate POST /banners（管理端）。
//
//	@Summary	创建轮播位
//	@Tags		轮播
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.BannerDto}
//	@ID			BannerController_create
//	@Param		body body dto.CreateBannerRequest true "创建轮播位"
//	@Router		/banners [post]
func (h *OpsHandler) BannerCreate(c *gin.Context) {
	var in model.BannerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.CommonInvalidParam, err.Error())
		return
	}
	b, err := h.Banner.Create(in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, b)
}

// BannerList GET /banners（管理端列表；非管理员强制只看 active）。
//
//	@Summary	轮播位列表（管理端）
//	@Tags		轮播
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.BannerDto}}
//	@ID			BannerController_findAll
//	@Router		/banners [get]
func (h *OpsHandler) BannerList(c *gin.Context) {
	q := model.BannerQuery{
		Page:      atoiDefault(c.Query("page"), 1),
		Limit:     atoiDefault(c.Query("limit"), 0),
		Status:    c.Query("status"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	}
	// 对齐原实现：无 banner:manage 权限的用户只能看到 active
	q.Status = service.EffectiveBannerStatus(q.Status, isAdmin(c))
	lr, err := h.Banner.List(q)
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// BannerActive GET /banners/active（公开）。
//
//	@Summary	当前生效轮播
//	@Tags		轮播
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.BannerDto}
//	@ID			BannerController_findActive
//	@Router		/banners/active [get]
func (h *OpsHandler) BannerActive(c *gin.Context) {
	list, err := h.Banner.Active()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// BannerGet GET /banners/:id（公开）。
//
//	@Summary	轮播位详情
//	@Tags		轮播
//	@Produce	json
//	@Param		id path string true "轮播 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.BannerDto}
//	@ID			BannerController_findOne
//	@Router		/banners/{id} [get]
func (h *OpsHandler) BannerGet(c *gin.Context) {
	b, err := h.Banner.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, b)
}

// BannerUpdate PATCH /banners/:id（管理端）。
//
//	@Summary	更新轮播位
//	@Tags		轮播
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "轮播 ID"
//	@Param		body body dto.UpdateBannerRequest true "bannerRequest"
//	@Success	200	{object}	dto.ResponseBody{data=dto.BannerDto}
//	@ID			BannerController_update
//	@Router		/banners/{id} [patch]
func (h *OpsHandler) BannerUpdate(c *gin.Context) {
	var in model.BannerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailKey(c, errcode.ErrBadRequest, i18n.CommonInvalidParam, err.Error())
		return
	}
	b, err := h.Banner.Update(c.Param("id"), in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, b)
}

// BannerDelete DELETE /banners/:id（管理端）。
//
//	@Summary	删除轮播位
//	@Tags		轮播
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "轮播 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@ID			BannerController_remove
//	@Router		/banners/{id} [delete]
func (h *OpsHandler) BannerDelete(c *gin.Context) {
	if err := h.Banner.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

var _ = dto.ResponseBody{}

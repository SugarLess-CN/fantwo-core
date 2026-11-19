// 轮播域 HTTP 层（原 BannerController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// BannerCreate POST /banners（管理端）。
func (h *OpsHandler) BannerCreate(c *gin.Context) {
	var in model.BannerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
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
func (h *OpsHandler) BannerList(c *gin.Context) {
	q := model.BannerQuery{
		Page:      atoiDefault(c.Query("page"), 1),
		Limit:     atoiDefault(c.Query("limit"), 0),
		Status:    c.Query("status"),
		SortBy:    c.Query("sortBy"),
		SortOrder: c.Query("sortOrder"),
	}
	// 对齐原实现：无 banner:manage 权限的用户只能看到 active
	if !isAdmin(c) {
		q.Status = model.BannerStatusActive
	}
	lr, err := h.Banner.List(q)
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// BannerActive GET /banners/active（公开）。
func (h *OpsHandler) BannerActive(c *gin.Context) {
	list, err := h.Banner.Active()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

// BannerGet GET /banners/:id（公开）。
func (h *OpsHandler) BannerGet(c *gin.Context) {
	b, err := h.Banner.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, b)
}

// BannerUpdate PATCH /banners/:id（管理端）。
func (h *OpsHandler) BannerUpdate(c *gin.Context) {
	var in model.BannerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
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
func (h *OpsHandler) BannerDelete(c *gin.Context) {
	if err := h.Banner.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

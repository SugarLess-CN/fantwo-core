// 举报域 HTTP 层（原 ReportController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// ReportCreate POST /report。
func (h *OpsHandler) ReportCreate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.ReportCreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	r, err := h.Report.Create(uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, r)
}

// ReportList GET /report（管理端）。
func (h *OpsHandler) ReportList(c *gin.Context) {
	q := model.ReportQuery{
		Page:       atoiDefault(c.Query("page"), 1),
		Limit:      atoiDefault(c.Query("limit"), 0),
		Type:       c.Query("type"),
		Status:     c.Query("status"),
		Category:   c.Query("category"),
		ReporterID: c.Query("reporterId"),
		Keyword:    c.Query("keyword"),
		SortBy:     c.Query("sortBy"),
		SortOrder:  c.Query("sortOrder"),
	}
	lr, err := h.Report.List(q)
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// ReportStatistics GET /report/statistics（管理端）。
func (h *OpsHandler) ReportStatistics(c *gin.Context) {
	st, err := h.Report.Statistics()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, st)
}

// ReportGet GET /report/:id（管理端）。
func (h *OpsHandler) ReportGet(c *gin.Context) {
	r, err := h.Report.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, r)
}

// ReportUpdate PATCH /report/:id（管理端：处理/驳回）。
func (h *OpsHandler) ReportUpdate(c *gin.Context) {
	uid := requireUser(c)
	if uid == "" {
		return
	}
	var in model.ReportUpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.FailWith(c, errcode.ErrBadRequest, bindingMsg(err))
		return
	}
	r, err := h.Report.Update(c.Param("id"), uid, in)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, r)
}

// ReportDelete DELETE /report/:id（管理端）。
func (h *OpsHandler) ReportDelete(c *gin.Context) {
	if err := h.Report.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

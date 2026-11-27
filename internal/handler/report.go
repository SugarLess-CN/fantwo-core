// 举报域 HTTP 层（原 ReportController 6 个操作）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// ReportCreate POST /report。
//
//	@Summary	提交举报
//	@Tags		举报
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.ReportDto}
//	@ID			ReportController_create
//	@Param		body body dto.CreateReportRequest true "请求体"
//	@Router		/report [post]
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
//
//	@Summary	举报列表（管理端）
//	@Tags		举报
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ReportDto}}
//	@ID			ReportController_findAll
//	@Router		/report [get]
func (h *OpsHandler) ReportList(c *gin.Context) {
	q := model.ReportQuery{
		Page:       atoiDefault(c.Query("page"), 1),
		Limit:      atoiDefault(c.Query("limit"), 0),
		Type:       c.Query("type"),
		Status:     c.Query("status"),
		Category:   c.Query("category"),
		ReporterID: c.Query("reporter_id"),
		Keyword:    c.Query("keyword"),
		SortBy:     c.Query("sort_by"),
		SortOrder:  c.Query("sort_order"),
	}
	lr, err := h.Report.List(q)
	if err != nil {
		fail(c, err)
		return
	}
	okPage(c, lr)
}

// ReportStatistics GET /report/statistics（管理端）。
//
//	@Summary	举报统计
//	@Tags		举报
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.ReportStatisticsDto}
//	@ID			ReportController_getStatistics
//	@Router		/report/statistics [get]
func (h *OpsHandler) ReportStatistics(c *gin.Context) {
	st, err := h.Report.Statistics()
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, st)
}

// ReportGet GET /report/:id（管理端）。
//
//	@Summary	举报详情
//	@Tags		举报
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "举报 ID example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ReportDto}
//	@ID			ReportController_findOne
//	@Router		/report/{id} [get]
func (h *OpsHandler) ReportGet(c *gin.Context) {
	r, err := h.Report.Get(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, r)
}

// ReportUpdate PATCH /report/:id（管理端：处理/驳回）。
//
//	@Summary	处理举报
//	@Tags		举报
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "举报 ID example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Param		body body dto.UpdateReportRequest true "handleRequest{status,remark}"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ReportDto}
//	@ID			ReportController_update
//	@Router		/report/{id} [patch]
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
//
//	@Summary	删除举报
//	@Tags		举报
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "举报 ID example(9f1d2e34-5678-4abc-9def-0123456789ab)"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@ID			ReportController_remove
//	@Router		/report/{id} [delete]
func (h *OpsHandler) ReportDelete(c *gin.Context) {
	if err := h.Report.Delete(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, gin.H{"deleted": true})
}

// 让 dto 包成为真实 import（swag 只按注解所在文件的 import 解析跨包类型）。
var _ = dto.ResponseBody{}

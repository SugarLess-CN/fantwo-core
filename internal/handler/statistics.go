// 统计模块 handler（2 操作，对齐 swagger tag「统计模块」）。
//
//	GET /statistics/overview   后台统计概览（用户/内容/订单/审核/互动）
//	GET /statistics/trends     近 N 天趋势（days 1..30，默认 7）
//
// 权限：statistics:read | system:monitor（OR，对齐原 @Permissions 两权限写法）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
)

// StatisticsHandler 统计端点。
type StatisticsHandler struct {
	svc *infra.Statistics
}

// NewStatisticsHandler 构造统计 handler。
func NewStatisticsHandler(svc *infra.Statistics) *StatisticsHandler {
	return &StatisticsHandler{svc: svc}
}

// Overview GET /statistics/overview。
//
//	@Summary	站点总览统计
//	@Tags		统计
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				StatisticsController_getOverview
//	@Router		/statistics/overview [get]
func (h *StatisticsHandler) Overview(c *gin.Context) {
	res, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// Trends GET /statistics/trends?days=N。
//
//	@Summary	站点趋势统计
//	@Tags		统计
//	@Produce	json
//	@Security	BearerAuth
//	@Param		startDate query string false "起始日期 YYYY-MM-DD"
//	@Param		endDate query string false "结束日期 YYYY-MM-DD"
//	@Success	200	{object}	ResponseBody
//	@ID				StatisticsController_getTrends
//	@Router		/statistics/trends [get]
func (h *StatisticsHandler) Trends(c *gin.Context) {
	days := infraAtoi(c.Query("days"), 7)
	res, err := h.svc.Trends(c.Request.Context(), days)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, res)
}

// 内容审核域 HTTP 处理层（对齐原 content-audit.controller.ts，6 操作 + 队列状态）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// AuditHandler 审核端点。
type AuditHandler struct {
	Svc *service.AuditService
}

// Config GET /content-audit/config（获取审核配置）。
//
//	@Summary	审核配置
//	@Tags		内容审核
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.AuditConfigDto}
//	@Router		/content-audit/config [get]
func (h *AuditHandler) Config(c *gin.Context) {
	response.Success(c, h.Svc.GetAuditConfig())
}

// Reload POST /content-audit/config/reload（重新加载审核配置）。
//
//	@Summary	重载审核配置
//	@Tags		内容审核
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.AuditConfigReloadDto}
//	@Router		/content-audit/config/reload [post]
func (h *AuditHandler) Reload(c *gin.Context) {
	cfg := h.Svc.GetAuditConfig() // 每次调用现读 config 表，等价于重载
	response.SuccessMsg(c, i18n.AuditConfigReloaded, dto.AuditConfigReloadDto{Provider: cfg.Provider})
}

// PendingArticles GET /content-audit/pending-articles（待审核文章列表）。
//
//	@Summary	待人工复审文章
//	@Tags		内容审核
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.PendingArticleItemDto}}
//	@Router		/content-audit/pending-articles [get]
func (h *AuditHandler) PendingArticles(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, err := h.Svc.ListPendingArticles(page, limit)
	if err != nil {
		failTx(c, err)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// ReviewArticle POST /content-audit/articles/:id/review（人工复审：通过/驳回）。
// 只发 article.status.changed 事件，由 content 域负责落库与计数。
//
//	@Summary	人工复审文章
//	@Tags		内容审核
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "文章 ID"
//	@Param		body body dto.ReviewArticleRequest true "复审入参 result=APPROVED|REJECTED"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AuditReviewResultDto}
//	@Router		/content-audit/articles/{id}/review [post]
func (h *AuditHandler) ReviewArticle(c *gin.Context) {
	var in dto.ReviewArticleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	data, err := h.Svc.ReviewArticle(c.Param("id"), service.ReviewArticleInput{
		Result: in.Result, Reason: in.Reason,
	}, middleware.CurrentUserID(c))
	if err != nil {
		failTx(c, err)
		return
	}
	response.Success(c, data)
}

// AuditText POST /content-audit/text（测试文本审核）。
//
//	@Summary	文本送审（调试）
//	@Tags		内容审核
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.AuditTextRequest true "文本送审入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AuditResultDto}
//	@Router		/content-audit/text [post]
func (h *AuditHandler) AuditText(c *gin.Context) {
	var body dto.AuditTextRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	response.Success(c, h.Svc.TestAuditText(body.Content))
}

// AuditImage POST /content-audit/image（测试图片审核）。
//
//	@Summary	图片送审（调试）
//	@Tags		内容审核
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.AuditImageRequest true "图片送审入参"
//	@Success	200	{object}	dto.ResponseBody{data=dto.AuditResultDto}
//	@Router		/content-audit/image [post]
func (h *AuditHandler) AuditImage(c *gin.Context) {
	var body dto.AuditImageRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	response.Success(c, h.Svc.TestAuditImage(body.URL))
}

// 让 dto 包成为本文件的真实 import（swag 只按「注解所在文件的 import 列表」解析
// 跨包类型，docs/18 §三之一 约束 B）。本文件的注解写了 dto.Xxx，这里引用一次。
// 不参与运行逻辑。
var _ = dto.ResponseBody{}

// 内容审核域 HTTP 处理层（对齐原 content-audit.controller.ts，6 操作 + 队列状态）。
package handler

import (
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
//	@Success	200	{object}	ResponseBody
//	@ID				ContentAuditController_getConfig
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
//	@Success	200	{object}	ResponseBody
//	@ID				ContentAuditController_reloadConfig
//	@Router		/content-audit/config/reload [post]
func (h *AuditHandler) Reload(c *gin.Context) {
	cfg := h.Svc.GetAuditConfig() // 每次调用现读 config 表，等价于重载
	response.SuccessMsg(c, i18n.AuditConfigReloaded, gin.H{"provider": cfg.Provider})
}

// PendingArticles GET /content-audit/pending-articles（待审核文章列表）。
//
//	@Summary	待人工复审文章
//	@Tags		内容审核
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				ContentAuditController_getPendingArticles
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
//	@Param		body body object true "reviewRequest{pass,reason}"
//	@Success	200	{object}	ResponseBody
//	@ID				ContentAuditController_reviewArticle
//	@Router		/content-audit/articles/{id}/review [post]
func (h *AuditHandler) ReviewArticle(c *gin.Context) {
	var in service.ReviewArticleInput
	if err := c.ShouldBindJSON(&in); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	data, err := h.Svc.ReviewArticle(c.Param("id"), in, middleware.CurrentUserID(c))
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
//	@Param		body body object true "textAuditRequest{content,scene}"
//	@Success	200	{object}	ResponseBody
//	@ID				ContentAuditController_auditText
//	@Router		/content-audit/text [post]
func (h *AuditHandler) AuditText(c *gin.Context) {
	var body struct {
		Content string `json:"content" binding:"required"`
	}
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
//	@Param		body body object true "imageAuditRequest{url,scene}"
//	@Success	200	{object}	ResponseBody
//	@ID				ContentAuditController_auditImage
//	@Router		/content-audit/image [post]
func (h *AuditHandler) AuditImage(c *gin.Context) {
	var body struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		failTx(c, errcode.ErrBadRequest)
		return
	}
	response.Success(c, h.Svc.TestAuditImage(body.URL))
}

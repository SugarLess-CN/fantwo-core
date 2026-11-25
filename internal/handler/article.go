package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ArticleHandler 文章管理 HTTP 处理（对齐原 ArticleController）。
type ArticleHandler struct {
	svc *service.ArticleService
	log *zap.Logger
}

// Create 创建文章 POST /article（权限 article:create）。
//
//	@Summary		创建文章
//	@Description	提交文章（图片/混合/视频），入内容审核流程
//	@Tags			文章
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		dto.CreateArticleRequest	true	"文章内容"
//	@Success		200		{object}	dto.ResponseBody{data=dto.ArticleDto}
//	@Failure		400		{object}	ResponseBody
//	@Failure		403		{object}	ResponseBody
//	@Router			/article [post]
func (h *ArticleHandler) Create(c *gin.Context) {
	var req service.CreateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
		return
	}
	view, e := h.svc.Create(c.Request.Context(), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// List 文章列表 GET /article（可选登录）。
//
//	@Summary		文章列表
//	@Description	分页浏览文章，支持标题/分类/标签/类型/状态筛选；携带 token 时返回当前用户互动状态
//	@Tags			文章
//	@Produce		json
//	@Param			page		query		int		false	"页码，默认 1"
//	@Param			limit		query		int		false	"每页条数，默认 10"
//	@Param			title		query		string	false	"标题关键字"
//	@Param			categoryId	query		string	false	"分类 ID"
//	@Param			tagId		query		string	false	"标签 ID"
//	@Param			type		query		string	false	"image/mixed/video，默认 all"
//	@Param			status		query		string	false	"状态筛选，逗号分隔"
//	@Success		200			{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}}
//	@Router			/article [get]
func (h *ArticleHandler) List(c *gin.Context) {
	page, limit := pageOf(c)
	q := service.ArticleListQuery{
		Title:      c.Query("title"),
		CategoryID: c.Query("categoryId"),
		TagID:      c.Query("tagId"),
		Type:       c.DefaultQuery("type", "all"),
		Status:     splitIDs(c.Query("status")),
		Page:       page,
		Limit:      limit,
	}
	list, total, e := h.svc.List(c.Request.Context(), q, actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// Drafts 当前用户草稿 GET /article/drafts。
//
//	@Summary	我的草稿箱
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}}
//	@Router		/article/drafts [get]
func (h *ArticleHandler) Drafts(c *gin.Context) {
	page, limit := pageOf(c)
	list, total, e := h.svc.FindDrafts(c.Request.Context(), actorOf(c), page, limit)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// Recommendations 相关文章 GET /article/recommend/:id。
//
//	@Summary	相关文章推荐
//	@Tags		文章
//	@Produce	json
//	@Param		id path string true "id"
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.ArticleDto}
//	@Router		/article/recommend/{id} [get]
func (h *ArticleHandler) Recommendations(c *gin.Context) {
	list, e := h.svc.FindRecommendations(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// Search 搜索文章 GET /article/search。
//
//	@Summary	文章搜索（ES）
//	@Tags		文章
//	@Produce	json
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}}
//	@Router		/article/search [get]
func (h *ArticleHandler) Search(c *gin.Context) {
	page, limit := pageOf(c)
	q := service.ArticleListQuery{
		Keyword:    c.Query("keyword"),
		CategoryID: c.Query("categoryId"),
		SortBy:     c.DefaultQuery("sortBy", "relevance"),
		Page:       page,
		Limit:      limit,
	}
	list, total, e := h.svc.List(c.Request.Context(), q, actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// HotSearch 热门搜索词 GET /article/hot-search。
//
//	@Summary	热搜词
//	@Tags		文章
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=[]dto.HotSearchItemDto}
//	@Router		/article/hot-search [get]
func (h *ArticleHandler) HotSearch(c *gin.Context) {
	limit := intQuery(c, "limit", 10)
	list, e := h.svc.HotSearch(c.Request.Context(), limit, c.Query("keyword"))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// ByAuthor 按作者取文章 GET /article/author/:id。
//
//	@Summary	作者主页文章
//	@Tags		文章
//	@Produce	json
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}}
//	@Router		/article/author/{id} [get]
func (h *ArticleHandler) ByAuthor(c *gin.Context) {
	page, limit := pageOf(c)
	q := service.ArticleListQuery{
		Keyword:    c.Query("keyword"),
		CategoryID: c.Query("categoryId"),
		Type:       c.DefaultQuery("type", "all"),
		Page:       page,
		Limit:      limit,
	}
	list, total, e := h.svc.FindByAuthor(c.Request.Context(), c.Param("id"), q, actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctPage(c, list, total, page, limit)
}

// PublishedIDs 已发布文章 ID 列表 GET /article/published/ids。
//
//	@Summary	已发布文章 ID 列表
//	@Tags		文章
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=[]string}
//	@Router		/article/published/ids [get]
func (h *ArticleHandler) PublishedIDs(c *gin.Context) {
	ids, e := h.svc.PublishedArticleIDs(c.Request.Context())
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, ids)
}

// FindOne 文章详情 GET /article/:id（可选登录）。
//
//	@Summary		文章详情
//	@Tags			文章
//	@Produce		json
//	@Param			id	path		string	true	"文章 ID"
//	@Success		200	{object}	dto.ResponseBody{data=dto.ArticleDto}
//	@Failure		404	{object}	ResponseBody
//	@Router			/article/{id} [get]
func (h *ArticleHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindOne(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新文章 PATCH /article/:id（权限 article:update）。
//
//	@Summary	更新文章
//	@Tags		文章
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		string	true	"文章 ID"
//	@Param		body	body		dto.UpdateArticleRequest	true	"待更新字段（部分更新）"
//	@Success	200		{object}	dto.ResponseBody{data=dto.ArticleDto}
//	@Failure	403		{object}	ResponseBody
//	@Router		/article/{id} [patch]
func (h *ArticleHandler) Update(c *gin.Context) {
	var req service.UpdateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, err.Error()))
		return
	}
	view, e := h.svc.Update(c.Request.Context(), c.Param("id"), actorOf(c), req)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Remove 删除文章 DELETE /article/:id（权限 article:delete）。
//
//	@Summary	删除文章
//	@Tags		文章
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"文章 ID"
//	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}
//	@Failure	403	{object}	ResponseBody
//	@Router		/article/{id} [delete]
func (h *ArticleHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// SetFeatured 设置精华 PATCH /article/:id/featured。
//
//	@Summary	设置/取消精华
//	@Tags		文章
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body dto.SetFeaturedRequest true "是否设精"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SetFeaturedResultDto}
//	@Router		/article/{id}/featured [patch]
func (h *ArticleHandler) SetFeatured(c *gin.Context) {
	var req service.SetFeaturedRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsFeatured == nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, "isFeatured must be a boolean"))
		return
	}
	out, e := h.svc.SetFeatured(c.Request.Context(), c.Param("id"), actorOf(c), *req.IsFeatured)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// SetProfilePin 设置个人主页置顶 PATCH /article/:id/profile-pin。
//
//	@Summary	设置/取消主页置顶
//	@Tags		文章
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body dto.SetProfilePinRequest true "是否主页置顶"
//	@Success	200	{object}	dto.ResponseBody{data=dto.ProfilePinResultDto}
//	@Router		/article/{id}/profile-pin [patch]
func (h *ArticleHandler) SetProfilePin(c *gin.Context) {
	var req service.SetProfilePinRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsPinned == nil {
		ctFail(c, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, "isPinned must be a boolean"))
		return
	}
	out, e := h.svc.SetProfilePin(c.Request.Context(), c.Param("id"), actorOf(c), *req.IsPinned)
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// parseTagQuery 解析标签列表查询参数（tag 域复用）。
func parseTagQuery(c *gin.Context) repository.TagListQuery {
	page, limit := pageOf(c)
	return repository.TagListQuery{
		Name:    c.Query("name"),
		SortBy:  c.Query("sortBy"),
		SortOrd: c.DefaultQuery("sortOrder", "DESC"),
		Page:    page,
		Limit:   limit,
	}
}

// intQuery 读取整型 query（缺省/非法返回默认值）。
func intQuery(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

// var _ 保证本文件对 dto 包的 import 不被判为未使用 —— swag 解析 @Success/@Param 的
// dto.Xxx 时**只看注解所在文件的 import 列表**（docs/18 §三之一 约束 B），
// 只在别的文件 import 无效，会报 "cannot find type definition"。
var _ = dto.ResponseBody{}

package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
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
func (h *ArticleHandler) Create(c *gin.Context) {
	var req service.CreateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
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
func (h *ArticleHandler) Recommendations(c *gin.Context) {
	list, e := h.svc.FindRecommendations(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, list)
}

// Search 搜索文章 GET /article/search。
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
func (h *ArticleHandler) PublishedIDs(c *gin.Context) {
	ids, e := h.svc.PublishedArticleIDs(c.Request.Context())
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, ids)
}

// FindOne 文章详情 GET /article/:id（可选登录）。
func (h *ArticleHandler) FindOne(c *gin.Context) {
	view, e := h.svc.FindOne(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, view)
}

// Update 更新文章 PATCH /article/:id（权限 article:update）。
func (h *ArticleHandler) Update(c *gin.Context) {
	var req service.UpdateArticleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ctFail(c, errcode.ErrBadRequest.With("参数校验失败："+err.Error()))
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
func (h *ArticleHandler) Remove(c *gin.Context) {
	out, e := h.svc.Remove(c.Request.Context(), c.Param("id"), actorOf(c))
	if e != nil {
		ctFail(c, e)
		return
	}
	ctOK(c, out)
}

// SetFeatured 设置精华 PATCH /article/:id/featured。
func (h *ArticleHandler) SetFeatured(c *gin.Context) {
	var req service.SetFeaturedRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsFeatured == nil {
		ctFail(c, errcode.ErrBadRequest.With("参数 isFeatured 必须为布尔值"))
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
func (h *ArticleHandler) SetProfilePin(c *gin.Context) {
	var req service.SetProfilePinRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IsPinned == nil {
		ctFail(c, errcode.ErrBadRequest.With("参数 isPinned 必须为布尔值"))
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

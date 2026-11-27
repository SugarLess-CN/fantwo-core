// 搜索管理 handler（3 个管理操作 + 1 个检索端点）。
//
// 端点与权限（对齐原 search.controller.ts 的 @Permissions("article:manage")）：
//
//	POST /search/sync/articles    article:manage  全量同步文章到 ES
//	GET  /search/status           article:manage  ES 状态（启用/索引/文档数）
//	POST /search/clear/articles   article:manage  清空文章索引
//	GET  /search                  （可选登录）    文章检索（ES 出 id → 回库取正文）
//
// 未配置 Elasticsearch 时的错误码：5030「服务未配置」（任务书约定），
// HTTP 503；/search/status 例外——原实现返回 {enabled:false, message} 而非错误，此处沿用。
package handler

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ErrServiceNotConfigured 服务未配置（errcode 5030，语义对齐任务书）。
// BizSearchNotConfigured 搜索未配置业务码（6 万段，基础设施域）。
const BizSearchNotConfigured = 60100

// ErrServiceNotConfigured 服务未配置（语义对齐原任务书 5030）。
var ErrServiceNotConfigured = &errcode.Err{
	BizCode: BizSearchNotConfigured,
	HTTP:    http.StatusServiceUnavailable,
	Key:     i18n.SearchNotConfigured,
}

// SearchHandler 搜索端点。
type SearchHandler struct {
	svc  *infra.Searcher
	db   *gorm.DB
	logs *infra.SearchLogService // 热门搜索词聚合（nil 表示未接入）
	log  *zap.Logger             // nil 安全：仅用于 ES 失败回落时的告警
}

// NewSearchHandler 构造搜索 handler（db / log 可为 nil）。
func NewSearchHandler(svc *infra.Searcher, db *gorm.DB, logs *infra.SearchLogService, log *zap.Logger) *SearchHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &SearchHandler{svc: svc, db: db, logs: logs, log: log}
}

// SyncArticles POST /search/sync/articles（对齐 syncArticles）。
//
// body 可选：{"batchSize":100,"status":"PUBLISHED"}；ES 未启用返回 5030。
//
//	@Summary	全量同步文章到 ES
//	@Tags		搜索
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.SearchSyncRequest false "batch_size/status 均可缺省"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SearchSyncResultDto}
//	@ID			SearchController_syncArticles
//	@Router		/search/sync/articles [post]
func (h *SearchHandler) SyncArticles(c *gin.Context) {
	if !h.svc.Enabled() {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	var body dto.SearchSyncRequest
	// body 可缺省（全量同步默认参数），解析失败不报错
	_ = c.ShouldBindJSON(&body)

	total, err := h.svc.SyncArticles(c.Request.Context(), h.db, body.BatchSize, body.Status)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, gin.H{
		"success":      true,
		"message":      i18n.Tf(i18n.SearchSynced, total),
		"data":         gin.H{"total_synced": total},
		"total_synced": total,
	})
}

// Status GET /search/status（对齐 getStatus：未启用时 success=true 且 enabled=false）。
//
//	@Summary	搜索索引状态
//	@Tags		搜索
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.SearchStatusDto}
//	@ID			SearchController_getStatus
//	@Router		/search/status [get]
func (h *SearchHandler) Status(c *gin.Context) {
	res := h.svc.Status(c.Request.Context())
	response.Success(c, res)
}

// ClearArticles POST /search/clear/articles（对齐 clearArticles）。
//
//	@Summary	清空文章索引
//	@Tags		搜索
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.SearchSyncResultDto}
//	@ID			SearchController_clearArticles
//	@Router		/search/clear/articles [post]
func (h *SearchHandler) ClearArticles(c *gin.Context) {
	if !h.svc.Enabled() {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	if err := h.svc.ClearIndex(c.Request.Context()); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.SuccessMsg(c, i18n.SearchIndexCleared, gin.H{"success": true})
}

// SearchArticles GET /search（文章检索：keyword / categoryId / sortBy / page / limit）。
//
// **ES 优先，DB 回落**（用户 2026-11-27 指定）：ES 未配置 / 查询失败时回落 DB
// （LIKE 检索 + 与 ES 同语义的排序），不再像旧版那样直接 503 —— 搜索是可降级能力，
// 不该把 DB 能扛的活绑死在 ES 的可用性上。只有 DB 也不可用时才报错。
//
//	@Summary	搜索文章
//	@Tags		搜索
//	@Produce	json
//	@Param		keyword query string false "关键字" example(风景)
//	@Param		category_id query string false "分类 ID" example(2b3c4d5e-0000-4f60-9bac-000000000002)
//	@Param		sort_by query string false "排序方式（默认 relevance）" example(relevance) enums(relevance, latest, views, likes)
//	@Param		page query int false "页码" example(1)
//	@Param		limit query int false "每页条数" example(10)
//	@Success	200	{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.SearchArticleItemDto}}
//	@ID			SearchController_searchArticles
//	@Router		/search [get]
func (h *SearchHandler) SearchArticles(c *gin.Context) {
	keyword := c.Query("keyword")
	if keyword == "" {
		response.FailKey(c, errcode.ErrBadRequest, i18n.SearchKeywordRequired)
		return
	}
	page := infraAtoi(c.Query("page"), 1)
	limit := infraAtoi(c.Query("limit"), 10)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}
	req := infra.ArticleSearchRequest{
		Keyword:    keyword,
		CategoryID: c.Query("category_id"),
		SortBy:     c.DefaultQuery("sort_by", "relevance"),
		Page:       page,
		Limit:      limit,
		UserID:     infraCurrentUserID(c),
		IP:         c.ClientIP(),
	}
	// **ES 优先，DB 回落**（用户 2026-11-27 指定）。
	//
	// 三种回落情形（都不再直接失败，除非 DB 也查不了）：
	//   ① ES 未配置/未启用 → SearchArticles 返回空结果集 → 检测到后走 DB；
	//   ② ES 报错（连不上/查询失败）→ 走 DB；
	//   ③ DB 也为 nil（降级启动）→ 才真的报错。
	if h.svc.Enabled() {
		res, err := h.svc.SearchArticles(c.Request.Context(), h.db, req)
		if err == nil {
			response.SuccessPage(c, res.List, res.Total, page, limit)
			return
		}
		// ES 报错：记日志后回落 DB，不把 ES 的可用性变成搜索的硬依赖。
		if h.log != nil {
			h.log.Warn("ES 检索失败，/search 回落 DB", zap.Error(err))
		}
	}
	// ---- DB 回落 ----
	list, total, ok := h.searchByDB(c, req, page, limit)
	if !ok {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// searchByDB /search 的 DB 回落路径
// （与 ES 路径产出同一套精简字段，前端无需分支处理）。
func (h *SearchHandler) searchByDB(c *gin.Context, req infra.ArticleSearchRequest, page, limit int) ([]map[string]interface{}, int64, bool) {
	if h.db == nil {
		return nil, 0, false
	}
	q := h.db.Table("articles").
		Select("id, title, summary, cover, type, author_id, category_id, status, views, likes, favorite_count, comment_count, created_at, updated_at").
		Where("deleted_at = 0").
		Where("status = ?", "PUBLISHED")
	if req.Keyword != "" {
		like := "%" + req.Keyword + "%"
		q = q.Where("(title LIKE ? OR summary LIKE ?)", like, like)
	}
	if req.CategoryID != "" {
		q = q.Where("category_id = ?", req.CategoryID)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, false
	}
	// 排序与 ES 路径保持同一套语义（见 articleOrder 的 relevance/latest/views/likes）。
	order := "likes DESC, views DESC, created_at DESC"
	switch req.SortBy {
	case "latest":
		order = "created_at DESC"
	case "views":
		order = "views DESC, created_at DESC"
	}
	var rows []map[string]interface{}
	if err := q.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, false
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		out = append(out, infra.RowToAPI(r))
	}
	return out, total, true
}

// infraItoa64 int64 转字符串（避免 handler 层引入 strconv 依赖链）。
func infraItoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// infraAtoi 字符串转整数，失败返回默认值。
func infraAtoi(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	sign := 1
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if i == 0 && (ch == '-' || ch == '+') {
			if ch == '-' {
				sign = -1
			}
			continue
		}
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return sign * n
}

// HotKeywords GET /search/hot?days=7&limit=10&scene=&minResults=1
//
// 热门搜索词聚合（近 N 天 top N），公开端点：前端热搜展示与 content 域
// /article/hot-search 均可直接使用（原实现为热门标签+分类合成，此处为真实检索数据）。
//
//	@Summary	热搜词
//	@Tags		搜索
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.HotKeywordsDto}
//	@ID			SearchController_getHotKeywords
//	@Router		/search/hot [get]
func (h *SearchHandler) HotKeywords(c *gin.Context) {
	days := infraAtoi(c.Query("days"), 7)
	limit := infraAtoi(c.Query("limit"), 10)
	minResults := infraAtoi(c.Query("min_results"), 1)
	scene := c.Query("scene")

	logs := h.logs
	if logs == nil {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	list, err := logs.TopKeywordsByScene(c.Request.Context(), scene, days, limit, minResults)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, gin.H{
		"days":  days,
		"total": len(list),
		"list":  list,
	})
}

// infraCurrentUserID 从 gin context 取当前用户 id（未登录返回空串）。
func infraCurrentUserID(c *gin.Context) string {
	raw, exists := c.Get(middleware.CtxUserKey)
	if !exists {
		return ""
	}
	claims, ok := raw.(*jwtx.Claims)
	if !ok || claims == nil {
		return ""
	}
	return claims.UserID
}

var _ = dto.ResponseBody{}

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

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ErrServiceNotConfigured 服务未配置（errcode 5030，语义对齐任务书）。
var ErrServiceNotConfigured = &errcode.Err{
	Code: 5030,
	HTTP: http.StatusServiceUnavailable,
	Msg:  "服务未配置",
}

// SearchHandler 搜索端点。
type SearchHandler struct {
	svc *infra.Searcher
	db  *gorm.DB
}

// NewSearchHandler 构造搜索 handler（db 可为 nil）。
func NewSearchHandler(svc *infra.Searcher, db *gorm.DB) *SearchHandler {
	return &SearchHandler{svc: svc, db: db}
}

// SyncArticles POST /search/sync/articles（对齐 syncArticles）。
//
// body 可选：{"batchSize":100,"status":"PUBLISHED"}；ES 未启用返回 5030。
func (h *SearchHandler) SyncArticles(c *gin.Context) {
	if !h.svc.Enabled() {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	var body struct {
		BatchSize int    `json:"batchSize"`
		Status    string `json:"status"`
	}
	// body 可缺省（全量同步默认参数），解析失败不报错
	_ = c.ShouldBindJSON(&body)

	total, err := h.svc.SyncArticles(c.Request.Context(), h.db, body.BatchSize, body.Status)
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, gin.H{
		"success":     true,
		"message":     "成功同步 " + infraItoa64(int64(total)) + " 篇文章到 Elasticsearch",
		"data":        gin.H{"totalSynced": total},
		"totalSynced": total,
	})
}

// Status GET /search/status（对齐 getStatus：未启用时 success=true 且 enabled=false）。
func (h *SearchHandler) Status(c *gin.Context) {
	res := h.svc.Status(c.Request.Context())
	response.Success(c, res)
}

// ClearArticles POST /search/clear/articles（对齐 clearArticles）。
func (h *SearchHandler) ClearArticles(c *gin.Context) {
	if !h.svc.Enabled() {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	if err := h.svc.ClearIndex(c.Request.Context()); err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.Success(c, gin.H{"success": true, "message": "文章搜索索引已清空"})
}

// SearchArticles GET /search（文章检索：keyword / categoryId / sortBy / page / limit）。
//
// ES 未配置时按任务书要求返回 5030（区别于管理端 status 的宽容语义）。
func (h *SearchHandler) SearchArticles(c *gin.Context) {
	if !h.svc.Enabled() {
		response.Fail(c, ErrServiceNotConfigured)
		return
	}
	keyword := c.Query("keyword")
	if keyword == "" {
		response.FailWith(c, errcode.ErrBadRequest, "keyword 不能为空")
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
	res, err := h.svc.SearchArticles(c.Request.Context(), h.db, infra.ArticleSearchRequest{
		Keyword:    keyword,
		CategoryID: c.Query("categoryId"),
		SortBy:     c.DefaultQuery("sortBy", "relevance"),
		Page:       page,
		Limit:      limit,
	})
	if err != nil {
		response.Fail(c, infraErrOrNil(err))
		return
	}
	response.SuccessPage(c, res.List, res.Total, page, limit)
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

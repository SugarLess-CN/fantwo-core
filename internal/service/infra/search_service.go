// 基础设施域：搜索（Elasticsearch 文章索引）。
//
// 对齐原仓库 tmp/picart/src/modules/search：
//   - search.service.ts：ES 客户端懒初始化（未配置即禁用，不阻塞启动）、
//     索引 mapping（article-search.mapping.ts）、bulk 批量写、查询构造；
//   - search.controller.ts：/search/sync/articles 全量同步、/search/status 状态、
//     /search/clear/articles 清空索引（均为 article:manage 权限）。
//
// 与原始实现的差异（因骨架主键改 UUID，见 docs/04 §5.4）：
//   - ES 文档 id 用 keyword（原为 integer 自增 id）；
//   - 文档正文不落全文 content（改为截断 8KB），避免索引膨胀；命中后回库取正文。
//
// 文章数据读取走原生表查询（不 import content 域的 model.Article），
// 保证跨域并行开发期不产生编译耦合；列名以 articles 表实际 snake_case 为准。
package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/elastic/go-elasticsearch/v8"
	"gorm.io/gorm"
)

// searchTimeout ES 调用超时。
const searchTimeout = 10 * time.Second

// articlesIndexDefault 默认索引名（对齐原 ELASTICSEARCH_INDEX_ARTICLE 语义）。
const articlesIndexDefault = "picart_articles"

// Searcher Elasticsearch 搜索服务（客户端懒初始化）。
type Searcher struct {
	cfg config.SearchEngineConfig

	mu      sync.Mutex
	client  *elasticsearch.Client // nil 表示尚未初始化
	initErr error                 // 初始化失败原因（避免每次重试打满日志）

	// logs 检索日志（热门搜索词来源，见 search_log.go）；nil 表示不记录
	logs *SearchLogService
}

// NewSearcher 构造搜索服务（不建立连接，首次调用才懒初始化）。
func NewSearcher(cfg config.SearchEngineConfig) *Searcher {
	if cfg.ArticlesIndex == "" {
		cfg.ArticlesIndex = articlesIndexDefault
	}
	return &Searcher{cfg: cfg}
}

// WithLogs 注入检索日志服务（热门搜索词的落库与聚合）。
func (s *Searcher) WithLogs(logs *SearchLogService) *Searcher { s.logs = logs; return s }

// Logs 取检索日志服务（nil 时调用方需自行判空）。
func (s *Searcher) Logs() *SearchLogService { return s.logs }

// Enabled ES 是否已配置（enabled=true 且至少一个地址）。
// 对齐原 isElasticsearchEnabled：未配置时 /search 系列端点返回 5030「服务未配置」。
func (s *Searcher) Enabled() bool {
	return s.cfg.Enabled && len(s.cfg.Addresses) > 0
}

// Index 索引名。
func (s *Searcher) Index() string { return s.cfg.ArticlesIndex }

// clientOrInit 懒初始化 ES 客户端（并发安全，只初始化一次）。
func (s *Searcher) clientOrInit() (*elasticsearch.Client, error) {
	if !s.Enabled() {
		return nil, svcUnavailable("Elasticsearch 未配置")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	if s.initErr != nil {
		return nil, svcUnavailable("Elasticsearch 初始化失败: " + s.initErr.Error())
	}
	cfg := elasticsearch.Config{Addresses: s.cfg.Addresses}
	if s.cfg.Username != "" {
		cfg.Username = s.cfg.Username
		cfg.Password = s.cfg.Password
	}
	c, err := elasticsearch.NewClient(cfg)
	if err != nil {
		s.initErr = err
		return nil, svcUnavailable("Elasticsearch 初始化失败: " + err.Error())
	}
	s.client = c
	return c, nil
}

// Ping ES 连通性探测。
func (s *Searcher) Ping(ctx context.Context) error {
	c, err := s.clientOrInit()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	res, err := c.Ping(c.Ping.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("Elasticsearch 返回 %s", res.Status())
	}
	return nil
}

// StatusResult /search/status 响应（对齐 getStatus）。
type StatusResult struct {
	Enabled   bool   `json:"enabled"`
	Index     string `json:"index,omitempty"`
	Exists    bool   `json:"indexExists"`
	Documents int64  `json:"documents"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Status 索引状态（未配置时 Enabled=false + message，不返回错误——对齐原实现语义）。
func (s *Searcher) Status(ctx context.Context) StatusResult {
	if !s.Enabled() {
		return StatusResult{Enabled: false, Message: "Elasticsearch 未配置"}
	}
	res := StatusResult{Enabled: true, Index: s.Index()}
	c, err := s.clientOrInit()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	// 索引是否存在
	exists, err := s.indexExists(ctx, c)
	if err != nil {
		res.Error = "Elasticsearch 连接异常: " + err.Error()
		return res
	}
	res.Exists = exists
	if !exists {
		res.Message = "索引尚未创建"
		return res
	}
	// 文档数（_all.primaries.docs.count）
	statRes, err := c.Indices.Stats(c.Indices.Stats.WithContext(ctx), c.Indices.Stats.WithIndex(s.Index()))
	if err != nil {
		res.Error = "Elasticsearch 连接异常: " + err.Error()
		return res
	}
	defer statRes.Body.Close()
	if statRes.IsError() {
		res.Error = "Elasticsearch 连接异常: " + statRes.Status()
		return res
	}
	var stats struct {
		Indices map[string]struct {
			Primaries struct {
				Docs struct {
					Count int64 `json:"count"`
				} `json:"docs"`
			} `json:"primaries"`
		} `json:"indices"`
	}
	if err := json.NewDecoder(statRes.Body).Decode(&stats); err == nil {
		if idx, ok := stats.Indices[s.Index()]; ok {
			res.Documents = idx.Primaries.Docs.Count
		}
	}
	return res
}

// indexExists 索引是否已创建。
func (s *Searcher) indexExists(ctx context.Context, c *elasticsearch.Client) (bool, error) {
	res, err := c.Indices.Exists([]string{s.Index()}, c.Indices.Exists.WithContext(ctx))
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200:
		return true, nil
	case 404:
		return false, nil
	default:
		return false, fmt.Errorf("查询索引失败: %s", res.Status())
	}
}

// EnsureIndex 确保索引存在（不存在则按 mapping 创建）。
func (s *Searcher) EnsureIndex(ctx context.Context) error {
	c, err := s.clientOrInit()
	if err != nil {
		return err
	}
	exists, err := s.indexExists(ctx, c)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	body := `{
  "mappings": {
    "properties": {
      "id":               {"type": "keyword"},
      "title":            {"type": "text", "analyzer": "standard", "fields": {"keyword": {"type": "keyword"}}},
      "content":          {"type": "text", "analyzer": "standard"},
      "summary":          {"type": "text", "analyzer": "standard"},
      "cover":            {"type": "keyword", "index": false},
      "type":             {"type": "keyword"},
      "authorId":         {"type": "keyword"},
      "authorName":       {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
      "categoryId":       {"type": "keyword"},
      "categoryName":     {"type": "keyword"},
      "tags":             {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
      "tagIds":           {"type": "keyword"},
      "status":           {"type": "keyword"},
      "views":            {"type": "integer"},
      "likes":            {"type": "integer"},
      "favoriteCount":    {"type": "integer"},
      "commentCount":     {"type": "integer"},
      "requireLogin":     {"type": "boolean"},
      "requireFollow":    {"type": "boolean"},
      "requireMembership":{"type": "boolean"},
      "requirePayment":   {"type": "boolean"},
      "viewPrice":        {"type": "float"},
      "sort":             {"type": "integer"},
      "isFeatured":       {"type": "boolean"},
      "createdAt":        {"type": "date"},
      "updatedAt":        {"type": "date"}
    }
  }
}`
	res, err := c.Indices.Create(s.Index(),
		c.Indices.Create.WithContext(ctx),
		c.Indices.Create.WithBody(strings.NewReader(body)))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("创建索引失败: %s %s", res.Status(), string(msg))
	}
	return nil
}

// ClearIndex 清空索引（删除后重建空索引，保证 mapping 仍在——对齐 clearIndex）。
func (s *Searcher) ClearIndex(ctx context.Context) error {
	c, err := s.clientOrInit()
	if err != nil {
		return err
	}
	res, err := c.Indices.Delete([]string{s.Index()}, c.Indices.Delete.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// 404 视为已不存在，继续重建
	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("删除索引失败: %s", res.Status())
	}
	return s.EnsureIndex(ctx)
}

// SyncArticles 全量同步文章到 ES（对齐 syncArticles：分批查库 → bulk 写）。
//
// batchSize 默认 100；status 默认 PUBLISHED。返回同步条数。
func (s *Searcher) SyncArticles(ctx context.Context, db *gorm.DB, batchSize int, status string) (int, error) {
	if db == nil {
		return 0, svcUnavailable("数据库不可用")
	}
	if _, err := s.clientOrInit(); err != nil {
		return 0, err
	}
	if err := s.EnsureIndex(ctx); err != nil {
		return 0, err
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	if status == "" {
		status = "PUBLISHED"
	}

	total := 0
	for page := 0; ; page++ {
		rows, err := s.fetchArticles(db, status, batchSize, page*batchSize)
		if err != nil {
			return total, err
		}
		if len(rows) == 0 {
			break
		}
		n, err := s.bulkIndex(ctx, rows)
		if err != nil {
			return total, err
		}
		total += n
		if len(rows) < batchSize {
			break
		}
	}
	return total, nil
}

// fetchArticles 分批取文章（原生表查询，避免跨域 model 依赖）。
//
// 只在表存在时工作；表缺失（content 域尚未迁移完成）返回明确错误。
func (s *Searcher) fetchArticles(db *gorm.DB, status string, limit, offset int) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	err := db.Table("articles").
		Select("id, title, summary, content, cover, type, author_id, category_id, status, "+
			"views, likes, favorite_count, comment_count, require_login, require_follow, "+
			"require_membership, require_payment, view_price, sort, is_featured, created_at, updated_at").
		Where("status = ?", status).
		Where("deleted_at = 0").
		Order("created_at ASC").
		Limit(limit).Offset(offset).
		Find(&rows).Error
	if err != nil {
		return nil, svcInternal("查询文章失败: %v", err)
	}
	return rows, nil
}

// bulkIndex 批量写入（_id = 文章 id，对齐 ES 文档结构）。
func (s *Searcher) bulkIndex(ctx context.Context, rows []map[string]interface{}) (int, error) {
	c, err := s.clientOrInit()
	if err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	for _, r := range rows {
		id := asString(r["id"])
		if id == "" {
			continue
		}
		action := map[string]interface{}{"index": map[string]interface{}{"_index": s.Index(), "_id": id}}
		line, err := json.Marshal(action)
		if err != nil {
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
		doc, err := json.Marshal(buildArticleDoc(r))
		if err != nil {
			continue
		}
		buf.Write(doc)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := c.Bulk(bytes.NewReader(buf.Bytes()),
		c.Bulk.WithContext(ctx),
		c.Bulk.WithRefresh("true"))
	if err != nil {
		return 0, svcInternal("ES bulk 写入失败: %v", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		msg, _ := io.ReadAll(res.Body)
		return 0, svcInternal("ES bulk 写入失败: %s %s", res.Status(), string(msg))
	}
	// 解析 bulk 结果中的成功条数
	var out struct {
		Items []map[string]struct {
			Status int `json:"status"`
		} `json:"items"`
	}
	n := 0
	if err := json.NewDecoder(res.Body).Decode(&out); err == nil {
		for _, item := range out.Items {
			for _, v := range item {
				if v.Status >= 200 && v.Status < 300 {
					n++
				}
			}
		}
	}
	if n == 0 {
		n = len(rows) // 无法解析时按提交条数计
	}
	return n, nil
}

// buildArticleDoc 把 DB 行映射成 ES 文档（camelCase，对齐 article-search.mapping）。
func buildArticleDoc(r map[string]interface{}) map[string]interface{} {
	doc := map[string]interface{}{
		"id":                asString(r["id"]),
		"title":             asString(r["title"]),
		"summary":           asString(r["summary"]),
		"cover":             asString(r["cover"]),
		"type":              asString(r["type"]),
		"status":            asString(r["status"]),
		"authorId":          asString(r["author_id"]),
		"categoryId":        asString(r["category_id"]),
		"views":             asInt(r["views"]),
		"likes":             asInt(r["likes"]),
		"favoriteCount":     asInt(r["favorite_count"]),
		"commentCount":      asInt(r["comment_count"]),
		"requireLogin":      asBool(r["require_login"]),
		"requireFollow":     asBool(r["require_follow"]),
		"requireMembership": asBool(r["require_membership"]),
		"requirePayment":    asBool(r["require_payment"]),
		"viewPrice":         asFloat(r["view_price"]),
		"sort":              asInt(r["sort"]),
		"isFeatured":        asBool(r["is_featured"]),
		"createdAt":         asTime(r["created_at"]),
		"updatedAt":         asTime(r["updated_at"]),
	}
	// 正文截断 8KB：全文入库会让索引膨胀几十倍，命中后回库取正文
	content := asString(r["content"])
	if len(content) > 8192 {
		content = content[:8192]
	}
	doc["content"] = content
	return doc
}

// ArticleSearchRequest 文章检索入参（GET /search，对齐 ArticleController.search 语义）。
type ArticleSearchRequest struct {
	Keyword    string
	CategoryID string
	SortBy     string // relevance / latest / views / likes
	Page       int
	Limit      int
	// UserID / IP 仅用于检索日志（游客 UserID 为空），不参与 ES 查询条件
	UserID string
	IP     string
}

// SearchResult 检索结果。
type SearchResult struct {
	List  []map[string]interface{} `json:"list"`
	Total int64                    `json:"total"`
}

// SearchArticles 文章检索：ES 出 id 与顺序 → 回库取完整行（保持 ES 排序）。
//
// ES 未配置时返回 503（errcode 5030 语义，见 handler 的分支处理）。
func (s *Searcher) SearchArticles(ctx context.Context, db *gorm.DB, req ArticleSearchRequest) (*SearchResult, error) {
	c, err := s.clientOrInit()
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, svcUnavailable("数据库不可用")
	}
	query := s.buildQuery(req)
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(query); err != nil {
		return nil, svcInternal("构造检索请求失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	res, err := c.Search(
		c.Search.WithContext(ctx),
		c.Search.WithIndex(s.Index()),
		c.Search.WithBody(&body),
		c.Search.WithFrom((req.Page-1)*req.Limit),
		c.Search.WithSize(req.Limit),
	)
	if err != nil {
		return nil, svcInternal("ES 检索失败: %v", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		msg, _ := io.ReadAll(res.Body)
		return nil, svcInternal("ES 检索失败: %s %s", res.Status(), string(msg))
	}
	var parsed struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID string `json:"_id"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, svcInternal("解析 ES 响应失败: %v", err)
	}
	ids := make([]string, 0, len(parsed.Hits.Hits))
	for _, h := range parsed.Hits.Hits {
		ids = append(ids, h.ID)
	}
	out := &SearchResult{Total: parsed.Hits.Total.Value, List: []map[string]interface{}{}}
	if len(ids) == 0 {
		s.recordSearch(req, 0)
		return out, nil
	}
	var rows []map[string]interface{}
	if err := db.Table("articles").
		Select("id, title, summary, cover, type, author_id, category_id, status, views, likes, favorite_count, comment_count, created_at, updated_at").
		Where("id IN ?", ids).Where("deleted_at = 0").Find(&rows).Error; err != nil {
		return nil, svcInternal("查询文章失败: %v", err)
	}
	// 按 ES 返回顺序重排（IN 查询不保序）
	byID := make(map[string]map[string]interface{}, len(rows))
	for _, r := range rows {
		byID[asString(r["id"])] = r
	}
	// 输出给前端：按 camelCase 组装（DB 原生查询得到的是 snake_case 列名，
	// 直接透出会违反「对外 JSON 一律 camelCase」的全局口径）。
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out.List = append(out.List, articleRowToAPI(r))
		}
	}
	s.recordSearch(req, int(out.Total))
	return out, nil
}

// recordSearch 旁路写入检索日志（不阻塞响应、失败静默）。
//
// 只记第 1 页：翻页会重复计数同一关键词，导致热门词被单次深度浏览刷高。
func (s *Searcher) recordSearch(req ArticleSearchRequest, resultCount int) {
	if s.logs == nil || req.Page > 1 {
		return
	}
	in := RecordInput{
		Keyword:     req.Keyword,
		UserID:      req.UserID,
		Scene:       model.SearchSceneArticle,
		IP:          req.IP,
		ResultCount: resultCount,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.logs.Record(ctx, in)
	}()
}

// articleRowToAPI 把 DB 原生查询行（snake_case 列）转成对外 camelCase 视图。
//
// 与 buildArticleDoc 的区别：不返回正文 content（列表页无需全文，避免响应膨胀），
// 只给列表展示需要的字段。
func articleRowToAPI(r map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":            asString(r["id"]),
		"title":         asString(r["title"]),
		"summary":       asString(r["summary"]),
		"cover":         asString(r["cover"]),
		"type":          asString(r["type"]),
		"status":        asString(r["status"]),
		"authorId":      asString(r["author_id"]),
		"categoryId":    asString(r["category_id"]),
		"views":         asInt(r["views"]),
		"likes":         asInt(r["likes"]),
		"favoriteCount": asInt(r["favorite_count"]),
		"commentCount":  asInt(r["comment_count"]),
		"createdAt":     asTime(r["created_at"]),
		"updatedAt":     asTime(r["updated_at"]),
	}
}

// buildQuery 构造 ES 查询 DSL（multi_match + 分类过滤 + 排序）。
func (s *Searcher) buildQuery(req ArticleSearchRequest) map[string]interface{} {
	must := []interface{}{
		map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  req.Keyword,
				"fields": []string{"title^3", "summary^2", "content", "authorName", "tags"},
				"type":   "best_fields",
			},
		},
	}
	filter := []interface{}{
		map[string]interface{}{"term": map[string]interface{}{"status": "PUBLISHED"}},
	}
	if req.CategoryID != "" {
		filter = append(filter, map[string]interface{}{
			"term": map[string]interface{}{"categoryId": req.CategoryID},
		})
	}
	q := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{"must": must, "filter": filter},
		},
	}
	switch req.SortBy {
	case "latest":
		q["sort"] = []interface{}{map[string]interface{}{"createdAt": map[string]interface{}{"order": "desc"}}}
	case "views":
		q["sort"] = []interface{}{map[string]interface{}{"views": map[string]interface{}{"order": "desc"}}}
	case "likes":
		q["sort"] = []interface{}{map[string]interface{}{"likes": map[string]interface{}{"order": "desc"}}}
	default:
		q["sort"] = []interface{}{"_score", map[string]interface{}{"createdAt": map[string]interface{}{"order": "desc"}}}
	}
	return q
}

// ---------- 行值转换（DB 原生查询返回 interface{}） ----------

func asString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v interface{}) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case []byte:
		var n int
		_, _ = fmt.Sscan(string(t), &n)
		return n
	default:
		var n int
		_, _ = fmt.Sscan(fmt.Sprint(t), &n)
		return n
	}
}

func asFloat(v interface{}) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case []byte:
		var f float64
		_, _ = fmt.Sscan(string(t), &f)
		return f
	default:
		var f float64
		_, _ = fmt.Sscan(fmt.Sprint(t), &f)
		return f
	}
}

func asBool(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case int:
		return t != 0
	case int64:
		return t != 0
	case []byte:
		s := strings.ToLower(string(t))
		return s == "t" || s == "true" || s == "1"
	case string:
		s := strings.ToLower(t)
		return s == "t" || s == "true" || s == "1"
	default:
		return false
	}
}

// asTime 时间值转 RFC3339（ES date 字段要求字符串或 epoch）。
func asTime(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return nil
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return asString(v)
	}
}

// 编译期守护：errors 在状态判定中使用。
var _ = errors.Is

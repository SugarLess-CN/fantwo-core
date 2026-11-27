// Package handler HTTP 处理层。
//
// 本文件为内容域依赖装配与公共解析工具（actor/分页/错误响应），
// 文章 / 评论 / 合集 / 标签 / 分类五个 handler 共用。
package handler

import (
	"strconv"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ContentHandlers 内容域五组 handler 的聚合体（由路由层一次性构造）。
type ContentHandlers struct {
	Article    *ArticleHandler
	Comment    *CommentHandler
	Tag        *TagHandler
	Category   *CategoryHandler
	Collection *CollectionHandler

	// db 保存装配期连接，供后续注入（搜索日志等）复用，不再从路由层透传。
	db *gorm.DB
}

// NewContentHandlers 装配内容域全部 handler。
//
// db 为 nil 时（无数据库降级启动）各 handler 仍可构造，但业务调用会返回 500，
// 由路由层决定是否注册（本实现仍注册，保证 /api/v1 路由表与文档一致）。
func NewContentHandlers(db *gorm.DB, log *zap.Logger) *ContentHandlers {
	if log == nil {
		log = zap.NewNop()
	}

	articleRepo := repository.NewArticleRepository(db)
	tagRepo := repository.NewTagRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	commentRepo := repository.NewCommentRepository(db)
	collectionRepo := repository.NewCollectionRepository(db)

	// 作者解析器 + 关注关系提供者由 user 域实现（同一个 UserAuthorResolver 实现两个接口）。
	authorResolver := service.NewUserAuthorResolver(service.NewDeps(db, nil, nil, nil, log))
	articleSvc := service.NewArticleService(articleRepo, tagRepo, categoryRepo, authorResolver, nil, log)
	// 关注流：/article?type=following 使用 followee 集合过滤作者。
	articleSvc.SetFollowProvider(authorResolver)
	commentSvc := service.NewCommentService(commentRepo, articleRepo, authorResolver, log)
	tagSvc := service.NewTagService(tagRepo, articleRepo, log)
	categorySvc := service.NewCategoryService(categoryRepo, log)
	collectionSvc := service.NewCollectionService(collectionRepo, articleRepo, log)

	// 注册内容域跨域事件订阅（report.handled 删内容 / 审核状态回写）。
	// DB 为 nil 时内部直接跳过；sync.Once 保证装配多次也只注册一次。
	service.RegisterContentEventSubscribers(service.ContentEventDeps{DB: db, Logger: log})

	return &ContentHandlers{
		Article:    &ArticleHandler{svc: articleSvc, log: log},
		Comment:    &CommentHandler{svc: commentSvc, log: log},
		Tag:        &TagHandler{svc: tagSvc, log: log},
		Category:   &CategoryHandler{svc: categorySvc, log: log},
		Collection: &CollectionHandler{svc: collectionSvc, log: log},
		db:         db,
	}
}

// BindSearcher 注入 ES 检索器（infra 域），使 /article/search **优先走 ES**。
//
// 不注入或 ES 未配置时，/article/search 走 DB（行为与引入本注入前一致）——
// 这保证了「装配遗漏」只会让搜索退化，不会让接口 5xx。
func (cs *ContentHandlers) BindSearcher(sr *infra.Searcher) {
	if cs == nil || cs.Article == nil || cs.Article.svc == nil {
		return
	}
	cs.Article.svc.SetSearcher(sr)
}

// BindSearchLogs 注入搜索日志服务（infra 域），使 /article/hot-search 走真实检索聚合。
// rdb 可为 nil（无 Redis 时服务内部退化为 DB 判重）。
func (cs *ContentHandlers) BindSearchLogs(rdb *redis.Client) {
	if cs == nil || cs.db == nil || cs.Article == nil || cs.Article.svc == nil {
		return
	}
	cs.Article.svc.SetSearchLogs(infra.NewSearchLogService(cs.db, rdb))
}

// BindAudit 注入审核提交器（装配层在 AuditService 就绪后调用）。
//
// 注入后：文章「请求发布」会落 PENDING 并提交审核、评论创建后立即提交审核；
// 审核结果经 article.status.changed / comment.status.changed 事件回写（content_events.go）。
func (cs *ContentHandlers) BindAudit(auditor *service.AuditService) {
	if cs == nil || auditor == nil {
		return
	}
	if cs.Article != nil && cs.Article.svc != nil {
		cs.Article.svc.SetAuditor(auditor)
	}
	if cs.Comment != nil && cs.Comment.svc != nil {
		cs.Comment.svc.SetAuditor(auditor)
	}
}

// actorOf 从 gin context 取当前操作者（未登录返回零值 Actor）。
func actorOf(c *gin.Context) service.Actor {
	raw, exists := c.Get(middleware.CtxUserKey)
	if !exists || raw == nil {
		return service.Actor{}
	}
	claims, ok := raw.(*jwtx.Claims)
	if !ok || claims == nil {
		return service.Actor{}
	}
	return service.Actor{
		UserID:      claims.UserID,
		Roles:       claims.Roles,
		Permissions: claims.Permissions,
	}
}

// pageOf 解析分页参数（page 默认 1、limit 默认 10、上限 100）。
func pageOf(c *gin.Context) (int, int) {
	page := 1
	limit := 10
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// boolQuery 解析布尔型 query（true/1/yes/on 为真）。
func boolQuery(c *gin.Context, key string) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query(key))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// ctOK 成功响应。
func ctOK(c *gin.Context, data interface{}) { response.Success(c, data) }

// ctPage 分页成功响应。
func ctPage(c *gin.Context, list interface{}, total int64, page, limit int) {
	response.SuccessPage(c, list, total, page, limit)
}

// ctFail 业务错误响应。
func ctFail(c *gin.Context, e *errcode.Err) {
	response.Fail(c, e)
}

// splitIDs 解析逗号分隔的 ID 列表（query 场景）。
func splitIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

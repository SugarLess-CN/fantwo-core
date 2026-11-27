package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AuthorResolver 作者信息解析器：由用户域实现并注入。
// 为 nil 时文章视图只输出 authorId，不做跨域聚合（保证本域可独立编译/测试）。
type AuthorResolver interface {
	ResolveAuthors(ctx context.Context, userIDs []string) (map[string]AuthorBrief, error)
}

// ArticleAuditor 文章审核提交器：由审核域 AuditService 实现并注入。
//
// 契约（internal/service/audit_service.go 的 SubmitArticleAudit）：
// 仅入队，不写 articles 表；审核结果经事件 article.status.changed 回写，
// 由本域 contentStatusSubscriber 落库。为 nil 时不排队，发布请求即时生效。
type ArticleAuditor interface {
	SubmitArticleAudit(articleID, content string, imageURLs []string) error
}

// resolvePublishStatus 解析「请求状态 → 落地状态 + 是否需审核」。
//
// 防的 bug：文章发布链路漏审 / 误审。
//   - 请求 PUBLISHED 且审核域已接入 → 落 PENDING 并入队（绝不允许绕过审核直接可见）；
//   - 请求 PUBLISHED 但未接入审核域 → 直接 PUBLISHED（降级启动不阻塞发帖）；
//   - 其余状态原样落地（DRAFT/PENDING/ARCHIVED 等由调用方决定）。
func resolvePublishStatus(requested string, hasAuditor bool) (status string, needAudit bool) {
	if requested == model.ArticleStatusPublished && hasAuditor {
		return model.ArticleStatusPending, true
	}
	return requested, false
}

// isValidArticleStatusTransition 文章状态迁移是否合法。
//
// 防的 bug：把已发布文章直接改回草稿造成「内容凭空消失」，
// 或把已删除文章「复活」绕过删除语义。
//
// 设计取舍（对齐原项目实际行为）：
//   - 作者可以主动下架自己的文章：PUBLISHED → ARCHIVED / DRAFT 允许；
//   - 审核驳回后可改稿重投：REJECTED → DRAFT / PENDING / PUBLISHED 允许；
//   - DELETED 是终态：只能保持，不可迁移（删除走软删列，不需要状态回退）；
//   - 同状态「改」视为合法（幂等，前端重复提交不报错）。
func isValidArticleStatusTransition(from, to string) bool {
	if from == to {
		return true
	}
	if from == model.ArticleStatusDeleted {
		return false // 终态，禁止复活
	}
	switch to {
	case model.ArticleStatusDraft, model.ArticleStatusPending,
		model.ArticleStatusPublished, model.ArticleStatusRejected,
		model.ArticleStatusArchived, model.ArticleStatusBanned:
		return true
	case model.ArticleStatusDeleted:
		return true // 允许显式置删除态（软删由 Delete 接口负责，此处手工处置）
	default:
		return false // 未知状态一律拒绝，避免脏值入库
	}
}

// ArticleService 文章业务逻辑（对齐原 NestJS ArticleService）。
type ArticleService struct {
	repo       *repository.ArticleRepository
	tags       *repository.TagRepository
	cats       *repository.CategoryRepository
	authors    AuthorResolver
	follow     FollowProvider
	audit      ArticleAuditor
	searchLogs *infra.SearchLogService
	log        *zap.Logger
}

// NewArticleService 构造文章服务。
// authors / audit 允许为 nil（对应跨域依赖尚未接入的中间态）。
func NewArticleService(
	repo *repository.ArticleRepository,
	tags *repository.TagRepository,
	cats *repository.CategoryRepository,
	authors AuthorResolver,
	audit ArticleAuditor,
	log *zap.Logger,
) *ArticleService {
	if log == nil {
		log = zap.NewNop()
	}
	return &ArticleService{repo: repo, tags: tags, cats: cats, authors: authors, audit: audit, log: log}
}

// SetAuthorResolver 运行期注入作者解析器（避免构造顺序耦合）。
func (s *ArticleService) SetAuthorResolver(r AuthorResolver) { s.authors = r }

// SetSearchLogs 运行期注入搜索日志服务（infra 域；/article/hot-search 真实聚合来源）。
// 未注入或聚合为空时回退到「热门标签 + 热门分类」合成，保证端点始终可用。
func (s *ArticleService) SetSearchLogs(l *infra.SearchLogService) { s.searchLogs = l }

// SetFollowProvider 运行期注入关注关系提供者（user 域 UserAuthorResolver 实现）。
// 未注入时 type=following 退化为最新排序，不返回错误。
func (s *ArticleService) SetFollowProvider(p FollowProvider) { s.follow = p }

// SetAuditor 运行期注入审核提交器（审核域装配后调用）。
func (s *ArticleService) SetAuditor(a ArticleAuditor) { s.audit = a }

// ErrArticleNotFound 文章不存在。
var ErrArticleNotFound = errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)

// getArticle 取文章，不存在返回 404。
func (s *ArticleService) getArticle(ctx context.Context, id string) (*model.Article, *errcode.Err) {
	a, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrArticleNotFound
		}
		s.log.Error("查询文章失败", zap.String("id", id), zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return a, nil
}

// ---------- 创建 / 更新 / 删除 ----------

// Create 创建文章。
//
// 业务要点：
//   - 默认草稿（DRAFT）；请求 PUBLISHED 时，若已接入审核管线则落 PENDING 并投递审核，
//     未接入则直接 PUBLISHED（对齐原 content-audit 联动）。
//   - 标签支持 tagIds 与 tagNames 二选一：tagNames 中不存在的标签自动创建（原实现语义）。
//   - 同步维护标签 article_count 与分类 article_count 冗余计数。
func (s *ArticleService) Create(ctx context.Context, actor Actor, req CreateArticleRequest) (*ArticleView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}

	articleType := req.Type
	if articleType == "" {
		articleType = model.ArticleTypeMixed
	}
	// 对齐原校验：type=mixed 时 content 必填；type=video 时 videoUrl 必填
	if articleType == model.ArticleTypeMixed && (req.Content == nil || strings.TrimSpace(*req.Content) == "") {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ArticleContentRequiredMixed)
	}
	if articleType == model.ArticleTypeVideo && (req.VideoURL == nil || strings.TrimSpace(*req.VideoURL) == "") {
		return nil, errcode.ErrBadRequest.WithKey(i18n.ArticleVideoURLRequired)
	}

	// 分类存在性校验（分类可为空，与原 DTO 的 categoryId 必填语义放宽为可选更安全）
	if req.CategoryID != nil && *req.CategoryID != "" {
		if _, err := s.cats.FindByID(ctx, *req.CategoryID); err != nil {
			return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryNotFound)
		}
	}

	tagIDs, e := s.resolveTagIDs(ctx, req.TagIDs, req.TagNames)
	if e != nil {
		return nil, e
	}

	authorID := actor.UserID
	a := &model.Article{
		Title:             strings.TrimSpace(req.Title),
		Content:           req.Content,
		Images:            req.Images.ToJSONText(),
		VideoURL:          req.VideoURL,
		Cover:             req.Cover,
		Summary:           req.Summary,
		Type:              articleType,
		CategoryID:        req.CategoryID,
		AuthorID:          &authorID,
		ActivityID:        req.ActivityID,
		RequireLogin:      boolVal(req.RequireLogin),
		RequireFollow:     boolVal(req.RequireFollow),
		RequirePayment:    boolVal(req.RequirePayment),
		RequireMembership: boolVal(req.RequireMembership),
		ListRequireLogin:  boolVal(req.ListRequireLogin),
		ViewPrice:         floatVal(req.ViewPrice),
		AllowReprint:      boolVal(req.AllowReprint),
		IsPinnedOnProfile: boolVal(req.IsPinnedOnProfile),
		Sort:              intVal(req.Sort),
	}
	if a.CategoryID != nil && *a.CategoryID == "" {
		a.CategoryID = nil
	}

	// 状态与审核
	status := req.Status
	if status == "" {
		status = model.ArticleStatusDraft
	}
	status, needAudit := resolvePublishStatus(status, s.audit != nil)
	a.Status = status

	// 设精仅管理员可写
	if boolVal(req.IsFeatured) {
		if !actor.IsManager() {
			return nil, errcode.ErrForbidden.WithKey(i18n.ArticleFeaturedAdminOnly)
		}
		a.IsFeatured = true
		now := time.Now()
		a.FeaturedAt = &now
	}
	if a.IsPinnedOnProfile {
		now := time.Now()
		a.PinnedAt = &now
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		tagRepo := s.tags.WithTx(tx)
		catRepo := s.cats.WithTx(tx)
		if err := repo.Create(ctx, a); err != nil {
			return err
		}
		if _, _, err := repo.ReplaceArticleTags(ctx, a.ID, tagIDs); err != nil {
			return err
		}
		if err := tagRepo.IncrCounters(ctx, tagIDs, "article_count", 1); err != nil {
			return err
		}
		if a.CategoryID != nil {
			if err := catRepo.IncrCounter(ctx, *a.CategoryID, "article_count", 1); err != nil {
				return err
			}
		}
		// 下载资源（对齐原 article.downloads 一对多）：与文章同事务写入。
		if len(req.Downloads) > 0 {
			if err := repo.ReplaceDownloads(ctx, a.ID, toDownloadModels(req.Downloads)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("创建文章失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	if needAudit {
		s.enqueueAudit(a)
	}
	// 事务已提交：发布创建事件（成就域消费，草稿不计）。
	if shouldPublishArticleCreated(a.Status) {
		publishArticleCreated(a)
	}
	view := s.BuildViews(ctx, []model.Article{*a}, actor, true)[0]
	return &view, nil
}

// Update 更新文章。
//
// 权限语义：handler 侧已用 article:update 权限点拦截（对齐原 PermissionGuard）；
// 此处额外校验「作者本人或管理员」，避免普通持权用户越权改他人文章。
func (s *ArticleService) Update(ctx context.Context, id string, actor Actor, req UpdateArticleRequest) (*ArticleView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	a, e := s.getArticle(ctx, id)
	if e != nil {
		return nil, e
	}
	if !s.canModify(a, actor) {
		return nil, errcode.ErrForbidden.WithKey(i18n.ArticleNoPermission)
	}

	fields := map[string]interface{}{}
	if req.Title != nil {
		fields["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Content != nil {
		fields["content"] = *req.Content
	}
	if req.Summary != nil {
		fields["summary"] = *req.Summary
	}
	if req.Images != nil {
		fields["images"] = req.Images.ToJSONText()
	}
	if req.VideoURL != nil {
		fields["video_url"] = *req.VideoURL
	}
	if req.Cover != nil {
		fields["cover"] = *req.Cover
	}
	if req.Sort != nil {
		fields["sort"] = *req.Sort
	}
	if req.Type != "" {
		fields["type"] = req.Type
	}
	if req.CategoryID != nil {
		if *req.CategoryID == "" {
			fields["category_id"] = nil
		} else {
			if _, err := s.cats.FindByID(ctx, *req.CategoryID); err != nil {
				return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryNotFound)
			}
			fields["category_id"] = *req.CategoryID
		}
	}
	if req.ActivityID != nil {
		fields["activity_id"] = *req.ActivityID
	}
	if req.RequireLogin != nil {
		fields["require_login"] = *req.RequireLogin
	}
	if req.RequireFollow != nil {
		fields["require_follow"] = *req.RequireFollow
	}
	if req.RequirePayment != nil {
		fields["require_payment"] = *req.RequirePayment
	}
	if req.RequireMembership != nil {
		fields["require_membership"] = *req.RequireMembership
	}
	if req.ListRequireLogin != nil {
		fields["list_require_login"] = *req.ListRequireLogin
	}
	if req.ViewPrice != nil {
		fields["view_price"] = *req.ViewPrice
	}
	if req.AllowReprint != nil {
		fields["allow_reprint"] = *req.AllowReprint
	}

	// 状态流转 + 审核联动
	needAudit := false
	if req.Status != "" && isValidArticleStatusTransition(a.Status, req.Status) {
		status, auditNeeded := resolvePublishStatus(req.Status, s.audit != nil)
		needAudit = auditNeeded
		fields["status"] = status
	}

	// 个人主页置顶：作者或管理员
	if req.IsPinnedOnProfile != nil {
		fields["is_pinned_on_profile"] = *req.IsPinnedOnProfile
		if *req.IsPinnedOnProfile {
			fields["pinned_at"] = time.Now()
		} else {
			fields["pinned_at"] = nil
		}
	}
	// 设精：仅管理员
	if req.IsFeatured != nil {
		if !actor.IsManager() {
			return nil, errcode.ErrForbidden.WithKey(i18n.ArticleFeaturedAdminOnly)
		}
		fields["is_featured"] = *req.IsFeatured
		if *req.IsFeatured {
			fields["featured_at"] = time.Now()
		} else {
			fields["featured_at"] = nil
		}
	}

	var tagIDs []string
	hasTagChange := req.TagIDs != nil || req.TagNames != nil
	if hasTagChange {
		var e2 *errcode.Err
		tagIDs, e2 = s.resolveTagIDs(ctx, req.TagIDs, req.TagNames)
		if e2 != nil {
			return nil, e2
		}
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		tagRepo := s.tags.WithTx(tx)
		catRepo := s.cats.WithTx(tx)

		if err := repo.UpdateFields(ctx, id, fields); err != nil {
			return err
		}

		// 分类计数迁移
		if newCat, ok := fields["category_id"]; ok {
			oldCat := a.CategoryID
			var newCatID *string
			if v, isStr := newCat.(string); isStr {
				newCatID = &v
			}
			if !samePtr(oldCat, newCatID) {
				if oldCat != nil {
					if err := catRepo.IncrCounter(ctx, *oldCat, "article_count", -1); err != nil {
						return err
					}
				}
				if newCatID != nil {
					if err := catRepo.IncrCounter(ctx, *newCatID, "article_count", 1); err != nil {
						return err
					}
				}
			}
		}

		// 标签计数迁移
		if hasTagChange {
			removed, added, err := repo.ReplaceArticleTags(ctx, id, tagIDs)
			if err != nil {
				return err
			}
			if err := tagRepo.IncrCounters(ctx, removed, "article_count", -1); err != nil {
				return err
			}
			if err := tagRepo.IncrCounters(ctx, added, "article_count", 1); err != nil {
				return err
			}
		}

		// 下载资源：传了才替换（nil = 不改，空数组 = 清空）。
		//
		// 这个语义很重要：PATCH 里「没传 downloads」和「传了空数组」是两件事，
		// 前者不动、后者清空。用 nil 切片区分（JSON 里缺字段 → nil）。
		if req.Downloads != nil {
			if err := repo.ReplaceDownloads(ctx, id, toDownloadModels(req.Downloads)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("更新文章失败", zap.String("id", id), zap.Error(err))
		return nil, errcode.ErrInternal
	}

	updated, e := s.getArticle(ctx, id)
	if e != nil {
		return nil, e
	}
	if needAudit {
		s.enqueueAudit(updated)
	}
	// 草稿/驳回稿「首次进入可发布链路」同样计入一次创作成就；
	// 已发布文章反复编辑不重复发布（避免刷 ARTICLE_10/50/100）。
	if !shouldPublishArticleCreated(a.Status) && shouldPublishArticleCreated(updated.Status) {
		publishArticleCreated(updated)
	}
	view := s.BuildViews(ctx, []model.Article{*updated}, actor, true)[0]
	return &view, nil
}

// Remove 删除文章（软删）并级联清理关联与计数。
//
// 级联范围：文章标签关联、评论（软删）、点赞/收藏/不喜欢/浏览历史（保留记录但文章不可见，
// 与原 onDelete CASCADE 语义相比更保守——原实现物理删除，这里保留审计痕迹）。
func (s *ArticleService) Remove(ctx context.Context, id string, actor Actor) (*dto.DeleteResultDto, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	a, e := s.getArticle(ctx, id)
	if e != nil {
		return nil, e
	}
	if !s.canModify(a, actor) {
		return nil, errcode.ErrForbidden.WithKey(i18n.ArticleDeleteNoPermission)
	}

	tagIDs, _ := s.repo.ArticleTagIDs(ctx, id)

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		tagRepo := s.tags.WithTx(tx)
		catRepo := s.cats.WithTx(tx)
		commentRepo := repository.NewCommentRepository(tx)

		if err := repo.SoftDelete(ctx, id); err != nil {
			return err
		}
		if err := repo.DeleteArticleTags(ctx, id); err != nil {
			return err
		}
		if err := tagRepo.IncrCounters(ctx, tagIDs, "article_count", -1); err != nil {
			return err
		}
		if a.CategoryID != nil {
			if err := catRepo.IncrCounter(ctx, *a.CategoryID, "article_count", -1); err != nil {
				return err
			}
		}
		// 作者文章数回退（users.article_count，跨域计数由内容域维护）
		if a.AuthorID != nil {
			if err := repository.NewUserRepo().IncrCounter(tx, *a.AuthorID, "article_count", -1); err != nil {
				return err
			}
		}
		return commentRepo.SoftDeleteByArticle(ctx, id)
	})
	if err != nil {
		s.log.Error("删除文章失败", zap.String("id", id), zap.Error(err))
		return nil, errcode.ErrInternal
	}

	return &dto.DeleteResultDto{ID: id, Deleted: true}, nil
}

// ---------- 查询 ----------

// ArticleListQuery 列表查询入参（handler 从 query string 解析本域原生字段后传入）。
type ArticleListQuery struct {
	Title      string
	Keyword    string
	CategoryID string
	TagID      string
	AuthorID   string
	Status     []string
	Type       string // all / popular / latest / following
	SortBy     string // 搜索场景：relevance / latest / views / likes
	Page       int
	Limit      int
}

// List 文章列表（对齐 GET /article）。
//
// 可见性规则：
//   - 未登录：仅 PUBLISHED 且 listRequireLogin=false；
//   - 登录用户：PUBLISHED 全量；
//   - 显式传 status：仅管理员生效（对齐原 @ApiQuery 说明）。
func (s *ArticleService) List(ctx context.Context, q ArticleListQuery, actor Actor) ([]ArticleView, int64, *errcode.Err) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}

	statuses := s.visibleStatuses(actor, q.Status)
	orderBy := "sort"
	var followeeIDs []string
	switch q.Type {
	case "popular":
		orderBy = "popular"
	case "latest":
		orderBy = "latest"
	case "following":
		// 关注流：取当前用户 followee 集合作者过滤（user 域 FollowProvider）。
		// 未登录 / 未注入提供者 / 未关注任何人时返回空列表，而非退化为全站流。
		orderBy = "latest"
		if actor.Anonymous() || s.follow == nil {
			return []ArticleView{}, 0, nil
		}
		ids, err := s.follow.FollowingIDs(ctx, actor.UserID)
		if err != nil {
			s.log.Error("查询关注列表失败", zap.String("userId", actor.UserID), zap.Error(err))
			return nil, 0, errcode.ErrInternal
		}
		if len(ids) == 0 {
			return []ArticleView{}, 0, nil
		}
		followeeIDs = ids
	}
	if q.SortBy != "" {
		switch q.SortBy {
		case "latest":
			orderBy = "latest"
		case "views":
			orderBy = "popular"
		case "likes":
			orderBy = "relevance"
		}
	}

	// 匿名列表：排除仅登录可见的文章
	rows, total, err := s.repo.List(ctx, repository.ArticleListQuery{
		Title:      q.Title,
		Keyword:    q.Keyword,
		CategoryID: q.CategoryID,
		TagID:      q.TagID,
		AuthorID:   q.AuthorID,
		AuthorIDs:  followeeIDs,
		Status:     statuses,
		OrderBy:    orderBy,
		Page:       q.Page,
		Limit:      q.Limit,
	})
	if err != nil {
		s.log.Error("查询文章列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	if actor.Anonymous() {
		rows = filterAnonymousVisible(rows)
	}
	return s.BuildViews(ctx, rows, actor, false), total, nil
}

// FindByAuthor 按作者取文章（作者本人可见自己全部状态，他人仅已发布）。
func (s *ArticleService) FindByAuthor(ctx context.Context, authorID string, q ArticleListQuery, actor Actor) ([]ArticleView, int64, *errcode.Err) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}

	var statuses []string
	if actor.UserID == authorID {
		// 本人主页：全部状态
		statuses = nil
	} else {
		statuses = []string{model.ArticleStatusPublished}
	}

	orderBy := "latest"
	switch q.Type {
	case "popular":
		orderBy = "popular"
	}

	rows, total, err := s.repo.List(ctx, repository.ArticleListQuery{
		AuthorID:   authorID,
		Keyword:    q.Keyword,
		CategoryID: q.CategoryID,
		Status:     statuses,
		OrderBy:    orderBy,
		Page:       q.Page,
		Limit:      q.Limit,
	})
	if err != nil {
		s.log.Error("按作者查询文章失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.BuildViews(ctx, rows, actor, false), total, nil
}

// FindDrafts 当前用户草稿箱（对齐 GET /article/drafts）。
func (s *ArticleService) FindDrafts(ctx context.Context, actor Actor, page, limit int) ([]ArticleView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	rows, total, err := s.repo.List(ctx, repository.ArticleListQuery{
		AuthorID: actor.UserID,
		Status:   []string{model.ArticleStatusDraft},
		OrderBy:  "latest",
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		s.log.Error("查询草稿失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.BuildViews(ctx, rows, actor, false), total, nil
}

// FindOne 文章详情。
//
// 可见性：非发布状态仅作者本人与管理员可读（对齐原 findOne 的权限分支）。
// 阅读量在正文返回时 +1（原实现同样在详情读时累加）。
func (s *ArticleService) FindOne(ctx context.Context, id string, actor Actor) (*ArticleView, *errcode.Err) {
	a, e := s.getArticle(ctx, id)
	if e != nil {
		return nil, e
	}
	isOwner := a.AuthorID != nil && *a.AuthorID == actor.UserID
	if a.Status != model.ArticleStatusPublished && !isOwner && !actor.IsManager() {
		return nil, errcode.ErrForbidden.WithKey(i18n.ArticleNotPublished)
	}
	// 阅读量自增（失败不影响主流程）
	if err := s.repo.IncrCounter(ctx, id, "views", 1); err != nil {
		s.log.Warn("文章阅读量自增失败", zap.String("id", id), zap.Error(err))
	} else {
		a.Views++
	}
	view := s.BuildViews(ctx, []model.Article{*a}, actor, true)[0]
	return &view, nil
}

// FindRecommendations 相关文章推荐（对齐 GET /article/recommend/:id）。
//
// 策略：同分类优先 → 有共同标签 → 排除自身与用户「不喜欢」的文章；
// 数量不足时以热门文章补齐。
func (s *ArticleService) FindRecommendations(ctx context.Context, id string, actor Actor) ([]ArticleView, *errcode.Err) {
	a, e := s.getArticle(ctx, id)
	if e != nil {
		return nil, e
	}

	exclude := []string{id}
	if !actor.Anonymous() {
		if disliked, err := s.repo.AllDislikedArticleIDs(ctx, actor.UserID); err == nil {
			exclude = append(exclude, disliked...)
		}
	}

	pageSize := 10
	var picked []model.Article
	if a.CategoryID != nil {
		rows, _, err := s.repo.List(ctx, repository.ArticleListQuery{
			CategoryID: *a.CategoryID,
			Status:     []string{model.ArticleStatusPublished},
			ExcludeIDs: exclude,
			OrderBy:    "popular",
			Page:       1,
			Limit:      pageSize,
		})
		if err != nil {
			s.log.Warn("相关文章推荐查询失败", zap.Error(err))
		}
		picked = rows
	}

	// 共同标签补齐
	if len(picked) < pageSize {
		if tagIDs, err := s.repo.ArticleTagIDs(ctx, id); err == nil && len(tagIDs) > 0 {
			seen := map[string]struct{}{}
			for _, p := range picked {
				seen[p.ID] = struct{}{}
			}
			for _, tagID := range tagIDs {
				if len(picked) >= pageSize {
					break
				}
				rows, _, err := s.repo.List(ctx, repository.ArticleListQuery{
					TagID:      tagID,
					Status:     []string{model.ArticleStatusPublished},
					ExcludeIDs: exclude,
					OrderBy:    "popular",
					Page:       1,
					Limit:      pageSize,
				})
				if err != nil {
					continue
				}
				for _, r := range rows {
					if _, ok := seen[r.ID]; ok {
						continue
					}
					seen[r.ID] = struct{}{}
					picked = append(picked, r)
					if len(picked) >= pageSize {
						break
					}
				}
			}
		}
	}

	return s.BuildViews(ctx, picked, actor, false), nil
}

// HotSearch 热门搜索词（GET /article/hot-search）。
//
// 数据源优先级：
//  1. infra 域 search_logs 真实聚合（TopKeywordsByScene，近 7 天、过滤零结果词）；
//  2. 回退：热门标签 + 热门分类合成（infra 未注入或日志为空时，保证端点不空返回）。
func (s *ArticleService) HotSearch(ctx context.Context, limit int, keyword string) ([]dto.HotSearchItemDto, *errcode.Err) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// 优先：infra 域真实检索日志聚合（近 7 天，只统计至少命中 1 条结果的关键词）
	if s.searchLogs != nil {
		rows, err := s.searchLogs.TopKeywordsByScene(ctx, "article", 7, limit, 1)
		if err != nil {
			s.log.Warn("热搜聚合查询失败，回退标签/分类合成", zap.Error(err))
		} else if len(rows) > 0 {
			out := make([]dto.HotSearchItemDto, 0, len(rows))
			for _, r := range rows {
				if keyword != "" && !strings.Contains(strings.ToLower(r.Keyword), strings.ToLower(keyword)) {
					continue
				}
				out = append(out, dto.HotSearchItemDto{Keyword: r.Keyword, Type: "search", Count: r.Count})
				if len(out) >= limit {
					break
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
	}

	items := make([]dto.HotSearchItemDto, 0, limit)

	hotTags, err := s.tags.Hot(ctx, limit)
	if err != nil {
		s.log.Warn("热门标签查询失败", zap.Error(err))
	}
	for _, t := range hotTags {
		if keyword != "" && !strings.Contains(strings.ToLower(t.Name), strings.ToLower(keyword)) {
			continue
		}
		items = append(items, dto.HotSearchItemDto{Keyword: t.Name, Type: "tag", Count: int64(t.ArticleCount)})
		if len(items) >= limit {
			return items, nil
		}
	}

	// 热门分类补齐
	cats, _, _ := s.cats.List(ctx, repository.CategoryListQuery{SortBy: "articleCount", SortOrd: "DESC", Page: 1, Limit: limit})
	for _, c := range cats {
		if len(items) >= limit {
			break
		}
		if keyword != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(keyword)) {
			continue
		}
		if c.ArticleCount == 0 {
			continue
		}
		items = append(items, dto.HotSearchItemDto{Keyword: c.Name, Type: "category", Count: int64(c.ArticleCount)})
	}
	return items, nil
}

// PublishedArticleIDs 已发布文章 ID 列表（对齐 GET /article/published/ids）。
func (s *ArticleService) PublishedArticleIDs(ctx context.Context) ([]string, *errcode.Err) {
	ids, err := s.repo.PublishedArticleIDs(ctx)
	if err != nil {
		s.log.Error("查询已发布文章 ID 失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// ---------- 内部工具 ----------

// visibleStatuses 计算列表可见状态白名单。
func (s *ArticleService) visibleStatuses(actor Actor, requested []string) []string {
	if len(requested) > 0 && actor.IsManager() {
		return requested
	}
	return []string{model.ArticleStatusPublished}
}

// canModify 是否可修改/删除该文章：作者本人或管理员。
func (s *ArticleService) canModify(a *model.Article, actor Actor) bool {
	if actor.IsManager() {
		return true
	}
	return a.AuthorID != nil && *a.AuthorID == actor.UserID
}

// resolveTagIDs 解析标签：tagIds 优先；tagNames 中不存在的自动创建。
func (s *ArticleService) resolveTagIDs(ctx context.Context, tagIDs, tagNames []string) ([]string, *errcode.Err) {
	if len(tagIDs) > 0 {
		// 校验存在性，忽略无效 ID
		rows, err := s.tags.FindByIDs(ctx, tagIDs)
		if err != nil {
			s.log.Error("查询标签失败", zap.Error(err))
			return nil, errcode.ErrInternal
		}
		out := make([]string, 0, len(rows))
		for _, t := range rows {
			out = append(out, t.ID)
		}
		return out, nil
	}
	if len(tagNames) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(tagNames))
	for _, name := range tagNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		existing, err := s.tags.FindByName(ctx, name)
		if err == nil {
			out = append(out, existing.ID)
			continue
		}
		t := &model.Tag{Name: name}
		if err := s.tags.Create(ctx, t); err != nil {
			s.log.Error("自动创建标签失败", zap.String("name", name), zap.Error(err))
			return nil, errcode.ErrInternal
		}
		out = append(out, t.ID)
	}
	return out, nil
}

// enqueueAudit 提交文章审核（未接入审核域时静默跳过）。
//
// 审核域只入队、不写库；状态回写由 article.status.changed 事件驱动。
func (s *ArticleService) enqueueAudit(a *model.Article) {
	if s.audit == nil || a == nil {
		return
	}
	content := ""
	if a.Content != nil {
		content = *a.Content
	}
	images := ParseImagesList(a.Images)
	if err := s.audit.SubmitArticleAudit(a.ID, content, images); err != nil {
		s.log.Warn("文章审核提交失败", zap.String("id", a.ID), zap.Error(err))
	}
}

// filterAnonymousVisible 匿名可见性过滤：剔除「仅登录后列表可见」的文章。
func filterAnonymousVisible(rows []model.Article) []model.Article {
	out := make([]model.Article, 0, len(rows))
	for _, r := range rows {
		if r.ListRequireLogin {
			continue
		}
		out = append(out, r)
	}
	return out
}

func boolVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func intVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func floatVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func samePtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// toDownloadModels 请求里的下载资源 → 表记录（对齐原 DownloadDto → Download）。
//
// 空字符串按「未填写」处理：原版 password / extractionCode 都是 nullable，
// 传空串等于不填，落库统一写 NULL（数据库要区分 NULL 与空串）。
func toDownloadModels(in []ArticleDownloadRequest) []model.Download {
	out := make([]model.Download, 0, len(in))
	for _, d := range in {
		m := model.Download{
			Type:                     d.Type,
			URL:                      d.URL,
			VisibleWithoutPermission: d.VisibleWithoutPermission,
		}
		if d.Password != "" {
			v := d.Password
			m.Password = &v
		}
		if d.ExtractionCode != "" {
			v := d.ExtractionCode
			m.ExtractionCode = &v
		}
		out = append(out, m)
	}
	return out
}

// fromDownloadModels 表记录 → 对外 DTO（空值按 docs/21：字符串 ""、数组 []）。
func fromDownloadModels(in []model.Download) []ArticleDownloadRequest {
	out := make([]ArticleDownloadRequest, 0, len(in))
	for _, d := range in {
		out = append(out, ArticleDownloadRequest{
			Type:                     d.Type,
			URL:                      d.URL,
			Password:                 strOrEmpty(d.Password),
			ExtractionCode:           strOrEmpty(d.ExtractionCode),
			VisibleWithoutPermission: d.VisibleWithoutPermission,
		})
	}
	return out
}

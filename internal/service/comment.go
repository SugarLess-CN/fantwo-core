package service

import (
	"context"
	"errors"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ---------- 评论请求 DTO ----------

// CreateCommentRequest 创建评论。
type CreateCommentRequest struct {
	Content   string     `json:"content" binding:"required"`
	ArticleID string     `json:"article_id" binding:"required"`
	ParentID  *string    `json:"parent_id"`
	Images    StringList `json:"images"`
}

// UpdateCommentRequest 更新评论。
type UpdateCommentRequest struct {
	Content  *string    `json:"content"`
	Images   StringList `json:"images"`
	IsPinned *bool      `json:"is_pinned"` // 仅文章作者或管理员可设置
}

// SetCommentPinRequest 置顶评论。
type SetCommentPinRequest struct {
	IsPinned *bool `json:"is_pinned" binding:"required"`
}

// CommentListQuery 评论列表查询参数。
type CommentListQuery struct {
	Page       int
	Limit      int
	SortBy     string // latest / oldest / hot
	OnlyAuthor bool   // 只看帖主
	Keyword    string // 管理端搜索
	UserID     string // 管理端按用户过滤
}

// CommentView 评论视图（dto.CommentDto 的别名；对外契约见 internal/dto/comment.go）。
//
// 字段空值口径由 dto 保证（docs/21）：content/images 空即 ""/[]，
// parentId/rootId 一级评论为空串（不是 null），author 缺失时整个字段不传。
type CommentView = dto.CommentDto

// CommentAuditor 评论审核提交器：由审核域 AuditService 实现并注入。
//
// 契约（audit_service.go 的 SubmitCommentAudit）：仅入队，不写 comments 表；
// 审核结果经事件 comment.status.changed 回写，由本域订阅者落库。
type CommentAuditor interface {
	SubmitCommentAudit(commentID, content string) error
}

// resolveCommentRootID 计算回复所属的楼层根评论 ID。
//
// 防的 bug：多级回复挂错楼层根——回复「回复」时必须继承父评论已有的 rootID，
// 否则二级回复会被当成新的楼层根，评论树出现「孤儿子树」。
// 规则：父评论本身就是楼层根（RootID 为空）→ 父 ID 即根；否则继承父的 RootID。
func resolveCommentRootID(parent *model.Comment) *string {
	if parent == nil {
		return nil
	}
	if parent.RootID != nil && *parent.RootID != "" {
		root := *parent.RootID
		return &root
	}
	root := parent.ID
	return &root
}

// nextCommentFloor 计算下一个楼层号（一级评论专用）。
//
// 防的 bug：楼层从 0 开始 / 跳号 / 并发下重复。
// 现有最大楼层 +1，空表时为 1（0 保留给「非一级评论」）。
func nextCommentFloor(maxFloor int) int {
	if maxFloor < 0 {
		maxFloor = 0
	}
	return maxFloor + 1
}

// CommentService 评论业务逻辑（对齐原 NestJS CommentService）。
type CommentService struct {
	repo *repository.CommentRepository
	// arts 文章仓储（评论内嵌 article 精简视图的批量来源；为 nil 时降级为不内嵌）。
	arts *repository.ArticleRepository
	// cats 分类仓储（内嵌文章的 category 来源）。
	cats    *repository.CategoryRepository
	log     *zap.Logger
	owner   ArticleOwnerChecker
	authors AuthorResolver
	auditor CommentAuditor
}

// ArticleOwnerChecker 文章归属校验器（评论置顶/管理需知道文章作者）。
type ArticleOwnerChecker interface {
	ArticleAuthorID(ctx context.Context, articleID string) (string, bool)
}

// articleOwnerChecker 默认实现。
type articleOwnerChecker struct{ repo *repository.ArticleRepository }

func (c articleOwnerChecker) ArticleAuthorID(ctx context.Context, articleID string) (string, bool) {
	a, err := c.repo.FindByID(ctx, articleID)
	if err != nil || a.AuthorID == nil {
		return "", false
	}
	return *a.AuthorID, true
}

// NewCommentService 构造评论服务。
// authors 允许为 nil（用户域未接入时评论视图只输出 authorId）。
func NewCommentService(
	repo *repository.CommentRepository,
	articleRepo *repository.ArticleRepository,
	authors AuthorResolver,
	log *zap.Logger,
) *CommentService {
	if log == nil {
		log = zap.NewNop()
	}
	svc := &CommentService{
		repo:    repo,
		arts:    articleRepo,
		log:     log,
		owner:   articleOwnerChecker{repo: articleRepo},
		authors: authors,
	}
	if articleRepo != nil {
		svc.owner = articleOwnerChecker{repo: articleRepo}
		// 分类仓储从文章仓储的连接派生，避免改动 NewCommentService 的调用方（路由装配）
		svc.cats = repository.NewCategoryRepository(articleRepo.DB())
	}
	return svc
}

// SetAuthorResolver 运行期注入作者解析器。
func (s *CommentService) SetAuthorResolver(r AuthorResolver) { s.authors = r }

// SetAuditor 运行期注入审核提交器（审核域装配后调用）。
func (s *CommentService) SetAuditor(a CommentAuditor) { s.auditor = a }

// submitAudit 提交评论审核（未接入审核域时静默跳过）。
func (s *CommentService) submitAudit(c *model.Comment) {
	if s.auditor == nil || c == nil {
		return
	}
	if err := s.auditor.SubmitCommentAudit(c.ID, c.Content); err != nil {
		s.log.Warn("评论审核提交失败", zap.String("id", c.ID), zap.Error(err))
	}
}

// ErrCommentNotFound 评论不存在。
var ErrCommentNotFound = errcode.ErrNotFound.WithKey(i18n.CommentNotFound)

// getComment 取评论。
func (s *CommentService) getComment(ctx context.Context, id string) (*model.Comment, *errcode.Err) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCommentNotFound
		}
		s.log.Error("查询评论失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return c, nil
}

// Create 创建评论（对齐 POST /comment）。
//
// 树形规则：带 parentId 时，父评论必须存在且未被删除；
// RootID 取父评论的 RootID（父即根时取父 ID），保证二级及以上都归拢到同一楼层根。
// 回复时父评论 reply_count +1；文章 comment_count +1（仅一级评论计数，楼层语义）。
// 楼层号：一级评论按文章内 floor 最大值 +1。
func (s *CommentService) Create(ctx context.Context, actor Actor, req CreateCommentRequest) (*CommentView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}

	c := &model.Comment{
		Content:   req.Content,
		Images:    req.Images.ToJSONText(),
		Status:    model.CommentStatusPublished,
		AuthorID:  actor.UserID,
		ArticleID: req.ArticleID,
	}

	// 文章存在性校验：对齐原仓库 comment.service.ts:570-572（article 不存在直接报错，不落脏数据）。
	_, artErr := repository.NewArticleRepository(s.repo.DB()).FindByID(ctx, req.ArticleID)
	if artErr != nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)
	}

	if req.ParentID != nil && *req.ParentID != "" {
		parent, e := s.getComment(ctx, *req.ParentID)
		if e != nil {
			return nil, e
		}
		c.ParentID = req.ParentID
		c.RootID = resolveCommentRootID(parent)
	} else {
		// 一级评论：分配楼层号
		max, err := s.repo.MaxFloor(ctx, req.ArticleID)
		if err != nil {
			s.log.Error("查询楼层失败", zap.Error(err))
			return nil, errcode.ErrInternal
		}
		c.Floor = nextCommentFloor(max)
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		articleRepo := repository.NewArticleRepository(tx)
		if err := repo.Create(ctx, c); err != nil {
			return err
		}
		// 文章评论数（仅一级评论计入 comment_count，对齐楼层口径）
		if c.RootID == nil {
			if err := articleRepo.IncrCounter(ctx, req.ArticleID, "comment_count", 1); err != nil {
				return err
			}
		} else if c.ParentID != nil {
			if err := repo.IncrCounter(ctx, *c.ParentID, "reply_count", 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("创建评论失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	// 入队审核（异步结果经 comment.status.changed 事件回写，不影响本次返回）
	s.submitAudit(c)
	// 事务已提交：发布创建事件（成就域消费）。
	publishCommentCreated(c)
	return s.BuildView(ctx, *c, actor), nil
}

// Update 更新评论（作者本人或管理员；内容/图片）。
func (s *CommentService) Update(ctx context.Context, id string, actor Actor, req UpdateCommentRequest) (*CommentView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	c, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	if c.AuthorID != actor.UserID && !actor.IsManager() {
		return nil, errcode.ErrForbidden.WithKey(i18n.CommentNoPermission)
	}
	fields := map[string]interface{}{}
	if req.Content != nil {
		fields["content"] = *req.Content
	}
	if req.Images != nil {
		fields["images"] = req.Images.ToJSONText()
	}
	if err := s.repo.UpdateFields(ctx, id, fields); err != nil {
		s.log.Error("更新评论失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	updated, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	return s.BuildView(ctx, *updated, actor), nil
}

// SetPin 设置评论置顶（对齐 PATCH /comment/:id/pin）。
// 仅文章作者或管理员；同一文章同时只保留一个置顶评论。
func (s *CommentService) SetPin(ctx context.Context, id string, actor Actor, isPinned bool) (*dto.CommentPinResultDto, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	c, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	authorID, ok := s.owner.ArticleAuthorID(ctx, c.ArticleID)
	if !ok || (authorID != actor.UserID && !actor.IsManager()) {
		return nil, errcode.ErrForbidden.WithKey(i18n.CommentPinAuthorOrAdmin)
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if isPinned {
			if err := repo.ClearPinned(ctx, c.ArticleID); err != nil {
				return err
			}
		}
		fields := map[string]interface{}{"is_pinned": isPinned}
		if isPinned {
			now := nowPtr()
			fields["pinned_at"] = now
		} else {
			fields["pinned_at"] = nil
		}
		return repo.UpdateFields(ctx, id, fields)
	})
	if err != nil {
		s.log.Error("设置评论置顶失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return &dto.CommentPinResultDto{ID: id, IsPinned: isPinned}, nil
}

// Remove 删除评论（作者本人或管理员；软删）。
// 一级评论删除时同步回扣文章 comment_count；回复删除回扣父评论 reply_count。
func (s *CommentService) Remove(ctx context.Context, id string, actor Actor) (*dto.DeleteResultDto, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	c, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	if c.AuthorID != actor.UserID && !actor.IsManager() {
		return nil, errcode.ErrForbidden.WithKey(i18n.CommentNoPermission)
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		articleRepo := repository.NewArticleRepository(tx)
		if err := repo.SoftDelete(ctx, id); err != nil {
			return err
		}
		if c.RootID == nil {
			if err := articleRepo.IncrCounter(ctx, c.ArticleID, "comment_count", -1); err != nil {
				return err
			}
		} else if c.ParentID != nil {
			if err := repo.IncrCounter(ctx, *c.ParentID, "reply_count", -1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("删除评论失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return &dto.DeleteResultDto{ID: id, Deleted: true}, nil
}

// Like 点赞评论（对齐 POST /comment/:id/like；切换语义：已赞则取消）。
func (s *CommentService) Like(ctx context.Context, id string, actor Actor) (*dto.CommentLikeResultDto, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getComment(ctx, id); e != nil {
		return nil, e
	}

	liked := true
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		_, err := repo.FindLike(ctx, id, actor.UserID)
		switch {
		case err == nil:
			liked = false
			if err := repo.DeleteLike(ctx, id, actor.UserID); err != nil {
				return err
			}
			return repo.IncrCounter(ctx, id, "likes", -1)
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := repo.CreateLike(ctx, &model.CommentLike{CommentID: id, UserID: actor.UserID}); err != nil {
				return err
			}
			return repo.IncrCounter(ctx, id, "likes", 1)
		default:
			return err
		}
	})
	if err != nil {
		s.log.Error("评论点赞失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return &dto.CommentLikeResultDto{IsLiked: liked}, nil
}

// FindByArticle 文章评论列表（对齐 GET /comment/article/:id）。
//
// 结构：一级评论分页（置顶优先），每条一级评论附带其回复（一次批量查询组树）。
func (s *CommentService) FindByArticle(ctx context.Context, articleID string, q CommentListQuery, actor Actor) ([]CommentView, int64, *errcode.Err) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.SortBy == "" {
		q.SortBy = "latest"
	}

	// 仅看帖主：取文章作者 ID 作为过滤条件
	onlyAuthorID := ""
	if q.OnlyAuthor {
		if authorID, ok := s.owner.ArticleAuthorID(ctx, articleID); ok {
			onlyAuthorID = authorID
		}
	}

	roots, total, err := s.repo.List(ctx, repository.CommentListQuery{
		ArticleID:  articleID,
		RootOnly:   true,
		Status:     []string{model.CommentStatusPublished},
		OnlyAuthor: q.OnlyAuthor,
		AuthorID:   onlyAuthorID,
		OrderBy:    q.SortBy,
		Page:       q.Page,
		Limit:      q.Limit,
	})
	if err != nil {
		s.log.Error("查询文章评论失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.buildTree(ctx, roots, actor), total, nil
}

// FindCommentDetail 评论详情（对齐 GET /comment/:id）。
//
// replies 分页（D7）：page/limit 作用于**回复列表**（评论本身不分页）。
// 楼层根取 `root_id 或自身 id`，对齐原 JS findCommentDetail 的
// `const rootId = comment.rootId || comment.id` —— 点开一条回复看到的是它所在楼层的回复。
// 这样大评论区首屏只取一页回复，不必一次拉全楼。
func (s *CommentService) FindCommentDetail(ctx context.Context, id string, actor Actor, page, limit int) (*CommentView, *errcode.Err) {
	c, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	rootID := c.ID
	if c.RootID != nil && *c.RootID != "" {
		rootID = *c.RootID
	}
	page, limit = normPage(page, limit)
	rows, _, err := s.repo.ListRepliesPaged(ctx, rootID, page, limit)
	if err != nil {
		s.log.Error("查询评论回复失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	// 本体 + 回复一次预取（作者 / 点赞 / 文章 / 父评论），组装阶段不再触库
	all := make([]model.Comment, 0, len(rows)+1)
	all = append(all, *c)
	all = append(all, rows...)
	rc := s.buildRenderCtx(ctx, actor, all)

	v := renderComment(*c, rc)
	if len(rows) > 0 {
		v.Replies = make([]CommentView, 0, len(rows))
		for _, r := range rows {
			v.Replies = append(v.Replies, renderComment(r, rc))
		}
	}
	return &v, nil
}

// FindAllComments 全部评论（管理端，对齐 GET /comment）。
func (s *CommentService) FindAllComments(ctx context.Context, actor Actor, q CommentListQuery) ([]CommentView, int64, *errcode.Err) {
	if !actor.IsManager() {
		return nil, 0, errcode.ErrForbidden
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	rows, total, err := s.repo.List(ctx, repository.CommentListQuery{
		Keyword: q.Keyword,
		UserID:  q.UserID,
		Page:    q.Page,
		Limit:   q.Limit,
		OrderBy: "latest",
	})
	if err != nil {
		s.log.Error("查询全部评论失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	// 批量组装（原来逐条 BuildView 会 N+1；顺带把内嵌 article 一并预取）
	rc := s.buildRenderCtx(ctx, actor, rows)
	out := make([]CommentView, 0, len(rows))
	for _, c := range rows {
		out = append(out, renderComment(c, rc))
	}
	return out, total, nil
}

// UserComments 用户评论列表（对齐 GET /comment/user/:userId）。
func (s *CommentService) UserComments(ctx context.Context, userID string, actor Actor, page, limit int) ([]CommentView, int64, *errcode.Err) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	rows, total, err := s.repo.List(ctx, repository.CommentListQuery{
		UserID:  userID,
		Status:  []string{model.CommentStatusPublished},
		OrderBy: "latest",
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		s.log.Error("查询用户评论失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	// 批量组装（含内嵌 article：前端「我的评论」卡片要显示文章封面/标题/分类）
	rc := s.buildRenderCtx(ctx, actor, rows)
	out := make([]CommentView, 0, len(rows))
	for _, c := range rows {
		out = append(out, renderComment(c, rc))
	}
	return out, total, nil
}

// CommentCount 文章评论数（对齐 GET /comment/article/:id/count）。
func (s *CommentService) CommentCount(ctx context.Context, articleID string) (*dto.CountDto, *errcode.Err) {
	n, err := s.repo.CountByArticle(ctx, articleID)
	if err != nil {
		s.log.Error("统计评论数失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return &dto.CountDto{Count: n}, nil
}

// ---------- 视图组装 ----------

// commentRenderCtx 一次请求内的批量预取结果（文章 / 父评论 / 作者 / 点赞态）。
//
// 为什么要有它：评论列表要内嵌 article 与 parent。若照「每条评论查一次文章、
// 再查一次父评论」写，20 条评论就是 40+ 次查询（N+1）。这里统一改成
// 「收集 ID 集合 → 每类一条 IN 查询」，与 article_view.go 的预取同思路：
// **查询次数固定 5 次，与评论条数无关**。
type commentRenderCtx struct {
	actor   Actor
	author  map[string]AuthorBrief
	liked   map[string]bool
	article map[string]*dto.CommentArticleBriefDto
	parent  map[string]*dto.CommentParentBriefDto
}

// buildRenderCtx 批量预取（固定 5 次查询）：
//
//  1. 父评论        WHERE id IN (parentIds)
//  2. 文章精简      WHERE id IN (articleIds)
//  3. 分类          WHERE id IN (categoryIds)   —— 仅当文章带分类
//  4. 作者          WHERE id IN (评论作者 ∪ 父评论作者)
//  5. 点赞态        WHERE user_id=? AND comment_id IN (commentIds)
func (s *CommentService) buildRenderCtx(ctx context.Context, actor Actor, cmts []model.Comment) *commentRenderCtx {
	rc := &commentRenderCtx{
		actor:   actor,
		author:  map[string]AuthorBrief{},
		liked:   map[string]bool{},
		article: map[string]*dto.CommentArticleBriefDto{},
		parent:  map[string]*dto.CommentParentBriefDto{},
	}
	if len(cmts) == 0 {
		return rc
	}

	commentIDs := make([]string, 0, len(cmts))
	parentIDs := make([]string, 0, len(cmts))
	articleIDs := make([]string, 0, len(cmts))
	authorIDs := map[string]bool{}
	for i := range cmts {
		c := &cmts[i]
		commentIDs = append(commentIDs, c.ID)
		articleIDs = append(articleIDs, c.ArticleID)
		authorIDs[c.AuthorID] = true
		if c.ParentID != nil && *c.ParentID != "" {
			parentIDs = append(parentIDs, *c.ParentID)
		}
	}

	// 1) 父评论：一次 IN 查询
	parents, err := s.repo.FindBriefByIDs(ctx, parentIDs)
	if err != nil {
		s.log.Warn("批量查询父评论失败", zap.Error(err))
	}
	for i := range parents {
		authorIDs[parents[i].AuthorID] = true // 父评论作者也要昵称（「回复 @某某」）
	}

	// 2) + 3) 文章精简 + 分类：各一次 IN 查询
	if s.arts != nil {
		arts, err2 := s.arts.FindBriefByIDs(ctx, articleIDs)
		if err2 != nil {
			s.log.Warn("批量查询评论内嵌文章失败", zap.Error(err2))
		} else {
			catIDs := make([]string, 0, len(arts))
			for _, a := range arts {
				if a.CategoryID != nil && *a.CategoryID != "" {
					catIDs = append(catIDs, *a.CategoryID)
				}
			}
			cats := s.loadCategoryBriefs(ctx, catIDs)
			for id, a := range arts {
				brief := &dto.CommentArticleBriefDto{
					ID:       id,
					Title:    a.Title,
					Cover:    strOrEmpty(a.Cover),
					Summary:  strOrEmpty(a.Summary),
					AuthorID: strOrEmpty(a.AuthorID),
				}
				if a.CategoryID != nil {
					if cv, ok := cats[*a.CategoryID]; ok {
						c := cv
						brief.Category = &c
					}
				}
				rc.article[id] = brief
			}
		}
	}

	// 4) 作者：一次批量解析
	rc.author = s.resolveAuthorMap(ctx, authorIDs)

	// 父评论组装（此时作者昵称已就绪）
	for i := range parents {
		p := &parents[i]
		brief := &dto.CommentParentBriefDto{
			ID:        p.ID,
			Content:   p.Content,
			AuthorID:  p.AuthorID,
			CreatedAt: p.CreatedAt,
		}
		if a, ok := rc.author[p.AuthorID]; ok {
			ab := authorBriefDto(a, nil)
			brief.Author = &ab
		}
		rc.parent[p.ID] = brief
	}

	// 5) 点赞态：一次 IN 查询
	if !actor.Anonymous() {
		var liked []string
		if err := s.repo.DB().WithContext(ctx).Model(&model.CommentLike{}).
			Where("user_id = ? AND comment_id IN ?", actor.UserID, commentIDs).
			Pluck("comment_id", &liked).Error; err == nil {
			for _, id := range liked {
				rc.liked[id] = true
			}
		}
	}
	return rc
}

// loadCategoryBriefs 批量取分类精简信息（内嵌文章的分类）。
func (s *CommentService) loadCategoryBriefs(ctx context.Context, ids []string) map[string]dto.CategoryBriefDto {
	out := map[string]dto.CategoryBriefDto{}
	if s.cats == nil || len(ids) == 0 {
		return out
	}
	cats, err := s.cats.FindByIDs(ctx, ids)
	if err != nil {
		s.log.Warn("批量查询分类失败", zap.Error(err))
		return out
	}
	for _, c := range cats {
		out[c.ID] = dto.CategoryBriefDto{
			ID:     c.ID,
			Name:   c.Name,
			Link:   strOrEmpty(c.Link),
			Avatar: strOrEmpty(c.Avatar),
			Status: c.Status,
		}
	}
	return out
}

// renderComment 组装单条评论视图，并挂载批量预取到的 article / parent。
func renderComment(c model.Comment, rc *commentRenderCtx) CommentView {
	v := buildCommentView(c, rc.author, rc.liked)
	if a := rc.article[c.ArticleID]; a != nil {
		v.Article = a
	}
	if v.ParentID != "" {
		if p := rc.parent[v.ParentID]; p != nil {
			v.Parent = p
		}
	}
	return v
}

// buildTree 一级评论 + 回复组树。
func (s *CommentService) buildTree(ctx context.Context, roots []model.Comment, actor Actor) []CommentView {
	if len(roots) == 0 {
		return []CommentView{}
	}
	rootIDs := make([]string, 0, len(roots))
	for _, r := range roots {
		rootIDs = append(rootIDs, r.ID)
	}
	replies, err := s.repo.FindReplies(ctx, rootIDs)
	if err != nil {
		s.log.Warn("查询评论回复失败", zap.Error(err))
	}

	// 一次预取（作者 / 点赞 / 文章 / 父评论），此后组装不再触库
	all := make([]model.Comment, 0, len(roots)+len(replies))
	all = append(all, roots...)
	all = append(all, replies...)
	rc := s.buildRenderCtx(ctx, actor, all)

	views := make([]CommentView, 0, len(roots))
	byID := map[string]int{}
	for _, r := range roots {
		v := renderComment(r, rc)
		v.Replies = []CommentView{}
		byID[r.ID] = len(views)
		views = append(views, v)
	}
	for _, r := range replies {
		if r.RootID == nil {
			continue
		}
		if idx, ok := byID[*r.RootID]; ok {
			views[idx].Replies = append(views[idx].Replies, renderComment(r, rc))
		}
	}
	return views
}

// BuildView 单条评论视图。
func (s *CommentService) BuildView(ctx context.Context, c model.Comment, actor Actor) *CommentView {
	authorMap := s.resolveAuthorMap(ctx, map[string]bool{c.AuthorID: true})
	liked := false
	if !actor.Anonymous() {
		if _, err := s.repo.FindLike(ctx, c.ID, actor.UserID); err == nil {
			liked = true
		}
	}
	v := buildCommentView(c, authorMap, map[string]bool{c.ID: liked})
	return &v
}

// buildCommentView 纯组装（不含跨域查询）。
func buildCommentView(c model.Comment, authorMap map[string]AuthorBrief, likedSet map[string]bool) CommentView {
	v := CommentView{
		ID:         c.ID,
		Content:    c.Content,
		Images:     ParseImagesList(c.Images),
		Likes:      c.Likes,
		ReplyCount: c.ReplyCount,
		Floor:      c.Floor,
		IsPinned:   c.IsPinned,
		Status:     c.Status,
		AuthorID:   c.AuthorID,
		ArticleID:  c.ArticleID,
		ParentID:   strOrEmpty(c.ParentID),
		RootID:     strOrEmpty(c.RootID),
		IsLiked:    likedSet[c.ID],
		Replies:    make([]CommentView, 0),
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
	if a, ok := authorMap[c.AuthorID]; ok {
		brief := authorBriefDto(a, nil)
		v.Author = &brief
	}
	return v
}

// resolveAuthorMap 批量解析作者信息（用户域未接入时返回空映射，视图仅保留 authorId）。
func (s *CommentService) resolveAuthorMap(ctx context.Context, set map[string]bool) map[string]AuthorBrief {
	out := map[string]AuthorBrief{}
	if s.authors == nil || len(set) == 0 {
		return out
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out
	}
	if m, err := s.authors.ResolveAuthors(ctx, ids); err == nil {
		out = m
	} else {
		s.log.Warn("批量解析评论作者失败", zap.Error(err))
	}
	return out
}

// nowPtr 返回当前时间的指针（可空时间列赋值辅助）。
func nowPtr() *time.Time {
	t := time.Now()
	return &t
}

package service

import (
	"context"
	"errors"
	"time"

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
	ArticleID string     `json:"articleId" binding:"required"`
	ParentID  *string    `json:"parentId"`
	Images    StringList `json:"images"`
}

// UpdateCommentRequest 更新评论。
type UpdateCommentRequest struct {
	Content  *string    `json:"content"`
	Images   StringList `json:"images"`
	IsPinned *bool      `json:"isPinned"` // 仅文章作者或管理员可设置
}

// SetCommentPinRequest 置顶评论。
type SetCommentPinRequest struct {
	IsPinned *bool `json:"isPinned" binding:"required"`
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

// CommentView 评论视图。
type CommentView struct {
	ID string `json:"id"`

	Content string      `json:"content"`
	Images  interface{} `json:"images"`

	Likes      int    `json:"likes"`
	ReplyCount int    `json:"replyCount"`
	Floor      int    `json:"floor"`
	IsPinned   bool   `json:"isPinned"`
	Status     string `json:"status"`

	AuthorID  string       `json:"authorId"`
	Author    *AuthorBrief `json:"author,omitempty"`
	ArticleID string       `json:"articleId"`
	ParentID  *string      `json:"parentId"`
	RootID    *string      `json:"rootId"`

	// 当前查看者是否已点赞
	IsLiked bool `json:"isLiked"`

	// 二级回复（树形组装；一级评论填充，二级评论为空）
	Replies []CommentView `json:"replies,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

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
	repo    *repository.CommentRepository
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
		log:     log,
		owner:   articleOwnerChecker{repo: articleRepo},
		authors: authors,
	}
	if articleRepo != nil {
		svc.owner = articleOwnerChecker{repo: articleRepo}
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
func (s *CommentService) SetPin(ctx context.Context, id string, actor Actor, isPinned bool) (*map[string]interface{}, *errcode.Err) {
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
	out := map[string]interface{}{"id": id, "isPinned": isPinned}
	return &out, nil
}

// Remove 删除评论（作者本人或管理员；软删）。
// 一级评论删除时同步回扣文章 comment_count；回复删除回扣父评论 reply_count。
func (s *CommentService) Remove(ctx context.Context, id string, actor Actor) (*map[string]interface{}, *errcode.Err) {
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
	out := map[string]interface{}{"id": id, "deleted": true}
	return &out, nil
}

// Like 点赞评论（对齐 POST /comment/:id/like；切换语义：已赞则取消）。
func (s *CommentService) Like(ctx context.Context, id string, actor Actor) (*map[string]interface{}, *errcode.Err) {
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
	out := map[string]interface{}{"isLiked": liked}
	return &out, nil
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
func (s *CommentService) FindCommentDetail(ctx context.Context, id string, actor Actor) (*CommentView, *errcode.Err) {
	c, e := s.getComment(ctx, id)
	if e != nil {
		return nil, e
	}
	return s.BuildView(ctx, *c, actor), nil
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
	out := make([]CommentView, 0, len(rows))
	for _, c := range rows {
		out = append(out, *s.BuildView(ctx, c, actor))
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
	out := make([]CommentView, 0, len(rows))
	for _, c := range rows {
		out = append(out, *s.BuildView(ctx, c, actor))
	}
	return out, total, nil
}

// CommentCount 文章评论数（对齐 GET /comment/article/:id/count）。
func (s *CommentService) CommentCount(ctx context.Context, articleID string) (*map[string]interface{}, *errcode.Err) {
	n, err := s.repo.CountByArticle(ctx, articleID)
	if err != nil {
		s.log.Error("统计评论数失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"count": n}
	return &out, nil
}

// ---------- 视图组装 ----------

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

	// 点赞状态（批量）
	allIDs := make([]string, 0, len(roots)+len(replies))
	for _, r := range roots {
		allIDs = append(allIDs, r.ID)
	}
	for _, r := range replies {
		allIDs = append(allIDs, r.ID)
	}
	likedSet := map[string]bool{}
	if !actor.Anonymous() && len(allIDs) > 0 {
		var ids []string
		if err := s.repo.DB().WithContext(ctx).Model(&model.CommentLike{}).
			Where("user_id = ? AND comment_id IN ?", actor.UserID, allIDs).
			Pluck("comment_id", &ids).Error; err == nil {
			for _, id := range ids {
				likedSet[id] = true
			}
		}
	}

	// 作者信息
	authorIDs := map[string]bool{}
	for _, r := range roots {
		authorIDs[r.AuthorID] = true
	}
	for _, r := range replies {
		authorIDs[r.AuthorID] = true
	}
	authorMap := s.resolveAuthorMap(ctx, authorIDs)

	views := make([]CommentView, 0, len(roots))
	byID := map[string]int{}
	for _, r := range roots {
		v := buildCommentView(r, authorMap, likedSet)
		v.Replies = []CommentView{}
		byID[r.ID] = len(views)
		views = append(views, v)
	}
	for _, r := range replies {
		if r.RootID == nil {
			continue
		}
		if idx, ok := byID[*r.RootID]; ok {
			views[idx].Replies = append(views[idx].Replies, buildCommentView(r, authorMap, likedSet))
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
		Images:     ParseImages(c.Images),
		Likes:      c.Likes,
		ReplyCount: c.ReplyCount,
		Floor:      c.Floor,
		IsPinned:   c.IsPinned,
		Status:     c.Status,
		AuthorID:   c.AuthorID,
		ArticleID:  c.ArticleID,
		ParentID:   c.ParentID,
		RootID:     c.RootID,
		IsLiked:    likedSet[c.ID],
		Replies:    nil,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
	if a, ok := authorMap[c.AuthorID]; ok {
		brief := a
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

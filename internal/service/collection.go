package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ---------- 合集 DTO ----------

// CreateCollectionRequest 创建合集。
type CreateCollectionRequest struct {
	Name        string  `json:"name" binding:"required,max=100"`
	Description *string `json:"description"`
	Avatar      *string `json:"avatar"`
	Cover       *string `json:"cover"`
	IsPublic    *bool   `json:"isPublic"`
	Sort        *int    `json:"sort"`
}

// UpdateCollectionRequest 更新合集（全字段可选）。
type UpdateCollectionRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=100"`
	Description *string `json:"description"`
	Avatar      *string `json:"avatar"`
	Cover       *string `json:"cover"`
	IsPublic    *bool   `json:"isPublic"`
	Sort        *int    `json:"sort"`
}

// ReorderCollectionItemsRequest 合集内文章重排。
type ReorderCollectionItemsRequest struct {
	ArticleIDs []string `json:"articleIds" binding:"required,min=1"`
}

// CollectionView 合集视图。
type CollectionView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Avatar      *string   `json:"avatar,omitempty"`
	Cover       *string   `json:"cover,omitempty"`
	IsPublic    bool      `json:"isPublic"`
	Sort        int       `json:"sort"`
	UserID      string    `json:"userId"`
	ItemCount   int       `json:"itemCount"`
	Views       int       `json:"views"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// 详情视角：内嵌条目
	Items []CollectionItemView `json:"items,omitempty"`
}

// CollectionItemView 合集条目视图。
type CollectionItemView struct {
	ID        string       `json:"id"`
	ArticleID string       `json:"articleId"`
	Sort      int          `json:"sort"`
	Note      *string      `json:"note,omitempty"`
	Article   *ArticleView `json:"article,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
}

// reorderSortValue 合集重排后的排序值：传入顺序即展示顺序，越靠前越大。
//
// 防的 bug：合集「拖拽排序」后顺序反了 / 首个元素被挤到末尾。
// 例：3 个元素，idx=0 → 3（最前），idx=2 → 1（最后）。
func reorderSortValue(total, idx int) int {
	return total - idx
}

// CollectionService 合集业务逻辑（对齐原 NestJS CollectionService）。
type CollectionService struct {
	repo *repository.CollectionRepository
	arts *repository.ArticleRepository
	log  *zap.Logger
}

// NewCollectionService 构造合集服务。
func NewCollectionService(
	repo *repository.CollectionRepository,
	arts *repository.ArticleRepository,
	log *zap.Logger,
) *CollectionService {
	if log == nil {
		log = zap.NewNop()
	}
	return &CollectionService{repo: repo, arts: arts, log: log}
}

// ErrCollectionNotFound 合集不存在。
var ErrCollectionNotFound = errcode.ErrNotFound.WithKey(i18n.CollectionNotFound)

// getCollection 取合集并校验归属（ownerOnly 时要求本人）。
func (s *CollectionService) getCollection(ctx context.Context, id string, actor Actor, ownerOnly bool) (*model.Collection, *errcode.Err) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCollectionNotFound
		}
		s.log.Error("查询合集失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if ownerOnly && c.UserID != actor.UserID {
		return nil, errcode.ErrForbidden.WithKey(i18n.CollectionNoPermission)
	}
	// 私有合集：非本人不可见
	if !ownerOnly && !c.IsPublic && c.UserID != actor.UserID {
		return nil, errcode.ErrForbidden.WithKey(i18n.CollectionPrivate)
	}
	return c, nil
}

// Create 创建合集（对齐 POST /collection）。
func (s *CollectionService) Create(ctx context.Context, actor Actor, req CreateCollectionRequest) (*CollectionView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.CollectionNameRequired)
	}
	c := &model.Collection{
		Name:        name,
		Description: req.Description,
		Avatar:      req.Avatar,
		Cover:       req.Cover,
		IsPublic:    boolVal(req.IsPublic),
		Sort:        intVal(req.Sort),
		UserID:      actor.UserID,
	}
	if err := s.repo.Create(ctx, c); err != nil {
		s.log.Error("创建合集失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	v := toCollectionView(*c)
	return &v, nil
}

// List 合集列表（对齐 GET /collection）。
func (s *CollectionService) List(ctx context.Context, actor Actor, q repository.CollectionListQuery) ([]CollectionView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	rows, total, err := s.repo.List(ctx, q)
	if err != nil {
		s.log.Error("查询合集列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	out := make([]CollectionView, 0, len(rows))
	for _, r := range rows {
		// 私有合集仅本人可见
		if !r.IsPublic && r.UserID != actor.UserID {
			total--
			continue
		}
		out = append(out, toCollectionView(r))
	}
	if total < 0 {
		total = 0
	}
	return out, total, nil
}

// FindOne 合集详情（对齐 GET /collection/:id），附带条目与文章概要。
func (s *CollectionService) FindOne(ctx context.Context, id string, actor Actor) (*CollectionView, *errcode.Err) {
	c, e := s.getCollection(ctx, id, actor, false)
	if e != nil {
		return nil, e
	}
	// 访问量自增（失败不影响主流程）
	if err := s.repo.IncrCounter(ctx, id, "views", 1); err != nil {
		s.log.Warn("合集访问量自增失败", zap.Error(err))
	} else {
		c.Views++
	}

	v := toCollectionView(*c)
	items, err := s.repo.AllItems(ctx, id)
	if err != nil {
		s.log.Warn("查询合集条目失败", zap.Error(err))
		return &v, nil
	}
	v.Items = s.buildItemViews(ctx, items, actor)
	return &v, nil
}

// Update 更新合集（仅本人）。
func (s *CollectionService) Update(ctx context.Context, id string, actor Actor, req UpdateCollectionRequest) (*CollectionView, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, true); e != nil {
		return nil, e
	}

	fields := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errcode.ErrBadRequest.WithKey(i18n.CollectionNameRequired)
		}
		fields["name"] = name
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if req.Avatar != nil {
		fields["avatar"] = *req.Avatar
	}
	if req.Cover != nil {
		fields["cover"] = *req.Cover
	}
	if req.IsPublic != nil {
		fields["is_public"] = *req.IsPublic
	}
	if req.Sort != nil {
		fields["sort"] = *req.Sort
	}
	if err := s.repo.UpdateFields(ctx, id, fields); err != nil {
		s.log.Error("更新合集失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	v := toCollectionView(*c)
	return &v, nil
}

// Remove 删除合集并清空条目（仅本人）。
func (s *CollectionService) Remove(ctx context.Context, id string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, true); e != nil {
		return nil, e
	}
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if err := repo.DeleteItemsByCollection(ctx, id); err != nil {
			return err
		}
		return repo.SoftDelete(ctx, id)
	})
	if err != nil {
		s.log.Error("删除合集失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"id": id, "deleted": true}
	return &out, nil
}

// AddArticle 添加文章到合集（对齐 POST /collection/:id/article/:articleId）。
// 重复添加幂等返回（不报错，保留原排序）。
func (s *CollectionService) AddArticle(ctx context.Context, id, articleID string, actor Actor, note *string) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, true); e != nil {
		return nil, e
	}
	// 文章存在性
	if _, err := s.arts.FindByID(ctx, articleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)
		}
		s.log.Error("查询文章失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if _, err := s.repo.FindItem(ctx, id, articleID); err == nil {
		// message 提到顶层：handler 按 added=false 用 SuccessMsg 提示已存在
		out := map[string]interface{}{"added": false}
		return &out, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询合集条目失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	sort, err := s.repo.NextItemSort(ctx, id)
	if err != nil {
		s.log.Error("计算合集排序失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	err = s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if err := repo.CreateItem(ctx, &model.CollectionItem{
			CollectionID: id,
			ArticleID:    articleID,
			UserID:       actor.UserID,
			Sort:         sort,
			Note:         note,
		}); err != nil {
			return err
		}
		return repo.IncrCounter(ctx, id, "item_count", 1)
	})
	if err != nil {
		s.log.Error("添加合集条目失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"added": true, "sort": sort}
	return &out, nil
}

// RemoveArticle 从合集移除文章（对齐 DELETE /collection/:id/article/:articleId）。
func (s *CollectionService) RemoveArticle(ctx context.Context, id, articleID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, true); e != nil {
		return nil, e
	}
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if err := repo.DeleteItem(ctx, id, articleID); err != nil {
			return err
		}
		return repo.IncrCounter(ctx, id, "item_count", -1)
	})
	if err != nil {
		s.log.Error("移除合集条目失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"removed": true}
	return &out, nil
}

// ReorderItems 合集内文章重排（按下标顺序写入 sort，越大越靠前）。
func (s *CollectionService) ReorderItems(ctx context.Context, id string, actor Actor, articleIDs []string) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, true); e != nil {
		return nil, e
	}
	total := len(articleIDs)
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		for idx, articleID := range articleIDs {
			it, err := repo.FindItem(ctx, id, articleID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue // 不在合集内则跳过，保持整体幂等
				}
				return err
			}
			// 传入顺序即展示顺序：越靠前 sort 越大
			if err := repo.UpdateItemFields(ctx, it.ID, map[string]interface{}{"sort": reorderSortValue(total, idx)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Error("合集重排失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"reordered": len(articleIDs)}
	return &out, nil
}

// ListItems 合集内文章列表（对齐 GET /collection/:id/items）。
func (s *CollectionService) ListItems(ctx context.Context, id string, actor Actor, page, limit int) ([]CollectionItemView, int64, *errcode.Err) {
	if actor.Anonymous() {
		return nil, 0, errcode.ErrUnauthorized
	}
	if _, e := s.getCollection(ctx, id, actor, false); e != nil {
		return nil, 0, e
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.repo.ListItems(ctx, id, page, limit)
	if err != nil {
		s.log.Error("查询合集条目失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	return s.buildItemViews(ctx, items, actor), total, nil
}

// CheckArticle 检查文章是否在（我的）合集中（对齐 GET /collection/check/:articleId）。
func (s *CollectionService) CheckArticle(ctx context.Context, actor Actor, articleID string) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	collections, err := s.repo.UserCollectionsContaining(ctx, actor.UserID, articleID)
	if err != nil {
		s.log.Error("查询文章所属合集失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	list := make([]map[string]interface{}, 0, len(collections))
	for _, c := range collections {
		list = append(list, map[string]interface{}{
			"id":        c.ID,
			"name":      c.Name,
			"isPublic":  c.IsPublic,
			"itemCount": c.ItemCount,
		})
	}
	out := map[string]interface{}{"inCollection": len(list) > 0, "collections": list}
	return &out, nil
}

// ArticleCollectionInfo 文章所在合集信息（对齐 GET /collection/article/:articleId/info）。
// 返回该文章在「当前用户合集」与「公开合集」中的归属，供文章详情页跳转。
func (s *CollectionService) ArticleCollectionInfo(ctx context.Context, actor Actor, articleID string) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	mine, err := s.repo.UserCollectionsContaining(ctx, actor.UserID, articleID)
	if err != nil {
		s.log.Error("查询文章合集归属失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	list := make([]map[string]interface{}, 0, len(mine))
	for _, c := range mine {
		list = append(list, map[string]interface{}{
			"id":       c.ID,
			"name":     c.Name,
			"userId":   c.UserID,
			"isPublic": c.IsPublic,
		})
	}
	out := map[string]interface{}{"articleId": articleID, "collections": list}
	return &out, nil
}

// buildItemViews 组装条目视图（批量取文章，避免 N+1）。
func (s *CollectionService) buildItemViews(ctx context.Context, items []model.CollectionItem, actor Actor) []CollectionItemView {
	out := make([]CollectionItemView, 0, len(items))
	if len(items) == 0 {
		return out
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ArticleID)
	}
	articleViews := s.buildArticleViewsFor(ctx, ids)
	byID := map[string]*ArticleView{}
	for i := range articleViews {
		byID[articleViews[i].ID] = &articleViews[i]
	}
	for _, it := range items {
		v := CollectionItemView{
			ID:        it.ID,
			ArticleID: it.ArticleID,
			Sort:      it.Sort,
			Note:      it.Note,
			CreatedAt: it.CreatedAt,
		}
		if a, ok := byID[it.ArticleID]; ok {
			v.Article = a
		}
		out = append(out, v)
	}
	return out
}

// buildArticleViewsFor 合集条目内嵌文章视图（轻量：不含正文）。
func (s *CollectionService) buildArticleViewsFor(ctx context.Context, ids []string) []ArticleView {
	m, err := s.arts.FindByIDs(ctx, ids)
	if err != nil {
		s.log.Warn("批量查询合集文章失败", zap.Error(err))
		return nil
	}
	rows := articleListFromMap(m, ids)
	// 复用 ArticleService 的视图组装能力（保持字段一致）
	svc := &ArticleService{
		repo: s.arts,
		log:  s.log,
	}
	return svc.BuildViews(ctx, rows, Actor{}, false)
}

// toCollectionView 实体转视图。
func toCollectionView(c model.Collection) CollectionView {
	return CollectionView{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		Avatar:      c.Avatar,
		Cover:       c.Cover,
		IsPublic:    c.IsPublic,
		Sort:        c.Sort,
		UserID:      c.UserID,
		ItemCount:   c.ItemCount,
		Views:       c.Views,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

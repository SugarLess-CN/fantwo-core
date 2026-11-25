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
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ---------- 标签 DTO ----------

// CreateTagRequest 创建标签。
type CreateTagRequest struct {
	Name        string  `json:"name" binding:"required,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Sort        *int    `json:"sort"`
}

// UpdateTagRequest 更新标签（全字段可选）。
type UpdateTagRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Sort        *int    `json:"sort"`
}

// TagView 标签视图。
type TagView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  *string   `json:"description,omitempty"`
	Avatar       *string   `json:"avatar,omitempty"`
	Background   *string   `json:"background,omitempty"`
	Cover        *string   `json:"cover,omitempty"`
	Sort         int       `json:"sort"`
	ArticleCount int       `json:"articleCount"`
	FollowCount  int       `json:"followCount"`
	IsFollowed   bool      `json:"isFollowed"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TagService 标签业务逻辑（对齐原 NestJS TagService）。
type TagService struct {
	repo *repository.TagRepository
	arts *repository.ArticleRepository
	log  *zap.Logger
}

// NewTagService 构造标签服务。
func NewTagService(repo *repository.TagRepository, arts *repository.ArticleRepository, log *zap.Logger) *TagService {
	if log == nil {
		log = zap.NewNop()
	}
	return &TagService{repo: repo, arts: arts, log: log}
}

// ErrTagNotFound 标签不存在。
var ErrTagNotFound = errcode.ErrNotFound.WithKey(i18n.TagNotFound)

// Create 创建标签（名称唯一）。
func (s *TagService) Create(ctx context.Context, actor Actor, req CreateTagRequest) (*TagView, *errcode.Err) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.TagNameRequired)
	}
	if _, err := s.repo.FindByName(ctx, name); err == nil {
		return nil, errcode.ErrConflict.WithKeyf(i18n.TagNameExists, name)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	t := &model.Tag{
		Name:        name,
		Description: req.Description,
		Avatar:      req.Avatar,
		Background:  req.Background,
		Cover:       req.Cover,
		Sort:        intVal(req.Sort),
	}
	if err := s.repo.Create(ctx, t); err != nil {
		s.log.Error("创建标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	v := toTagView(*t, false)
	return &v, nil
}

// List 标签列表（对齐 GET /tag）。
func (s *TagService) List(ctx context.Context, actor Actor, q repository.TagListQuery) ([]TagView, int64, *errcode.Err) {
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
		s.log.Error("查询标签列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	followed, _ := s.repo.UserFollowedSet(ctx, actor.UserID, ids)

	out := make([]TagView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toTagView(r, followed[r.ID]))
	}
	return out, total, nil
}

// Recommended 推荐标签（对齐 GET /tag/recommended）：按文章数排序。
func (s *TagService) Recommended(ctx context.Context, actor Actor, page, limit int) ([]TagView, int64, *errcode.Err) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	rows, total, err := s.repo.Recommended(ctx, page, limit)
	if err != nil {
		s.log.Error("查询推荐标签失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	followed, _ := s.repo.UserFollowedSet(ctx, actor.UserID, ids)
	out := make([]TagView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toTagView(r, followed[r.ID]))
	}
	return out, total, nil
}

// Hot 热门标签统计（本域扩展接口，对齐 lead 要求的「热门标签统计」）。
func (s *TagService) Hot(ctx context.Context, actor Actor, limit int) ([]TagView, *errcode.Err) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.repo.Hot(ctx, limit)
	if err != nil {
		s.log.Error("查询热门标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	followed, _ := s.repo.UserFollowedSet(ctx, actor.UserID, ids)
	out := make([]TagView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toTagView(r, followed[r.ID]))
	}
	return out, nil
}

// FindOne 标签详情（对齐 GET /tag/:id）。
func (s *TagService) FindOne(ctx context.Context, id string, actor Actor) (*TagView, *errcode.Err) {
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTagNotFound
		}
		s.log.Error("查询标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	followed := false
	if !actor.Anonymous() {
		if _, err := s.repo.FindFollow(ctx, actor.UserID, id); err == nil {
			followed = true
		}
	}
	v := toTagView(*t, followed)
	return &v, nil
}

// Update 更新标签。
func (s *TagService) Update(ctx context.Context, id string, req UpdateTagRequest) (*TagView, *errcode.Err) {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTagNotFound
		}
		s.log.Error("查询标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	fields := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errcode.ErrBadRequest.WithKey(i18n.TagNameRequired)
		}
		// 名称唯一性校验（排除自身）
		if other, err := s.repo.FindByName(ctx, name); err == nil && other.ID != id {
			return nil, errcode.ErrConflict.WithKeyf(i18n.TagNameExists, name)
		}
		fields["name"] = name
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if req.Avatar != nil {
		fields["avatar"] = *req.Avatar
	}
	if req.Background != nil {
		fields["background"] = *req.Background
	}
	if req.Cover != nil {
		fields["cover"] = *req.Cover
	}
	if req.Sort != nil {
		fields["sort"] = *req.Sort
	}
	if err := s.repo.UpdateFields(ctx, id, fields); err != nil {
		s.log.Error("更新标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	v := toTagView(*t, false)
	return &v, nil
}

// Remove 删除标签并清理文章关联与关注记录。
func (s *TagService) Remove(ctx context.Context, id string) (*dto.DeleteResultDto, *errcode.Err) {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTagNotFound
		}
		return nil, errcode.ErrInternal
	}
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		// 清文章标签关联（不删除文章本身）
		if err := tx.WithContext(ctx).Where("tag_id = ?", id).
			Delete(&model.ArticleTag{}).Error; err != nil {
			return err
		}
		// 清关注记录
		if err := tx.WithContext(ctx).Where("tag_id = ?", id).
			Delete(&model.TagFollow{}).Error; err != nil {
			return err
		}
		return repo.SoftDelete(ctx, id)
	})
	if err != nil {
		s.log.Error("删除标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	return &dto.DeleteResultDto{ID: id, Deleted: true}, nil
}

// Follow 关注标签（对齐 POST /tag/:id/follow）。
func (s *TagService) Follow(ctx context.Context, tagID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, err := s.repo.FindByID(ctx, tagID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTagNotFound
		}
		return nil, errcode.ErrInternal
	}
	if _, err := s.repo.FindFollow(ctx, actor.UserID, tagID); err == nil {
		return nil, errcode.ErrBadRequest.WithKey(i18n.TagAlreadyFollowed)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询标签关注失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if err := repo.CreateFollow(ctx, &model.TagFollow{UserID: actor.UserID, TagID: tagID}); err != nil {
			return err
		}
		return repo.IncrCounter(ctx, tagID, "follow_count", 1)
	})
	if err != nil {
		s.log.Error("关注标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"followed": true}
	return &out, nil
}

// Unfollow 取消关注标签（对齐 DELETE /tag/:id/follow）。
func (s *TagService) Unfollow(ctx context.Context, tagID string, actor Actor) (*map[string]interface{}, *errcode.Err) {
	if actor.Anonymous() {
		return nil, errcode.ErrUnauthorized
	}
	if _, err := s.repo.FindFollow(ctx, actor.UserID, tagID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.ErrBadRequest.WithKey(i18n.TagNotFollowed)
		}
		return nil, errcode.ErrInternal
	}
	err := s.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if err := repo.DeleteFollow(ctx, actor.UserID, tagID); err != nil {
			return err
		}
		return repo.IncrCounter(ctx, tagID, "follow_count", -1)
	})
	if err != nil {
		s.log.Error("取消关注标签失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"followed": false}
	return &out, nil
}

// FollowedList 用户关注的标签列表（对齐 GET /tag/followed/list）。
//
// targetUserID 为空时看自己；匿名且无目标用户时返回空分页（对齐原 buildEmptyFollowedTagsList 语义）。
func (s *TagService) FollowedList(ctx context.Context, actor Actor, targetUserID, name string, page, limit int) ([]TagView, int64, *errcode.Err) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}

	userID := targetUserID
	if userID == "" {
		userID = actor.UserID
	}
	if userID == "" {
		return []TagView{}, 0, nil
	}

	var ids []string
	var total int64
	var err error
	if name != "" {
		all, e := s.repo.FollowedTagIDsByName(ctx, userID, name)
		if e != nil {
			s.log.Error("查询关注标签失败", zap.Error(e))
			return nil, 0, errcode.ErrInternal
		}
		total = int64(len(all))
		start := (page - 1) * limit
		if start > len(all) {
			start = len(all)
		}
		end := start + limit
		if end > len(all) {
			end = len(all)
		}
		ids = all[start:end]
	} else {
		ids, total, err = s.repo.FollowedTagIDs(ctx, userID, page, limit)
		if err != nil {
			s.log.Error("查询关注标签失败", zap.Error(err))
			return nil, 0, errcode.ErrInternal
		}
	}

	if len(ids) == 0 {
		return []TagView{}, total, nil
	}
	rows, err := s.repo.FindByIDs(ctx, ids)
	if err != nil {
		s.log.Error("批量查询标签失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	// 保持关注顺序
	byID := map[string]model.Tag{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	followed, _ := s.repo.UserFollowedSet(ctx, actor.UserID, ids)
	out := make([]TagView, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, toTagView(t, followed[id]))
		}
	}
	return out, total, nil
}

// toTagView 实体转视图。
func toTagView(t model.Tag, followed bool) TagView {
	return TagView{
		ID:           t.ID,
		Name:         t.Name,
		Description:  t.Description,
		Avatar:       t.Avatar,
		Background:   t.Background,
		Cover:        t.Cover,
		Sort:         t.Sort,
		ArticleCount: t.ArticleCount,
		FollowCount:  t.FollowCount,
		IsFollowed:   followed,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

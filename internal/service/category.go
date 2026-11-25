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

// ---------- 分类 DTO ----------

// CreateCategoryRequest 创建分类。
type CreateCategoryRequest struct {
	Name        string  `json:"name" binding:"required,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Sort        *int    `json:"sort"`
	ParentID    *string `json:"parentId"`
	Link        *string `json:"link" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Status      string  `json:"status" binding:"omitempty,oneof=ENABLED DISABLED"`
}

// UpdateCategoryRequest 更新分类（全字段可选）。
type UpdateCategoryRequest struct {
	Name        *string `json:"name" binding:"omitempty,max=50"`
	Description *string `json:"description" binding:"omitempty,max=200"`
	Sort        *int    `json:"sort"`
	ParentID    *string `json:"parentId"`
	Link        *string `json:"link" binding:"omitempty,max=200"`
	Avatar      *string `json:"avatar"`
	Background  *string `json:"background"`
	Cover       *string `json:"cover"`
	Status      string  `json:"status" binding:"omitempty,oneof=ENABLED DISABLED"`
}

// CategoryView 分类视图（Tree 版本携带子分类）。
// CategoryView 分类对外视图。
//
// 空值规则见 docs/21-JSON序列化规范.md：
//   - 字符串：值类型，空时 ""（ParentID 曾是指针且无 omitempty → 响应里出现 null）
//   - 数组：Children 非 nil，空时 []（不再用 omitempty 省略）
//   - 时间：CreatedAt/UpdatedAt 必填，值类型不变
type CategoryView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// 字符串字段：空时 ""，不出现 null
	Description string `json:"description"`
	// ParentID 为空表示「这是一级分类」。用 "" 而不是 null：
	// 前端判断 `if (cat.parentId)` 时 "" 与 null 都是 falsy，行为一致，但类型可信。
	ParentID     string `json:"parentId"`
	Link         string `json:"link"`
	Avatar       string `json:"avatar"`
	Background   string `json:"background"`
	Cover        string `json:"cover"`
	Sort         int    `json:"sort"`
	Status       string `json:"status"`
	ArticleCount int    `json:"articleCount"`
	FollowCount  int    `json:"followCount"`
	// 数组：空时 [] 而不是省略
	Children  []CategoryView `json:"children"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// CategoryService 分类业务逻辑（对齐原 NestJS CategoryService）。
type CategoryService struct {
	repo *repository.CategoryRepository
	log  *zap.Logger
}

// NewCategoryService 构造分类服务。
func NewCategoryService(repo *repository.CategoryRepository, log *zap.Logger) *CategoryService {
	if log == nil {
		log = zap.NewNop()
	}
	return &CategoryService{repo: repo, log: log}
}

// ErrCategoryNotFound 分类不存在。
var ErrCategoryNotFound = errcode.ErrNotFound.WithKey(i18n.CategoryNotFound)

// Create 创建分类（名称唯一，父分类需存在）。
func (s *CategoryService) Create(ctx context.Context, req CreateCategoryRequest) (*CategoryView, *errcode.Err) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryNameRequired)
	}
	if _, err := s.repo.FindByName(ctx, name); err == nil {
		return nil, errcode.ErrConflict.WithKeyf(i18n.CategoryNameExists, name)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		s.log.Error("查询分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	// 父分类校验
	if req.ParentID != nil && *req.ParentID != "" {
		if _, err := s.repo.FindByID(ctx, *req.ParentID); err != nil {
			return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryParentMissing)
		}
	}

	status := req.Status
	if status == "" {
		status = model.CategoryStatusEnabled
	}
	c := &model.Category{
		Name:        name,
		Description: req.Description,
		ParentID:    req.ParentID,
		Link:        req.Link,
		Avatar:      req.Avatar,
		Background:  req.Background,
		Cover:       req.Cover,
		Sort:        intVal(req.Sort),
		Status:      status,
	}
	if c.ParentID != nil && *c.ParentID == "" {
		c.ParentID = nil
	}
	if err := s.repo.Create(ctx, c); err != nil {
		s.log.Error("创建分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	v := toCategoryView(*c)
	return &v, nil
}

// List 分类列表（对齐 GET /category）。
// withTree=true 时返回两级树（父分类的 children 内嵌子分类）。
func (s *CategoryService) List(ctx context.Context, q repository.CategoryListQuery, withTree bool) ([]CategoryView, int64, *errcode.Err) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}

	if withTree {
		all, err := s.repo.All(ctx)
		if err != nil {
			s.log.Error("查询分类树失败", zap.Error(err))
			return nil, 0, errcode.ErrInternal
		}
		return buildCategoryTree(all), int64(len(all)), nil
	}

	rows, total, err := s.repo.List(ctx, q)
	if err != nil {
		s.log.Error("查询分类列表失败", zap.Error(err))
		return nil, 0, errcode.ErrInternal
	}
	out := make([]CategoryView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCategoryView(r))
	}
	return out, total, nil
}

// FindOne 分类详情（对齐 GET /category/:id），附带直接子分类。
func (s *CategoryService) FindOne(ctx context.Context, id string) (*CategoryView, *errcode.Err) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		s.log.Error("查询分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	v := toCategoryView(*c)
	if children, err := s.repo.Children(ctx, id); err == nil && len(children) > 0 {
		for _, ch := range children {
			v.Children = append(v.Children, toCategoryView(ch))
		}
	}
	return &v, nil
}

// Update 更新分类。
func (s *CategoryService) Update(ctx context.Context, id string, req UpdateCategoryRequest) (*CategoryView, *errcode.Err) {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		s.log.Error("查询分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}

	fields := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryNameRequired)
		}
		if other, err := s.repo.FindByName(ctx, name); err == nil && other.ID != id {
			return nil, errcode.ErrConflict.WithKeyf(i18n.CategoryNameExists, name)
		}
		fields["name"] = name
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if req.Sort != nil {
		fields["sort"] = *req.Sort
	}
	if req.Link != nil {
		fields["link"] = *req.Link
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
	if req.Status != "" {
		fields["status"] = req.Status
	}
	if req.ParentID != nil {
		parentID := *req.ParentID
		if parentID == "" {
			fields["parent_id"] = nil
		} else {
			if parentID == id {
				return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryParentSelf)
			}
			if _, err := s.repo.FindByID(ctx, parentID); err != nil {
				return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryParentMissing)
			}
			fields["parent_id"] = parentID
		}
	}

	if err := s.repo.UpdateFields(ctx, id, fields); err != nil {
		s.log.Error("更新分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	v := toCategoryView(*c)
	return &v, nil
}

// Remove 删除分类；存在子分类时拒绝删除（防止树断裂）。
func (s *CategoryService) Remove(ctx context.Context, id string) (*map[string]interface{}, *errcode.Err) {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, errcode.ErrInternal
	}
	has, err := s.repo.HasChildren(ctx, id)
	if err != nil {
		s.log.Error("查询子分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	if has {
		return nil, errcode.ErrBadRequest.WithKey(i18n.CategoryHasChildren)
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		s.log.Error("删除分类失败", zap.Error(err))
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"id": id, "deleted": true}
	return &out, nil
}

// toCategoryView 实体转视图。
func toCategoryView(c model.Category) CategoryView {
	return CategoryView{
		ID:           c.ID,
		Name:         c.Name,
		Description:  strOrEmpty(c.Description),
		ParentID:     strOrEmpty(c.ParentID),
		Link:         strOrEmpty(c.Link),
		Avatar:       strOrEmpty(c.Avatar),
		Background:   strOrEmpty(c.Background),
		Cover:        strOrEmpty(c.Cover),
		Sort:         c.Sort,
		Status:       c.Status,
		ArticleCount: c.ArticleCount,
		FollowCount:  c.FollowCount,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
		// 在这里兜底而不是在每个调用点：toCategoryView 是**唯一**的组装入口，
		// 放在这能保证 tree / list / findOne 三条路径的 children 都是 [] 而非 null。
		Children: make([]CategoryView, 0),
	}
}

// buildCategoryTree 构建两级分类树。
func buildCategoryTree(all []model.Category) []CategoryView {
	byParent := map[string][]model.Category{}
	roots := make([]model.Category, 0, len(all))
	for _, c := range all {
		if c.ParentID == nil || *c.ParentID == "" {
			roots = append(roots, c)
			continue
		}
		byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
	}
	out := make([]CategoryView, 0, len(roots))
	for _, r := range roots {
		v := toCategoryView(r)
		// 显式初始化为空切片：叶节点没有子分类时，nil 会序列化成 null，
		// 而规范要求 []（docs/21-JSON序列化规范.md）
		v.Children = make([]CategoryView, 0, len(byParent[r.ID]))
		for _, ch := range byParent[r.ID] {
			v.Children = append(v.Children, toCategoryView(ch))
		}
		out = append(out, v)
	}
	return out
}

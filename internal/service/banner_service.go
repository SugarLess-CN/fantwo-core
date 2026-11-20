// 轮播服务：公开列表 + 管理端 CRUD（对齐原 banner.service.ts）。
package service

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// BannerService 轮播业务。
type BannerService struct {
	DB      *gorm.DB
	banners repository.BannerRepo
}

// NewBannerService 构造。
func NewBannerService(db *gorm.DB) *BannerService { return &BannerService{DB: db} }

// Create 创建轮播。
func (s *BannerService) Create(in model.BannerInput) (*model.Banner, error) {
	if in.Title == nil || *in.Title == "" {
		return nil, errcode.ErrBadRequest.With("轮播标题不能为空")
	}
	if in.ImageURL == nil || *in.ImageURL == "" {
		return nil, errcode.ErrBadRequest.With("轮播图片不能为空")
	}
	b := &model.Banner{Title: *in.Title, ImageURL: *in.ImageURL, Status: model.BannerStatusActive}
	applyBannerInput(b, in)
	if err := s.banners.Create(s.DB, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Update 更新轮播。
func (s *BannerService) Update(id string, in model.BannerInput) (*model.Banner, error) {
	b, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	applyBannerInput(b, in)
	if err := s.banners.Save(s.DB, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Delete 删除轮播。
func (s *BannerService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.banners.Delete(s.DB, id)
}

// Get 轮播详情。
func (s *BannerService) Get(id string) (*model.Banner, error) {
	b, err := s.banners.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, errcode.ErrNotFound.With("轮播不存在")
	}
	return b, nil
}

// List 管理端列表（status 过滤；非管理员由 handler 强制 status=active）。
func (s *BannerService) List(q model.BannerQuery) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.banners.List(s.DB, q)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, q.Page, q.Limit)}, nil
}

// Active 前台公开列表（只返回启用中，按 sort_order 升序）。
func (s *BannerService) Active() ([]model.Banner, error) {
	return s.banners.ListActive(s.DB)
}

// effectiveBannerStatus 计算列表查询实际生效的状态过滤（对齐原 BannerController）。
//
// 防的 bug：非管理员传 ?status=inactive 就能看到已下线的运营位
// （原实现只判 banner:manage 权限，这里把判定抽成纯函数以可测）。
func EffectiveBannerStatus(requested string, isAdmin bool) string {
	if isAdmin {
		return requested
	}
	return model.BannerStatusActive
}

// applyBannerInput 非 nil 字段写入。
func applyBannerInput(b *model.Banner, in model.BannerInput) {
	if in.Title != nil {
		b.Title = *in.Title
	}
	if in.Description != nil {
		b.Description = *in.Description
	}
	if in.ImageURL != nil {
		b.ImageURL = *in.ImageURL
	}
	if in.LinkURL != nil {
		b.LinkURL = *in.LinkURL
	}
	if in.SortOrder != nil {
		b.SortOrder = *in.SortOrder
	}
	if in.Status != nil {
		b.Status = *in.Status
	}
}

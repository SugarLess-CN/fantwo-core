// 表情包服务：表情 CRUD、收藏、热门/最近/分类、使用计数、表情包与包内条目。
//
// 对齐原 emoji.service.ts + emoji-pack.service.ts。权限口径：
//   - 系统表情（type=system）只有管理端可改；
//   - 用户表情仅创建者可改删。
//
// 上传（POST /emoji/upload）的落盘由上传域负责，本服务只接收已上传好的 URL。
package service

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// EmojiService 表情业务。
type EmojiService struct {
	DB *gorm.DB

	emojis    repository.EmojiRepo
	favorites repository.EmojiFavoriteRepo
	packs     repository.EmojiPackRepo
	items     repository.EmojiPackItemRepo
}

// NewEmojiService 构造。
func NewEmojiService(db *gorm.DB) *EmojiService { return &EmojiService{DB: db} }

// Create 创建表情。
func (s *EmojiService) Create(userID string, in model.EmojiInput, isAdmin bool) (*model.Emoji, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.With("表情名称不能为空")
	}
	if in.URL == nil || *in.URL == "" {
		return nil, errcode.ErrBadRequest.With("表情图片地址不能为空")
	}
	if in.Code != nil && *in.Code != "" {
		exists, err := s.emojis.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if exists != nil {
			return nil, errcode.ErrBadRequest.With("表情代码已存在")
		}
	}

	e := &model.Emoji{
		Name:     *in.Name,
		URL:      *in.URL,
		Type:     model.EmojiTypeUser,
		Status:   model.EmojiStatusActive,
		IsPublic: true,
	}
	if isAdmin && in.Type != nil && *in.Type == model.EmojiTypeSystem {
		e.Type = model.EmojiTypeSystem
	}
	if e.Type == model.EmojiTypeUser {
		e.UserID = userID
	}
	applyEmojiInput(e, in)
	if err := s.emojis.Create(s.DB, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Update 更新表情。
func (s *EmojiService) Update(userID, id string, in model.EmojiInput, isAdmin bool) (*model.Emoji, error) {
	e, err := s.Get(id, userID)
	if err != nil {
		return nil, err
	}
	if err := s.checkOwner(e, userID, isAdmin); err != nil {
		return nil, err
	}
	if in.Code != nil && *in.Code != "" && *in.Code != e.Code {
		exists, err := s.emojis.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if exists != nil {
			return nil, errcode.ErrBadRequest.With("表情代码已存在")
		}
	}
	applyEmojiInput(e, in)
	if err := s.emojis.Save(s.DB, e); err != nil {
		return nil, err
	}
	return e, nil
}

// Delete 删除表情。
func (s *EmojiService) Delete(userID, id string, isAdmin bool) error {
	e, err := s.Get(id, userID)
	if err != nil {
		return err
	}
	if err := s.checkOwner(e, userID, isAdmin); err != nil {
		return err
	}
	return s.emojis.Delete(s.DB, id)
}

// checkOwner 归属校验：系统表情需管理端；用户表情需本人。
func (s *EmojiService) checkOwner(e *model.Emoji, userID string, isAdmin bool) error {
	if e.Type == model.EmojiTypeSystem {
		if !isAdmin {
			return errcode.ErrForbidden.With("系统表情只能由管理员维护")
		}
		return nil
	}
	if e.UserID != "" && e.UserID != userID && !isAdmin {
		return errcode.ErrForbidden.With("只能操作自己的表情")
	}
	return nil
}

// Get 表情详情（非公开表情仅本人可见）。
func (s *EmojiService) Get(id, viewerID string) (*model.Emoji, error) {
	e, err := s.emojis.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errcode.ErrNotFound.With("表情不存在")
	}
	if !e.IsPublic && e.UserID != "" && e.UserID != viewerID {
		return nil, errcode.ErrNotFound.With("表情不存在")
	}
	return e, nil
}

// List 表情列表。grouped=true 时按分类分组返回。
func (s *EmojiService) List(q model.EmojiQuery, viewerID string) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.emojis.List(s.DB, q, viewerID)
	if err != nil {
		return nil, err
	}
	if q.Grouped != nil && *q.Grouped {
		return &ListResult{PageData: groupByCategory(list)}, nil
	}
	return &ListResult{PageData: pageData(list, total, q.Page, q.Limit)}, nil
}

// groupByCategory 按分类聚合成 {分类: [表情]}（对齐原 grouped 语义）。
func groupByCategory(list []model.Emoji) map[string][]model.Emoji {
	out := make(map[string][]model.Emoji)
	for _, e := range list {
		key := e.Category
		if key == "" {
			key = "未分类"
		}
		out[key] = append(out[key], e)
	}
	return out
}

// Categories 分类列表。
func (s *EmojiService) Categories() ([]string, error) {
	return s.emojis.ListCategories(s.DB)
}

// Popular 热门表情。
func (s *EmojiService) Popular(limit int) ([]model.Emoji, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return s.emojis.ListPopular(s.DB, limit)
}

// Recent 最近添加（含本人私有）。
func (s *EmojiService) Recent(userID string, limit int) ([]model.Emoji, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return s.emojis.ListRecent(s.DB, userID, limit)
}

// Use 增加使用次数。
func (s *EmojiService) Use(id string) error {
	e, err := s.emojis.FindByID(s.DB, id)
	if err != nil {
		return err
	}
	if e == nil {
		return errcode.ErrNotFound.With("表情不存在")
	}
	return s.emojis.IncUseCount(s.DB, id)
}

// ---- 收藏 ----

// FavoriteAdd 收藏（幂等：已收藏直接成功）。
func (s *EmojiService) FavoriteAdd(userID, emojiID string) error {
	if _, err := s.Get(emojiID, userID); err != nil {
		return err
	}
	exists, err := s.favorites.Exists(s.DB, userID, emojiID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.favorites.Create(s.DB, &model.EmojiFavorite{UserID: userID, EmojiID: emojiID})
}

// FavoriteRemove 取消收藏。
func (s *EmojiService) FavoriteRemove(userID, emojiID string) error {
	return s.favorites.Delete(s.DB, userID, emojiID)
}

// Favorites 我的收藏列表（返回表情本体）。
func (s *EmojiService) Favorites(userID string, page, limit int) (*ListResult, error) {
	page, limit = normPage(page, limit)
	favs, total, err := s.favorites.ListByUser(s.DB, userID, page, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(favs))
	for _, f := range favs {
		ids = append(ids, f.EmojiID)
	}
	list := make([]model.Emoji, 0, len(ids))
	if len(ids) > 0 {
		if err := s.DB.Where("id IN ?", ids).Find(&list).Error; err != nil {
			return nil, err
		}
	}
	return &ListResult{PageData: pageData(list, total, page, limit)}, nil
}

// ---- 表情包 ----

// PackCreate 创建表情包。
func (s *EmojiService) PackCreate(userID string, in model.EmojiPackInput) (*model.EmojiPack, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.With("表情包名称不能为空")
	}
	p := &model.EmojiPack{
		Name:   *in.Name,
		UserID: userID,
		Status: model.EmojiStatusActive,
	}
	applyEmojiPackInput(p, in)
	if err := s.packs.Create(s.DB, p); err != nil {
		return nil, err
	}
	return p, nil
}

// PackUpdate 更新表情包。
func (s *EmojiService) PackUpdate(userID, id string, in model.EmojiPackInput) (*model.EmojiPack, error) {
	p, err := s.PackGet(id, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.With("只能操作自己的表情包")
	}
	applyEmojiPackInput(p, in)
	if err := s.packs.Save(s.DB, p); err != nil {
		return nil, err
	}
	return p, nil
}

// PackDelete 删除表情包（连带清除包内条目）。
func (s *EmojiService) PackDelete(userID, id string) error {
	p, err := s.PackGet(id, userID)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return errcode.ErrForbidden.With("只能操作自己的表情包")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pack_id = ?", id).Delete(&model.EmojiPackItem{}).Error; err != nil {
			return err
		}
		return s.packs.Delete(tx, id)
	})
}

// PackGet 表情包详情（非公开仅本人可见）。
func (s *EmojiService) PackGet(id, viewerID string) (*model.EmojiPack, error) {
	p, err := s.packs.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errcode.ErrNotFound.With("表情包不存在")
	}
	if !p.IsPublic && p.UserID != viewerID {
		return nil, errcode.ErrNotFound.With("表情包不存在")
	}
	return p, nil
}

// EmojiPackDetail 表情包详情（含条目内表情）。
type EmojiPackDetail struct {
	Pack   model.EmojiPack `json:"pack"`
	Emojis []model.Emoji   `json:"emojis"`
}

// PackDetail 表情包详情 + 内容。
func (s *EmojiService) PackDetail(id, viewerID string) (*EmojiPackDetail, error) {
	p, err := s.PackGet(id, viewerID)
	if err != nil {
		return nil, err
	}
	items, err := s.items.ListByPack(s.DB, id)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	pos := make(map[string]int, len(items))
	for i, it := range items {
		ids = append(ids, it.EmojiID)
		pos[it.EmojiID] = i
	}
	emojis := make([]model.Emoji, 0, len(ids))
	if len(ids) > 0 {
		var found []model.Emoji
		if err := s.DB.Where("id IN ?", ids).Find(&found).Error; err != nil {
			return nil, err
		}
		// 按包内 sort 顺序重排
		emojis = make([]model.Emoji, len(items))
		idx := 0
		for _, e := range found {
			if i, ok := pos[e.ID]; ok && emojis[i].ID == "" {
				emojis[i] = e
				idx++
			}
		}
		// 去掉未命中的空位
		compact := make([]model.Emoji, 0, idx)
		for _, e := range emojis {
			if e.ID != "" {
				compact = append(compact, e)
			}
		}
		emojis = compact
	}
	return &EmojiPackDetail{Pack: *p, Emojis: emojis}, nil
}

// PackList 表情包列表（公开 ∪ 自己的）。
func (s *EmojiService) PackList(q model.EmojiPackQuery, viewerID string) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.packs.ListVisible(s.DB, q, viewerID)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, q.Page, q.Limit)}, nil
}

// PackAddEmoji 向表情包添加表情（幂等：已在包内直接成功）。
func (s *EmojiService) PackAddEmoji(userID, packID, emojiID string) (*model.EmojiPackItem, error) {
	p, err := s.PackGet(packID, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.With("只能操作自己的表情包")
	}
	if _, err := s.Get(emojiID, userID); err != nil {
		return nil, err
	}
	exists, err := s.items.Exists(s.DB, packID, emojiID)
	if err != nil {
		return nil, err
	}
	if exists {
		return s.items.FindItem(s.DB, packID, emojiID)
	}
	sort, err := s.items.NextSort(s.DB, packID)
	if err != nil {
		return nil, err
	}
	item := &model.EmojiPackItem{PackID: packID, EmojiID: emojiID, Sort: sort}
	if err := s.items.Create(s.DB, item); err != nil {
		return nil, err
	}
	return item, nil
}

// PackUpdateEmojiSort 调整包内排序。
func (s *EmojiService) PackUpdateEmojiSort(userID, packID, emojiID string, sort int) (*model.EmojiPackItem, error) {
	p, err := s.PackGet(packID, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.With("只能操作自己的表情包")
	}
	item, err := s.items.FindItem(s.DB, packID, emojiID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errcode.ErrNotFound.With("表情不在该表情包中")
	}
	item.Sort = sort
	if err := s.DB.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

// PackRemoveEmoji 从表情包移除表情。
func (s *EmojiService) PackRemoveEmoji(userID, packID, emojiID string) error {
	p, err := s.PackGet(packID, userID)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return errcode.ErrForbidden.With("只能操作自己的表情包")
	}
	return s.items.Delete(s.DB, packID, emojiID)
}

// applyEmojiInput 非 nil 字段写入。
func applyEmojiInput(e *model.Emoji, in model.EmojiInput) {
	if in.Name != nil {
		e.Name = *in.Name
	}
	if in.URL != nil {
		e.URL = *in.URL
	}
	if in.Code != nil {
		e.Code = *in.Code
	}
	if in.Type != nil {
		e.Type = *in.Type
	}
	if in.Category != nil {
		e.Category = *in.Category
	}
	if in.Tags != nil {
		e.Tags = *in.Tags
	}
	if in.IsPublic != nil {
		e.IsPublic = *in.IsPublic
	}
	if in.Width != nil {
		e.Width = *in.Width
	}
	if in.Height != nil {
		e.Height = *in.Height
	}
	if in.FileSize != nil {
		e.FileSize = *in.FileSize
	}
	if in.MimeType != nil {
		e.MimeType = *in.MimeType
	}
	if in.Status != nil {
		e.Status = *in.Status
	}
}

// applyEmojiPackInput 非 nil 字段写入。
func applyEmojiPackInput(p *model.EmojiPack, in model.EmojiPackInput) {
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.CoverURL != nil {
		p.CoverURL = *in.CoverURL
	}
	if in.IsPublic != nil {
		p.IsPublic = *in.IsPublic
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
}

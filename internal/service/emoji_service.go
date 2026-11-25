// 表情包服务：表情 CRUD、收藏、热门/最近/分类、使用计数、表情包与包内条目。
//
// 对齐原 emoji.service.ts + emoji-pack.service.ts。权限口径：
//   - 系统表情（type=system）只有管理端可改；
//   - 用户表情仅创建者可改删。
//
// 上传（POST /emoji/upload）的落盘由上传域负责，本服务只接收已上传好的 URL。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
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

// Create 创建表情。返回对外的 dto.EmojiDto（service 不把 *model.Emoji 当对外类型，
// 否则给表加一列就自动泄漏到 API —— AGENTS「JSON 空值序列化口径」）。
func (s *EmojiService) Create(userID string, in model.EmojiInput, isAdmin bool) (*dto.EmojiDto, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.EmojiNameRequired)
	}
	if in.URL == nil || *in.URL == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.EmojiURLRequired)
	}
	if in.Code != nil && *in.Code != "" {
		exists, err := s.emojis.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if exists != nil {
			return nil, errcode.ErrBadRequest.WithKey(i18n.EmojiCodeExists)
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
	return emojiViewPtr(e), nil
}

// Update 更新表情。
func (s *EmojiService) Update(userID, id string, in model.EmojiInput, isAdmin bool) (*dto.EmojiDto, error) {
	e, err := s.getModel(id, userID)
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
			return nil, errcode.ErrBadRequest.WithKey(i18n.EmojiCodeExists)
		}
	}
	applyEmojiInput(e, in)
	if err := s.emojis.Save(s.DB, e); err != nil {
		return nil, err
	}
	return emojiViewPtr(e), nil
}

// Delete 删除表情。
func (s *EmojiService) Delete(userID, id string, isAdmin bool) error {
	e, err := s.getModel(id, userID)
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
	if reject := emojiEditRejection(e, userID, isAdmin); reject != nil {
		return reject
	}
	return nil
}

// emojiEditRejection 表情编辑/删除的权限判定，返回 nil 表示允许。
//
// 防的 bug：
//   - 普通用户改删系统表情（系统表情是全站资源，只能管理端动）；
//   - 用户改删别人的表情（水平越权：A 传 B 的 emoji id 就能删）。
//     无归属（UserID 为空）的第三方/系统遗留数据仅管理端可动。
func emojiEditRejection(e *model.Emoji, userID string, isAdmin bool) *errcode.Err {
	if e == nil {
		return errcode.ErrNotFound.WithKey(i18n.EmojiNotFound)
	}
	if isAdmin {
		return nil
	}
	if e.Type == model.EmojiTypeSystem {
		return errcode.ErrForbidden.WithKey(i18n.EmojiSystemOnlyAdmin)
	}
	if e.UserID == "" || e.UserID != userID {
		return errcode.ErrForbidden.WithKey(i18n.EmojiNoPermission)
	}
	return nil
}

// Get 表情详情（非公开表情仅本人可见）。对外返回 dto。
func (s *EmojiService) Get(id, viewerID string) (*dto.EmojiDto, error) {
	e, err := s.getModel(id, viewerID)
	if err != nil {
		return nil, err
	}
	return emojiViewPtr(e), nil
}

// getModel 表情详情（内部：归属校验、关联查询需要 model 本体）。
func (s *EmojiService) getModel(id, viewerID string) (*model.Emoji, error) {
	e, err := s.emojis.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.EmojiNotFound)
	}
	if !e.IsPublic && e.UserID != "" && e.UserID != viewerID {
		return nil, errcode.ErrNotFound.WithKey(i18n.EmojiNotFound)
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
	return &ListResult{PageData: pageData(emojiViews(list), total, q.Page, q.Limit)}, nil
}

// groupByCategory 按分类聚合成 {分类: [表情]}（对齐原 grouped 语义）。
func groupByCategory(list []model.Emoji) map[string][]dto.EmojiDto {
	out := make(map[string][]dto.EmojiDto)
	for _, e := range list {
		key := e.Category
		if key == "" {
			// 有意保留中文：这是分组的数据值（响应 data 里作为 map key 返回给前端做
			// 分组归并），不是响应 message。改英文会让老数据里已有的「未分类」分组
			// 与新产生的分组对不上，前端归并结果分裂。不要 i18n 化。
			key = "未分类"
		}
		out[key] = append(out[key], emojiView(e))
	}
	return out
}

// Categories 分类列表。
func (s *EmojiService) Categories() ([]string, error) {
	return s.emojis.ListCategories(s.DB)
}

// Popular 热门表情。
func (s *EmojiService) Popular(limit int) ([]dto.EmojiDto, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	list, err := s.emojis.ListPopular(s.DB, limit)
	if err != nil {
		return nil, err
	}
	return emojiViews(list), nil
}

// Recent 最近添加（含本人私有）。
func (s *EmojiService) Recent(userID string, limit int) ([]dto.EmojiDto, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	list, err := s.emojis.ListRecent(s.DB, userID, limit)
	if err != nil {
		return nil, err
	}
	return emojiViews(list), nil
}

// Use 增加使用次数。
func (s *EmojiService) Use(id string) error {
	e, err := s.emojis.FindByID(s.DB, id)
	if err != nil {
		return err
	}
	if e == nil {
		return errcode.ErrNotFound.WithKey(i18n.EmojiNotFound)
	}
	return s.emojis.IncUseCount(s.DB, id)
}

// ---- 收藏 ----

// FavoriteAdd 收藏（幂等：已收藏直接成功）。
func (s *EmojiService) FavoriteAdd(userID, emojiID string) error {
	if _, err := s.getModel(emojiID, userID); err != nil {
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
	return &ListResult{PageData: pageData(emojiViews(list), total, page, limit)}, nil
}

// ---- 表情包 ----

// PackCreate 创建表情包。
func (s *EmojiService) PackCreate(userID string, in model.EmojiPackInput) (*dto.EmojiPackDto, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.EmojiPackNameRequired)
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
	return emojiPackViewPtr(p), nil
}

// PackUpdate 更新表情包。
func (s *EmojiService) PackUpdate(userID, id string, in model.EmojiPackInput) (*dto.EmojiPackDto, error) {
	p, err := s.getPackModel(id, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.WithKey(i18n.EmojiPackNoPermission)
	}
	applyEmojiPackInput(p, in)
	if err := s.packs.Save(s.DB, p); err != nil {
		return nil, err
	}
	return emojiPackViewPtr(p), nil
}

// PackDelete 删除表情包（连带清除包内条目）。
func (s *EmojiService) PackDelete(userID, id string) error {
	p, err := s.getPackModel(id, userID)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return errcode.ErrForbidden.WithKey(i18n.EmojiPackNoPermission)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("pack_id = ?", id).Delete(&model.EmojiPackItem{}).Error; err != nil {
			return err
		}
		return s.packs.Delete(tx, id)
	})
}

// PackGet 表情包详情（非公开仅本人可见）。对外返回 dto。
func (s *EmojiService) PackGet(id, viewerID string) (*dto.EmojiPackDto, error) {
	p, err := s.getPackModel(id, viewerID)
	if err != nil {
		return nil, err
	}
	return emojiPackViewPtr(p), nil
}

// getPackModel 表情包详情（内部：归属校验需要 model 本体）。
func (s *EmojiService) getPackModel(id, viewerID string) (*model.EmojiPack, error) {
	p, err := s.packs.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.EmojiPackNotFound)
	}
	if !p.IsPublic && p.UserID != viewerID {
		return nil, errcode.ErrNotFound.WithKey(i18n.EmojiPackNotFound)
	}
	return p, nil
}

// PackDetail 表情包详情 + 内容（对齐 dto.EmojiPackDetailDto）。
func (s *EmojiService) PackDetail(id, viewerID string) (*dto.EmojiPackDetailDto, error) {
	p, err := s.getPackModel(id, viewerID)
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
	return &dto.EmojiPackDetailDto{Pack: emojiPackView(*p), Emojis: emojiViews(emojis)}, nil
}

// PackList 表情包列表（公开 ∪ 自己的）。
func (s *EmojiService) PackList(q model.EmojiPackQuery, viewerID string) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.packs.ListVisible(s.DB, q, viewerID)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(emojiPackViews(list), total, q.Page, q.Limit)}, nil
}

// PackAddEmoji 向表情包添加表情（幂等：已在包内直接成功）。
func (s *EmojiService) PackAddEmoji(userID, packID, emojiID string) (*dto.EmojiPackItemDto, error) {
	p, err := s.getPackModel(packID, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.WithKey(i18n.EmojiPackNoPermission)
	}
	if _, err := s.getModel(emojiID, userID); err != nil {
		return nil, err
	}
	exists, err := s.items.Exists(s.DB, packID, emojiID)
	if err != nil {
		return nil, err
	}
	if exists {
		item, err := s.items.FindItem(s.DB, packID, emojiID)
		if err != nil || item == nil {
			return nil, err
		}
		return emojiPackItemViewPtr(item), nil
	}
	sort, err := s.items.NextSort(s.DB, packID)
	if err != nil {
		return nil, err
	}
	item := &model.EmojiPackItem{PackID: packID, EmojiID: emojiID, Sort: sort}
	if err := s.items.Create(s.DB, item); err != nil {
		return nil, err
	}
	return emojiPackItemViewPtr(item), nil
}

// PackUpdateEmojiSort 调整包内排序。
func (s *EmojiService) PackUpdateEmojiSort(userID, packID, emojiID string, sort int) (*dto.EmojiPackItemDto, error) {
	p, err := s.getPackModel(packID, userID)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errcode.ErrForbidden.WithKey(i18n.EmojiPackNoPermission)
	}
	item, err := s.items.FindItem(s.DB, packID, emojiID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.EmojiNotInPack)
	}
	item.Sort = sort
	if err := s.DB.Save(item).Error; err != nil {
		return nil, err
	}
	return emojiPackItemViewPtr(item), nil
}

// PackRemoveEmoji 从表情包移除表情。
func (s *EmojiService) PackRemoveEmoji(userID, packID, emojiID string) error {
	p, err := s.getPackModel(packID, userID)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return errcode.ErrForbidden.WithKey(i18n.EmojiPackNoPermission)
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

// ---------- model → dto 转换（对外契约组装层） ----------
//
// 为什么不在 model 上加 json tag 后直接用：service 层不许把 *model.Xxx 当对外
// 返回类型（AGENTS「JSON 空值序列化口径」），否则给表加一列就自动泄漏到 API。
// 所有对外字段都在这里显式列出，加列不会自动出圈。

// emojiView 表情 model → dto。
func emojiView(e model.Emoji) dto.EmojiDto {
	return dto.EmojiDto{
		ID:        e.ID,
		CreatedAt: emojiTimeText(e.CreatedAt),
		UpdatedAt: emojiTimeText(e.UpdatedAt),
		Name:      e.Name,
		URL:       e.URL,
		Code:      e.Code,
		Type:      e.Type,
		UserID:    e.UserID,
		Category:  e.Category,
		Tags:      e.Tags,
		UseCount:  e.UseCount,
		IsPublic:  e.IsPublic,
		Status:    e.Status,
		Width:     e.Width,
		Height:    e.Height,
		FileSize:  e.FileSize,
		MimeType:  e.MimeType,
	}
}

// emojiViewPtr 单个表情 → dto 指针（Create / Update / Get 用）。
func emojiViewPtr(e *model.Emoji) *dto.EmojiDto {
	if e == nil {
		return nil
	}
	v := emojiView(*e)
	return &v
}

// emojiViews 表情切片 → dto 切片。恒返回非 nil 切片（空时 [] 而不是 null）。
func emojiViews(list []model.Emoji) []dto.EmojiDto {
	out := make([]dto.EmojiDto, 0, len(list))
	for _, e := range list {
		out = append(out, emojiView(e))
	}
	return out
}

// emojiPackView 表情包 model → dto。
func emojiPackView(p model.EmojiPack) dto.EmojiPackDto {
	return dto.EmojiPackDto{
		ID:          p.ID,
		CreatedAt:   emojiTimeText(p.CreatedAt),
		UpdatedAt:   emojiTimeText(p.UpdatedAt),
		Name:        p.Name,
		Description: p.Description,
		CoverURL:    p.CoverURL,
		UserID:      p.UserID,
		IsPublic:    p.IsPublic,
		Status:      p.Status,
	}
}

// emojiPackViewPtr 单个表情包 → dto 指针。
func emojiPackViewPtr(p *model.EmojiPack) *dto.EmojiPackDto {
	if p == nil {
		return nil
	}
	v := emojiPackView(*p)
	return &v
}

// emojiPackViews 表情包切片 → dto 切片（空时 []）。
func emojiPackViews(list []model.EmojiPack) []dto.EmojiPackDto {
	out := make([]dto.EmojiPackDto, 0, len(list))
	for _, p := range list {
		out = append(out, emojiPackView(p))
	}
	return out
}

// emojiPackItemView 包内条目 model → dto。
func emojiPackItemView(it model.EmojiPackItem) dto.EmojiPackItemDto {
	return dto.EmojiPackItemDto{
		ID:        it.ID,
		CreatedAt: emojiTimeText(it.CreatedAt),
		UpdatedAt: emojiTimeText(it.UpdatedAt),
		PackID:    it.PackID,
		EmojiID:   it.EmojiID,
		Sort:      it.Sort,
	}
}

// emojiPackItemViewPtr 单个包内条目 → dto 指针。
func emojiPackItemViewPtr(it *model.EmojiPackItem) *dto.EmojiPackItemDto {
	if it == nil {
		return nil
	}
	v := emojiPackItemView(*it)
	return &v
}

// emojiTimeText 时间 → RFC3339 文本（与 dto 其余域的对外时间口径一致）。
func emojiTimeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

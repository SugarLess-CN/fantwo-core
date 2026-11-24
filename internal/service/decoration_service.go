// 装饰品服务：装饰品 CRUD、持有/佩戴、购买/赠送、活动与领取、过期清理。
//
// 对齐原 decoration.service.ts。跨域边界：
//   - 购买需要扣钱包余额，钱包归用户域 —— 这里只声明 WalletDebitor 接口，
//     由装配处（main / 用户域）注入实现；运营域不直接写 wallets 表。
//   - 成就勋章由成就域回调 GrantAchievementBadge 授予（同属运营域内部）。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
)

// WalletDebitor 钱包扣款能力（由用户域实现并注入）。
type WalletDebitor interface {
	// DeductBalance 扣减余额；余额不足返回 errcode.ErrBadRequest 语义的错误。
	DeductBalance(userID string, amount float64, source, description string) error
}

// DecorationService 装饰品业务。
type DecorationService struct {
	DB *gorm.DB

	decorations repository.DecorationRepo
	userDeco    repository.UserDecorationRepo
	activities  repository.DecorationActivityRepo
	progress    repository.UserActivityProgressRepo

	// wallet 钱包扣款（可为 nil：购买端点会明确报「支付通道未接入」）。
	wallet WalletDebitor
}

// NewDecorationService 构造。
func NewDecorationService(db *gorm.DB, wallet WalletDebitor) *DecorationService {
	return &DecorationService{DB: db, wallet: wallet}
}

// SetWallet 运行期注入钱包能力（装配顺序解耦用）。
func (s *DecorationService) SetWallet(w WalletDebitor) { s.wallet = w }

// ---- 装饰品定义 ----

// Create 创建装饰品。
func (s *DecorationService) Create(in model.DecorationInput) (*model.Decoration, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationNameRequired)
	}
	if in.Type == nil || *in.Type == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationTypeRequired)
	}
	d := &model.Decoration{
		Name:         *in.Name,
		Type:         *in.Type,
		Rarity:       model.DecorationRarityCommon,
		ObtainMethod: model.DecorationObtainPurchase,
		Status:       model.DecorationStatusActive,
	}
	applyDecorationInput(d, in)
	if err := s.decorations.Create(s.DB, d); err != nil {
		return nil, err
	}
	return d, nil
}

// Update 更新装饰品。
func (s *DecorationService) Update(id string, in model.DecorationInput) (*model.Decoration, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	applyDecorationInput(d, in)
	if err := s.decorations.Save(s.DB, d); err != nil {
		return nil, err
	}
	return d, nil
}

// Delete 删除装饰品。
func (s *DecorationService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.decorations.Delete(s.DB, id)
}

// Get 装饰品详情。
func (s *DecorationService) Get(id string) (*model.Decoration, error) {
	d, err := s.decorations.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.DecorationNotFound)
	}
	return d, nil
}

// List 装饰品列表。
func (s *DecorationService) List(q model.DecorationQuery) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.decorations.List(s.DB, q)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, q.Page, q.Limit)}, nil
}

// ListResult 分页结果包装（handler 直接塞进统一响应）。
type ListResult struct {
	PageData interface{}
}

// ---- 持有与佩戴 ----

// MyDecorations 我的装饰品列表。
func (s *DecorationService) MyDecorations(userID, decoType string, page, limit int) (*ListResult, error) {
	page, limit = normPage(page, limit)
	list, total, err := s.userDeco.ListByUser(s.DB, userID, decoType, page, limit)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, page, limit)}, nil
}

// UserDecorations 指定用户的装饰品（公开查看）。
func (s *DecorationService) UserDecorations(userID, decoType string, page, limit int) (*ListResult, error) {
	return s.MyDecorations(userID, decoType, page, limit)
}

// MyAchievementBadges 我的成就勋章。
func (s *DecorationService) MyAchievementBadges(userID string, page, limit int) (*ListResult, error) {
	return s.MyDecorations(userID, model.DecorationTypeAchievementBadge, page, limit)
}

// CurrentDecorations 当前佩戴（头像框 / 评论气泡）。
type CurrentDecorations struct {
	AvatarFrame   *model.UserDecoration `json:"avatarFrame"`
	CommentBubble *model.UserDecoration `json:"commentBubble"`
}

// MyCurrent 当前佩戴。
func (s *DecorationService) MyCurrent(userID string) (*CurrentDecorations, error) {
	out := &CurrentDecorations{}
	frame, err := s.userDeco.FindUsingByType(s.DB, userID, model.DecorationTypeAvatarFrame)
	if err != nil {
		return nil, err
	}
	bubble, err := s.userDeco.FindUsingByType(s.DB, userID, model.DecorationTypeCommentBubble)
	if err != nil {
		return nil, err
	}
	out.AvatarFrame = frame
	out.CommentBubble = bubble
	return out, nil
}

// Use 佩戴装饰品（同类型只保留一个佩戴中）。
func (s *DecorationService) Use(userID, decorationID string) (*model.UserDecoration, error) {
	d, err := s.Get(decorationID)
	if err != nil {
		return nil, err
	}
	if d.Status != model.DecorationStatusActive {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationUnavailable)
	}

	ud, err := s.userDeco.FindByUserAndDecoration(s.DB, userID, decorationID)
	if err != nil {
		return nil, err
	}
	if ud == nil {
		// 默认装饰品（0 价 + DEFAULT）可直接装备
		if d.ObtainMethod == model.DecorationObtainDefault && d.Price == 0 {
			ud = &model.UserDecoration{
				UserID:       userID,
				DecorationID: decorationID,
				ObtainMethod: model.DecorationObtainDefault,
				IsPermanent:  true,
			}
			if err := s.userDeco.Create(s.DB, ud); err != nil {
				return nil, err
			}
		} else {
			return nil, errcode.ErrNotFound.WithKey(i18n.DecorationNotOwned)
		}
	}
	if !ud.IsPermanent && ud.ExpiresAt != nil && ud.ExpiresAt.Before(time.Now()) {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationExpired)
	}

	if err := s.userDeco.UnsetType(s.DB, userID, d.Type); err != nil {
		return nil, err
	}
	ud.IsUsing = true
	if err := s.userDeco.Save(s.DB, ud); err != nil {
		return nil, err
	}
	return ud, nil
}

// Unuse 取消佩戴。
func (s *DecorationService) Unuse(userID, decorationID string) (*model.UserDecoration, error) {
	ud, err := s.userDeco.FindByUserAndDecoration(s.DB, userID, decorationID)
	if err != nil {
		return nil, err
	}
	if ud == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.DecorationNotOwned)
	}
	ud.IsUsing = false
	if err := s.userDeco.Save(s.DB, ud); err != nil {
		return nil, err
	}
	return ud, nil
}

// Purchase 购买装饰品（扣钱包余额 → 授予）。
func (s *DecorationService) Purchase(userID, decorationID string) (*model.UserDecoration, error) {
	d, err := s.Get(decorationID)
	if err != nil {
		return nil, err
	}
	existing, err := s.userDeco.FindByUserAndDecoration(s.DB, userID, decorationID)
	if err != nil {
		return nil, err
	}
	if reject := decorationPurchaseRejection(d, existing, time.Now()); reject != nil {
		return nil, reject
	}
	if s.wallet == nil {
		return nil, errcode.ErrInternal.WithKey(i18n.DecorationWalletUnavailable)
	}
	if err := s.wallet.DeductBalance(userID, d.Price, "DECORATION", "购买装饰品："+d.Name); err != nil {
		return nil, err
	}
	return s.grant(userID, d, model.DecorationObtainPurchase, "", "")
}

// decorationPurchaseRejection 购买前置校验：返回 nil 表示可买。
//
// 防的 bug：
//   - 不可购买 / 已下架的装饰品仍被下单（前端缓存过期时会踩到）；
//   - 已持有且未过期（永久或未到期）重复购买导致重复扣款。
//     「过期记录」要允许复购，所以判定必须是「永久 OR 到期时间在未来」，
//     而不是「存在记录即拒绝」。
func decorationPurchaseRejection(d *model.Decoration, existing *model.UserDecoration, now time.Time) *errcode.Err {
	if d == nil {
		return errcode.ErrNotFound.WithKey(i18n.DecorationNotFound)
	}
	if !d.IsPurchasable {
		return errcode.ErrBadRequest.WithKey(i18n.DecorationNotPurchasable)
	}
	if d.Status != model.DecorationStatusActive {
		return errcode.ErrBadRequest.WithKey(i18n.DecorationUnavailable)
	}
	if ownsUnexpired(existing, now) {
		return errcode.ErrBadRequest.WithKey(i18n.DecorationAlreadyOwned)
	}
	return nil
}

// ownsUnexpired 是否「有效持有」：永久，或到期时间仍在未来。
// 过期记录视为未持有（允许重新获取/购买/领取）。
func ownsUnexpired(ud *model.UserDecoration, now time.Time) bool {
	if ud == nil {
		return false
	}
	if ud.IsPermanent {
		return true
	}
	return ud.ExpiresAt != nil && ud.ExpiresAt.After(now)
}

// Gift 赠送装饰品。
func (s *DecorationService) Gift(fromUserID, toUserID, decorationID, message string) (*model.UserDecoration, error) {
	if fromUserID == toUserID {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationCannotGiftSelf)
	}
	d, err := s.Get(decorationID)
	if err != nil {
		return nil, err
	}
	from, err := s.userDeco.FindByUserAndDecoration(s.DB, fromUserID, decorationID)
	if err != nil {
		return nil, err
	}
	if from == nil {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationNotOwned)
	}
	if !ownsUnexpired(from, time.Now()) {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationExpired)
	}
	to, err := s.userDeco.FindByUserAndDecoration(s.DB, toUserID, decorationID)
	if err != nil {
		return nil, err
	}
	if ownsUnexpired(to, time.Now()) {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationRecipientOwned)
	}
	return s.grant(toUserID, d, model.DecorationObtainGift, fromUserID, message)
}

// grant 授予装饰品（已存在过期记录则复用该行）。
func (s *DecorationService) grant(userID string, d *model.Decoration, method, giftFrom, remark string) (*model.UserDecoration, error) {
	var expiresAt *time.Time
	if !d.IsPermanent && d.ValidDays > 0 {
		t := time.Now().AddDate(0, 0, d.ValidDays)
		expiresAt = &t
	}

	ud, err := s.userDeco.FindByUserAndDecoration(s.DB, userID, d.ID)
	if err != nil {
		return nil, err
	}
	if ud == nil {
		ud = &model.UserDecoration{UserID: userID, DecorationID: d.ID}
	}
	ud.ObtainMethod = method
	ud.IsPermanent = d.IsPermanent
	ud.ExpiresAt = expiresAt
	ud.GiftFromUserID = giftFrom
	ud.Remark = remark
	if ud.ID == "" {
		err = s.userDeco.Create(s.DB, ud)
	} else {
		err = s.userDeco.Save(s.DB, ud)
	}
	if err != nil {
		return nil, err
	}
	return ud, nil
}

// GrantAchievementBadge 授予成就勋章（成就域完成成就时回调）。
// 语义对齐原 grantAchievementBadge：先按 achievement_id 找勋章，找不到就建，再授予用户。
func (s *DecorationService) GrantAchievementBadge(userID string, achievement model.Achievement) (*model.Decoration, error) {
	badge, err := s.decorations.FindByAchievement(s.DB, achievement.ID)
	if err != nil {
		return nil, err
	}
	if badge == nil && achievement.RewardDecorationID != "" {
		badge, err = s.decorations.FindByID(s.DB, achievement.RewardDecorationID)
		if err != nil {
			return nil, err
		}
	}
	if badge == nil {
		image := achievement.Icon
		if image == "" {
			image = "/default-achievement-badge.png"
		}
		badge = &model.Decoration{
			Name:          achievement.Name,
			Type:          model.DecorationTypeAchievementBadge,
			Description:   achievement.Description,
			ImageURL:      image,
			PreviewURL:    image,
			Rarity:        achievement.Rarity,
			ObtainMethod:  model.DecorationObtainAchievement,
			IsPurchasable: false,
			Price:         0,
			IsPermanent:   true,
			Status:        model.DecorationStatusActive,
			AchievementID: achievement.ID,
		}
		if err := s.decorations.Create(s.DB, badge); err != nil {
			// 并发下可能被另一请求抢先创建：重新查询一次
			if again, ferr := s.decorations.FindByAchievement(s.DB, achievement.ID); ferr == nil && again != nil {
				badge = again
			} else {
				return nil, err
			}
		} else if achievement.RewardDecorationID == "" {
			achievement.RewardDecorationID = badge.ID
			if err := s.DB.Model(&model.Achievement{}).
				Where("id = ?", achievement.ID).
				Update("reward_decoration_id", badge.ID).Error; err != nil {
				return nil, err
			}
		}
	}

	// 授予（已持有则保持现状）
	existing, err := s.userDeco.FindByUserAndDecoration(s.DB, userID, badge.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if _, err := s.grant(userID, badge, model.DecorationObtainAchievement, "", "完成成就："+achievement.Name); err != nil {
			return nil, err
		}
	}
	return badge, nil
}

// CleanExpired 清理过期装饰品（软删已过期持有记录）。
func (s *DecorationService) CleanExpired() (int64, error) {
	return s.decorations.CleanExpired(s.DB)
}

// ---- 装饰品活动 ----

// CreateActivity 创建活动。
func (s *DecorationService) CreateActivity(in model.DecorationActivityInput) (*model.DecorationActivity, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityNameReq)
	}
	if in.DecorationID == nil || *in.DecorationID == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityRewardReq)
	}
	if _, err := s.Get(*in.DecorationID); err != nil {
		return nil, err
	}
	a := &model.DecorationActivity{
		Name:         *in.Name,
		Type:         model.DecorationActivityCustom,
		DecorationID: *in.DecorationID,
		Status:       model.DecorationActivityStatusActive,
		StartTime:    time.Now(),
		EndTime:      time.Now().AddDate(1, 0, 0),
	}
	applyDecorationActivityInput(a, in)
	if err := s.activities.Create(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateActivity 更新活动。
func (s *DecorationService) UpdateActivity(id string, in model.DecorationActivityInput) (*model.DecorationActivity, error) {
	a, err := s.GetActivity(id)
	if err != nil {
		return nil, err
	}
	applyDecorationActivityInput(a, in)
	if err := s.activities.Save(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteActivity 删除活动。
func (s *DecorationService) DeleteActivity(id string) error {
	if _, err := s.GetActivity(id); err != nil {
		return err
	}
	return s.activities.Delete(s.DB, id)
}

// GetActivity 活动详情。
func (s *DecorationService) GetActivity(id string) (*model.DecorationActivity, error) {
	a, err := s.activities.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errcode.ErrNotFound.WithKey(i18n.DecorationActivityNotFound)
	}
	return a, nil
}

// Activities 活动列表。
func (s *DecorationService) Activities(status, actType string, page, limit int) (*ListResult, error) {
	page, limit = normPage(page, limit)
	list, total, err := s.activities.List(s.DB, status, actType, page, limit)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(list, total, page, limit)}, nil
}

// MyActivityProgress 我的活动进度。
func (s *DecorationService) MyActivityProgress(userID string) ([]model.UserActivityProgress, error) {
	return s.progress.ListByUser(s.DB, userID)
}

// claimableProgress 判断进度是否满足活动要求。
//
// 防的 bug：条件值 0 表示「该项不需要」，若写成 `current < required` 无条件比较，
// 0 要求会被误判为不达标，活动永远领不了奖。
func claimableProgress(a model.DecorationActivity, p model.UserActivityProgress) bool {
	if a.RequiredLikes > 0 && p.CurrentLikes < a.RequiredLikes {
		return false
	}
	if a.RequiredComments > 0 && p.CurrentComments < a.RequiredComments {
		return false
	}
	if a.RequiredShares > 0 && p.CurrentShares < a.RequiredShares {
		return false
	}
	if a.RequiredRecharge > 0 && p.CurrentRecharge < a.RequiredRecharge {
		return false
	}
	if a.RequiredSignInDays > 0 && p.CurrentSignInDays < a.RequiredSignInDays {
		return false
	}
	return true
}

// ClaimActivityReward 领取活动奖励。
func (s *DecorationService) ClaimActivityReward(userID, activityID string) (*model.UserDecoration, error) {
	a, err := s.GetActivity(activityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if a.Status != model.DecorationActivityStatusActive {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityNotActive)
	}
	if !a.EndTime.IsZero() && a.EndTime.Before(now) {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityEnded)
	}

	progress, err := s.progress.FindByUserAndActivity(s.DB, userID, activityID)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityNotJoined)
	}
	if progress.IsRewarded {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityRewardGot)
	}
	if !claimableProgress(*a, *progress) {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityReqNotMet)
	}

	d, err := s.Get(a.DecorationID)
	if err != nil {
		return nil, err
	}
	ud, err := s.grant(userID, d, model.DecorationObtainActivity, "", "完成活动："+a.Name)
	if err != nil {
		return nil, err
	}

	progress.IsCompleted = true
	progress.IsRewarded = true
	progress.CompletedAt = &now
	progress.RewardedAt = &now
	if err := s.progress.Save(s.DB, progress); err != nil {
		return nil, err
	}
	if err := s.activities.IncCounters(s.DB, activityID, 0, 1); err != nil {
		return nil, err
	}
	// 关联活动ID回写
	ud.ActivityID = activityID
	if err := s.userDeco.Save(s.DB, ud); err != nil {
		return nil, err
	}
	return ud, nil
}

// UpdateActivityProgress 推进活动进度（事件驱动入口；不存在则建）。
func (s *DecorationService) UpdateActivityProgress(userID, activityID string, fn func(*model.UserActivityProgress)) error {
	if userID == "" || activityID == "" {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		p, err := s.progress.FindByUserAndActivity(tx, userID, activityID)
		if err != nil {
			return err
		}
		created := false
		if p == nil {
			p = &model.UserActivityProgress{UserID: userID, ActivityID: activityID}
			created = true
		}
		fn(p)
		if err := s.progress.Save(tx, p); err != nil {
			return err
		}
		if created {
			return s.activities.IncCounters(tx, activityID, 1, 0)
		}
		return nil
	})
}

// applyDecorationInput 非 nil 字段写入（PATCH 语义）。
func applyDecorationInput(d *model.Decoration, in model.DecorationInput) {
	if in.Name != nil {
		d.Name = *in.Name
	}
	if in.Type != nil {
		d.Type = *in.Type
	}
	if in.Description != nil {
		d.Description = *in.Description
	}
	if in.ImageURL != nil {
		d.ImageURL = *in.ImageURL
	}
	if in.PreviewURL != nil {
		d.PreviewURL = *in.PreviewURL
	}
	if in.Rarity != nil {
		d.Rarity = *in.Rarity
	}
	if in.BubbleColor != nil {
		d.BubbleColor = *in.BubbleColor
	}
	if in.ObtainMethod != nil {
		d.ObtainMethod = *in.ObtainMethod
	}
	if in.IsPurchasable != nil {
		d.IsPurchasable = *in.IsPurchasable
	}
	if in.Price != nil {
		d.Price = *in.Price
	}
	if in.IsPermanent != nil {
		d.IsPermanent = *in.IsPermanent
	}
	if in.ValidDays != nil {
		d.ValidDays = *in.ValidDays
	}
	if in.Sort != nil {
		d.Sort = *in.Sort
	}
	if in.Status != nil {
		d.Status = *in.Status
	}
}

// applyDecorationActivityInput 非 nil 字段写入（PATCH 语义）。
func applyDecorationActivityInput(a *model.DecorationActivity, in model.DecorationActivityInput) {
	if in.Name != nil {
		a.Name = *in.Name
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if in.Type != nil {
		a.Type = *in.Type
	}
	if in.DecorationID != nil {
		a.DecorationID = *in.DecorationID
	}
	if in.ArticleID != nil {
		a.ArticleID = *in.ArticleID
	}
	if in.RequiredLikes != nil {
		a.RequiredLikes = *in.RequiredLikes
	}
	if in.RequiredComments != nil {
		a.RequiredComments = *in.RequiredComments
	}
	if in.RequiredShares != nil {
		a.RequiredShares = *in.RequiredShares
	}
	if in.RequiredRecharge != nil {
		a.RequiredRecharge = *in.RequiredRecharge
	}
	if in.RequiredSignInDays != nil {
		a.RequiredSignInDays = *in.RequiredSignInDays
	}
	if in.IsPermanent != nil {
		a.IsPermanent = *in.IsPermanent
	}
	if in.ValidDays != nil {
		a.ValidDays = *in.ValidDays
	}
	if in.StartTime != nil {
		a.StartTime = *in.StartTime
	}
	if in.EndTime != nil {
		a.EndTime = *in.EndTime
	}
	if in.Status != nil {
		a.Status = *in.Status
	}
}

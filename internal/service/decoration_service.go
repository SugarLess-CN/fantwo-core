// 装饰品服务：装饰品 CRUD、持有/佩戴、购买/赠送、活动与领取、过期清理。
//
// 对齐原 decoration.service.ts。跨域边界：
//   - 购买需要扣钱包余额，钱包归用户域 —— 这里只声明 WalletDebitor 接口，
//     由装配处（main / 用户域）注入实现；运营域不直接写 wallets 表。
//   - 成就勋章由成就域回调 GrantAchievementBadge 授予（同属运营域内部）。
package service

import (
	"time"

	"errors"
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
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
func (s *DecorationService) Create(in model.DecorationInput) (*dto.DecorationDto, error) {
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
	return decoViewPtr(d), nil
}

// Update 更新装饰品。
func (s *DecorationService) Update(id string, in model.DecorationInput) (*dto.DecorationDto, error) {
	d, err := s.getModel(id)
	if err != nil {
		return nil, err
	}
	applyDecorationInput(d, in)
	if err := s.decorations.Save(s.DB, d); err != nil {
		return nil, err
	}
	return decoViewPtr(d), nil
}

// Delete 删除装饰品。
func (s *DecorationService) Delete(id string) error {
	if _, err := s.getModel(id); err != nil {
		return err
	}
	return s.decorations.Delete(s.DB, id)
}

// Get 装饰品详情。对外返回 dto。
//
// viewerID 为空（未登录/无语境）时 is_owned / is_using 不传，不影响其它字段。
func (s *DecorationService) Get(id, viewerID string) (*dto.DecorationDto, error) {
	d, err := s.getModel(id)
	if err != nil {
		return nil, err
	}
	views := s.shopViews(viewerID, []model.Decoration{*d})
	return &views[0], nil
}

// getModel 装饰品详情（内部：改删与归属校验需要 model 本体）。
func (s *DecorationService) getModel(id string) (*model.Decoration, error) {
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
func (s *DecorationService) List(q model.DecorationQuery, viewerID string) (*ListResult, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	list, total, err := s.decorations.List(s.DB, q)
	if err != nil {
		return nil, err
	}
	return &ListResult{PageData: pageData(s.shopViews(viewerID, list), total, q.Page, q.Limit)}, nil
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
	return &ListResult{PageData: pageData(s.userDecoViews(list), total, page, limit)}, nil
}

// UserDecorations 指定用户的装饰品（公开查看）。
func (s *DecorationService) UserDecorations(userID, decoType string, page, limit int) (*ListResult, error) {
	return s.MyDecorations(userID, decoType, page, limit)
}

// MyAchievementBadges 我的成就勋章。
func (s *DecorationService) MyAchievementBadges(userID string, page, limit int) (*ListResult, error) {
	return s.MyDecorations(userID, model.DecorationTypeAchievementBadge, page, limit)
}

// MyCurrent 当前佩戴。未佩戴的类型整个字段不传（不是 null）。
func (s *DecorationService) MyCurrent(userID string) (*dto.CurrentDecorationsDto, error) {
	out := &dto.CurrentDecorationsDto{}
	frame, err := s.userDeco.FindUsingByType(s.DB, userID, model.DecorationTypeAvatarFrame)
	if err != nil {
		return nil, err
	}
	bubble, err := s.userDeco.FindUsingByType(s.DB, userID, model.DecorationTypeCommentBubble)
	if err != nil {
		return nil, err
	}
	out.AvatarFrame = s.userDecoView(frame)
	out.CommentBubble = s.userDecoView(bubble)
	return out, nil
}

// Use 佩戴装饰品（同类型只保留一个佩戴中）。
func (s *DecorationService) Use(userID, decorationID string) (*dto.UserDecorationDto, error) {
	d, err := s.getModel(decorationID)
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
	return s.userDecoView(ud), nil
}

// Unuse 取消佩戴。
func (s *DecorationService) Unuse(userID, decorationID string) (*dto.UserDecorationDto, error) {
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
	return s.userDecoView(ud), nil
}

// Purchase 购买装饰品（扣钱包余额 → 授予）。
func (s *DecorationService) Purchase(userID, decorationID string) (*dto.UserDecorationDto, error) {
	d, err := s.getModel(decorationID)
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
	// 0 元装饰（免费发放）**不走扣款**：DeductBalance 对 amount<=0 会直接报错
	// 「扣款金额必须大于 0」，原样透出会变成 10007 内部错误 —— 用户点购买永远失败。
	if d.Price > 0 {
		if err := s.wallet.DeductBalance(userID, d.Price, "DECORATION", "购买装饰品："+d.Name); err != nil {
			// DeductBalance 返回的是**普通 error**（不是 *errcode.Err），原样透出会被
			// handler 的 fail() 收敛成 10007「内部错误」，客户端既看不到「余额不足」
			// 也没法引导充值。这里按语义翻译成对应的业务码。
			if errors.Is(err, errInsufficientBalance) {
				return nil, errcode.ErrInsufficientBalance
			}
			return nil, errcode.ErrInternal.WithKey(i18n.DecorationWalletUnavailable)
		}
	}
	ud, err := s.grantModel(userID, d, model.DecorationObtainPurchase, "", "")
	if err != nil {
		return nil, err
	}
	return s.userDecoView(ud), nil
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
func (s *DecorationService) Gift(fromUserID, toUserID, decorationID, message string) (*dto.UserDecorationDto, error) {
	if fromUserID == toUserID {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationCannotGiftSelf)
	}
	d, err := s.getModel(decorationID)
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
	ud, err := s.grantModel(toUserID, d, model.DecorationObtainGift, fromUserID, message)
	if err != nil {
		return nil, err
	}
	return s.userDecoView(ud), nil
}

// grantModel 授予装饰品（已存在过期记录则复用该行）。
//
// 返回 model 而不是 dto：内部还有「授予后继续改行再落库」的调用方
// （ClaimActivityReward 要回写 ActivityID），对外转换由调用方做。
func (s *DecorationService) grantModel(userID string, d *model.Decoration, method, giftFrom, remark string) (*model.UserDecoration, error) {
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
		if _, err := s.grantModel(userID, badge, model.DecorationObtainAchievement, "", "完成成就："+achievement.Name); err != nil {
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
func (s *DecorationService) CreateActivity(in model.DecorationActivityInput) (*dto.DecorationActivityDto, error) {
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityNameReq)
	}
	if in.DecorationID == nil || *in.DecorationID == "" {
		return nil, errcode.ErrBadRequest.WithKey(i18n.DecorationActivityRewardReq)
	}
	if _, err := s.getModel(*in.DecorationID); err != nil {
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
	return decoActivityViewPtr(a), nil
}

// UpdateActivity 更新活动。
func (s *DecorationService) UpdateActivity(id string, in model.DecorationActivityInput) (*dto.DecorationActivityDto, error) {
	a, err := s.getActivityModel(id)
	if err != nil {
		return nil, err
	}
	applyDecorationActivityInput(a, in)
	if err := s.activities.Save(s.DB, a); err != nil {
		return nil, err
	}
	return decoActivityViewPtr(a), nil
}

// DeleteActivity 删除活动。
func (s *DecorationService) DeleteActivity(id string) error {
	if _, err := s.getActivityModel(id); err != nil {
		return err
	}
	return s.activities.Delete(s.DB, id)
}

// GetActivity 活动详情。对外返回 dto。
func (s *DecorationService) GetActivity(id string) (*dto.DecorationActivityDto, error) {
	a, err := s.getActivityModel(id)
	if err != nil {
		return nil, err
	}
	return s.activityViewPtrFor(a), nil
}

// getActivityModel 活动详情（内部：改删与领奖校验需要 model 本体）。
func (s *DecorationService) getActivityModel(id string) (*model.DecorationActivity, error) {
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
	return &ListResult{PageData: pageData(s.activityViews(list), total, page, limit)}, nil
}

// MyActivityProgress 我的活动进度。
func (s *DecorationService) MyActivityProgress(userID string) ([]dto.UserActivityProgressDto, error) {
	list, err := s.progress.ListByUser(s.DB, userID)
	if err != nil {
		return nil, err
	}
	return progressViews(list), nil
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
func (s *DecorationService) ClaimActivityReward(userID, activityID string) (*dto.UserDecorationDto, error) {
	a, err := s.getActivityModel(activityID)
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

	d, err := s.getModel(a.DecorationID)
	if err != nil {
		return nil, err
	}
	ud, err := s.grantModel(userID, d, model.DecorationObtainActivity, "", "完成活动："+a.Name)
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
	return s.userDecoView(ud), nil
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

// ---------- model → dto 转换（对外契约组装层） ----------
//
// service 层不许把 *model.Xxx 当对外返回类型（AGENTS「JSON 空值序列化口径」），
// 否则给表加一列就自动泄漏到 API。所有对外字段在这里显式列出。
//
// 时间字段一律转 *string + omitempty：expiresAt / completedAt / rewardedAt 在库里
// 可为 NULL，传 null 违反 docs/21；传 "" 会让前端 new Date("") 得到 Invalid Date；
// 传 time.Time 零值会输出 0001-01-01 —— 不传是唯一诚实的表示。

// decoView 装饰品 model → dto。
func decoView(d model.Decoration) dto.DecorationDto {
	return dto.DecorationDto{
		ID:            d.ID,
		CreatedAt:     decoTimeText(d.CreatedAt),
		UpdatedAt:     decoTimeText(d.UpdatedAt),
		Name:          d.Name,
		Type:          d.Type,
		Description:   d.Description,
		ImageURL:      d.ImageURL,
		PreviewURL:    d.PreviewURL,
		Rarity:        d.Rarity,
		BubbleColor:   d.BubbleColor,
		ObtainMethod:  d.ObtainMethod,
		IsPurchasable: d.IsPurchasable,
		Price:         d.Price,
		IsPermanent:   d.IsPermanent,
		ValidDays:     d.ValidDays,
		Sort:          d.Sort,
		Status:        d.Status,
		ActivityID:    d.ActivityID,
		AchievementID: d.AchievementID,
	}
}

// decoViewPtr 单个装饰品 → dto 指针。
func decoViewPtr(d *model.Decoration) *dto.DecorationDto {
	if d == nil {
		return nil
	}
	v := decoView(*d)
	return &v
}

// decoViews 装饰品切片 → dto 切片（空时 []）。
func decoViews(list []model.Decoration) []dto.DecorationDto {
	out := make([]dto.DecorationDto, 0, len(list))
	for _, d := range list {
		out = append(out, decoView(d))
	}
	return out
}

// userDecoView 持有记录 model → dto 指针（单条接口用）。
//
// 会按 decoration_id 查一次装饰品定义填入 decoration 嵌套 —— 单条场景（use/unuse/
// purchase/gift/claim）额外一次查询可接受，换来前端不用二次请求画头像框。
func (s *DecorationService) userDecoView(ud *model.UserDecoration) *dto.UserDecorationDto {
	if ud == nil {
		return nil
	}
	v := userDecoBase(ud)
	if d, err := s.decorations.FindByID(s.DB, ud.DecorationID); err == nil && d != nil {
		nested := decoView(*d)
		// 嵌套的定义同样带归属：能出现在「我的装饰品」里的必然已拥有，
		// is_using 直接取这条持有行的状态 —— 零额外查询（数据就在手上）。
		attachOwnership(&nested, *ud, true)
		v.Decoration = &nested
	}
	return &v
}

// userDecoViews 持有记录切片 → dto 切片（列表接口用）。
//
// 嵌套 decoration 用**一次 IN 查询**批量补填，不做 N+1：列表 100 条时是 1 次查询
// 而不是 100 次。补不到的记录整个 decoration 字段不传（不是塞 null）。
func (s *DecorationService) userDecoViews(list []model.UserDecoration) []dto.UserDecorationDto {
	out := make([]dto.UserDecorationDto, 0, len(list))
	if len(list) == 0 {
		return out
	}
	ids := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, ud := range list {
		if ud.DecorationID == "" || seen[ud.DecorationID] {
			continue
		}
		seen[ud.DecorationID] = true
		ids = append(ids, ud.DecorationID)
	}
	decos := make(map[string]model.Decoration, len(ids))
	if len(ids) > 0 && s.DB != nil {
		var found []model.Decoration
		if err := s.DB.Where("id IN ?", ids).Find(&found).Error; err == nil {
			for _, d := range found {
				decos[d.ID] = d
			}
		}
	}
	for _, ud := range list {
		v := userDecoBase(&ud)
		if d, ok := decos[ud.DecorationID]; ok {
			nested := decoView(d)
			// 同 userDecoView：归属直接由持有行得出，不需要再查 user_decorations。
			attachOwnership(&nested, ud, true)
			v.Decoration = &nested
		}
		out = append(out, v)
	}
	return out
}

// userDecoBase 持有记录的基础字段转换（不含 decoration 嵌套，避免 N+1）。
func userDecoBase(ud *model.UserDecoration) dto.UserDecorationDto {
	return dto.UserDecorationDto{
		ID:             ud.ID,
		CreatedAt:      decoTimeText(ud.CreatedAt),
		UpdatedAt:      decoTimeText(ud.UpdatedAt),
		UserID:         ud.UserID,
		DecorationID:   ud.DecorationID,
		ObtainMethod:   ud.ObtainMethod,
		IsPermanent:    ud.IsPermanent,
		IsUsing:        ud.IsUsing,
		GiftFromUserID: ud.GiftFromUserID,
		OrderID:        ud.OrderID,
		ActivityID:     ud.ActivityID,
		Remark:         ud.Remark,
		ExpiresAt:      decoTimePtr(ud.ExpiresAt),
	}
}

// decoActivityView 活动 model → dto。
func decoActivityView(a model.DecorationActivity) dto.DecorationActivityDto {
	return dto.DecorationActivityDto{
		ID:                 a.ID,
		CreatedAt:          decoTimeText(a.CreatedAt),
		UpdatedAt:          decoTimeText(a.UpdatedAt),
		Name:               a.Name,
		Description:        a.Description,
		Type:               a.Type,
		DecorationID:       a.DecorationID,
		ArticleID:          a.ArticleID,
		RequiredLikes:      a.RequiredLikes,
		RequiredComments:   a.RequiredComments,
		RequiredShares:     a.RequiredShares,
		RequiredRecharge:   a.RequiredRecharge,
		RequiredSignInDays: a.RequiredSignInDays,
		IsPermanent:        a.IsPermanent,
		ValidDays:          a.ValidDays,
		StartTime:          decoTimeText(a.StartTime),
		EndTime:            decoTimeText(a.EndTime),
		Status:             a.Status,
		ParticipantCount:   a.ParticipantCount,
		CompletedCount:     a.CompletedCount,
	}
}

// decoActivityViewPtr 单个活动 → dto 指针。
func decoActivityViewPtr(a *model.DecorationActivity) *dto.DecorationActivityDto {
	if a == nil {
		return nil
	}
	v := decoActivityView(*a)
	return &v
}

// decoActivityViews 活动切片 → dto 切片（空时 []）。
func decoActivityViews(list []model.DecorationActivity) []dto.DecorationActivityDto {
	out := make([]dto.DecorationActivityDto, 0, len(list))
	for _, a := range list {
		out = append(out, decoActivityView(a))
	}
	return out
}

// progressView 活动进度 model → dto。
func progressView(p model.UserActivityProgress) dto.UserActivityProgressDto {
	return dto.UserActivityProgressDto{
		ID:                p.ID,
		CreatedAt:         decoTimeText(p.CreatedAt),
		UpdatedAt:         decoTimeText(p.UpdatedAt),
		UserID:            p.UserID,
		ActivityID:        p.ActivityID,
		CurrentLikes:      p.CurrentLikes,
		CurrentComments:   p.CurrentComments,
		CurrentShares:     p.CurrentShares,
		CurrentRecharge:   p.CurrentRecharge,
		CurrentSignInDays: p.CurrentSignInDays,
		IsCompleted:       p.IsCompleted,
		IsRewarded:        p.IsRewarded,
		CompletedAt:       decoTimePtr(p.CompletedAt),
		RewardedAt:        decoTimePtr(p.RewardedAt),
	}
}

// progressViews 活动进度切片 → dto 切片（空时 []）。
func progressViews(list []model.UserActivityProgress) []dto.UserActivityProgressDto {
	out := make([]dto.UserActivityProgressDto, 0, len(list))
	for _, p := range list {
		out = append(out, progressView(p))
	}
	return out
}

// decoTimeText 时间 → RFC3339 文本（零值回 ""，仅用于库里 NOT NULL 的列）。
func decoTimeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// decoTimePtr 时间 → RFC3339 文本指针；nil / 零值保持 nil（配合 omitempty = 整个字段不传）。
func decoTimePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// ---------------------------------------------------------------------------
// 商城视图：归属 / 装备状态 / 解锁门槛（前端交接单第三版 §二.5）
// ---------------------------------------------------------------------------
//
// 性能约定（本文件所有「批量」方法共享同一原则）：
//   查询次数必须与**页大小**无关，只与「有几种实体」有关。
//   一页 20 条装饰品 = 固定 3 条 SQL（本体 + 持有 + 来源活动门槛），不是 1+2*20 条。
//   禁止在 for 循环里查库。

// shopViews 商城/详情视图：装饰品本体 + 当前用户的归属状态 + 活动来源的解锁门槛。
//
// viewerID 为空时不查询持有表（省一次查询），is_owned / is_using 保持 nil（不传）。
func (s *DecorationService) shopViews(viewerID string, list []model.Decoration) []dto.DecorationDto {
	out := make([]dto.DecorationDto, 0, len(list))
	if len(list) == 0 {
		return out
	}
	ids := make([]string, 0, len(list))
	for _, d := range list {
		ids = append(ids, d.ID)
	}
	owned := s.ownedMap(viewerID, ids) // 1 条查询
	limits := s.unlockLimitsMap(ids)   // 1 条查询
	for _, d := range list {
		v := decoView(d)
		attachOwnership(&v, owned[d.ID], viewerID != "")
		if act, ok := limits[d.ID]; ok {
			v.RequiredLikes = intPtr(act.RequiredLikes)
			v.RequiredComments = intPtr(act.RequiredComments)
		}
		out = append(out, v)
	}
	return out
}

// ownedMap 批量取当前用户对一批装饰品的持有记录（1 条 SQL，key = decoration_id）。
//
// 只查未软删、且「仍有效」的持有记录之外不过滤租期：过期记录仍算「曾经拥有」，
// 前端若要区分「已过期」可再结合 expires_at —— 这里只回答「有没有这条持有记录」。
func (s *DecorationService) ownedMap(userID string, decorationIDs []string) map[string]model.UserDecoration {
	out := make(map[string]model.UserDecoration, len(decorationIDs))
	if userID == "" || len(decorationIDs) == 0 || s.DB == nil {
		return out
	}
	var rows []model.UserDecoration
	if err := s.DB.Where("user_id = ? AND decoration_id IN ?", userID, decorationIDs).
		Find(&rows).Error; err != nil {
		return out
	}
	for _, ud := range rows {
		out[ud.DecorationID] = ud
	}
	return out
}

// unlockLimitsMap 批量取「活动来源」装饰品对应的活动门槛（1 条 SQL，key = decoration_id）。
//
// 为什么读活动而不是装饰品表：model.Decoration **没有** required_likes /
// required_comments 这两列（它们只在 decoration_activities 上）。对活动获取的装饰品，
// 活动声明的门槛就是真实解锁条件；同一装饰品可能被多个活动引用，取要求最高的那个
// （先满足最严的门槛才算解锁，语义最保守也最诚实）。
func (s *DecorationService) unlockLimitsMap(decorationIDs []string) map[string]model.DecorationActivity {
	out := make(map[string]model.DecorationActivity, len(decorationIDs))
	if len(decorationIDs) == 0 || s.DB == nil {
		return out
	}
	var acts []model.DecorationActivity
	if err := s.DB.Where("decoration_id IN ?", decorationIDs).Find(&acts).Error; err != nil {
		return out
	}
	for _, a := range acts {
		prev, seen := out[a.DecorationID]
		if !seen || a.RequiredLikes > prev.RequiredLikes ||
			a.RequiredComments > prev.RequiredComments {
			out[a.DecorationID] = a
		}
	}
	return out
}

// attachOwnership 把归属状态写到 dto。hasViewer=false 时保持 nil（字段不传）。
//
// 注意：false 必须**显式出现**，不能被 omitempty 吃掉 —— 这是 *bool 而非 bool 的原因。
func attachOwnership(v *dto.DecorationDto, ud model.UserDecoration, hasViewer bool) {
	if !hasViewer {
		return
	}
	isOwned := ud.ID != "" // 查不到持有记录 = 未拥有
	isUsing := ud.ID != "" && ud.IsUsing
	v.IsOwned = &isOwned
	v.IsUsing = &isUsing
}

// intPtr 取地址（解锁门槛用 *int 表达「该装饰品没有门槛声明」= nil）。
func intPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// 活动视图：内嵌 article / decoration（前端交接单第三版 §二.4）
// ---------------------------------------------------------------------------

// activityViews 活动列表 → dto 列表，批量预取内嵌引用。
//
// 固定 **3 条 SQL**（活动本体 + 文章 IN + 装饰品 IN），与页大小无关。
func (s *DecorationService) activityViews(list []model.DecorationActivity) []dto.DecorationActivityDto {
	out := make([]dto.DecorationActivityDto, 0, len(list))
	if len(list) == 0 {
		return out
	}
	articleIDs := make([]string, 0, len(list))
	decoIDs := make([]string, 0, len(list))
	for _, a := range list {
		if a.ArticleID != "" {
			articleIDs = append(articleIDs, a.ArticleID)
		}
		if a.DecorationID != "" {
			decoIDs = append(decoIDs, a.DecorationID)
		}
	}
	articles := s.articleBriefsMap(articleIDs) // 1 条查询
	decos := s.decoBriefsMap(decoIDs)          // 1 条查询
	for _, a := range list {
		v := decoActivityView(a)
		if br, ok := articles[a.ArticleID]; ok {
			v.Article = &br
		}
		if br, ok := decos[a.DecorationID]; ok {
			v.Decoration = &br
		}
		out = append(out, v)
	}
	return out
}

// activityViewPtrFor 单条活动（详情）→ dto，复用批量实现（两条 IN 查询，各 1 条）。
func (s *DecorationService) activityViewPtrFor(a *model.DecorationActivity) *dto.DecorationActivityDto {
	if a == nil {
		return nil
	}
	views := s.activityViews([]model.DecorationActivity{*a})
	return &views[0]
}

// articleBriefsMap 批量取文章概要（1 条 SQL，key = article_id）。
//
// 只查活动卡片需要的 5 列，不 SELECT *（image 列表是 text 大字段，但只 5 列可接受）。
func (s *DecorationService) articleBriefsMap(ids []string) map[string]dto.DecorationArticleBriefDto {
	out := make(map[string]dto.DecorationArticleBriefDto, len(ids))
	if len(ids) == 0 || s.DB == nil {
		return out
	}
	var rows []model.Article
	if err := s.DB.Select("id, title, cover, images, summary").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, a := range rows {
		out[a.ID] = dto.DecorationArticleBriefDto{
			ID:      a.ID,
			Title:   a.Title,
			Cover:   strOrEmpty(a.Cover),
			Images:  ParseImagesList(a.Images),
			Summary: strOrEmpty(a.Summary),
		}
	}
	return out
}

// decoBriefsMap 批量取装饰品概要（1 条 SQL，key = decoration_id）。
func (s *DecorationService) decoBriefsMap(ids []string) map[string]dto.DecorationBriefDto {
	out := make(map[string]dto.DecorationBriefDto, len(ids))
	if len(ids) == 0 || s.DB == nil {
		return out
	}
	var rows []model.Decoration
	if err := s.DB.Select("id, name, type, image_url, preview_url, rarity, bubble_color").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return out
	}
	for _, d := range rows {
		out[d.ID] = dto.DecorationBriefDto{
			ID:          d.ID,
			Name:        d.Name,
			Type:        d.Type,
			ImageURL:    d.ImageURL,
			PreviewURL:  d.PreviewURL,
			Rarity:      d.Rarity,
			BubbleColor: d.BubbleColor,
		}
	}
	return out
}

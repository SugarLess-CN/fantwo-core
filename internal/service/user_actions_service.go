// Package service 用户域扩展动作：关注 / 签到 / 钱包 / 抽成 / 认证 / 账号注销。
//
// 对齐原 user.service.ts 与 user-certification.service.ts 的对应方法；
// 账号注销三态（任务书 §2）：提交→冷静期 7 天（asynq 延迟任务）→撤销/到期执行。
package service

import (
	"context"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// ---------- 关注关系 ----------

// Follow 关注用户（自关注禁止；关系幂等）。
func (s *UserService) Follow(ctx context.Context, currentID, targetID string) (*dto.FollowResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if currentID == targetID {
		return nil, errcode.ErrFollowSelf
	}
	if _, err := s.d.Users.FindPlainByID(s.d.DB, targetID); IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	var (
		created bool
		err     error
	)
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		created, err = s.d.Users.Follow(tx, currentID, targetID)
		if err != nil || !created {
			return err
		}
		if err := s.d.Users.IncrCounter(tx, targetID, "follower_count", 1); err != nil {
			return err
		}
		return s.d.Users.IncrCounter(tx, currentID, "following_count", 1)
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	// 事务提交后发布双向关注事件（关注者/被关注者各自的成就）。
	if created {
		publishFollowed(currentID, targetID)
	}
	return &dto.FollowResultDto{Following: true, Changed: created}, nil
}

// Unfollow 取关。
func (s *UserService) Unfollow(ctx context.Context, currentID, targetID string) (*dto.FollowResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	var removed bool
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		removed, err = s.d.Users.Unfollow(tx, currentID, targetID)
		if err != nil || !removed {
			return err
		}
		if err := s.d.Users.IncrCounter(tx, targetID, "follower_count", -1); err != nil {
			return err
		}
		return s.d.Users.IncrCounter(tx, currentID, "following_count", -1)
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.FollowResultDto{Following: false, Changed: removed}, nil
}

// FollowStats 粉丝/关注数。
func (s *UserService) FollowStats(ctx context.Context, userID string) (*dto.FollowStatsDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	followers, err := s.d.Users.CountFollowers(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	following, err := s.d.Users.CountFollowing(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.FollowStatsDto{FollowerCount: followers, FollowingCount: following}, nil
}

// FollowerCount 粉丝数量。
func (s *UserService) FollowerCount(ctx context.Context, userID string) (*dto.FollowCountDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	n, err := s.d.Users.CountFollowers(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.FollowCountDto{Count: n}, nil
}

// FollowingCount 关注数量。
func (s *UserService) FollowingCount(ctx context.Context, userID string) (*dto.FollowCountDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	n, err := s.d.Users.CountFollowing(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.FollowCountDto{Count: n}, nil
}

// FollowListInput 关注/粉丝列表入参。
type FollowListInput struct {
	Page    int
	Limit   int
	Keyword string
	// ViewerID 当前登录用户（用于标注 isFollowing）。
	ViewerID string
}

// ListFollowers 粉丝列表。
func (s *UserService) ListFollowers(ctx context.Context, userID string, in FollowListInput) ([]*UserView, int64, *errcode.Err) {
	ids, err := s.d.Users.FollowerIDs(s.d.DB, userID)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	return s.listRelatedUsers(ctx, ids, in)
}

// ListFollowings 关注列表。
func (s *UserService) ListFollowings(ctx context.Context, userID string, in FollowListInput) ([]*UserView, int64, *errcode.Err) {
	ids, err := s.d.Users.FollowingIDs(s.d.DB, userID)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	return s.listRelatedUsers(ctx, ids, in)
}

// listRelatedUsers 关系用户分页。
func (s *UserService) listRelatedUsers(ctx context.Context, ids []string, in FollowListInput) ([]*UserView, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit := normPage(in.Page, in.Limit)
	users, total, err := s.d.Users.ListByIDs(s.d.DB, ids, page, limit, in.Keyword)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	views := make([]*UserView, 0, len(users))
	for i := range users {
		views = append(views, buildUserView(&users[i], true, isMembershipActive(&users[i]), s.d.Cfg))
	}
	// 关注/粉丝列表的每一行都要带 is_followed + 佩戴装扮 —— 否则按钮全显示「未关注」，
	// 用户点一次才准（交接单 §二.3）。批量装配，两次查询覆盖整页。
	s.attachListContext(ctx, views, Actor{UserID: in.ViewerID})
	return views, total, nil
}

// IsFollowing 是否已关注。
func (s *UserService) IsFollowing(ctx context.Context, currentID, targetID string) (*dto.IsFollowingDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	ok, err := s.d.Users.IsFollowing(s.d.DB, currentID, targetID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.IsFollowingDto{Following: ok}, nil
}

// ---------- 签到 ----------

// Sign 签到（manual=true 手动；auto=true 为 getProfile 自动签到，已签到不报错）。
func (s *UserService) Sign(ctx context.Context, userID string, auto bool) (*dto.SignInResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	today := dayStart(time.Now())
	if _, err := s.d.Users.FindSignInByDate(s.d.DB, userID, today); err == nil {
		if auto {
			return &dto.SignInResultDto{Signed: true, Already: true}, nil
		}
		return nil, errcode.ErrSignInAlready
	} else if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	// 连续天数：昨天有签到则 +1，否则从 1 开始。
	consecutive := 1
	if latest, err := s.d.Users.LatestSignIn(s.d.DB, userID); err == nil {
		if dayStart(latest.SignInDate).Equal(today.Add(-24 * time.Hour)) {
			consecutive = latest.ConsecutiveDays + 1
		}
	}
	rec := &model.UserSignIn{
		UserID: userID, SignInDate: today,
		ConsecutiveDays: consecutive, IsAuto: auto,
	}
	if err := s.d.Users.CreateSignIn(s.d.DB, rec); err != nil {
		return nil, errcode.ErrInternal
	}
	// 连续 7 的倍数天额外积分（对齐原 performSignIn 奖励语义）。
	if consecutive > 0 && consecutive%7 == 0 {
		_ = s.d.Users.UpdateFields(s.d.DB, userID, map[string]interface{}{
			"points": gorm.Expr("points + ?", 7),
		})
	}

	// ---- 事务已提交：发事件 + 发积分 + 加经验（T-32 事件契约补齐） ----
	// 1) user.dailyLogin：积分活动 DAILY_LOGIN 与「连续登录 7/30 天」成就的触发源。
	publishDailyLogin(userID, consecutive)
	// 2) 按活动规则发放 DAILY_LOGIN 积分（活动未配置/已超限则静默跳过）。
	var grant int
	if pts := NewPointsService(s.d.DB); pts != nil {
		if res, applied, err := pts.TryAddByRule(userID, "DAILY_LOGIN", "sign_in",
			today.Format("2006-01-02")); err != nil {
			s.d.Logger.Warn("签到积分发放失败 user=" + userID)
		} else if applied && res != nil {
			grant = res.Amount
		}
	}
	// 3) 经验 +5，升级时内部会发 user.levelUp。
	_, newLevel, leveledUp, e := s.AddExperience(ctx, AddExperienceInput{
		UserID: userID, Amount: 5, Source: "SIGN_IN",
	})
	if e != nil {
		s.d.Logger.Warn("签到加经验失败 user=" + userID)
	}
	return &dto.SignInResultDto{
		Signed: true, Already: false,
		ConsecutiveDays: consecutive, SignInDate: today.Format("2006-01-02"),
		PointsGranted: grant, Level: newLevel, LeveledUp: leveledUp,
	}, nil
}

// SignRecords 签到记录（近 days 天）。
func (s *UserService) SignRecords(ctx context.Context, userID string, days int) ([]*dto.SignInRecordDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if days <= 0 {
		days = 30
	}
	since := dayStart(time.Now()).AddDate(0, 0, -days+1)
	list, err := s.d.Users.ListSignIns(s.d.DB, userID, since)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	out := make([]*dto.SignInRecordDto, 0, len(list))
	for _, r := range list {
		out = append(out, &dto.SignInRecordDto{
			SignInDate:      r.SignInDate.Format("2006-01-02"),
			ConsecutiveDays: r.ConsecutiveDays,
			IsAuto:          r.IsAuto,
		})
	}
	return out, nil
}

// SignStats 签到统计。
func (s *UserService) SignStats(ctx context.Context, userID string) (*dto.SignInStatsDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	total, err := s.d.Users.CountSignIns(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	out := &dto.SignInStatsDto{TotalDays: total}
	if latest, err := s.d.Users.LatestSignIn(s.d.DB, userID); err == nil {
		out.ConsecutiveDays = latest.ConsecutiveDays
		out.LastSignInDate = latest.SignInDate.Format("2006-01-02")
		out.SignedToday = dayStart(latest.SignInDate).Equal(dayStart(time.Now()))
	}
	return out, nil
}

// ---------- 钱包 ----------

// WalletBalance 余额。
func (s *UserService) WalletBalance(ctx context.Context, userID string) (*dto.WalletBalanceDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	u, err := s.d.Users.FindPlainByID(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.WalletBalanceDto{Balance: u.Wallet}, nil
}

// WalletTransactions 交易记录分页（type 过滤）。
func (s *UserService) WalletTransactions(ctx context.Context, userID, txType string, page, limit int) ([]model.WalletTransaction, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	q := s.d.DB.Model(&model.WalletTransaction{}).Where("user_id = ?", userID)
	if txType != "" {
		q = q.Where("type = ?", txType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, errcode.ErrInternal
	}
	var list []model.WalletTransaction
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, errcode.ErrInternal
	}
	return list, total, nil
}

// WalletStatistics 收支统计。
func (s *UserService) WalletStatistics(ctx context.Context, userID string) (*dto.WalletStatisticsDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	type agg struct {
		Type  string  `gorm:"column:type"`
		Total float64 `gorm:"column:total"`
		Count int64   `gorm:"column:cnt"`
	}
	var rows []agg
	if err := s.d.DB.Model(&model.WalletTransaction{}).
		Select("type, SUM(amount) AS total, COUNT(*) AS cnt").
		Where("user_id = ?", userID).Group("type").Scan(&rows).Error; err != nil {
		return nil, errcode.ErrInternal
	}
	byType := make(map[string]dto.WalletTxSummaryDto, len(rows))
	for _, r := range rows {
		byType[r.Type] = dto.WalletTxSummaryDto{Total: r.Total, Count: r.Count}
	}
	balance, _ := s.WalletBalance(ctx, userID)
	res := &dto.WalletStatisticsDto{ByType: byType}
	if balance != nil {
		res.Balance = balance.Balance
	}
	return res, nil
}

// WithdrawInput 提现入参。
type WithdrawInput struct {
	Amount   float64                `json:"amount"`
	BankInfo map[string]interface{} `json:"bank_info"`
}

// Withdraw 钱包提现：扣款 + 流水（事务）。
func (s *UserService) Withdraw(ctx context.Context, userID string, in WithdrawInput) (*dto.WithdrawResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Amount <= 0 {
		return nil, errcode.ErrInvalidParam
	}
	var (
		before, after float64
	)
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		u, err := s.d.Users.FindPlainByID(tx, userID)
		if err != nil {
			return err
		}
		before = u.Wallet
		if before < in.Amount {
			return gorm.ErrRecordNotFound
		}
		rows, err := s.d.Users.IncrWallet(tx, userID, -in.Amount)
		if err != nil {
			return err
		}
		if rows == 0 {
			return gorm.ErrRecordNotFound
		}
		after = before - in.Amount
		desc := "Wallet withdrawal"
		return tx.Create(&model.WalletTransaction{
			UserID: userID, Type: model.WalletTxWithdraw, Amount: -in.Amount,
			BalanceBefore: before, BalanceAfter: after, Description: &desc,
		}).Error
	}); err != nil {
		if IsNotFound(err) {
			return nil, errcode.ErrInsufficientBalance
		}
		return nil, errcode.ErrInternal
	}
	return &dto.WithdrawResultDto{Success: true, Balance: after, Amount: in.Amount}, nil
}

// ---------- 抽成 ----------

// CommissionConfig 当前用户抽成配置。
func (s *UserService) CommissionConfig(ctx context.Context, userID string) (*dto.CommissionConfigDto, *errcode.Err) {
	p, e := s.GetConfig(ctx, userID)
	if e != nil {
		return nil, e
	}
	return &dto.CommissionConfigDto{
		ArticleCommissionRate:    p.ArticleCommissionRate,
		MembershipCommissionRate: p.MembershipCommissionRate,
		ProductCommissionRate:    p.ProductCommissionRate,
		ServiceCommissionRate:    p.ServiceCommissionRate,
		EnableCustomCommission:   p.EnableCustomCommission,
	}, nil
}

// SetCommissionInput 设置抽成配置（POST /user/commission/config）。
type SetCommissionInput struct {
	ArticleCommissionRate    float64 `json:"article_commission_rate"`
	MembershipCommissionRate float64 `json:"membership_commission_rate"`
	ProductCommissionRate    float64 `json:"product_commission_rate"`
	ServiceCommissionRate    float64 `json:"service_commission_rate"`
	EnableCustomCommission   bool    `json:"enable_custom_commission"`
	Remark                   string  `json:"remark"`
}

// SetCommission 管理员/本人设置抽成配置。
func (s *UserService) SetCommission(ctx context.Context, userID string, in SetCommissionInput) *errcode.Err {
	if _, e := s.GetConfig(ctx, userID); e != nil {
		return e
	}
	fields := map[string]interface{}{
		"article_commission_rate":    in.ArticleCommissionRate,
		"membership_commission_rate": in.MembershipCommissionRate,
		"product_commission_rate":    in.ProductCommissionRate,
		"service_commission_rate":    in.ServiceCommissionRate,
		"enable_custom_commission":   in.EnableCustomCommission,
	}
	if in.Remark != "" {
		fields["remark"] = in.Remark
	}
	if err := s.d.Users.UpdateProfile(s.d.DB, userID, fields); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// CalculateCommission 抽成金额计算（amount × 对应费率）。
func (s *UserService) CalculateCommission(ctx context.Context, userID string, amount float64, bizType string) (*dto.CommissionResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if amount <= 0 || bizType == "" {
		return nil, errcode.ErrInvalidParam
	}
	p, e := s.GetConfig(ctx, userID)
	if e != nil {
		return nil, e
	}
	rate := 0.0
	switch bizType {
	case "article":
		rate = p.ArticleCommissionRate
	case "membership":
		rate = p.MembershipCommissionRate
	case "product":
		rate = p.ProductCommissionRate
	case "service":
		rate = p.ServiceCommissionRate
	default:
		return nil, errcode.ErrInvalidParam.WithKey(i18n.UserCommissionTypeUnknown)
	}
	commission := amount * rate / 100
	return &dto.CommissionResultDto{
		Amount: amount, Rate: rate, Type: bizType,
		Commission: commission, Income: amount - commission,
	}, nil
}

// ---------- 账号注销（三态 + 7 天冷静期） ----------

// RequestDeletion 提交注销（status→1，投递 7 天延迟任务）。
type RequestDeletionInput struct {
	Reason string `json:"reason"`
}

// RequestDeletion 提交账号注销申请。
func (s *UserService) RequestDeletion(ctx context.Context, userID string, in RequestDeletionInput) (*dto.DeletionRequestResultDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if rec, err := s.d.Users.FindDeletion(s.d.DB, userID); err == nil &&
		rec.Status == model.DeletionStatusPending {
		return nil, errcode.ErrDeletionPending
	}
	scheduled := time.Now().AddDate(0, 0, AccountDeletionCoolingDays)
	rec := &model.AccountDeletion{
		UserID: userID, Status: model.DeletionStatusPending,
		Reason: nilIfEmpty(in.Reason), ScheduledAt: scheduled,
	}
	if err := s.d.Users.UpsertDeletion(s.d.DB, rec); err != nil {
		return nil, errcode.ErrInternal
	}
	// 冷却期任务入 asynq 延迟队列（无 Redis 时仅靠定时扫描 DueDeletions 兜底）。
	if s.d.Queue != nil {
		payload, _ := asynqJSON(map[string]interface{}{"userId": userID})
		task := asynq.NewTask(TypeAccountDeletion, payload)
		if _, err := s.d.Queue.Enqueue(task, asynq.ProcessIn(AccountDeletionCoolingDays*24*time.Hour)); err != nil {
			s.d.Logger.Warn("account deletion enqueue failed")
		}
	}
	return &dto.DeletionRequestResultDto{
		Status:          model.DeletionStatusPending,
		ScheduledAt:     scheduled.Format(time.RFC3339),
		CoolingDays:     AccountDeletionCoolingDays,
		DeletionPending: true,
	}, nil
}

// DeletionStatus 查询注销状态。
func (s *UserService) DeletionStatus(ctx context.Context, userID string) (*dto.DeletionStatusDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	rec, err := s.d.Users.FindDeletion(s.d.DB, userID)
	if IsNotFound(err) || err == nil && rec.Status == model.DeletionStatusCanceled {
		return &dto.DeletionStatusDto{Status: model.DeletionStatusNone, DeletionPending: false}, nil
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	out := &dto.DeletionStatusDto{
		Status:          rec.Status,
		DeletionPending: rec.Status == model.DeletionStatusPending,
		ScheduledAt:     rec.ScheduledAt.Format(time.RFC3339),
	}
	if rec.Reason != nil {
		out.Reason = *rec.Reason
	}
	return out, nil
}

// CancelDeletion 撤销注销（status→0 语义：清除待注销）。
func (s *UserService) CancelDeletion(ctx context.Context, userID string) (*dto.DeletionStatusDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	rec, err := s.d.Users.FindDeletion(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, errcode.ErrDeletionNotPending
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if rec.Status != model.DeletionStatusPending {
		return nil, errcode.ErrDeletionNotPending
	}
	now := time.Now()
	if err := s.d.Users.UpsertDeletion(s.d.DB, &model.AccountDeletion{
		UserID: userID, Status: model.DeletionStatusNone,
		ScheduledAt: rec.ScheduledAt, CanceledAt: &now,
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.DeletionStatusDto{Status: model.DeletionStatusNone, DeletionPending: false}, nil
}

// ---------- 认证（官方/创作者） ----------

// certDto 认证视图（返回 dto 类型，字段空值规则见 docs/21）。
func certDto(c *model.UserCertification) *dto.CertificationDto {
	return &dto.CertificationDto{
		ID:               c.ID,
		UserID:           c.UserID,
		Type:             c.Type,
		Status:           c.Status,
		RealName:         strOrEmpty(c.RealName),
		OrganizationName: strOrEmpty(c.OrganizationName),
		Description:      strOrEmpty(c.Description),
		Slogan:           strOrEmpty(c.Slogan),
		ReviewReason:     strOrEmpty(c.ReviewReason),
		ReviewedAt:       timePtrRFC3339(c.ReviewedAt),
		// CertifiedAt 读独立列：只有 APPROVED 有值，拒绝为 NULL、撤销后清空。
		// 与 ReviewedAt 是两个语义（见 model.UserCertification.CertifiedAt 注释），不共用。
		CertifiedAt: timePtrRFC3339(c.CertifiedAt),
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
	}
}

// ApplyCertInput 认证申请入参。
type ApplyCertInput struct {
	Type             string   `json:"type"`
	RealName         string   `json:"real_name"`
	OrganizationName string   `json:"organization_name"`
	Description      string   `json:"description"`
	Slogan           string   `json:"slogan"`
	Materials        []string `json:"materials"`
}

// ApplyCertification 提交认证申请。
func (s *UserService) ApplyCertification(ctx context.Context, userID string, in ApplyCertInput) (*dto.CertificationDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Type != model.CertTypeOfficial && in.Type != model.CertTypeCreator {
		return nil, errcode.ErrInvalidParam
	}
	if latest, err := s.d.Users.FindLatestCert(s.d.DB, userID); err == nil {
		if latest.Status == model.CertStatusPending || latest.Status == model.CertStatusApproved {
			return nil, errcode.ErrCertificationExists
		}
	}
	cert := &model.UserCertification{
		UserID: userID, Type: in.Type, Status: model.CertStatusPending,
		RealName: nilIfEmpty(in.RealName), OrganizationName: nilIfEmpty(in.OrganizationName),
		Description: nilIfEmpty(in.Description), Slogan: nilIfEmpty(in.Slogan),
		Materials: "[]",
	}
	if len(in.Materials) > 0 {
		if raw, err := asynqJSON(in.Materials); err == nil {
			cert.Materials = string(raw)
		}
	}
	if err := s.d.Users.CreateCert(s.d.DB, cert); err != nil {
		return nil, errcode.ErrInternal
	}
	return certDto(cert), nil
}

// LatestCertification 我的最新认证记录（无则 null）。
func (s *UserService) LatestCertification(ctx context.Context, userID string) (interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	cert, err := s.d.Users.FindLatestCert(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return certDto(cert), nil
}

// MyCertifications 我的全部认证记录（GET /user/certification/me）。
//
// 与 LatestCertification（/status）的区别：这里返回列表，供前端展示历史申请。
func (s *UserService) MyCertifications(ctx context.Context, userID string) ([]*dto.CertificationDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	rows, err := s.d.Users.ListCertsByUser(s.d.DB, userID)
	if err != nil {
		s.d.Logger.Error("list certifications", zapErr(err))
		return nil, errcode.ErrInternal
	}
	out := make([]*dto.CertificationDto, 0, len(rows))
	for i := range rows {
		out = append(out, certDto(&rows[i]))
	}
	return out, nil
}

// Identity 当前用户认证身份（official / creator / user）。
func (s *UserService) Identity(ctx context.Context, userID string) (*dto.IdentityDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	return s.identityOf(userID), nil
}

// IdentityOf 指定用户认证身份。
func (s *UserService) IdentityOf(ctx context.Context, userID string) (*dto.IdentityDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	return s.identityOf(userID), nil
}

// identityOf 身份判定（已通过认证的官方优先于创作者）。
func (s *UserService) identityOf(userID string) *dto.IdentityDto {
	cert, err := s.d.Users.FindApprovedCert(s.d.DB, userID)
	if err != nil || cert == nil {
		return &dto.IdentityDto{UserID: userID, Identity: "user", IsOfficial: false, IsCreator: false}
	}
	identity := "creator"
	isOfficial := cert.Type == model.CertTypeOfficial
	if isOfficial {
		identity = "official"
	}
	return &dto.IdentityDto{
		UserID: userID, Identity: identity,
		IsOfficial: isOfficial, IsCreator: !isOfficial,
		Type: cert.Type, Slogan: strOrEmpty(cert.Slogan),
	}
}

// AdminListCerts 管理员分页查认证申请。
func (s *UserService) AdminListCerts(ctx context.Context, page, limit int, status, certType string) ([]*dto.CertificationDto, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	list, total, err := s.d.Users.ListCerts(s.d.DB, page, limit, status, certType)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	out := make([]*dto.CertificationDto, 0, len(list))
	for i := range list {
		out = append(out, certDto(&list[i]))
	}
	return out, total, nil
}

// AuditCertInput 审核入参。
type AuditCertInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// AuditCertification 审核（通过/拒绝）。
func (s *UserService) AuditCertification(ctx context.Context, certID, reviewerID string, in AuditCertInput) (*dto.CertificationDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Status != model.CertStatusApproved && in.Status != model.CertStatusRejected {
		return nil, errcode.ErrInvalidParam
	}
	cert, err := s.d.Users.FindCertByID(s.d.DB, certID)
	if IsNotFound(err) {
		return nil, errcode.ErrCertificationNotFound
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	if cert.Status != model.CertStatusPending {
		return nil, errcode.ErrCertificationNotPending
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status": in.Status, "reviewer_id": reviewerID,
		"review_reason": nilIfEmpty(in.Reason), "reviewed_at": now,
	}
	// certified_at 只在**通过**时写；拒绝必须保持 NULL ——
	// 否则前端「认证于 xxx」会把一次被拒的时间当成通过时间显示。
	var certifiedAt *time.Time
	if in.Status == model.CertStatusApproved {
		certifiedAt = &now
		updates["certified_at"] = now
	}
	if err := s.d.Users.UpdateCert(s.d.DB, certID, updates); err != nil {
		return nil, errcode.ErrInternal
	}
	cert.Status, cert.ReviewedAt, cert.CertifiedAt = in.Status, &now, certifiedAt
	return certDto(cert), nil
}

// RevokeCertification 撤销已通过的认证。
func (s *UserService) RevokeCertification(ctx context.Context, certID, reviewerID, reason string) (*dto.CertificationDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	cert, err := s.d.Users.FindCertByID(s.d.DB, certID)
	if IsNotFound(err) {
		return nil, errcode.ErrCertificationNotFound
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	now := time.Now()
	if err := s.d.Users.UpdateCert(s.d.DB, certID, map[string]interface{}{
		"status": model.CertStatusRevoked, "reviewer_id": reviewerID,
		"review_reason": nilIfEmpty(reason), "reviewed_at": now,
		// 撤销后不再有「认证生效时间」——置 NULL，前端据此不显示「认证于」。
		"certified_at": nil,
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	cert.Status, cert.ReviewedAt, cert.CertifiedAt = model.CertStatusRevoked, &now, nil
	return certDto(cert), nil
}

// ExecuteDueDeletions 扫描到期注销并执行（asynq handler / 定时任务兜底）。
func (s *UserService) ExecuteDueDeletions(ctx context.Context) (int, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return 0, e
	}
	list, err := s.d.Users.DueDeletions(s.d.DB, time.Now())
	if err != nil {
		return 0, errcode.ErrInternal
	}
	count := 0
	for _, rec := range list {
		now := time.Now()
		if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
			if err := s.d.Users.UpdateFields(tx, rec.UserID, map[string]interface{}{
				"status": model.UserStatusInactive,
			}); err != nil {
				return err
			}
			return s.d.Users.UpsertDeletion(tx, &model.AccountDeletion{
				UserID: rec.UserID, Status: model.DeletionStatusCanceled,
				ScheduledAt: rec.ScheduledAt, ExecutedAt: &now,
			})
		}); err != nil {
			s.d.Logger.Warn("execute deletion failed")
			continue
		}
		_ = authtoken.SetLogoutAll(ctx, s.d.AuthTok, rec.UserID, now)
		count++
	}
	return count, nil
}

// authtokenBumpOne 单个用户权限版本递增。
func authtokenBumpOne(ctx context.Context, d *Deps, userID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	authtoken.BumpPermVer(ctx, d.AuthTok, userID)
	// 实时权限缓存也失效（改角色后立即生效，不用等 TTL）
	middleware.InvalidateUserPermissions(userID)
}

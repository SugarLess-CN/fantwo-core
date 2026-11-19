// Package service 用户域扩展动作：关注 / 签到 / 钱包 / 抽成 / 认证 / 账号注销。
//
// 对齐原 user.service.ts 与 user-certification.service.ts 的对应方法；
// 账号注销三态（任务书 §2）：提交→冷静期 7 天（asynq 延迟任务）→撤销/到期执行。
package service

import (
	"context"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// ---------- 关注关系 ----------

// Follow 关注用户（自关注禁止；关系幂等）。
func (s *UserService) Follow(ctx context.Context, currentID, targetID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"following": true, "changed": created}, nil
}

// Unfollow 取关。
func (s *UserService) Unfollow(ctx context.Context, currentID, targetID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"following": false, "changed": removed}, nil
}

// FollowStats 粉丝/关注数。
func (s *UserService) FollowStats(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"followerCount": followers, "followingCount": following}, nil
}

// FollowerCount 粉丝数量。
func (s *UserService) FollowerCount(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	n, err := s.d.Users.CountFollowers(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{"count": n}, nil
}

// FollowingCount 关注数量。
func (s *UserService) FollowingCount(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	n, err := s.d.Users.CountFollowing(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{"count": n}, nil
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
		views = append(views, buildUserView(&users[i], true, isMembershipActive(&users[i])))
	}
	return views, total, nil
}

// IsFollowing 是否已关注。
func (s *UserService) IsFollowing(ctx context.Context, currentID, targetID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	ok, err := s.d.Users.IsFollowing(s.d.DB, currentID, targetID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{"following": ok}, nil
}

// ---------- 签到 ----------

// Sign 签到（manual=true 手动；auto=true 为 getProfile 自动签到，已签到不报错）。
func (s *UserService) Sign(ctx context.Context, userID string, auto bool) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	today := dayStart(time.Now())
	if _, err := s.d.Users.FindSignInByDate(s.d.DB, userID, today); err == nil {
		if auto {
			return map[string]interface{}{"signed": true, "already": true}, nil
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
	return map[string]interface{}{
		"signed": true, "already": false,
		"consecutiveDays": consecutive, "signInDate": today.Format("2006-01-02"),
		"pointsGranted": grant, "level": newLevel, "leveledUp": leveledUp,
	}, nil
}

// SignRecords 签到记录（近 days 天）。
func (s *UserService) SignRecords(ctx context.Context, userID string, days int) ([]map[string]interface{}, *errcode.Err) {
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
	out := make([]map[string]interface{}, 0, len(list))
	for _, r := range list {
		out = append(out, map[string]interface{}{
			"signInDate":      r.SignInDate.Format("2006-01-02"),
			"consecutiveDays": r.ConsecutiveDays,
			"isAuto":          r.IsAuto,
		})
	}
	return out, nil
}

// SignStats 签到统计。
func (s *UserService) SignStats(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	total, err := s.d.Users.CountSignIns(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{"totalDays": total, "consecutiveDays": 0, "signedToday": false}
	if latest, err := s.d.Users.LatestSignIn(s.d.DB, userID); err == nil {
		out["consecutiveDays"] = latest.ConsecutiveDays
		out["lastSignInDate"] = latest.SignInDate.Format("2006-01-02")
		out["signedToday"] = dayStart(latest.SignInDate).Equal(dayStart(time.Now()))
	}
	return out, nil
}

// ---------- 钱包 ----------

// WalletBalance 余额。
func (s *UserService) WalletBalance(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"balance": u.Wallet}, nil
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
func (s *UserService) WalletStatistics(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	byType := make(map[string]map[string]interface{}, len(rows))
	for _, r := range rows {
		byType[r.Type] = map[string]interface{}{"total": r.Total, "count": r.Count}
	}
	balance, _ := s.WalletBalance(ctx, userID)
	return map[string]interface{}{"byType": byType, "balance": balance["balance"]}, nil
}

// WithdrawInput 提现入参。
type WithdrawInput struct {
	Amount   float64                `json:"amount"`
	BankInfo map[string]interface{} `json:"bankInfo"`
}

// Withdraw 钱包提现：扣款 + 流水（事务）。
func (s *UserService) Withdraw(ctx context.Context, userID string, in WithdrawInput) (map[string]interface{}, *errcode.Err) {
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
		desc := "钱包提现"
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
	return map[string]interface{}{"success": true, "balance": after, "amount": in.Amount}, nil
}

// ---------- 抽成 ----------

// CommissionConfig 当前用户抽成配置。
func (s *UserService) CommissionConfig(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	p, e := s.GetConfig(ctx, userID)
	if e != nil {
		return nil, e
	}
	return map[string]interface{}{
		"articleCommissionRate":    p.ArticleCommissionRate,
		"membershipCommissionRate": p.MembershipCommissionRate,
		"productCommissionRate":    p.ProductCommissionRate,
		"serviceCommissionRate":    p.ServiceCommissionRate,
		"enableCustomCommission":   p.EnableCustomCommission,
	}, nil
}

// SetCommissionInput 设置抽成配置（POST /user/commission/config）。
type SetCommissionInput struct {
	ArticleCommissionRate    float64 `json:"articleCommissionRate"`
	MembershipCommissionRate float64 `json:"membershipCommissionRate"`
	ProductCommissionRate    float64 `json:"productCommissionRate"`
	ServiceCommissionRate    float64 `json:"serviceCommissionRate"`
	EnableCustomCommission   bool    `json:"enableCustomCommission"`
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
func (s *UserService) CalculateCommission(ctx context.Context, userID string, amount float64, bizType string) (map[string]interface{}, *errcode.Err) {
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
		return nil, errcode.ErrInvalidParam.With("未知的抽成类型")
	}
	commission := amount * rate / 100
	return map[string]interface{}{
		"amount": amount, "rate": rate, "type": bizType,
		"commission": commission, "income": amount - commission,
	}, nil
}

// ---------- 账号注销（三态 + 7 天冷静期） ----------

// RequestDeletion 提交注销（status→1，投递 7 天延迟任务）。
type RequestDeletionInput struct {
	Reason string `json:"reason"`
}

// RequestDeletion 提交账号注销申请。
func (s *UserService) RequestDeletion(ctx context.Context, userID string, in RequestDeletionInput) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{
		"status":          model.DeletionStatusPending,
		"scheduledAt":     scheduled,
		"coolingDays":     AccountDeletionCoolingDays,
		"deletionPending": true,
	}, nil
}

// DeletionStatus 查询注销状态。
func (s *UserService) DeletionStatus(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	rec, err := s.d.Users.FindDeletion(s.d.DB, userID)
	if IsNotFound(err) || err == nil && rec.Status == model.DeletionStatusCanceled {
		return map[string]interface{}{"status": model.DeletionStatusNone, "deletionPending": false}, nil
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	out := map[string]interface{}{
		"status":          rec.Status,
		"deletionPending": rec.Status == model.DeletionStatusPending,
		"scheduledAt":     rec.ScheduledAt,
	}
	if rec.Reason != nil {
		out["reason"] = *rec.Reason
	}
	return out, nil
}

// CancelDeletion 撤销注销（status→0 语义：清除待注销）。
func (s *UserService) CancelDeletion(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"status": model.DeletionStatusNone, "deletionPending": false}, nil
}

// ---------- 认证（官方/创作者） ----------

// certView 认证视图。
func certView(c *model.UserCertification) map[string]interface{} {
	return map[string]interface{}{
		"id": c.ID, "userId": c.UserID, "type": c.Type, "status": c.Status,
		"realName": c.RealName, "organizationName": c.OrganizationName,
		"description": c.Description, "slogan": c.Slogan,
		"reviewReason": c.ReviewReason, "reviewedAt": c.ReviewedAt,
		"createdAt": c.CreatedAt,
	}
}

// ApplyCertInput 认证申请入参。
type ApplyCertInput struct {
	Type             string   `json:"type"`
	RealName         string   `json:"realName"`
	OrganizationName string   `json:"organizationName"`
	Description      string   `json:"description"`
	Slogan           string   `json:"slogan"`
	Materials        []string `json:"materials"`
}

// ApplyCertification 提交认证申请。
func (s *UserService) ApplyCertification(ctx context.Context, userID string, in ApplyCertInput) (map[string]interface{}, *errcode.Err) {
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
	return certView(cert), nil
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
	return certView(cert), nil
}

// Identity 当前用户认证身份（official / creator / user）。
func (s *UserService) Identity(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	return s.identityOf(userID), nil
}

// IdentityOf 指定用户认证身份。
func (s *UserService) IdentityOf(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	return s.identityOf(userID), nil
}

// identityOf 身份判定（已通过认证的官方优先于创作者）。
func (s *UserService) identityOf(userID string) map[string]interface{} {
	cert, err := s.d.Users.FindApprovedCert(s.d.DB, userID)
	if err != nil || cert == nil {
		return map[string]interface{}{"userId": userID, "identity": "user", "isOfficial": false, "isCreator": false}
	}
	identity := "creator"
	isOfficial := cert.Type == model.CertTypeOfficial
	if isOfficial {
		identity = "official"
	}
	return map[string]interface{}{
		"userId": userID, "identity": identity,
		"isOfficial": isOfficial, "isCreator": !isOfficial,
		"type": cert.Type, "slogan": cert.Slogan,
	}
}

// AdminListCerts 管理员分页查认证申请。
func (s *UserService) AdminListCerts(ctx context.Context, page, limit int, status, certType string) ([]map[string]interface{}, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	list, total, err := s.d.Users.ListCerts(s.d.DB, page, limit, status, certType)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	out := make([]map[string]interface{}, 0, len(list))
	for i := range list {
		out = append(out, certView(&list[i]))
	}
	return out, total, nil
}

// AuditCertInput 审核入参。
type AuditCertInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// AuditCertification 审核（通过/拒绝）。
func (s *UserService) AuditCertification(ctx context.Context, certID, reviewerID string, in AuditCertInput) (map[string]interface{}, *errcode.Err) {
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
	if err := s.d.Users.UpdateCert(s.d.DB, certID, map[string]interface{}{
		"status": in.Status, "reviewer_id": reviewerID,
		"review_reason": nilIfEmpty(in.Reason), "reviewed_at": now,
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	cert.Status, cert.ReviewedAt = in.Status, &now
	return certView(cert), nil
}

// RevokeCertification 撤销已通过的认证。
func (s *UserService) RevokeCertification(ctx context.Context, certID, reviewerID, reason string) (map[string]interface{}, *errcode.Err) {
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
	}); err != nil {
		return nil, errcode.ErrInternal
	}
	cert.Status, cert.ReviewedAt = model.CertStatusRevoked, &now
	return certView(cert), nil
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
		_ = authtoken.SetLogoutAll(ctx, s.d.Redis, rec.UserID, now)
		count++
	}
	return count, nil
}

// authtokenBumpOne 单个用户权限版本递增。
func authtokenBumpOne(ctx context.Context, d *Deps, userID string) {
	if d.Redis == nil || userID == "" {
		return
	}
	authtoken.BumpPermVer(ctx, d.Redis, userID)
}

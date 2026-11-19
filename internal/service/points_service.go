// 积分服务：账户余额、发放/消费、活动规则、任务进度、流水与统计。
//
// 对齐原 points.service.ts。并发安全策略：
//   - 账户行用 SELECT ... FOR UPDATE 行锁，所有余额变动在同一事务内完成；
//   - 流水只在余额落库后追加，保证 amount/balance 一致。
//
// 与原实现的差异：余额落独立的 points_accounts 表（不写 users.points），
// 消除运营域对用户表列的隐式依赖；响应字段名改为 snake_case 与 Go 侧统一。
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PointsService 积分业务。
type PointsService struct {
	DB *gorm.DB

	accounts   repository.PointsAccountRepo
	activities repository.PointsActivityRepo
	tasks      repository.PointsTaskRecordRepo
	txs        repository.PointsTxRepo
}

// NewPointsService 构造。
func NewPointsService(db *gorm.DB) *PointsService { return &PointsService{DB: db} }

// AddPointsInput 增加积分入参（对齐原 AddPointsDto）。
type AddPointsInput struct {
	Amount      int    `json:"amount" binding:"required"`
	Source      string `json:"source" binding:"required"`
	Description string `json:"description"`
	RelatedType string `json:"relatedType"`
	RelatedID   string `json:"relatedId"`
	ValidDays   int    `json:"validDays"`
}

// SpendPointsInput 消费积分入参（对齐原 SpendPointsDto）。
type SpendPointsInput struct {
	Amount      int    `json:"amount" binding:"required"`
	Source      string `json:"source" binding:"required"`
	Description string `json:"description"`
	RelatedType string `json:"relatedType"`
	RelatedID   string `json:"relatedId"`
}

// PointsChangeResult 积分变动结果（发放/消费共用）。
type PointsChangeResult struct {
	Amount        int                      `json:"amount"`
	CurrentPoints int                      `json:"currentPoints"`
	Transaction   *model.PointsTransaction `json:"transaction"`
}

// ensureAccount 取（必要时创建）用户积分账户并在事务内加行锁。
// OnConflict DoNothing + user_id 唯一索引保证并发下只建一行。
func (s *PointsService) ensureAccount(tx *gorm.DB, userID string) (*model.PointsAccount, error) {
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&model.PointsAccount{UserID: userID}).Error; err != nil {
		return nil, err
	}
	acc, err := s.accounts.FindByUserForUpdate(tx, userID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, errcode.ErrInternal.With("积分账户创建失败")
	}
	return acc, nil
}

// Add 发放积分（事务 + 行锁 + 流水）。
func (s *PointsService) Add(userID string, in AddPointsInput) (*PointsChangeResult, error) {
	if in.Amount <= 0 {
		return nil, errcode.ErrBadRequest.With("积分数量必须大于 0")
	}
	var expiredAt *time.Time
	if in.ValidDays > 0 {
		t := time.Now().AddDate(0, 0, in.ValidDays)
		expiredAt = &t
	}

	var result *PointsChangeResult
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		acc, err := s.ensureAccount(tx, userID)
		if err != nil {
			return err
		}
		acc.Balance += in.Amount
		acc.TotalEarned += int64(in.Amount)
		if err := tx.Save(acc).Error; err != nil {
			return err
		}

		txRow := &model.PointsTransaction{
			UserID:      userID,
			Amount:      in.Amount,
			Balance:     acc.Balance,
			Type:        model.PointsTxEarn,
			Source:      in.Source,
			RelatedType: in.RelatedType,
			RelatedID:   in.RelatedID,
			Description: in.Description,
			ExpiredAt:   expiredAt,
		}
		if err := s.txs.Create(tx, txRow); err != nil {
			return err
		}
		result = &PointsChangeResult{Amount: in.Amount, CurrentPoints: acc.Balance, Transaction: txRow}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Spend 消费积分（余额不足直接失败，不会扣成负数）。
func (s *PointsService) Spend(userID string, in SpendPointsInput) (*PointsChangeResult, error) {
	if in.Amount <= 0 {
		return nil, errcode.ErrBadRequest.With("积分数量必须大于 0")
	}
	var result *PointsChangeResult
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		acc, err := s.ensureAccount(tx, userID)
		if err != nil {
			return err
		}
		if acc.Balance < in.Amount {
			return errcode.ErrBadRequest.With("积分不足")
		}
		acc.Balance -= in.Amount
		acc.TotalSpent += int64(in.Amount)
		if err := tx.Save(acc).Error; err != nil {
			return err
		}

		txRow := &model.PointsTransaction{
			UserID:      userID,
			Amount:      -in.Amount,
			Balance:     acc.Balance,
			Type:        model.PointsTxSpend,
			Source:      in.Source,
			RelatedType: in.RelatedType,
			RelatedID:   in.RelatedID,
			Description: in.Description,
		}
		if err := s.txs.Create(tx, txRow); err != nil {
			return err
		}
		result = &PointsChangeResult{Amount: in.Amount, CurrentPoints: acc.Balance, Transaction: txRow}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// AddByRule 按活动代码发放积分（校验每日/总限额，对齐原 addPointsByRule）。
func (s *PointsService) AddByRule(userID, activityCode, relatedType, relatedID string) (*PointsChangeResult, error) {
	activity, err := s.activities.FindByCode(s.DB, activityCode)
	if err != nil {
		return nil, err
	}
	if activity == nil || !activity.IsActive {
		return nil, errcode.ErrNotFound.With("积分活动不存在")
	}

	if activity.DailyLimit > 0 {
		dayStart := time.Now().Truncate(24 * time.Hour)
		n, err := s.txs.CountTodayBySource(s.DB, userID, activityCode, dayStart)
		if err != nil {
			return nil, err
		}
		if n >= int64(activity.DailyLimit) {
			return nil, errcode.ErrBadRequest.With("已达该活动每日次数上限")
		}
	}
	if activity.TotalLimit > 0 {
		n, err := s.txs.CountBySource(s.DB, userID, activityCode)
		if err != nil {
			return nil, err
		}
		if n >= int64(activity.TotalLimit) {
			return nil, errcode.ErrBadRequest.With("已达该活动总次数上限")
		}
	}

	return s.Add(userID, AddPointsInput{
		Amount:      activity.RewardPoints,
		Source:      activity.Code,
		Description: activity.Name,
		RelatedType: relatedType,
		RelatedID:   relatedID,
		ValidDays:   activity.ValidDays,
	})
}

// TryAddByRule 按活动代码发放积分，用于事件驱动的自动发分（如 order.paid）：
// 活动不存在 / 已停用 / 已达限额时返回 applied=false 且不报错（避免日志噪音）。
func (s *PointsService) TryAddByRule(userID, activityCode, relatedType, relatedID string) (*PointsChangeResult, bool, error) {
	if userID == "" || activityCode == "" {
		return nil, false, nil
	}
	activity, err := s.activities.FindByCode(s.DB, activityCode)
	if err != nil {
		return nil, false, err
	}
	if activity == nil || !activity.IsActive {
		return nil, false, nil
	}
	if activity.DailyLimit > 0 {
		dayStart := time.Now().Truncate(24 * time.Hour)
		n, err := s.txs.CountTodayBySource(s.DB, userID, activityCode, dayStart)
		if err != nil {
			return nil, false, err
		}
		if n >= int64(activity.DailyLimit) {
			return nil, false, nil
		}
	}
	if activity.TotalLimit > 0 {
		n, err := s.txs.CountBySource(s.DB, userID, activityCode)
		if err != nil {
			return nil, false, err
		}
		if n >= int64(activity.TotalLimit) {
			return nil, false, nil
		}
	}
	res, err := s.Add(userID, AddPointsInput{
		Amount:      activity.RewardPoints,
		Source:      activity.Code,
		Description: activity.Name,
		RelatedType: relatedType,
		RelatedID:   relatedID,
		ValidDays:   activity.ValidDays,
	})
	if err != nil {
		return nil, false, err
	}
	return res, true, nil
}

// Balance 查询余额（无账户视为 0）。
func (s *PointsService) Balance(userID string) (int, error) {
	acc, err := s.accounts.FindByUser(s.DB, userID)
	if err != nil {
		return 0, err
	}
	if acc == nil {
		return 0, nil
	}
	return acc.Balance, nil
}

// Account 查询账户（统计用；无账户返回零值）。
func (s *PointsService) Account(userID string) (*model.PointsAccount, error) {
	acc, err := s.accounts.FindByUser(s.DB, userID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return &model.PointsAccount{UserID: userID}, nil
	}
	return acc, nil
}

// Transactions 分页查流水（userID 为空 = 全量，管理端用）。
func (s *PointsService) Transactions(q model.PointsTxQuery) ([]model.PointsTransaction, int64, error) {
	q.Page, q.Limit = normPage(q.Page, q.Limit)
	return s.txs.List(s.DB, q)
}

// ---- 积分活动 ----

// CreateActivity 创建活动（code 唯一，应用层校验）。
func (s *PointsService) CreateActivity(in model.PointsActivityInput) (*model.PointsActivity, error) {
	if in.Code == nil || *in.Code == "" {
		return nil, errcode.ErrBadRequest.With("活动代码不能为空")
	}
	if in.Name == nil || *in.Name == "" {
		return nil, errcode.ErrBadRequest.With("活动名称不能为空")
	}
	exists, err := s.activities.FindByCode(s.DB, *in.Code)
	if err != nil {
		return nil, err
	}
	if exists != nil {
		return nil, errcode.ErrBadRequest.With("活动代码已存在")
	}

	a := &model.PointsActivity{
		Code:         *in.Code,
		Name:         *in.Name,
		Type:         model.PointsActivityInstant,
		TargetCount:  1,
		RewardPoints: 0,
		IsActive:     true,
	}
	applyPointsActivityInput(a, in)
	if err := s.activities.Create(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateActivity 更新活动（code 变更需重查唯一）。
func (s *PointsService) UpdateActivity(id string, in model.PointsActivityInput) (*model.PointsActivity, error) {
	a, err := s.activities.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errcode.ErrNotFound.With("积分活动不存在")
	}
	if in.Code != nil && *in.Code != "" && *in.Code != a.Code {
		other, err := s.activities.FindByCode(s.DB, *in.Code)
		if err != nil {
			return nil, err
		}
		if other != nil {
			return nil, errcode.ErrBadRequest.With("活动代码已存在")
		}
	}
	applyPointsActivityInput(a, in)
	if err := s.activities.Save(s.DB, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteActivity 删除活动（软删）。
func (s *PointsService) DeleteActivity(id string) error {
	a, err := s.activities.FindByID(s.DB, id)
	if err != nil {
		return err
	}
	if a == nil {
		return errcode.ErrNotFound.With("积分活动不存在")
	}
	return s.activities.Delete(s.DB, id)
}

// Activity 活动详情。
func (s *PointsService) Activity(id string) (*model.PointsActivity, error) {
	a, err := s.activities.FindByID(s.DB, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errcode.ErrNotFound.With("积分活动不存在")
	}
	return a, nil
}

// Activities 公开活动列表。
func (s *PointsService) Activities(actType, keyword string) ([]model.PointsActivity, error) {
	return s.activities.ListPublic(s.DB, actType, keyword)
}

// ClaimTaskReward 领取任务奖励（幂等：已领取直接报错）。
func (s *PointsService) ClaimTaskReward(userID, activityID string) (*PointsChangeResult, error) {
	activity, err := s.Activity(activityID)
	if err != nil {
		return nil, err
	}
	record, err := s.tasks.FindByUserAndActivity(s.DB, userID, activityID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errcode.ErrNotFound.With("任务记录不存在")
	}
	if !record.IsCompleted {
		return nil, errcode.ErrBadRequest.With("任务尚未完成")
	}
	if record.IsRewarded {
		return nil, errcode.ErrBadRequest.With("奖励已领取")
	}

	result, err := s.Add(userID, AddPointsInput{
		Amount:      activity.RewardPoints,
		Source:      activity.Code,
		Description: "任务奖励：" + activity.Name,
		ValidDays:   activity.ValidDays,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now()
	record.IsRewarded = true
	record.RewardedAt = &now
	if err := s.tasks.Save(s.DB, record); err != nil {
		return nil, err
	}
	return result, nil
}

// MyTasks 我的任务记录。
func (s *PointsService) MyTasks(userID string) ([]model.PointsTaskRecord, error) {
	return s.tasks.ListByUser(s.DB, userID)
}

// MyTaskProgress 指定活动进度。
func (s *PointsService) MyTaskProgress(userID, activityID string) (*model.PointsTaskRecord, error) {
	r, err := s.tasks.FindByUserAndActivity(s.DB, userID, activityID)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errcode.ErrNotFound.With("任务记录不存在")
	}
	return r, nil
}

// UpdateTaskProgress 推进任务进度（周期性任务；INSTANT 不跟踪）。
func (s *PointsService) UpdateTaskProgress(userID, activityCode string, increment int) error {
	if increment <= 0 {
		increment = 1
	}
	activity, err := s.activities.FindByCode(s.DB, activityCode)
	if err != nil {
		return err
	}
	if activity == nil || !activity.IsActive || activity.Type == model.PointsActivityInstant {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		record, err := s.tasks.FindByUserAndActivity(tx, userID, activity.ID)
		if err != nil {
			return err
		}
		if record == nil {
			record = &model.PointsTaskRecord{
				UserID:       userID,
				ActivityID:   activity.ID,
				TargetCount:  activity.TargetCount,
				RewardPoints: activity.RewardPoints,
			}
		}
		record.CurrentCount += increment
		record.IsCompleted = record.CurrentCount >= activity.TargetCount
		if record.IsCompleted && record.CompletedAt == nil {
			now := time.Now()
			record.CompletedAt = &now
		}
		return s.tasks.Save(tx, record)
	})
}

// PointsStatistics 积分系统统计（管理端）。
type PointsStatistics struct {
	Transactions struct {
		Total  int64 `json:"total"`
		Earned int64 `json:"earned"`
		Spent  int64 `json:"spent"`
	} `json:"transactions"`
	Points struct {
		TotalEarned   int64 `json:"totalEarned"`
		TotalSpent    int64 `json:"totalSpent"`
		InCirculation int64 `json:"inCirculation"`
	} `json:"points"`
	Users struct {
		ActiveUsers int64 `json:"activeUsers"`
	} `json:"users"`
	Activities struct {
		Total  int64 `json:"total"`
		Active int64 `json:"active"`
	} `json:"activities"`
	Tasks struct {
		TotalRecords   int64  `json:"totalRecords"`
		Completed      int64  `json:"completed"`
		Claimed        int64  `json:"claimed"`
		CompletionRate string `json:"completionRate"`
		ClaimRate      string `json:"claimRate"`
	} `json:"tasks"`
	BySource []SourceStat `json:"bySource"`
}

// SourceStat 按来源统计。
type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
	Total  int64  `json:"total"`
}

// Statistics 统计。
func (s *PointsService) Statistics() (*PointsStatistics, error) {
	var st PointsStatistics
	db := s.DB

	if err := db.Model(&model.PointsTransaction{}).Count(&st.Transactions.Total).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.PointsTransaction{}).Where("type = ?", model.PointsTxEarn).
		Count(&st.Transactions.Earned).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.PointsTransaction{}).Where("type = ?", model.PointsTxSpend).
		Count(&st.Transactions.Spent).Error; err != nil {
		return nil, err
	}

	var agg struct {
		Earned int64
		Spent  int64
	}
	if err := db.Model(&model.PointsTransaction{}).
		Select("COALESCE(SUM(CASE WHEN type = 'EARN' THEN amount ELSE 0 END), 0) AS earned, " +
			"COALESCE(SUM(CASE WHEN type = 'SPEND' THEN ABS(amount) ELSE 0 END), 0) AS spent").
		Scan(&agg).Error; err != nil {
		return nil, err
	}
	st.Points.TotalEarned = agg.Earned
	st.Points.TotalSpent = agg.Spent
	if err := db.Model(&model.PointsAccount{}).
		Select("COALESCE(SUM(balance), 0)").Scan(&st.Points.InCirculation).Error; err != nil {
		return nil, err
	}

	if err := db.Model(&model.PointsTransaction{}).
		Select("COUNT(DISTINCT user_id)").Scan(&st.Users.ActiveUsers).Error; err != nil {
		return nil, err
	}

	if err := db.Model(&model.PointsActivity{}).Count(&st.Activities.Total).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.PointsActivity{}).Where("is_active = ?", true).
		Count(&st.Activities.Active).Error; err != nil {
		return nil, err
	}

	if err := db.Model(&model.PointsTaskRecord{}).Count(&st.Tasks.TotalRecords).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.PointsTaskRecord{}).Where("is_completed = ?", true).
		Count(&st.Tasks.Completed).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.PointsTaskRecord{}).Where("is_rewarded = ?", true).
		Count(&st.Tasks.Claimed).Error; err != nil {
		return nil, err
	}
	st.Tasks.CompletionRate = rate(st.Tasks.Completed, st.Tasks.TotalRecords)
	st.Tasks.ClaimRate = rate(st.Tasks.Claimed, st.Tasks.Completed)

	var bySource []SourceStat
	if err := db.Model(&model.PointsTransaction{}).
		Select("source AS source, COUNT(*) AS count, COALESCE(SUM(amount), 0) AS total").
		Where("type = ?", model.PointsTxEarn).
		Group("source").Order("total DESC").Scan(&bySource).Error; err != nil {
		return nil, err
	}
	st.BySource = bySource

	return &st, nil
}

// applyPointsActivityInput 把非 nil 字段写入活动（PATCH 语义）。
func applyPointsActivityInput(a *model.PointsActivity, in model.PointsActivityInput) {
	setStr := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	setInt := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	setStr(&a.Code, in.Code)
	setStr(&a.Name, in.Name)
	setStr(&a.Description, in.Description)
	setStr(&a.Type, in.Type)
	setStr(&a.Icon, in.Icon)
	setStr(&a.Link, in.Link)
	setInt(&a.RewardPoints, in.RewardPoints)
	setInt(&a.TargetCount, in.TargetCount)
	setInt(&a.DailyLimit, in.DailyLimit)
	setInt(&a.TotalLimit, in.TotalLimit)
	setInt(&a.ValidDays, in.ValidDays)
	setInt(&a.Sort, in.Sort)
	if in.IsActive != nil {
		a.IsActive = *in.IsActive
	}
}

// rate 计算百分比字符串（保留两位；分母为 0 返回 "0"）。
func rate(part, total int64) string {
	if total <= 0 {
		return "0"
	}
	return formatFloat2(float64(part) / float64(total) * 100)
}

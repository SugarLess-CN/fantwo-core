// 积分域 model → dto 转换（对外视图的唯一组装点）。
//
// 为什么单独一个文件：转换逻辑是「空值口径」的落地点（docs/21），
// 集中放一处才能被单测直接覆盖（points_dto_test.go 逐字段断言 0 null）。
//
// 三条转换规则：
//   - 字符串：model 的指针/值 → dto 的 string，空即 ""（strOrEmpty）
//   - 时间：*time.Time → *string（RFC3339），nil 保持 nil → omitempty 不传字段
//   - 数组：一律 make 初始化，空即 []
package service

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// toPointsActivityDto 积分活动 → dto（17 字段）。
func toPointsActivityDto(a *model.PointsActivity) *dto.PointsActivityDto {
	if a == nil {
		return nil
	}
	return &dto.PointsActivityDto{
		ID:           a.ID,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    a.UpdatedAt.Format(time.RFC3339),
		Code:         a.Code,
		Name:         a.Name,
		Description:  a.Description,
		Type:         a.Type,
		RewardPoints: a.RewardPoints,
		TargetCount:  a.TargetCount,
		DailyLimit:   a.DailyLimit,
		TotalLimit:   a.TotalLimit,
		ValidDays:    a.ValidDays,
		Icon:         a.Icon,
		Link:         a.Link,
		IsActive:     a.IsActive,
		Sort:         a.Sort,
	}
}

// toPointsTransactionDto 积分流水 → dto（14 字段）。
//
// ExpiredAt 是 *time.Time：nil 时必须保持 nil，靠 omitempty 让字段整个不出现 ——
// 直接透传 model 指针会输出 `"expiredAt": null`（本批要消灭的就是这个）。
func toPointsTransactionDto(t *model.PointsTransaction) dto.PointsTransactionDto {
	return dto.PointsTransactionDto{
		ID:          t.ID,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
		UserID:      t.UserID,
		Amount:      t.Amount,
		Balance:     t.Balance,
		Type:        t.Type,
		Source:      t.Source,
		RelatedType: t.RelatedType,
		RelatedID:   t.RelatedID,
		Description: t.Description,
		ExpiredAt:   timePtrRFC3339(t.ExpiredAt),
		Expired:     t.Expired,
	}
}

// toPointsTaskDto 任务记录 → dto（12 字段；两个时间字段 nil 时不传）。
func toPointsTaskDto(r *model.PointsTaskRecord) *dto.PointsTaskDto {
	if r == nil {
		return nil
	}
	return &dto.PointsTaskDto{
		ID:           r.ID,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    r.UpdatedAt.Format(time.RFC3339),
		UserID:       r.UserID,
		ActivityID:   r.ActivityID,
		CurrentCount: r.CurrentCount,
		TargetCount:  r.TargetCount,
		IsCompleted:  r.IsCompleted,
		CompletedAt:  timePtrRFC3339(r.CompletedAt),
		RewardPoints: r.RewardPoints,
		IsRewarded:   r.IsRewarded,
		RewardedAt:   timePtrRFC3339(r.RewardedAt),
	}
}

// toPointsChangeResult 变动结果转换：把 model 流水收窄为 dto 流水。
//
// 流水指针为 nil 时返回 nil → omitempty 让 transaction 字段整个不传（不是 null）。
func toPointsChangeResult(amount, current int, tx *model.PointsTransaction) *dto.PointsChangeResultDto {
	out := &dto.PointsChangeResultDto{Amount: amount, CurrentPoints: current}
	if tx != nil {
		d := toPointsTransactionDto(tx)
		out.Transaction = &d
	}
	return out
}

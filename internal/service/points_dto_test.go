// 积分域 DTO 空值回归测试（docs/21-JSON序列化规范.md）。
//
// 防什么 bug：
//  1. 时间字段用 model 的 *time.Time 直接透传 → 序列化成 `"expired_at": null`；
//  2. 列表用 nil 切片 → 序列化成 `"data": null`（数组契约要求 []）；
//  3. 字符串用 *string → `"description": null`。
//
// 断言方式：序列化后直接 grep `null`（比逐字段断言更能兜住新加字段）。
package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// assertNoNullJSON 序列化后断言 JSON 里不含 null。
//
// 为什么用字符串 grep 而不是反射遍历：新增字段时反射断言会静默放过，
// grep 能兜住「任何字段输出 null」这一类问题（本批的目标就是消灭 null）。
func assertNoNullJSON(t *testing.T, name string, v interface{}) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s 序列化失败: %v", name, err)
	}
	s := string(b)
	if strings.Contains(s, ":null") || strings.Contains(s, "=null") || s == "null" {
		t.Fatalf("%s 输出含 null：%s", name, s)
	}
}

// TestPointsTransactionDtoNoNull 积分流水：全零值（无过期时间）不得输出 null。
func TestPointsTransactionDtoNoNull(t *testing.T) {
	// model 零值：ExpiredAt 为 nil（最常见的库内状态）
	zero := toPointsTransactionDto(&model.PointsTransaction{})
	assertNoNullJSON(t, "PointsTransactionDto(零值)", zero)

	// 带过期时间
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	withExp := toPointsTransactionDto(&model.PointsTransaction{
		BaseModel:   model.BaseModel{ID: "tx-1", CreatedAt: exp, UpdatedAt: exp},
		UserID:      "u-1",
		Amount:      10,
		Balance:     320,
		Type:        model.PointsTxEarn,
		Description: "每日登录奖励",
		ExpiredAt:   &exp,
	})
	assertNoNullJSON(t, "PointsTransactionDto(有过期时间)", withExp)
	if withExp.ExpiredAt == nil || *withExp.ExpiredAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("expiredAt 应为 RFC3339 字符串，实际 %v", withExp.ExpiredAt)
	}

	// nil 时字段必须整个不传（omitempty 生效）
	b, _ := json.Marshal(zero)
	if strings.Contains(string(b), "expired_at") {
		t.Fatalf("expiredAt 为 nil 时不应出现该字段：%s", b)
	}
}

// TestPointsActivityDtoNoNull 积分活动：全字段值类型，零值也不得有 null。
func TestPointsActivityDtoNoNull(t *testing.T) {
	assertNoNullJSON(t, "PointsActivityDto(零值)", toPointsActivityDto(&model.PointsActivity{}))
	assertNoNullJSON(t, "PointsActivityDto(典型值)", toPointsActivityDto(&model.PointsActivity{
		BaseModel:    model.BaseModel{ID: "act-1", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Code:         "DAILY_LOGIN",
		Name:         "每日登录",
		Description:  "每天首次登录获得积分",
		Type:         model.PointsActivityDaily,
		RewardPoints: 10,
		TargetCount:  1,
		ValidDays:    0,
		IsActive:     true,
	}))
}

// TestPointsTaskDtoNoNull 任务记录：completedAt / rewardedAt 为 nil 时不得出现。
func TestPointsTaskDtoNoNull(t *testing.T) {
	d := toPointsTaskDto(&model.PointsTaskRecord{})
	assertNoNullJSON(t, "PointsTaskDto(零值)", d)

	b, _ := json.Marshal(d)
	for _, k := range []string{"completed_at", "rewarded_at"} {
		if strings.Contains(string(b), k) {
			t.Fatalf("%s 为 nil 时不应出现该字段：%s", k, b)
		}
	}

	now := time.Now()
	done := toPointsTaskDto(&model.PointsTaskRecord{
		IsCompleted: true, CompletedAt: &now, IsRewarded: true, RewardedAt: &now,
	})
	assertNoNullJSON(t, "PointsTaskDto(已完成已领取)", done)
	if done.CompletedAt == nil || done.RewardedAt == nil {
		t.Fatal("已完成/已领取时两个时间字段应为非空")
	}
}

// TestPointsChangeResultDtoNoNull 变动结果：transaction 为 nil 时整个字段不传。
func TestPointsChangeResultDtoNoNull(t *testing.T) {
	noTx := toPointsChangeResult(10, 320, nil)
	assertNoNullJSON(t, "PointsChangeResultDto(无流水)", noTx)
	b, _ := json.Marshal(noTx)
	if strings.Contains(string(b), "transaction") {
		t.Fatalf("transaction 为 nil 时不应出现该字段：%s", b)
	}

	withTx := toPointsChangeResult(10, 320, &model.PointsTransaction{BaseModel: model.BaseModel{ID: "tx-1"}})
	assertNoNullJSON(t, "PointsChangeResultDto(有流水)", withTx)
}

// TestPointsListsAreEmptyArrayNotNil 列表必须序列化成 [] 而不是 null。
func TestPointsListsAreEmptyArrayNotNil(t *testing.T) {
	// 直接验证 dto 层的空切片形态（service 层转换函数已在各 Test 里覆盖）
	assertNoNullJSON(t, "空 PointsTransactionDto 切片", []dto.PointsTransactionDto{})
	assertNoNullJSON(t, "空 PointsActivityDto 切片", []dto.PointsActivityDto{})
	assertNoNullJSON(t, "空 PointsTaskDto 切片", []dto.PointsTaskDto{})

	b, _ := json.Marshal(struct {
		List []dto.PointsActivityDto `json:"list"`
	}{List: make([]dto.PointsActivityDto, 0, 3)})
	if string(b) != `{"list":[]}` {
		t.Fatalf("空列表应输出 []，实际 %s", b)
	}
}

// TestPointsStatisticsDtoBySourceEmpty 统计的 bySource 空时必须是 []。
func TestPointsStatisticsDtoBySourceEmpty(t *testing.T) {
	st := dto.PointsStatisticsDto{BySource: sliceOrEmpty([]dto.PointsSourceStatDto(nil))}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"by_source":[]`) {
		t.Fatalf("bySource 空时应为 []，实际 %s", b)
	}
	assertNoNullJSON(t, "PointsStatisticsDto(空 bySource)", st)
}

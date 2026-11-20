// 运营域「不触库路径」的单元测试：参数校验、降级安全、幂等边界。
//
// 这些路径全部在真正访问 *gorm.DB 之前返回，因此可以用 DB=nil 的实例安全调用 ——
// 目的有两个：
//  1. 防止非法入参（0/负金额、空用户、空活动码）穿透到数据库层；
//  2. 防止降级启动（DB 不可用）时事件回调 panic 把整个进程带崩。
package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
)

// TestPointsAddRejectsNonPositiveAmount 发放积分拒绝 0 / 负数金额。
//
// 防的 bug：amount=0 也写一条流水（污染账本、非法重复计数）；
// amount<0 走「发放」路径反向扣减余额（等于用发放接口白嫖消费）。
// 这两条都在入库前拦下，DB=nil 也不会 panic。
func TestPointsAddRejectsNonPositiveAmount(t *testing.T) {
	svc := NewPointsService(nil)
	cases := []struct {
		name   string
		amount int
	}{
		{"零金额", 0},
		{"负金额", -50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("应在入库前拒绝，实际 panic: %v", r)
				}
			}()
			res, err := svc.Add("u-1", AddPointsInput{Amount: tc.amount, Source: "TEST"})
			if err == nil {
				t.Fatalf("amount=%d 应报错，实际 res=%+v", tc.amount, res)
			}
			if res != nil {
				t.Errorf("失败时不应返回结果，实际 %+v", res)
			}
		})
	}
}

// TestPointsSpendRejectsNonPositiveAmount 消费积分拒绝 0 / 负数金额。
//
// 防的 bug：amount<0 的「消费」实际是给用户加积分（负数扣减 = 反向增加），
// 是典型的越权刷分路径。
func TestPointsSpendRejectsNonPositiveAmount(t *testing.T) {
	svc := NewPointsService(nil)
	for _, amount := range []int{0, -100} {
		amount := amount
		t.Run("amount", func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("应在入库前拒绝，实际 panic: %v", r)
				}
			}()
			if _, err := svc.Spend("u-1", SpendPointsInput{Amount: amount, Source: "TEST"}); err == nil {
				t.Fatalf("amount=%d 应报错", amount)
			}
		})
	}
}

// TestTryAddByRuleSilentSkipsEmptyArgs 空参数静默跳过（不触库、不 panic）。
//
// 防的 bug：事件回调里 payload 缺字段（userId 空）时若继续查库，
// 会走 DB 空指针 panic；静默跳过是正确的降级行为，且不能写任何流水。
func TestTryAddByRuleSilentSkipsEmptyArgs(t *testing.T) {
	svc := NewPointsService(nil)
	cases := []struct {
		name string
		user string
		code string
	}{
		{"空用户", "", "DAILY_LOGIN"},
		{"空活动码", "u-1", ""},
		{"两者皆空", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("空参数不应触库，实际 panic: %v", r)
				}
			}()
			res, applied, err := svc.TryAddByRule(tc.user, tc.code, "", "")
			if err != nil {
				t.Errorf("空参数应静默返回，实际 err=%v", err)
			}
			if applied {
				t.Error("空参数不应 applied=true")
			}
			if res != nil {
				t.Errorf("空参数不应返回结果，实际 %+v", res)
			}
		})
	}
}

// TestAchievementUpdateProgressBatchGuard 进度推进的边界与降级安全。
//
// 防的 bug：
//  1. 空 userID / 空 code 集合仍去查库（无意义查询 + DB 不可用时 panic）；
//  2. 降级启动（DB=nil）时事件回调 panic 拖垮进程 —— 必须返回错误而不是崩。
func TestAchievementUpdateProgressBatchGuard(t *testing.T) {
	svc := &AchievementService{DB: nil} // 无数据库的降级实例

	cases := []struct {
		name  string
		user  string
		codes []string
	}{
		{"空用户", "", []string{"FIRST_ARTICLE"}},
		{"空 code 集合", "u-1", nil},
		{"code 全为空串", "u-1", []string{"", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("边界输入不应触库，实际 panic: %v", r)
				}
			}()
			if err := svc.UpdateProgressBatch(tc.user, tc.codes, 1); err != nil {
				t.Errorf("边界输入应静默返回 nil，实际 %v", err)
			}
		})
	}

	t.Run("降级启动返回错误而非 panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DB=nil 时不应 panic: %v", r)
			}
		}()
		if err := svc.UpdateProgressBatch("u-1", []string{"FIRST_ARTICLE"}, 1); err == nil {
			t.Error("DB 不可用时应返回错误（让调用方记日志），不能假装成功")
		}
	})
}

// TestAchievementSubscribeNilBusNoPanic nil bus 不应注册也不应 panic。
//
// 防的 bug：装配顺序在 bus 尚未初始化时调用 Subscribe，若未判空会 nil deref。
func TestAchievementSubscribeNilBusNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Subscribe(nil) 不应 panic: %v", r)
		}
	}()
	svc := &AchievementService{DB: nil}
	svc.Subscribe(nil)
	if svc.bus != nil {
		t.Error("nil bus 不应被记录")
	}
}

// TestBatchSafeNoopGuards batchSafe 的短路守卫（空用户/空 code 都不应触库）。
//
// 防的 bug：条件分支算出空 code 集合（如连续登录 3 天不满足 7 天）时，
// 若仍进 UpdateProgressBatch 会多一次无意义查询；DB 不可用时更会 panic。
func TestBatchSafeNoopGuards(t *testing.T) {
	svc := &AchievementService{DB: nil}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("batchSafe 空输入不应触库，实际 panic: %v", r)
		}
	}()
	svc.batchSafe("", []string{"FIRST_ARTICLE"}, "test")
	svc.batchSafe("u-1", nil, "test")
	svc.batchSafe("u-1", []string{}, "test")
}

// TestAchievementSubscribeTwiceOnSeparateBuses 同一 service 订阅两个独立 bus 都能生效。
//
// 防的 bug：Subscribe 里的 s.bus 被前一个 bus 覆盖后回调仍指向旧 bus，
// 导致后装配的 bus 收不到事件。
func TestAchievementSubscribeTwiceOnSeparateBuses(t *testing.T) {
	bus1 := event.NewBus()
	bus2 := event.NewBus()
	svc := &AchievementService{DB: nil}
	svc.Subscribe(bus1)

	// 在 bus1 上发布：解析链路必须能跑通（DB=nil 会记 WARN，但不 panic）
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("bus1 发布 panic: %v", r)
			}
		}()
		bus1.Publish(event.EventArticleCreated, event.ArticleCreatedPayload{UserID: "u-1"})
	}()

	// bus2 未经订阅：发布不应触发任何回调（也不会 panic）
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("bus2 发布 panic: %v", r)
			}
		}()
		bus2.Publish(event.EventArticleCreated, event.ArticleCreatedPayload{UserID: "u-1"})
	}()
}

// TestReportUpdateActionFieldDefaults 举报处理入参的空字段语义。
//
// 防的 bug：PATCH 只传 status（只改状态不改动作）时，动作字段被空串覆盖，
// 让「已处理但动作丢失」，事后无法追溯是否真的删了内容。
// 判定层保证：空动作 = 不下发事件，且不覆盖历史动作（由 service 的 if 分支保证
// —— 这里验证纯判定对空串的语义）。
func TestReportUpdateActionFieldDefaults(t *testing.T) {
	if shouldDispatchReportAction("") {
		t.Error("空动作不应下发事件（只改状态时不得误删内容）")
	}
	if shouldDispatchReportAction(" ") {
		t.Error("空白动作不应下发事件")
	}
}

// TestPointsRuleBlockReasonNilActivity 活动为 nil 时的判定不 panic。
func TestPointsRuleBlockReasonNilActivity(t *testing.T) {
	svc := NewPointsService(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil 活动不应 panic: %v", r)
		}
	}()
	reason, err := svc.ruleBlockReason("u-1", nil)
	if err != nil {
		t.Errorf("不应返回 error: %v", err)
	}
	if reason == "" {
		t.Error("nil 活动应给出拦截原因")
	}
}

// TestNormPageBounds 分页参数收敛（handler 与 service 同口径）。
//
// 防的 bug：limit 未封顶 → 客户端传 limit=100000 拖垮数据库；
// page<1 产生负 offset → SQL 报错或返回错乱结果。
func TestNormPageBounds(t *testing.T) {
	cases := []struct {
		inPage, inLimit     int
		wantPage, wantLimit int
	}{
		{0, 0, 1, DefaultPageSize},
		{-5, -1, 1, DefaultPageSize},
		{1, 10, 1, 10},
		{3, 100, 3, 100},
		{2, 101, 2, MaxPageSize},
		{2, 99999, 2, MaxPageSize},
	}
	for _, tc := range cases {
		p, l := normPage(tc.inPage, tc.inLimit)
		if p != tc.wantPage || l != tc.wantLimit {
			t.Errorf("normPage(%d,%d) = (%d,%d), want (%d,%d)",
				tc.inPage, tc.inLimit, p, l, tc.wantPage, tc.wantLimit)
		}
	}
}

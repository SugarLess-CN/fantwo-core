// 积分账本契约测试：并发安全与「余额不为负」。
//
// 用内存替身（memoryLedger）实现 PointsLedger 契约，对 PointsService.Add/Spend
// 做真并发压测 —— 不需要 PG/Redis。
//
// 说明边界：生产实现 GormLedger 的并发安全靠 PostgreSQL 的
// SELECT ... FOR UPDATE 行锁 + 事务串行化，单测覆盖不到；这里验证的是
// ①账本契约本身（两个实现必须共同满足的不变量）与
// ②service 层对账本结果/错误的处理。GormLedger 的等价性由 test/integration 覆盖。
package service

import (
	"sync"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// memoryLedger 内存账本：互斥锁串行化，语义严格按 PointsLedger 契约。
//
// 刻意实现成「正确」的：如果 service 层在扣减判断上有错（比如把余额当成可以透支），
// 契约断言会直接失败；如果并发路径有数据竞争，-race 会报出来。
type memoryLedger struct {
	mu       sync.Mutex
	balances map[string]int
	records  map[string][]model.PointsTransaction
	calls    int
}

func newMemoryLedger() *memoryLedger {
	return &memoryLedger{
		balances: map[string]int{},
		records:  map[string][]model.PointsTransaction{},
	}
}

// Change 见 PointsLedger 契约。
func (l *memoryLedger) Change(userID string, delta int, meta LedgerMeta) (int, *model.PointsTransaction, error) {
	if delta == 0 {
		return 0, nil, errcode.ErrBadRequest.WithKeyf(i18n.CommonInvalidParam, "amount must not be zero")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++

	balance := l.balances[userID]
	if delta < 0 && !balanceSufficient(balance, -delta) {
		return balance, nil, errcode.ErrBadRequest.WithKey(i18n.PointsInsufficient)
	}
	balance += delta
	l.balances[userID] = balance

	row := model.PointsTransaction{
		UserID:      userID,
		Amount:      delta,
		Balance:     balance,
		Type:        model.PointsTxEarn,
		Source:      meta.Source,
		RelatedType: meta.RelatedType,
		RelatedID:   meta.RelatedID,
		Description: meta.Description,
	}
	if delta < 0 {
		row.Type = model.PointsTxSpend
	}
	l.records[userID] = append(l.records[userID], row)
	out := row
	return balance, &out, nil
}

func (l *memoryLedger) balanceOf(userID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.balances[userID]
}

func (l *memoryLedger) recordCount(userID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records[userID])
}

// TestLedgerContractConcurrentSpendNeverNegative 并发扣减：余额绝不为负、成功次数精确。
//
// 防的 bug：并发扣减时「读余额 → 判断 → 扣减」不是原子操作，
// 两个请求都读到 10 就都能扣 10，余额变 -10（积分被透支）；
// 以及失败路径仍写流水（账本与余额对不上）。
func TestLedgerContractConcurrentSpendNeverNegative(t *testing.T) {
	const (
		initial   = 1000
		goroutine = 200
		perSpend  = 10
	)
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)

	// 初始额度
	if _, err := svc.Add("u-1", AddPointsInput{Amount: initial, Source: "INIT"}); err != nil {
		t.Fatalf("初始化积分失败: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	failed := 0
	negativeSeen := false

	for i := 0; i < goroutine; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Spend("u-1", SpendPointsInput{Amount: perSpend, Source: "TEST"})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if res != nil {
					t.Errorf("失败时不应返回结果")
				}
				return
			}
			success++
			if res.CurrentPoints < 0 {
				negativeSeen = true
			}
		}()
	}
	wg.Wait()

	if negativeSeen {
		t.Error("出现负余额 —— 扣减可被透支")
	}
	final := ledger.balanceOf("u-1")
	if final < 0 {
		t.Fatalf("最终余额为负: %d", final)
	}
	if success != initial/perSpend {
		t.Errorf("成功次数 = %d, want %d（成功次数必须精确等于可用额度/单次金额）", success, initial/perSpend)
	}
	if failed != goroutine-success {
		t.Errorf("失败次数 = %d, want %d", failed, goroutine-success)
	}
	if final != initial-success*perSpend {
		t.Errorf("最终余额 = %d, want %d", final, initial-success*perSpend)
	}
	// 流水条数必须等于成功次数（失败不写流水）
	if n := ledger.recordCount("u-1"); n != success+1 { // +1 是初始 Add 那条
		t.Errorf("流水条数 = %d, want %d（失败请求不得写流水）", n, success+1)
	}
}

// TestLedgerContractConcurrentMixed 混合并发：发放与消费交错，恒等式成立。
//
// 防的 bug：Add 与 Spend 走不同代码路径时口径分叉（例如一方忘了更新 TotalEarned/
// TotalSpent，或一方不做余额下界检查）。断言 Σ成功 delta == 最终余额。
func TestLedgerContractConcurrentMixed(t *testing.T) {
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)

	const rounds = 100
	var wg sync.WaitGroup
	var mu sync.Mutex
	deltaSum := 0

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.Add("u-2", AddPointsInput{Amount: 7, Source: "IN"}); err == nil {
				mu.Lock()
				deltaSum += 7
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := svc.Spend("u-2", SpendPointsInput{Amount: 3, Source: "OUT"}); err == nil {
				mu.Lock()
				deltaSum -= 3
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got := ledger.balanceOf("u-2"); got != deltaSum {
		t.Errorf("余额 %d != Σ成功delta %d（账实不符）", got, deltaSum)
	}
	if ledger.balanceOf("u-2") < 0 {
		t.Error("最终余额为负")
	}
}

// TestLedgerContractFlowConsistency 每次成功变动的流水 balance 快照必须单调自洽。
//
// 防的 bug：流水的 balance 字段写成「变动前余额」或「未取最新值」，
// 导致对账时按流水重算余额与账户余额不一致。
func TestLedgerContractFlowConsistency(t *testing.T) {
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)

	steps := []struct {
		add    bool
		amount int
	}{
		{true, 100}, {false, 30}, {true, 5}, {false, 70}, {false, 1},
	}
	for _, st := range steps {
		var err error
		if st.add {
			_, err = svc.Add("u-3", AddPointsInput{Amount: st.amount, Source: "S"})
		} else {
			_, err = svc.Spend("u-3", SpendPointsInput{Amount: st.amount, Source: "S"})
		}
		if err != nil {
			t.Fatalf("步骤 %+v 失败: %v", st, err)
		}
	}

	ledger.mu.Lock()
	records := append([]model.PointsTransaction(nil), ledger.records["u-3"]...)
	ledger.mu.Unlock()

	running := 0
	for i, r := range records {
		running += r.Amount
		if r.Balance != running {
			t.Errorf("第 %d 条流水 balance=%d，按流水累计应为 %d", i, r.Balance, running)
		}
		if r.Type == model.PointsTxEarn && r.Amount <= 0 {
			t.Errorf("EARN 流水金额应为正，实际 %d", r.Amount)
		}
		if r.Type == model.PointsTxSpend && r.Amount >= 0 {
			t.Errorf("SPEND 流水金额应为负，实际 %d", r.Amount)
		}
	}
	if got := ledger.balanceOf("u-3"); got != running {
		t.Errorf("账户余额 %d != 流水累计 %d", got, running)
	}
}

// TestServiceSpendInsufficientKeepsBalance 余额不足时：余额不变、无流水、明确报错。
//
// 防的 bug：余额不足仍扣减成负数（最常见），或扣减失败但流水已写。
func TestServiceSpendInsufficientKeepsBalance(t *testing.T) {
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)

	if _, err := svc.Add("u-4", AddPointsInput{Amount: 50, Source: "IN"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	before := ledger.balanceOf("u-4")
	beforeRecords := ledger.recordCount("u-4")

	res, err := svc.Spend("u-4", SpendPointsInput{Amount: 51, Source: "OUT"})
	if err == nil {
		t.Fatalf("余额不足应报错，实际成功: %+v", res)
	}
	if e, ok := err.(*errcode.Err); !ok || e.Code != errcode.ErrBadRequest.Code {
		t.Errorf("错误类型应为 400 业务错误，实际 %T %v", err, err)
	}
	if got := ledger.balanceOf("u-4"); got != before {
		t.Errorf("失败扣减改变了余额: %d -> %d", before, got)
	}
	if got := ledger.recordCount("u-4"); got != beforeRecords {
		t.Errorf("失败扣减写了流水: %d -> %d", beforeRecords, got)
	}
}

// TestServiceAddSpendResultShape 结果结构与入参回填（handler 直接透传给前端）。
//
// 防的 bug：CurrentPoints 取错（返回变动前余额，前端展示错误），
// 或 Transaction 为 nil 导致响应里 transaction 缺失。
func TestServiceAddSpendResultShape(t *testing.T) {
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)

	addRes, err := svc.Add("u-5", AddPointsInput{
		Amount: 200, Source: "SIGN_IN", Description: "签到",
		RelatedType: "signin", RelatedID: "r-1", ValidDays: 30,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if addRes.Amount != 200 || addRes.CurrentPoints != 200 {
		t.Errorf("add 结果 = %+v, want amount=200 current=200", addRes)
	}
	if addRes.Transaction == nil {
		t.Fatal("add 结果缺 transaction")
	}
	if addRes.Transaction.Type != model.PointsTxEarn {
		t.Errorf("发放流水类型应为 EARN，实际 %s", addRes.Transaction.Type)
	}

	spendRes, err := svc.Spend("u-5", SpendPointsInput{Amount: 80, Source: "BUY"})
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if spendRes.Amount != 80 || spendRes.CurrentPoints != 120 {
		t.Errorf("spend 结果 = %+v, want amount=80 current=120", spendRes)
	}
	if spendRes.Transaction == nil || spendRes.Transaction.Amount != -80 {
		t.Errorf("消费流水金额应为 -80，实际 %+v", spendRes.Transaction)
	}
	if spendRes.Transaction.Type != model.PointsTxSpend {
		t.Errorf("消费流水类型应为 SPEND，实际 %s", spendRes.Transaction.Type)
	}
}

// TestMemoryLedgerZeroDeltaRejected 账本契约：delta=0 必须拒绝。
//
// 防的 bug：0 金额变动写一条无意义流水（账本被污染，统计 COUNT 失真）。
func TestMemoryLedgerZeroDeltaRejected(t *testing.T) {
	ledger := newMemoryLedger()
	if _, _, err := ledger.Change("u-6", 0, LedgerMeta{Source: "X"}); err == nil {
		t.Fatal("delta=0 应被拒绝")
	}
	if n := ledger.recordCount("u-6"); n != 0 {
		t.Errorf("delta=0 不应写流水，实际 %d 条", n)
	}
}

// TestGormLedgerZeroDeltaRejected 生产实现的同一契约（不触库即可验证）。
func TestGormLedgerZeroDeltaRejected(t *testing.T) {
	l := GormLedger{DB: nil}
	if _, _, err := l.Change("u-7", 0, LedgerMeta{}); err == nil {
		t.Fatal("delta=0 应被拒绝（且不应触库）")
	}
}

// TestLedgerDeadlineBudget 并发压测的时间上限保护：账本实现不得因锁竞争退化。
//
// 防的 bug：内存替身若在持锁期间做 IO/日志，200 并发会拖到秒级；
// 这里给一个宽松上限（2s），超过说明实现有锁粒度问题。
func TestLedgerDeadlineBudget(t *testing.T) {
	ledger := newMemoryLedger()
	svc := NewPointsService(nil)
	svc.SetLedger(ledger)
	if _, err := svc.Add("u-8", AddPointsInput{Amount: 10000, Source: "IN"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Spend("u-8", SpendPointsInput{Amount: 1, Source: "T"})
		}()
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("500 次并发扣减耗时 %v，超过 2s 上限（锁粒度过粗）", elapsed)
	}
	if got := ledger.balanceOf("u-8"); got != 10000-500 {
		t.Errorf("余额 = %d, want %d", got, 10000-500)
	}
}

package service

// QQ 机器人发件池状态机与时效窗口的回归测试。
//
// 为什么这三组函数最需要测试：它们是**纯函数**，却是整个需求的核心逻辑 ——
// 　· qqbotPassiveEligible 决定「能不能用被动回复」（60 分钟窗口 / 4 次上限）
// 　· qqbotPlanFor 决定「失败后消息去哪」（留池 / 退避 / 死信）
// 　· qqbotBackoffFor 决定退避节奏
//
// 一旦这里判错，表现是「消息静默丢失」或「用户收到重复消息」——
// 两者在真机上都要等真实用户触发才能发现，回归成本极高。

import (
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/pkg/qqbot"
)

// ---------- 时效窗口 ----------

func TestQqbotPassiveEligible(t *testing.T) {
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	future := now.Add(time.Minute)

	task := func(msgID string, at *time.Time, seq int) *model.QqbotOutbox {
		return &model.QqbotOutbox{ReplyMsgID: msgID, ReplyMsgAt: at, MsgSeq: seq}
	}

	cases := []struct {
		name string
		task *model.QqbotOutbox
		want bool
		why  string
	}{
		{"nil 任务", nil, false, "防御"},
		{"无源消息 ID", task("", ago(time.Minute), 0), false, "没有 msg_id 无法被动回复"},
		{"无到达时间", task("ROBOT1.0_x", nil, 0), false, "不知道窗口起点就不能猜"},
		{"刚收到", task("ROBOT1.0_x", ago(time.Second), 0), true, "窗口内"},
		{"59 分钟", task("ROBOT1.0_x", ago(59*time.Minute), 0), true, "窗口内"},
		{"恰好 60 分钟", task("ROBOT1.0_x", ago(QqbotPassiveWindow), 0), false, "边界：age 必须严格小于窗口"},
		{"61 分钟", task("ROBOT1.0_x", ago(61*time.Minute), 0), false, "已过期"},
		{"未来时间", task("ROBOT1.0_x", &future, 0), false, "时钟异常时不冒进"},
		{"已回 4 次", task("ROBOT1.0_x", ago(time.Minute), QqbotMaxMsgSeq), false, "次数用尽"},
		{"已回 3 次", task("ROBOT1.0_x", ago(time.Minute), QqbotMaxMsgSeq-1), true, "还剩 1 次"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := qqbotPassiveEligible(c.task, now); got != c.want {
				t.Fatalf("qqbotPassiveEligible = %v, want %v（%s）", got, c.want, c.why)
			}
		})
	}
}

// ---------- 失败去向 ----------

func qerr(kind qqbot.ErrorKind, code int) *qqbot.Error {
	return &qqbot.Error{Kind: kind, Code: code, Op: "send_c2c"}
}

func TestQqbotPlanForWindowExpired(t *testing.T) {
	// 窗口过期 → DEFERRED 且**清空 reply_msg_id**。
	// 不清的话下次投递还会拿这个死 msg_id 反复撞 40034005。
	p := qqbotPlanFor(qerr(qqbot.KindWindowExpired, 40034005), 1)
	if p.Status != model.QqbotOutboxDeferred {
		t.Fatalf("Status = %q, want DEFERRED", p.Status)
	}
	if !p.ClearReply {
		t.Fatal("窗口过期必须 ClearReply —— 否则旧 msg_id 会一直撞 40034005")
	}
	if p.Code != 40034005 || p.KindLabel != "window_expired" {
		t.Fatalf("Code/KindLabel = %d/%q", p.Code, p.KindLabel)
	}
}

func TestQqbotPlanForPermanent(t *testing.T) {
	// 永久失败与配置错都必须进 FAILED（重投无意义，要人看见）。
	for _, tc := range []struct {
		kind qqbot.ErrorKind
		code int
		name string
	}{
		{qqbot.KindPermanent, 40054013, "用户拒收"},
		{qqbot.KindPermanent, 40034006, "内容违规"},
		{qqbot.KindConfig, 100007, "appid 无效"},
		{qqbot.KindInvalidArgument, 0, "调用姿势错"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := qqbotPlanFor(qerr(tc.kind, tc.code), 1)
			if p.Status != model.QqbotOutboxFailed {
				t.Fatalf("Status = %q, want FAILED", p.Status)
			}
			if p.ClearReply {
				t.Fatal("FAILED 不该动 reply_msg_id")
			}
		})
	}
}

func TestQqbotPlanForRetryable(t *testing.T) {
	before := time.Now()
	p := qqbotPlanFor(qerr(qqbot.KindRetryable, 40034100), 1)
	if p.Status != model.QqbotOutboxPending {
		t.Fatalf("Status = %q, want PENDING", p.Status)
	}
	if p.NextRetryAt == nil {
		t.Fatal("可重试必须设置 NextRetryAt，否则 worker 会立刻再捞")
	}
	want := before.Add(qqbotBackoffFor(1))
	if d := p.NextRetryAt.Sub(want); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("NextRetryAt 偏差过大: %v", d)
	}
}

// TestQqbotPlanForRetryableExhausted 退避到顶**转 DEFERRED 而不是 FAILED**。
//
// 这条守的是用户明确要求的「消息不从池子出去」：
// 频控/上游抖动不是消息本身的问题，打进死信等于逼运维手动捞。
func TestQqbotPlanForRetryableExhausted(t *testing.T) {
	p := qqbotPlanFor(qerr(qqbot.KindRetryable, 50055002), QqbotMaxAttempts)
	if p.Status != model.QqbotOutboxDeferred {
		t.Fatalf("Status = %q, want DEFERRED（退避用尽不丢消息）", p.Status)
	}
	if p.NextRetryAt != nil {
		t.Fatal("DEFERRED 不该带 NextRetryAt（它不是定时重试，是等激活）")
	}
}

func TestQqbotPlanForUnknownTreatedAsRetryable(t *testing.T) {
	p := qqbotPlanFor(qerr(qqbot.KindUnknown, 999999), 1)
	if p.Status != model.QqbotOutboxPending {
		t.Fatalf("未知错误应保守按可重试处理，得到 %q", p.Status)
	}
}

func TestQqbotPlanForSuccess(t *testing.T) {
	p := qqbotPlanFor(nil, 1)
	if p.Status != model.QqbotOutboxPending {
		t.Fatalf("nil error 的占位状态 = %q, want PENDING（由调用方改写为 SENT）", p.Status)
	}
}

// ---------- 退避节奏 ----------

func TestQqbotBackoffFor(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Minute},
		{2, 5 * time.Minute},
		{3, 15 * time.Minute},
		{4, time.Hour},
		{5, time.Hour},    // 超表长度 → 取最后一档
		{0, time.Minute},  // 0 → 第一档（防御）
		{-1, time.Minute}, // 负数 → 第一档（防御）
	}
	for _, c := range cases {
		if got := qqbotBackoffFor(c.attempt); got != c.want {
			t.Errorf("qqbotBackoffFor(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

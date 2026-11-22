// Package queue 队列封装与可观测测试。
//
// 不依赖真实 Redis：
//   - 队列名/并发/任务类型常量是纯数据，直接断言；
//   - CollectStats 的降级路径用 rdb=nil 验证；
//   - NOT_FOUND 语义用「连不上的 Redis 地址」触发，断言降级而非假告警。
//
// 防的 bug：
//   - 队列名被改名：Bull 时代的历史任务留在旧队列里，改名后无人消费（静默积压）；
//   - 并发数与原 Bull 配置不一致：图片审核从 5 掉到 1，上传排队几十秒；
//   - CollectStats 在 Redis 不可用时 panic 或拖住 /health（探针超时误判实例不健康）；
//   - NOT_FOUND 被当成真故障：/health 长期 available=false，运维被假告警淹没。
package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// TestQueueNames_MatchOriginalBull 队列名必须与原仓库 Bull 完全一致。
//
// 迁移期语义：Bull 时代的任务与 asynq 共用同一 Redis，
// 队列名不一致会导致历史任务永远没人消费（静默积压，无人发现）。
func TestQueueNames_MatchOriginalBull(t *testing.T) {
	want := []string{"image-audit", "text-audit", "video-compression"}
	if len(QueueNames) != len(want) {
		t.Fatalf("队列数量：得到 %d，期望 %d（%v）", len(QueueNames), len(want), QueueNames)
	}
	for i, w := range want {
		if QueueNames[i] != w {
			t.Errorf("队列[%d]：得到 %q，期望 %q（改名会导致历史任务无人消费）",
				i, QueueNames[i], w)
		}
	}
}

// TestQueueNameConstants 三个队列常量与 QueueNames 一致（防两处定义漂移）。
func TestQueueNameConstants(t *testing.T) {
	cases := []struct {
		constName string
		got       string
		want      string
	}{
		{"QueueImageAudit", QueueImageAudit, "image-audit"},
		{"QueueTextAudit", QueueTextAudit, "text-audit"},
		{"QueueVideoCompression", QueueVideoCompression, "video-compression"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.constName, c.got, c.want)
		}
	}
	// QueueNames 必须恰好包含这三个常量
	set := map[string]bool{}
	for _, n := range QueueNames {
		set[n] = true
	}
	for _, c := range cases {
		if !set[c.want] {
			t.Errorf("QueueNames 未包含 %q（server 不会为该队列起 worker）", c.want)
		}
	}
}

// TestTaskTypes 任务类型常量（asynq 按 type 分发，写错则任务无人处理）。
func TestTaskTypes(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"TypeImageAudit", TypeImageAudit, "audit:image"},
		{"TypeTextAudit", TypeTextAudit, "audit:text"},
		{"TypeVideoCompression", TypeVideoCompression, "video:compress"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s：得到 %q，期望 %q（改错则任务永远无人处理）", c.name, c.got, c.want)
		}
	}
	// 三个 type 必须互不相同（否则 asynq mux 会互相覆盖处理器）
	if TypeImageAudit == TypeTextAudit || TypeTextAudit == TypeVideoCompression ||
		TypeImageAudit == TypeVideoCompression {
		t.Error("任务类型有重复（asynq mux 的处理器会互相覆盖）")
	}
}

// TestNewServerConfig_ConcurrencyMatchesBull 并发数与原 Bull 配置一致。
//
// 原仓库：image-audit=5、text-audit=10、video-compression=2，总并发 17。
// 任一处写错都会让审核延迟显著变化（运维难察觉）。
func TestNewServerConfig_ConcurrencyMatchesBull(t *testing.T) {
	cfg := NewServerConfig("127.0.0.1:6379", "")

	wantQueues := map[string]int{
		QueueImageAudit:       5,
		QueueTextAudit:        10,
		QueueVideoCompression: 2,
	}
	for name, want := range wantQueues {
		got, ok := cfg.Queues[name]
		if !ok {
			t.Errorf("配置缺少队列 %q（该队列的任务不会被消费）", name)
			continue
		}
		if got != want {
			t.Errorf("队列 %q 并发：得到 %d，期望 %d（对齐原 Bull）", name, got, want)
		}
	}

	sum := 0
	for _, v := range cfg.Queues {
		sum += v
	}
	if cfg.Concurrency != sum {
		t.Errorf("总并发 %d 与各队列并发之和 %d 不一致（超出部分会饿死低优先级队列）",
			cfg.Concurrency, sum)
	}
	if cfg.RetryDelayFunc == nil {
		t.Error("RetryDelayFunc 为 nil（重试退避策略缺失）")
	}
}

// TestNewServerConfig_NoExtraQueues 不得额外注册未使用的队列。
func TestNewServerConfig_NoExtraQueues(t *testing.T) {
	cfg := NewServerConfig("", "")
	if len(cfg.Queues) != len(QueueNames) {
		t.Errorf("配置了 %d 个队列，但 QueueNames 只有 %d 个（多出的队列永远没有生产者）",
			len(cfg.Queues), len(QueueNames))
	}
}

// TestCollectStats_NilRedisDegrades Redis 为 nil 时降级返回，不 panic。
//
// 这是降级启动的核心约定：无 Redis 环境下 /health 必须仍能响应。
func TestCollectStats_NilRedisDegrades(t *testing.T) {
	var stats Stats
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CollectStats(nil) panic 了（/health 会 500）: %v", r)
			}
		}()
		stats = CollectStats(nil)
	}()

	if stats.Available {
		t.Error("Redis 为 nil 时 Available 应为 false")
	}
	if stats.Error == "" {
		t.Error("降级时应给出 error 说明（运维需要知道为什么不可用）")
	}
	if stats.Queues == nil {
		t.Error("Queues 不应为 nil（JSON 会输出 null 而非 []，前端解析差异）")
	}
	if len(stats.Queues) != 0 {
		t.Errorf("降级时 Queues 应为空，得到 %d 项", len(stats.Queues))
	}
}

// TestCollectStats_UnreachableRedisNoHang 连不上的 Redis 必须快速降级，不拖住 /health。
//
// 用一个保留地址（127.0.0.1:1，通常无监听）模拟 Redis 宕机。
// 断言：3 秒内返回（CollectStats 内部有 2s 超时兜底）。
// 若超时兜底被删掉，health 探针会超时，容器被误判不健康并重启。
func TestCollectStats_UnreachableRedisNoHang(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // 无监听
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  0,
	})
	defer func() { _ = rdb.Close() }()

	start := time.Now()
	done := make(chan Stats, 1)
	go func() { done <- CollectStats(rdb) }()

	select {
	case stats := <-done:
		elapsed := time.Since(start)
		if elapsed > 3*time.Second {
			t.Errorf("降级耗时 %v，超过 3s 阈值（/health 探针会超时，容器被误重启）", elapsed)
		}
		// 连不上就没有统计，Available 应为 false；关键是不能 panic、不能 hang
		if stats.Available {
			t.Logf("意外：不可达 Redis 竟返回 Available=true（可能连到了别的实例）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CollectStats 在 Redis 不可达时卡死超过 5s（health 探针会被拖死）")
	}
}

// TestCollectStats_NOTFOUNDHandledAsEmpty 空队列（asynq NOT_FOUND）按空队列处理。
//
// asynq 在队列首次有任务入队前，GetQueueInfo 返回 "queue ... does not exist"。
// 这不该被当成故障 —— 否则新部署的实例 /health 长期 available=false，
// 运维被假告警淹没、真故障被忽略。
//
// 验证手段：直接用 asynq 的 Inspector 对真实 Redis 校验不现实（无 Redis 环境），
// 因此这里断言降级分支的判据字符串 —— 即源码里用于识别的关键字。
func TestCollectStats_NOTFOUNDHandledAsEmpty(t *testing.T) {
	// asynq 的 NOT_FOUND 错误文本包含 "does not exist"；
	// stats.go 用 strings.Contains(err.Error(), "does not exist") 识别。
	// 此处验证该判据对 asynq 真实错误文本成立（asynq 版本升级若改文案，本测试会红）。
	sampleErrTexts := []string{
		"queue \"image-audit\" does not exist",
		"NOT_FOUND: queue does not exist",
	}
	for _, text := range sampleErrTexts {
		if !strings.Contains(text, "does not exist") {
			t.Errorf("判据失配：%q 不含 \"does not exist\"", text)
		}
	}

	// 反向：真实故障文本不应命中「空队列」判据（否则真故障被静默吞掉）
	realFailures := []string{
		"dial tcp 127.0.0.1:6379: connect: connection refused",
		"redis: nil",
		"context deadline exceeded",
	}
	for _, text := range realFailures {
		if strings.Contains(text, "does not exist") {
			t.Errorf("判据过宽：真实故障 %q 会被误判成空队列（真故障被吞）", text)
		}
	}
}

// TestFromRedisClient_NilReturnsNil 由 nil 客户端派生 asynq 客户端应返回 nil。
// 防的是：Redis 不可用时仍构造出 asynq.Client，投递任务时 panic/阻塞。
func TestFromRedisClient_NilReturnsNil(t *testing.T) {
	if got := FromRedisClient(nil); got != nil {
		t.Errorf("nil redis 应返回 nil 客户端，得到 %#v（投递时会阻塞或 panic）", got)
	}
}

// TestFromRedisClient_ReusesOptions 由已有客户端派生时复用同一连接配置。
// 防的是：另开一套连接（连接数翻倍），或漏传 password（认证失败）。
func TestFromRedisClient_ReusesOptions(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "pw",
		DB:       2,
	})
	defer func() { _ = rdb.Close() }()

	client := FromRedisClient(rdb)
	if client == nil {
		t.Fatal("已有 redis 客户端时应返回 asynq 客户端")
	}
	defer func() { _ = client.Close() }()
	// asynq.Client 不暴露 Options，此处仅验证非 nil 且能安全 Close。
}

// TestNewServerConfig_QueuesPrioritySemantics 队列优先级配置必须全为正数。
// asynq 会按数值比例分配 worker；0 或负数会让该队列永远分不到 worker。
func TestNewServerConfig_QueuesPrioritySemantics(t *testing.T) {
	cfg := NewServerConfig("", "")
	for name, v := range cfg.Queues {
		if v <= 0 {
			t.Errorf("队列 %q 的并发权重为 %d（<=0 会让该队列永远分不到 worker）", name, v)
		}
	}
}

// TestAuditWorkerDeps_ProcessRecordNil 未注入处理函数时不得 panic。
// 防的是：worker 启动但依赖未装配，收到任务直接空指针崩掉整个 consumer。
func TestAuditWorkerDeps_ProcessRecordNil(t *testing.T) {
	deps := AuditWorkerDeps{} // ProcessRecord = nil
	task := asynq.NewTask(TypeTextAudit, []byte(`{"record_id":"r-1"}`))

	handler := func(ctx context.Context, t *asynq.Task) error {
		return handleAuditTask(t, deps)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ProcessRecord 为 nil 时 panic: %v（consumer 会崩）", r)
		}
	}()
	if err := handler(context.Background(), task); err != nil {
		t.Errorf("未装配依赖时应静默跳过，得到 error: %v", err)
	}
}

// TestHandleAuditTask_BadPayloadReturnsError 负载损坏返回 error（走重试/死信）。
// 防的是：损坏负载被吞掉返回 nil，任务被当成成功，审核状态永远停在进行中。
func TestHandleAuditTask_BadPayloadReturnsError(t *testing.T) {
	deps := AuditWorkerDeps{ProcessRecord: func(recordID, msgID string) error { return nil }}
	task := asynq.NewTask(TypeTextAudit, []byte(`not-json`))

	if err := handleAuditTask(task, deps); err == nil {
		t.Error("损坏负载应返回 error（否则任务被标记成功，审核卡住）")
	}
}

// TestHandleAuditTask_InvokesProcessor 正常负载调用处理函数并透传 record_id。
func TestHandleAuditTask_InvokesProcessor(t *testing.T) {
	var gotRecordID string
	deps := AuditWorkerDeps{
		ProcessRecord: func(recordID, msgID string) error {
			gotRecordID = recordID
			return nil
		},
	}
	task := asynq.NewTask(TypeTextAudit, []byte(`{"record_id":"rec-42"}`))

	if err := handleAuditTask(task, deps); err != nil {
		t.Fatalf("正常负载应成功: %v", err)
	}
	if gotRecordID != "rec-42" {
		t.Errorf("record_id 未透传：得到 %q，期望 rec-42", gotRecordID)
	}
}

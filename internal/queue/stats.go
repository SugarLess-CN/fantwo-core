// 队列可观测：读取 asynq 各队列的排队/执行/失败摘要，供 /health 暴露。
//
// 为什么用 Inspector 而不是自己读 Redis 键：asynq 的内部键布局属于实现细节，
// 官方 Inspector（asynq.NewInspectorFromRedisClient）才是稳定的读取入口，
// 且复用 main 已建立的 go-redis 连接，不额外开连接池。
package queue

import (
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// QueueStat 单个队列的统计快照。
type QueueStat struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`   // 待消费
	Active    int    `json:"active"`    // 正在执行
	Retry     int    `json:"retry"`     // 重试中
	Scheduled int    `json:"scheduled"` // 定时待执行
	Archived  int    `json:"archived"`  // 死信
	Completed int    `json:"completed"` // 已完成（受 asynq 保留策略限制）
}

// Stats 全部队列统计结果。
type Stats struct {
	Available bool        `json:"available"` // Redis 可用且读取成功
	Queues    []QueueStat `json:"queues"`
	Totals    QueueStat   `json:"totals"` // 各队列求和（Name 为空）
	Error     string      `json:"error,omitempty"`
}

// CollectStats 读取三队列统计。rdb 为 nil 时返回 Available=false 的空快照，不报错。
//
// 超时保护：health 可能被探针高频调用，Redis 慢时不能拖住 /health，
// 因此仅在 inspector 操作层面各自限时（asynq Inspector 走 go-redis，无统一 deadline，
// 这里用 goroutine + 超时兜底，超时返回 Available=false）。
func CollectStats(rdb *redis.Client) Stats {
	if rdb == nil {
		return Stats{Available: false, Queues: []QueueStat{}, Error: "redis 不可用"}
	}

	type result struct {
		s   Stats
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ch <- result{s: collect(rdb)}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return Stats{Available: false, Queues: []QueueStat{}, Error: r.err.Error()}
		}
		return r.s
	case <-time.After(2 * time.Second):
		return Stats{Available: false, Queues: []QueueStat{}, Error: "读取队列统计超时"}
	}
}

// collect 实际读取逻辑（在独立 goroutine 内执行）。
func collect(rdb *redis.Client) Stats {
	insp := asynq.NewInspectorFromRedisClient(rdb)
	defer func() { _ = insp.Close() }()

	out := Stats{Available: true, Queues: make([]QueueStat, 0, len(QueueNames))}
	for _, name := range QueueNames {
		info, err := insp.GetQueueInfo(name)
		if err != nil {
			// asynq 语义：队列在首次有任务入队前并不存在，GetQueueInfo 返回 NOT_FOUND。
			// 这不是故障，按「空队列」处理（计入结果、不写 Error），避免 /health 出现假告警。
			if strings.Contains(err.Error(), "does not exist") {
				out.Queues = append(out.Queues, QueueStat{Name: name})
				continue
			}
			// 其它错误：单个队列读失败不影响其它队列，记错误继续
			out.Error = err.Error()
			continue
		}
		qs := QueueStat{
			Name:      name,
			Pending:   info.Pending,
			Active:    info.Active,
			Retry:     info.Retry,
			Scheduled: info.Scheduled,
			Archived:  info.Archived,
			Completed: info.Completed,
		}
		out.Queues = append(out.Queues, qs)
		out.Totals.Pending += qs.Pending
		out.Totals.Active += qs.Active
		out.Totals.Retry += qs.Retry
		out.Totals.Scheduled += qs.Scheduled
		out.Totals.Archived += qs.Archived
		out.Totals.Completed += qs.Completed
	}
	return out
}

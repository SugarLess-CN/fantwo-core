// QQ 机器人定时扫描（asynq 周期任务，T-40）。
//
// # 为什么需要它
//
// 触发重投一共有三个入口（docs/29 §4.4）：
//
//	① webhook 收到该用户消息 → 立刻投递该用户全部 DEFERRED（activateWindow）
//	② **定时扫描（本文件）→ 处理 PENDING 且 next_retry_at 到期的任务**
//	③ 人工「重试」按钮 → RetryOutbox（唯一能让 FAILED 死信复活的入口）
//
// ② 负责的是「退避到期」这一路：频控（40034100）与去重（40054005）失败的任务留在
// PENDING 并推后 next_retry_at，到点后必须有人来重试，否则永远躺在池子里没人管。
//
// # 与 asynq 现有用法的关系
//
// 只借 asynq 的**定时调度**能力（Scheduler 周期投递一个无负载任务），任务本体
// 由本文件同步执行 —— 一条任务扫一批，而不是一条池任务一个 asynq 任务：
// 池子的状态与重试语义已经由 qqbot_outbox 表自己表达，再复制一份到 asynq 队列
// 会出现「两套重试计数互相打架」的经典问题。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// TypeQqbotOutboxScan asynq 任务类型：扫描一轮到期池任务。
//
// 命名与 queue 包现有 type（audit:text / image:audit）同风格：`<域>:<对象>:<动作>`。
const TypeQqbotOutboxScan = "qqbot:outbox:scan"

// QqbotScanInterval 扫描周期。
//
// 1 分钟的理由：退避最小档就是 1 分钟（qqbotBackoff[0]），周期比它长会让
// 退避形同虚设（5 分钟扫一次 = 所有 1 分钟档都变成 5 分钟）。代价是每分钟一条空扫，
// 在 PENDING 为空时只是一次带索引的 UPDATE + 一次 SELECT，可以忽略。
const QqbotScanInterval = time.Minute

// QqbotWorkerQueue 池子扫描任务使用的队列名。
//
// 复用 text-audit 队列而不是新开一个队列：asynq 的队列必须在 NewServerConfig 里
// 注册并发权重，新队列要改 internal/queue/queue.go（本域不碰那个文件）。
// 扫描任务极轻（DB 查询 + 少量上游 HTTP），与审核任务同队列不会互相饿死。
const QqbotWorkerQueue = queue.QueueTextAudit

// QqbotScanResult 一轮扫描的结果统计（进日志与健康检查）。
type QqbotScanResult struct {
	// Reclaimed 卡在 SENDING 被拉回 PENDING 的残留任务数（进程崩溃兜底）。
	Reclaimed int64
	// Scanned 本轮取到并尝试投递的任务数。
	Scanned int
	// Sent / Deferred / Retried / Failed 按终局状态分布。
	Sent     int
	Deferred int
	Retried  int // 仍在 PENDING（退避等待中）
	Failed   int
	// Errored 投递过程本身报错（DB/网络异常）的条数。
	Errored int
}

// ScanOutbox 扫描一轮：回收残留 → 捞到期任务 → 逐个投递。
//
// limit <= 0 时用 QqbotScanBatch。单轮上限存在的意义是「别一次扫五千条把
// 上游频控额度打光」，剩下的下一分钟接着扫（反正每分钟都跑）。
func (s *QqbotService) ScanOutbox(ctx context.Context, limit int) (QqbotScanResult, error) {
	var res QqbotScanResult
	if s.d != nil {
		if e := s.d.ready(); e != nil {
			return res, errors.New("qqbot: database unavailable")
		}
	}
	acc := s.accessor()

	// 1) 残留回收：进程在「抢占成功 → 写回结果」之间崩溃，任务会永远卡在 SENDING。
	//    超过 QqbotSendingTimeout 视为残留，拉回 PENDING 让它重新进入扫描范围。
	if n, err := acc.ReclaimStaleSending(s.now().Add(-QqbotSendingTimeout)); err == nil {
		res.Reclaimed = n
	} else {
		s.logger().Warn("qqbot 回收 SENDING 残留失败", zap.Error(err))
	}

	if limit <= 0 {
		limit = QqbotScanBatch
	}
	due, err := acc.ListOutboxDue(s.now(), limit)
	if err != nil {
		return res, fmt.Errorf("qqbot: list due outbox: %w", err)
	}

	for _, task := range due {
		res.Scanned++
		out, e := s.DeliverOutbox(ctx, task.ID)
		if e != nil {
			res.Errored++
			continue
		}
		switch out.Status {
		case model.QqbotOutboxSent:
			res.Sent++
		case model.QqbotOutboxDeferred:
			res.Deferred++
		case model.QqbotOutboxFailed:
			res.Failed++
		case model.QqbotOutboxPending:
			res.Retried++
		}
	}

	if res.Scanned > 0 || res.Reclaimed > 0 {
		s.logger().Info("qqbot 池子扫描完成",
			zap.Int("scanned", res.Scanned), zap.Int("sent", res.Sent),
			zap.Int("deferred", res.Deferred), zap.Int("retried", res.Retried),
			zap.Int("failed", res.Failed), zap.Int64("reclaimed", res.Reclaimed))
	}
	return res, nil
}

// NewQqbotScanTask 构造一轮扫描任务（无负载：参数全在本服务的常量里）。
func NewQqbotScanTask() (*asynq.Task, error) {
	return asynq.NewTask(TypeQqbotOutboxScan, nil), nil
}

// RegisterQqbotWorkerHandlers 把扫描任务挂到 asynq mux（main 启动 worker 时调用）。
//
// svc 为 nil 时注册一个空处理器：让「没配 DB 的降级启动」不会因为 worker
// 拿不到 service 而 panic（对齐 audit 侧「依赖未装配也不能崩」的教训）。
func RegisterQqbotWorkerHandlers(mux *asynq.ServeMux, svc *QqbotService) {
	if mux == nil {
		return
	}
	mux.HandleFunc(TypeQqbotOutboxScan, func(ctx context.Context, t *asynq.Task) error {
		if svc == nil {
			return nil
		}
		// 扫描本身不返回 error：单条投递失败已经由池子状态机记录，
		// 这里返回 error 只会让 asynq 重跑整轮，把已经处理过的任务再扫一遍。
		if _, err := svc.ScanOutbox(ctx, QqbotScanBatch); err != nil {
			svc.logger().Warn("qqbot 池子扫描失败", zap.Error(err))
		}
		return nil
	})
}

// NewQqbotScheduler 构造周期调度器（每 QqbotScanInterval 投一次扫描任务）。
//
// 返回的 *asynq.Scheduler 需要调用方 Run() 起来，并在退出时 Shutdown()
// （与 asynq.Server 同生命周期，见 cmd/server/main.go 的队列段落）。
// rdb 为 nil（Redis 不可用）时返回 nil —— 定时扫描自然降级为「只有 webhook 触发」。
func NewQqbotScheduler(rdb *redis.Client) (*asynq.Scheduler, error) {
	if rdb == nil {
		return nil, nil
	}
	o := rdb.Options()
	sch := asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: o.Addr, Password: o.Password, DB: o.DB},
		&asynq.SchedulerOpts{Location: time.Local},
	)
	task, err := NewQqbotScanTask()
	if err != nil {
		return nil, err
	}
	if _, err := sch.Register("@every "+QqbotScanInterval.String(), task,
		asynq.Queue(QqbotWorkerQueue), asynq.MaxRetry(0)); err != nil {
		return nil, err
	}
	return sch, nil
}

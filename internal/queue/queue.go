// Package queue asynq 队列封装（Bull → asynq 平移，见 docs/03）。
//
// 原仓库 Bull 队列 → asynq 队列映射：
//
//	image-audit        并发 5   图片内容审核
//	text-audit         并发 10  文本内容审核
//	video-compression  并发 2   ffmpeg 视频压缩
//
// 队列名保持与原仓库一致，便于排查对照。
package queue

import (
	"github.com/hibiken/asynq"
)

// 队列名常量（对齐原仓库 common/queue）。
const (
	QueueImageAudit       = "image-audit"
	QueueTextAudit        = "text-audit"
	QueueVideoCompression = "video-compression"
)

// QueueNames 全部队列名（server 启动时按此注册并发配置）。
var QueueNames = []string{QueueImageAudit, QueueTextAudit, QueueVideoCompression}

// TaskType 任务类型常量（asynq 按 type 分发；处理器在业务批次实现）。
const (
	TypeImageAudit       = "audit:image"
	TypeTextAudit        = "audit:text"
	TypeVideoCompression = "video:compress"
)

// NewClient 创建 asynq 客户端（投递任务用）。redisAddr 如 "127.0.0.1:6379"。
func NewClient(redisAddr, password string) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr, Password: password})
}

// NewServerConfig 队列服务端配置：各队列并发数对齐原 Bull 配置。
// 注意：asynq 的 Queues 配置中优先级数值即并发权重；这里用严格并发语义
// （原仓库 image-audit=5 / text-audit=10 / video-compression=2）。
func NewServerConfig(redisAddr, password string) asynq.Config {
	return asynq.Config{
		Concurrency: 17, // 5 + 10 + 2，全队列并发上限
		Queues: map[string]int{
			QueueImageAudit:       5,
			QueueTextAudit:        10,
			QueueVideoCompression: 2,
		},
		// 对齐原 Bull 全局 job 默认：attempts=5、指数退避（delay 2000ms）、超时 5 分钟
		RetryDelayFunc: asynq.DefaultRetryDelayFunc,
	}
}

// NewServer 创建 asq server（消费任务用）。
func NewServer(redisAddr, password string) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr, Password: password},
		NewServerConfig(redisAddr, password),
	)
}

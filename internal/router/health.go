// /health 健康检查与运行时摘要（骨架层可观测入口）。
//
// 设计要点：
//   - /health 必须**永远可通**（探针语义）：DB/Redis 不可用不返回 5xx，
//     而是在 data.dependencies 里标 down + 给出原因，HTTP 始终 200、code 始终 0。
//   - 依赖通过 SetHealthDeps 注入（main 装配），router.New 无需感知；
//     未注入时对应字段显示 unknown，不影响端点可用性。
//   - WS 连接数/房间数来自 ws 包的进程级原子计数（无锁，见 internal/ws/hub.go）。
package router

import (
	"context"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/queue"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// healthDeps 健康检查所需运行时依赖（可为 nil：无 DB/Redis 的降级启动）。
type healthDeps struct {
	db    *gorm.DB
	redis *redis.Client
}

var hDeps healthDeps

// SetHealthDeps 注入 /health 需要的运行时依赖（main 在 router.New 前调用）。
// 允许传 nil，表示该依赖未启用。
func SetHealthDeps(db *gorm.DB, rdb *redis.Client) {
	hDeps = healthDeps{db: db, redis: rdb}
}

// dependencyStatus 单项依赖状态。
type dependencyStatus struct {
	Status string `json:"status"`          // up / down / unknown
	Error  string `json:"error,omitempty"` // 仅 down 时给出原因
}

// wsStatus WS 连接摘要。
type wsStatus struct {
	Connections int64 `json:"connections"`
	Rooms       int64 `json:"rooms"`
}

// healthData /health 返回载荷。
type healthData struct {
	Status       string                      `json:"status"`       // 恒 up（进程存活即 up）
	Timestamp    int64                       `json:"timestamp"`    // 毫秒
	Uptime       int64                       `json:"uptime"`       // 进程运行秒数
	Dependencies map[string]dependencyStatus `json:"dependencies"` // postgres / redis
	WebSocket    wsStatus                    `json:"websocket"`
	Queue        queue.Stats                 `json:"queue"`
}

// processStartedAt 进程启动时间（uptime 基准）。
var processStartedAt = time.Now()

// health 存活/就绪探针：{code:0, message:"ok", data:{...}}。
//
// 注意：即便依赖全 down 也返回 HTTP 200 + code 0——探针只关心「服务进程是否在」，
// 依赖状态通过 data.dependencies 暴露给运维面板，避免探针因 DB 抖动重启实例。
func health(c *gin.Context) {
	data := healthData{
		Status:       "up",
		Timestamp:    time.Now().UnixMilli(),
		Uptime:       int64(time.Since(processStartedAt).Seconds()),
		Dependencies: make(map[string]dependencyStatus, 2),
		WebSocket: wsStatus{
			Connections: ws.ActiveConnections(),
			Rooms:       ws.ActiveRooms(),
		},
		Queue: queue.CollectStats(hDeps.redis),
	}

	// PostgreSQL：仅做连接对象存在性判断，不主动 Ping（避免探针高频打库）
	if hDeps.db == nil {
		data.Dependencies["postgres"] = dependencyStatus{Status: "down", Error: "未初始化（降级启动）"}
	} else {
		data.Dependencies["postgres"] = dependencyStatus{Status: "up"}
	}

	// Redis：复用连接做一次轻量 Ping（Redis 在本项目里是队列/缓存旁路，代价低）
	if hDeps.redis == nil {
		data.Dependencies["redis"] = dependencyStatus{Status: "down", Error: "未初始化（降级启动）"}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		if err := hDeps.redis.Ping(ctx).Err(); err != nil {
			data.Dependencies["redis"] = dependencyStatus{Status: "down", Error: err.Error()}
		} else {
			data.Dependencies["redis"] = dependencyStatus{Status: "up"}
		}
	}

	response.Health(c, data)
}

// ping 极轻量探活（无依赖调用，供负载均衡/容器 liveness 使用）。
func ping(c *gin.Context) {
	response.Health(c, gin.H{"pong": time.Now().UnixMilli()})
}

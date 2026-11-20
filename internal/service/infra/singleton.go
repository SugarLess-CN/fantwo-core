package infra

import (
	"sync"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 进程级配置服务单例。
//
// 为什么必须是单例：
//   - ConfigService 内部持有敏感词前缀树（trie）与配置一级缓存，这两者都是**有状态**的。
//   - 若 router 层与 asynq worker 层各自 NewConfigService，就会出现两份 trie：
//     管理端改词后只有 router 那份立即生效，worker 那份要等 syncWordsIfStale
//     （30s 节流）才追上 —— 期间「审核用旧词表、查询用新词表」，行为不一致。
//
// 结论：全进程只允许一个实例，任何需要 CheckText / GetString 的地方都走 Default。
var (
	configOnce     sync.Once
	configInstance *ConfigService
)

// Default 返回进程级配置服务（幂等；首次调用时构造并播种默认配置）。
//
// dsn 参数允许为 nil（降级启动）：此时 GetString 返回默认值、CheckText 恒未命中。
func Default(db *gorm.DB, rdb *redis.Client) *ConfigService {
	configOnce.Do(func() {
		configInstance = NewConfigService(db, rdb)
		// 幂等播种：已存在的 key 不覆盖
		_ = configInstance.EnsureSeeds()
	})
	return configInstance
}

// ResetDefaultForTest 重置单例（仅供单测使用）。
//
// 生产代码不得调用：单例一旦被并发使用，重置会让新老实例并存，
// 重新引入「两份 trie」的问题。
func ResetDefaultForTest() {
	configOnce = sync.Once{}
	configInstance = nil
}

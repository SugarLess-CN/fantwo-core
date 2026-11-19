// Package event 进程内事件总线（对应原 @nestjs/event-emitter）。
//
// 职责：
//   - 交易域（支付/审核）与内容域（文章/评论）、用户域（成就/通知）解耦：
//     支付成功、审核状态回写等通过 Publish 通知，订阅方自行监听；
//   - 同步顺序调用订阅者（对齐 NestJS EventEmitter2 默认 emit 语义），
//     单个订阅者 panic 不影响其它订阅者。
//
// 跨域事件名约定见 events.go。
package event

import (
	"fmt"
	"sync"
)

// Handler 事件处理器签名。
type Handler func(payload interface{})

// Bus 简单 pub/sub 总线。
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]Handler
}

// 全局默认总线（单体内各域共享一个实例；main 初始化后注入各 service）。
var defaultBus = NewBus()

// NewBus 创建总线。
func NewBus() *Bus {
	return &Bus{subs: make(map[string][]Handler)}
}

// Default 返回全局默认总线。
func Default() *Bus { return defaultBus }

// SetDefault 替换全局总线（测试用）。
func SetDefault(b *Bus) { defaultBus = b }

// SubscriberCount 某主题的订阅者数量（诊断用：0 表示无人监听，事件会被静默丢弃）。
func (b *Bus) SubscriberCount(topic string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[topic])
}

// Subscribe 订阅主题（可重复注册，按注册顺序调用）。
func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], h)
}

// Publish 发布事件：同步依次调用订阅者，单订阅者 panic 转为 error 不外抛。
func (b *Bus) Publish(topic string, payload interface{}) {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.subs[topic]...)
	b.mu.RUnlock()
	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[event] subscriber panic topic=%s err=%v\n", topic, r)
				}
			}()
			h(payload)
		}()
	}
}

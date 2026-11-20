package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// memKV 内存键值替身：同时实现 CodeStore（验证码）与 authtoken.Store（认证态），
// 让用户域测试不依赖真实 Redis。支持 TTL 与故障注入。
type memKV struct {
	mu   sync.Mutex
	data map[string]string
	ttl  map[string]time.Time
	fail bool
	ops  int
}

// newMemKV 构造空替身。
func newMemKV() *memKV {
	return &memKV{data: map[string]string{}, ttl: map[string]time.Time{}}
}

// Set 写入（ttl>0 时到期自动不可见）。
func (m *memKV) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return fmt.Errorf("kv down")
	}
	m.data[key] = fmt.Sprint(value)
	if ttl > 0 {
		m.ttl[key] = time.Now().Add(ttl)
	} else {
		delete(m.ttl, key)
	}
	return nil
}

// Get 读取。
func (m *memKV) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return "", fmt.Errorf("kv down")
	}
	if exp, has := m.ttl[key]; has && time.Now().After(exp) {
		delete(m.data, key)
		delete(m.ttl, key)
	}
	v, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("nil")
	}
	return v, nil
}

// Exists 是否存在（未过期）。
func (m *memKV) Exists(ctx context.Context, key string) (int64, error) {
	if _, err := m.Get(ctx, key); err != nil {
		if m.isFail() {
			return 0, fmt.Errorf("kv down")
		}
		return 0, nil
	}
	return 1, nil
}

// Del 删除。
func (m *memKV) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return fmt.Errorf("kv down")
	}
	delete(m.data, key)
	delete(m.ttl, key)
	return nil
}

// Incr 自增（不存在视为 0）。
func (m *memKV) Incr(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return 0, fmt.Errorf("kv down")
	}
	n, _ := strconv.ParseInt(m.data[key], 10, 64)
	n++
	m.data[key] = strconv.FormatInt(n, 10)
	return n, nil
}

// isFail 故障标志读取。
func (m *memKV) isFail() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fail
}

// expireNow 让某键立即过期（模拟 TTL 到期，无需 sleep）。
func (m *memKV) expireNow(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttl[key] = time.Now().Add(-time.Second)
}

// has 键是否存在（不触发 TTL 检查）。
func (m *memKV) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok
}

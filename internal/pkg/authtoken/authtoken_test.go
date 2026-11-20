package authtoken

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore 内存替身：实现 Redis 语义的最小子集（值 + TTL + 故障注入），
// 让认证态测试不依赖真实 Redis。
type memStore struct {
	mu      sync.Mutex
	data    map[string]string
	expires map[string]time.Time
	fail    bool // true 时所有操作返回错误（模拟 Redis 不可用/超时）
	ops     int  // 调用计数，用于断言「未配置时不写」
}

func newMemStore() *memStore {
	return &memStore{data: map[string]string{}, expires: map[string]time.Time{}}
}

func (m *memStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return fmt.Errorf("redis down")
	}
	m.data[key] = fmt.Sprint(value)
	if ttl > 0 {
		m.expires[key] = time.Now().Add(ttl)
	} else {
		delete(m.expires, key)
	}
	return nil
}

func (m *memStore) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return "", fmt.Errorf("redis down")
	}
	v, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("nil")
	}
	if exp, has := m.expires[key]; has && time.Now().After(exp) {
		delete(m.data, key)
		delete(m.expires, key)
		return "", fmt.Errorf("expired")
	}
	return v, nil
}

func (m *memStore) Exists(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return 0, fmt.Errorf("redis down")
	}
	if exp, has := m.expires[key]; has && time.Now().After(exp) {
		delete(m.data, key)
		delete(m.expires, key)
		return 0, nil
	}
	if _, ok := m.data[key]; ok {
		return 1, nil
	}
	return 0, nil
}

func (m *memStore) Incr(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops++
	if m.fail {
		return 0, fmt.Errorf("redis down")
	}
	n, _ := strconv.ParseInt(m.data[key], 10, 64)
	n++
	m.data[key] = strconv.FormatInt(n, 10)
	return n, nil
}

// TestBlacklistAddAndCheck 登出黑名单：写入后命中，TTL 到期后自动失效。
//
// 防的 bug：黑名单只写不查（登出后 token 仍可用）、或 TTL 未设置导致键永不过期
// （Redis 内存被已过期 token 撑爆）。
func TestBlacklistAddAndCheck(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	token := "header.payload.signature"

	if Blacklisted(ctx, st, token) {
		t.Fatal("未加入黑名单时不应命中")
	}
	if err := BlacklistAdd(ctx, st, token, 50*time.Millisecond); err != nil {
		t.Fatalf("加入黑名单失败: %v", err)
	}
	if !Blacklisted(ctx, st, token) {
		t.Fatal("加入黑名单后应命中")
	}
	// TTL 到期：等价于「token 已彻底过期，无需再拦」。
	time.Sleep(60 * time.Millisecond)
	if Blacklisted(ctx, st, token) {
		t.Error("TTL 到期后黑名单应自动失效")
	}
}

// TestBlacklistKeyDoesNotLeakToken 黑名单键不得包含 token 明文（改用摘要）。
//
// 防的 bug：直接拿 token 当 key → Redis 里出现完整凭证，运维可见即等于泄漏。
func TestBlacklistKeyDoesNotLeakToken(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiJ9.payload.signature"
	key := BlacklistKey(token)
	if strings.Contains(key, token) {
		t.Error("黑名单键不应包含 token 明文")
	}
	if !strings.HasPrefix(key, "jwt:blacklist:") {
		t.Errorf("键前缀不符: %s", key)
	}
	// 同一 token 必须得到稳定键（否则写入与查询对不上）。
	if BlacklistKey(token) != key {
		t.Error("同一 token 的键必须稳定")
	}
	if BlacklistKey(token) == BlacklistKey(token+"x") {
		t.Error("不同 token 的键必须不同")
	}
}

// TestBlacklistSkipsNonPositiveTTL 已过期 token（剩余生命 <= 0）不写黑名单，避免无意义写入。
func TestBlacklistSkipsNonPositiveTTL(t *testing.T) {
	st := newMemStore()
	ctx := context.Background()
	for _, ttl := range []time.Duration{0, -time.Minute} {
		if err := BlacklistAdd(ctx, st, "tk", ttl); err != nil {
			t.Fatalf("ttl=%v 不应报错: %v", ttl, err)
		}
	}
	if st.ops != 0 {
		t.Errorf("ttl<=0 时不应产生任何存储写入，实际 %d 次调用", st.ops)
	}
}

// TestNilStoreDegrades nil Store（Redis 不可用）时全部降级放行，不 panic 不报错。
//
// 防的 bug：降级分支漏判断 → 无 Redis 环境启动后所有请求 500。
func TestNilStoreDegrades(t *testing.T) {
	ctx := context.Background()
	if err := BlacklistAdd(ctx, nil, "tk", time.Minute); err != nil {
		t.Errorf("nil Store 加入黑名单应静默返回: %v", err)
	}
	if Blacklisted(ctx, nil, "tk") {
		t.Error("nil Store 不应判定为已拉黑")
	}
	if err := SetLogoutAll(ctx, nil, "u", time.Now()); err != nil {
		t.Errorf("nil Store 设置全端登出应静默返回: %v", err)
	}
	if !LogoutAllAt(ctx, nil, "u").IsZero() {
		t.Error("nil Store 读取全端登出应返回零值")
	}
	if PermVer(ctx, nil, "u") != 0 {
		t.Error("nil Store 权限版本应为 0")
	}
	if BumpPermVer(ctx, nil, "u") != 0 {
		t.Error("nil Store bump 应返回 0")
	}
	BumpPermVerMany(ctx, nil, []string{"u1", "u2"}) // 不应 panic
}

// TestStoreFailureDegrades Store 报错时读操作降级（放行/返回 0），写操作返回错误。
//
// 防的 bug：Redis 抖动被当成「权限过期」，全站用户被踢下线。
func TestStoreFailureDegrades(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	st.fail = true
	if Blacklisted(ctx, st, "tk") {
		t.Error("Store 故障时不应判定为已拉黑")
	}
	if PermVer(ctx, st, "u") != 0 {
		t.Error("Store 故障时权限版本应为 0")
	}
	if BumpPermVer(ctx, st, "u") != 0 {
		t.Error("Store 故障时 bump 应返回 0")
	}
	if !LogoutAllAt(ctx, st, "u").IsZero() {
		t.Error("Store 故障时全端登出时间应为零值")
	}
	if err := SetLogoutAll(ctx, st, "u", time.Now()); err == nil {
		t.Error("Store 故障时写入应返回错误（由调用方决定是否上抛）")
	}
}

// TestPermVerBumpAndRead 权限版本单调递增（perm_ver 方案核心）。
//
// 防的 bug：bump 用了 Set 而非 INCR → 多客户端并发 bump 后版本回退，
// 已失效的旧 token 又能通过校验。
func TestPermVerBumpAndRead(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	if v := PermVer(ctx, st, "u-1"); v != 0 {
		t.Errorf("初始版本应为 0，实际 %d", v)
	}
	if v := BumpPermVer(ctx, st, "u-1"); v != 1 {
		t.Errorf("首次 bump 应为 1，实际 %d", v)
	}
	if v := BumpPermVer(ctx, st, "u-1"); v != 2 {
		t.Errorf("二次 bump 应为 2，实际 %d", v)
	}
	if v := PermVer(ctx, st, "u-1"); v != 2 {
		t.Errorf("读取应返回最新版本 2，实际 %d", v)
	}
	// 用户之间互不影响。
	if v := PermVer(ctx, st, "u-2"); v != 0 {
		t.Errorf("其他用户版本不应受影响，实际 %d", v)
	}
}

// TestBumpPermVerMany 批量 bump：角色变更波及全部持角色用户。
func TestBumpPermVerMany(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()
	ids := []string{"a", "b", "c"}
	BumpPermVerMany(ctx, st, ids)
	BumpPermVerMany(ctx, st, ids)
	for _, id := range ids {
		if v := PermVer(ctx, st, id); v != 2 {
			t.Errorf("用户 %s 版本应为 2，实际 %d", id, v)
		}
	}
	// 空列表不应写入。
	before := st.ops
	BumpPermVerMany(ctx, st, nil)
	if st.ops != before {
		t.Error("空用户列表不应产生存储调用")
	}
}

// TestLogoutAllTimestamp 全端登出时间戳：秒级往返、未设置返回零值、脏数据降级。
//
// 防的 bug：时间戳精度/解析不一致 → 登出后 token 判定不失效（或误杀刚签发的 token）。
func TestLogoutAllTimestamp(t *testing.T) {
	ctx := context.Background()
	st := newMemStore()

	if !LogoutAllAt(ctx, st, "u-3").IsZero() {
		t.Error("未登出时应返回零值")
	}
	at := time.Unix(time.Now().Unix(), 0)
	if err := SetLogoutAll(ctx, st, "u-3", at); err != nil {
		t.Fatalf("写入登出时间失败: %v", err)
	}
	got := LogoutAllAt(ctx, st, "u-3")
	if !got.Equal(at) {
		t.Errorf("时间戳往返不符: want=%v got=%v", at, got)
	}
	// 脏数据（非数字）应降级为零值而不是 panic。
	st.data[logoutAllKey("u-4")] = "not-a-number"
	if !LogoutAllAt(ctx, st, "u-4").IsZero() {
		t.Error("脏数据应降级为零值")
	}
}

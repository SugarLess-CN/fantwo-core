// Package authtoken 认证态存储：JWT 黑名单、全端登出时间戳、权限版本号。
//
// 设计（T-05b 方案）：
//   - 单端登出：把 access/refresh token 的摘要写入黑名单，TTL = token 剩余生命，
//     到期自动清理，不产生堆积；中间件验签后先查黑名单。
//   - 全端登出：记录 user:{id}:logout_all 的时间戳（秒），签发时间早于它的
//     token 一律拒绝（等价于让该用户所有 token 失效）。
//   - 权限版本：user:{id}:permver 单调递增，签发时写入 JWT 的 pv；
//     权限/角色变更时 bump，中间件比对不一致即 401 要求重新登录。
//
// 存储通过 Store 接口抽象（生产 = Redis，测试 = 内存替身）；
// nil Store 表示降级：所有方法返回零值且不报错，认证按纯 JWT 语义放行，
// 与骨架「懒连接降级」约定一致。
package authtoken

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store 认证态存储能力（Redis 语义的最小子集）。
type Store interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Exists(ctx context.Context, key string) (int64, error)
	Incr(ctx context.Context, key string) (int64, error)
	Del(ctx context.Context, key string) error
}

// redisStore go-redis 实现。
type redisStore struct{ c *redis.Client }

// NewRedisStore 用 go-redis 客户端构造 Store；rdb 为 nil 时返回 nil（降级语义）。
func NewRedisStore(rdb *redis.Client) Store {
	if rdb == nil {
		return nil
	}
	return &redisStore{c: rdb}
}

func (s *redisStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return s.c.Set(ctx, key, value, ttl).Err()
}

func (s *redisStore) Get(ctx context.Context, key string) (string, error) {
	return s.c.Get(ctx, key).Result()
}

func (s *redisStore) Exists(ctx context.Context, key string) (int64, error) {
	return s.c.Exists(ctx, key).Result()
}

func (s *redisStore) Incr(ctx context.Context, key string) (int64, error) {
	return s.c.Incr(ctx, key).Result()
}

func (s *redisStore) Del(ctx context.Context, key string) error {
	return s.c.Del(ctx, key).Err()
}

// Key 构造函数。
func BlacklistKey(token string) string  { return "jwt:blacklist:" + digest(token) }
func logoutAllKey(userID string) string { return "user:" + userID + ":logout_all" }
func permVerKey(userID string) string   { return "user:" + userID + ":permver" }

// digest 对 token 取 SHA-256 摘要（黑名单键不落明文 token）。
func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// BlacklistAdd 将 token 加入黑名单；ttl<=0 或 Store 不可用时跳过（已过期 token 无需记录）。
func BlacklistAdd(ctx context.Context, st Store, token string, ttl time.Duration) error {
	if st == nil || token == "" || ttl <= 0 {
		return nil
	}
	return st.Set(ctx, BlacklistKey(token), 1, ttl)
}

// Blacklisted token 是否已被拉黑。Store 异常时返回 false（放行，保持可用性）。
func Blacklisted(ctx context.Context, st Store, token string) bool {
	if st == nil || token == "" {
		return false
	}
	n, err := st.Exists(ctx, BlacklistKey(token))
	return err == nil && n > 0
}

// SetLogoutAll 记录全端登出时间（秒级，够 token iat 比对）。
func SetLogoutAll(ctx context.Context, st Store, userID string, at time.Time) error {
	if st == nil {
		return nil
	}
	return st.Set(ctx, logoutAllKey(userID), at.Unix(), 0)
}

// LogoutAllAt 读取全端登出时间；无记录返回零值。
func LogoutAllAt(ctx context.Context, st Store, userID string) time.Time {
	if st == nil {
		return time.Time{}
	}
	s, err := st.Get(ctx, logoutAllKey(userID))
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// PermVer 读权限版本号；无记录视为 0（未发生过权限变更）。
func PermVer(ctx context.Context, st Store, userID string) int64 {
	if st == nil {
		return 0
	}
	s, err := st.Get(ctx, permVerKey(userID))
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// BumpPermVer 递增权限版本号，返回新值（Store 不可用返回 0）。
func BumpPermVer(ctx context.Context, st Store, userID string) int64 {
	if st == nil {
		return 0
	}
	v, err := st.Incr(ctx, permVerKey(userID))
	if err != nil {
		return 0
	}
	return v
}

// BumpPermVerMany 批量递增（角色权限变更时波及该角色下全部用户）。
// 逐个 Incr：受影响用户通常在几十量级，无需 pipeline 复杂度。
func BumpPermVerMany(ctx context.Context, st Store, userIDs []string) {
	if st == nil || len(userIDs) == 0 {
		return
	}
	for _, id := range userIDs {
		_, _ = st.Incr(ctx, permVerKey(id))
	}
}

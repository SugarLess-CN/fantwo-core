// Package authtoken 认证态存储（Redis）：JWT 黑名单、全端登出时间戳、权限版本号。
//
// 设计（T-05b 方案）：
//   - 单端登出：把 access/refresh token 的摘要写入黑名单，TTL = token 剩余生命，
//     到期自动清理，不产生堆积；中间件验签后先查黑名单。
//   - 全端登出：记录 user:{id}:logout_all 的时间戳（秒），签发时间早于它的
//     token 一律拒绝（等价于让该用户所有 token 失效）。
//   - 权限版本：user:{id}:permver 单调递增，签发时写入 JWT 的 pv；
//     权限/角色变更时 bump，中间件比对不一致即 401 要求重新登录。
//
// Redis 不可用（降级模式）时所有方法返回零值且不报错——认证按纯 JWT 语义放行，
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

// Key 构造函数。
func BlacklistKey(token string) string  { return "jwt:blacklist:" + digest(token) }
func logoutAllKey(userID string) string { return "user:" + userID + ":logout_all" }
func permVerKey(userID string) string   { return "user:" + userID + ":permver" }

// digest 对 token 取 SHA-256 摘要（黑名单键不落明文 token）。
func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// BlacklistAdd 将 token 加入黑名单；ttl<=0 时按 1 秒处理（已过期 token 无需记录）。
func BlacklistAdd(ctx context.Context, rdb *redis.Client, token string, ttl time.Duration) error {
	if rdb == nil || token == "" {
		return nil
	}
	if ttl <= 0 {
		return nil
	}
	return rdb.Set(ctx, BlacklistKey(token), 1, ttl).Err()
}

// Blacklisted token 是否已被拉黑。Redis 异常时返回 false（放行，保持可用性）。
func Blacklisted(ctx context.Context, rdb *redis.Client, token string) bool {
	if rdb == nil || token == "" {
		return false
	}
	n, err := rdb.Exists(ctx, BlacklistKey(token)).Result()
	return err == nil && n > 0
}

// SetLogoutAll 记录全端登出时间（秒级，够 token iat 比对）。
func SetLogoutAll(ctx context.Context, rdb *redis.Client, userID string, at time.Time) error {
	if rdb == nil {
		return nil
	}
	return rdb.Set(ctx, logoutAllKey(userID), at.Unix(), 0).Err()
}

// LogoutAllAt 读取全端登出时间；无记录返回零值。
func LogoutAllAt(ctx context.Context, rdb *redis.Client, userID string) time.Time {
	if rdb == nil {
		return time.Time{}
	}
	s, err := rdb.Get(ctx, logoutAllKey(userID)).Result()
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
func PermVer(ctx context.Context, rdb *redis.Client, userID string) int64 {
	if rdb == nil {
		return 0
	}
	s, err := rdb.Get(ctx, permVerKey(userID)).Result()
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// BumpPermVer 递增权限版本号，返回新值。
func BumpPermVer(ctx context.Context, rdb *redis.Client, userID string) int64 {
	if rdb == nil {
		return 0
	}
	v, err := rdb.Incr(ctx, permVerKey(userID)).Result()
	if err != nil {
		return 0
	}
	return v
}

// BumpPermVerMany 批量递增（角色权限变更时波及该角色下全部用户）。
func BumpPermVerMany(ctx context.Context, rdb *redis.Client, userIDs []string) {
	if rdb == nil || len(userIDs) == 0 {
		return
	}
	pipe := rdb.Pipeline()
	for _, id := range userIDs {
		pipe.Incr(ctx, permVerKey(id))
	}
	_, _ = pipe.Exec(ctx)
}

// Package service 验证码存储：发送/校验/防重发。
//
// 对齐原 user.service.ts sendVerificationCode / verifyCode：
//   - 键：verify:code:{scene}:{target}，TTL 10 分钟；
//   - 防重发：verify:sent:{scene}:{target}，TTL 1 分钟；
//   - 验证成功即删（一次性）。
//
// 存储经 CodeStore 接口抽象：生产用 Redis，测试用内存替身（无需真实 Redis）。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// errTooFrequent 1 分钟内重复发送。
var errTooFrequent = errors.New("verification code sent too frequently")

// 验证码 TTL 与防重发窗口（对齐原仓库 10 分钟 / 1 分钟）。
const (
	codeTTL     = 10 * time.Minute
	codeSendGap = time.Minute
)

// codeKey / sentKey 键构造。
func codeKey(scene, target string) string { return fmt.Sprintf("verify:code:%s:%s", scene, target) }
func sentKey(scene, target string) string { return fmt.Sprintf("verify:sent:%s:%s", scene, target) }

// CodeStore 验证码存储能力（Redis 语义子集；测试可用内存替身实现）。
type CodeStore interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Exists(ctx context.Context, key string) (int64, error)
	Del(ctx context.Context, key string) error
}

// redisCodeStore go-redis 实现。
type redisCodeStore struct{ c *redis.Client }

func (s *redisCodeStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return s.c.Set(ctx, key, value, ttl).Err()
}

func (s *redisCodeStore) Get(ctx context.Context, key string) (string, error) {
	return s.c.Get(ctx, key).Result()
}

func (s *redisCodeStore) Exists(ctx context.Context, key string) (int64, error) {
	return s.c.Exists(ctx, key).Result()
}

func (s *redisCodeStore) Del(ctx context.Context, key string) error {
	return s.c.Del(ctx, key).Err()
}

// VerifyCodeStore 验证码存取。
type VerifyCodeStore struct {
	kv CodeStore
}

// NewVerifyCodeStore 用 Redis 客户端构造（rdb 为 nil 时降级：发送/校验返回失败）。
func NewVerifyCodeStore(rdb *redis.Client) *VerifyCodeStore {
	if rdb == nil {
		return &VerifyCodeStore{}
	}
	return &VerifyCodeStore{kv: &redisCodeStore{c: rdb}}
}

// NewVerifyCodeStoreWith 注入自定义存储（测试用内存替身）。
func NewVerifyCodeStoreWith(kv CodeStore) *VerifyCodeStore { return &VerifyCodeStore{kv: kv} }

// Send 生成并存 6 位验证码，返回该验证码；1 分钟内重复发送返回 errTooFrequent。
// 实际投递（邮件/短信）由调用方经 provider 完成（本方法只负责存取）。
func (s *VerifyCodeStore) Send(ctx context.Context, target, scene string) (string, error) {
	if s.kv == nil {
		return "", fmt.Errorf("redis unavailable")
	}
	exists, err := s.kv.Exists(ctx, sentKey(scene, target))
	if err != nil {
		return "", err
	}
	if exists > 0 {
		return "", errTooFrequent
	}
	code := genCode()
	if err := s.kv.Set(ctx, codeKey(scene, target), code, codeTTL); err != nil {
		return "", err
	}
	if err := s.kv.Set(ctx, sentKey(scene, target), 1, codeSendGap); err != nil {
		return "", err
	}
	return code, nil
}

// Verify 校验验证码；成功即焚。code 为空或不存在返回 false。
func (s *VerifyCodeStore) Verify(ctx context.Context, target, scene, code string) (bool, error) {
	if s.kv == nil || code == "" {
		return false, nil
	}
	key := codeKey(scene, target)
	stored, err := s.kv.Get(ctx, key)
	if err != nil {
		return false, nil // 不存在/过期
	}
	if stored != code {
		return false, nil
	}
	_ = s.kv.Del(ctx, key)
	return true, nil
}

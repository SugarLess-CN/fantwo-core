// Package service 验证码存储（Redis）：发送/校验/防重发。
//
// 对齐原 user.service.ts sendVerificationCode / verifyCode：
//   - 键：verify:code:{scene}:{target}，TTL 10 分钟；
//   - 防重发：verify:sent:{scene}:{target}，TTL 1 分钟；
//   - 验证成功即删（一次性）。
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

// VerifyCodeStore 验证码存取。
type VerifyCodeStore struct {
	rdb *redis.Client
}

// NewVerifyCodeStore 构造（rdb 为 nil 时降级：发送/校验直接失败返回错误）。
func NewVerifyCodeStore(rdb *redis.Client) *VerifyCodeStore { return &VerifyCodeStore{rdb: rdb} }

// Send 生成并存 6 位验证码；1 分钟内重复发送返回 errTooFrequent。
// 发送动作由调用方 mailer/sms provider 完成（本方法只负责存取）。
func (s *VerifyCodeStore) Send(ctx context.Context, target, scene string) error {
	if s.rdb == nil {
		return fmt.Errorf("redis unavailable")
	}
	exists, err := s.rdb.Exists(ctx, sentKey(scene, target)).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return errTooFrequent
	}
	code := genCode()
	if err := s.rdb.Set(ctx, codeKey(scene, target), code, codeTTL).Err(); err != nil {
		return err
	}
	return s.rdb.Set(ctx, sentKey(scene, target), 1, codeSendGap).Err()
}

// Verify 校验验证码；成功即焚。code 为空或不存在返回 false。
func (s *VerifyCodeStore) Verify(ctx context.Context, target, scene, code string) (bool, error) {
	if s.rdb == nil || code == "" {
		return false, nil
	}
	key := codeKey(scene, target)
	stored, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		return false, nil // 不存在/过期
	}
	if stored != code {
		return false, nil
	}
	_ = s.rdb.Del(ctx, key).Err()
	return true, nil
}

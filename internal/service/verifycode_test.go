package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestVerifyCodeSendAndVerify 验证码全链路：发送 → 校验成功 → 二次校验失败（一次性）。
//
// 防的 bug：校验成功后未删除验证码 → 同一验证码可反复使用（重放注册/重置密码）。
func TestVerifyCodeSendAndVerify(t *testing.T) {
	ctx := context.Background()
	kv := newMemKV()
	s := NewVerifyCodeStoreWith(kv)

	code, err := s.Send(ctx, "a@b.com", "reset_password")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("验证码应为 6 位，实际 %q", code)
	}
	if _, err := strconvParse(code); err != nil {
		t.Errorf("验证码应为纯数字，实际 %q", code)
	}

	ok, err := s.Verify(ctx, "a@b.com", "reset_password", code)
	if err != nil || !ok {
		t.Fatalf("首次校验应成功: ok=%v err=%v", ok, err)
	}
	// 一次性：验后即焚
	ok, err = s.Verify(ctx, "a@b.com", "reset_password", code)
	if err != nil {
		t.Fatalf("二次校验不应报错: %v", err)
	}
	if ok {
		t.Error("同一验证码不应可重复使用")
	}
}

// TestVerifyCodeRejectsWrongCode 错误验证码不得通过。
func TestVerifyCodeRejectsWrongCode(t *testing.T) {
	ctx := context.Background()
	s := NewVerifyCodeStoreWith(newMemKV())
	if _, err := s.Send(ctx, "13800138000", "login"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	ok, err := s.Verify(ctx, "13800138000", "login", "000000")
	if err != nil {
		t.Fatalf("错误码校验不应报错: %v", err)
	}
	if ok {
		t.Error("错误验证码不应通过")
	}
}

// TestVerifyCodeSceneIsolation 不同场景/不同目标的验证码互不干扰。
//
// 防的 bug：key 未带 scene/target → 重置密码的码能用来登录，或 A 的码能验 B。
func TestVerifyCodeSceneIsolation(t *testing.T) {
	ctx := context.Background()
	s := NewVerifyCodeStoreWith(newMemKV())
	codeA, err := s.Send(ctx, "a@b.com", "reset_password")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	// 同目标不同场景
	if ok, _ := s.Verify(ctx, "a@b.com", "verification", codeA); ok {
		t.Error("跨场景不应通过")
	}
	// 同场景不同目标
	if ok, _ := s.Verify(ctx, "other@b.com", "reset_password", codeA); ok {
		t.Error("跨目标不应通过")
	}
	// 原场景仍可用
	if ok, _ := s.Verify(ctx, "a@b.com", "reset_password", codeA); !ok {
		t.Error("原场景应仍可通过")
	}
}

// TestVerifyCodeResendThrottled 1 分钟内重复发送被拒（防短信轰炸/邮件轰炸）。
func TestVerifyCodeResendThrottled(t *testing.T) {
	ctx := context.Background()
	s := NewVerifyCodeStoreWith(newMemKV())
	if _, err := s.Send(ctx, "13800138000", "login"); err != nil {
		t.Fatalf("首次发送失败: %v", err)
	}
	_, err := s.Send(ctx, "13800138000", "login")
	if !errors.Is(err, errTooFrequent) {
		t.Errorf("1 分钟内重发应返回 errTooFrequent，实际 %v", err)
	}
}

// TestVerifyCodeExpiry 验证码 TTL 到期后不可校验。
//
// 防的 bug：TTL 未设置 → 验证码永不过期，泄露后可长期使用。
func TestVerifyCodeExpiry(t *testing.T) {
	ctx := context.Background()
	kv := newMemKV()
	s := NewVerifyCodeStoreWith(kv)
	code, err := s.Send(ctx, "13800138000", "login")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	kv.expireNow(sentKey("login", "13800138000"))
	kv.expireNow(codeKey("login", "13800138000"))
	if ok, _ := s.Verify(ctx, "13800138000", "login", code); ok {
		t.Error("过期验证码不应通过")
	}
}

// TestVerifyCodeTTLConstants 有效期常量锁定为原仓库语义（10 分钟 / 1 分钟）。
func TestVerifyCodeTTLConstants(t *testing.T) {
	if codeTTL != 10*time.Minute {
		t.Errorf("验证码有效期应为 10 分钟，实际 %v", codeTTL)
	}
	if codeSendGap != time.Minute {
		t.Errorf("防重发窗口应为 1 分钟，实际 %v", codeSendGap)
	}
}

// TestVerifyCodeEmptyInputs 空 code / nil 存储的边界行为（不得 panic、不得误判通过）。
func TestVerifyCodeEmptyInputs(t *testing.T) {
	ctx := context.Background()
	s := NewVerifyCodeStoreWith(newMemKV())
	if ok, err := s.Verify(ctx, "a@b.com", "login", ""); ok || err != nil {
		t.Errorf("空验证码应返回 false/nil，实际 ok=%v err=%v", ok, err)
	}
	// 未配置 Redis（nil 存储）：发送报错、校验返回 false 而非 panic
	nilStore := NewVerifyCodeStore(nil)
	if _, err := nilStore.Send(ctx, "a@b.com", "login"); err == nil {
		t.Error("无存储时发送应报错")
	}
	if ok, err := nilStore.Verify(ctx, "a@b.com", "login", "123456"); ok || err != nil {
		t.Error("无存储时校验应安全返回 false")
	}
}

// TestVerifyCodeStoreFailure 存储故障时发送返回错误（不能静默「发送成功」）。
func TestVerifyCodeStoreFailure(t *testing.T) {
	ctx := context.Background()
	kv := newMemKV()
	kv.fail = true
	s := NewVerifyCodeStoreWith(kv)
	if _, err := s.Send(ctx, "a@b.com", "login"); err == nil {
		t.Error("存储故障时发送应报错")
	}
}

// TestGenCodeFormat 验证码格式：6 位纯数字、区间 [100000,999999]、多次生成存在差异。
func TestGenCodeFormat(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 200; i++ {
		code := genCode()
		if len(code) != 6 {
			t.Fatalf("应为 6 位，实际 %q", code)
		}
		if strings.HasPrefix(code, "0") {
			t.Fatalf("不应以 0 开头（原实现 100000+rand），实际 %q", code)
		}
		n, err := strconvParse(code)
		if err != nil || n < 100000 || n > 999999 {
			t.Fatalf("超出区间: %q", code)
		}
		seen[code] = struct{}{}
	}
	if len(seen) < 50 {
		t.Errorf("200 次生成仅 %d 个不同值，随机性异常", len(seen))
	}
}

// strconvParse 简易十进制解析（避免测试文件额外 import）。
func strconvParse(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not digit")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

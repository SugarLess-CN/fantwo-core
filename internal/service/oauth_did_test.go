package service

// OAuth 的 device-id 契约测试（2026-11-28 用户规定：did 必填且走请求头）。
//
// 这些断言守的是**对外行为契约**，不是实现细节：
//   · /url 阶段就拒绝空 did（而不是等回调才报 20016，让用户白跑一趟）
//   · did 长度上限（它会被存进 Redis 的 state）
//   · state 能把它带回回调（浏览器 302 加不了 header 的唯一出路）

import (
	"context"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
)

// TestOAuthAuthURLRequiresDeviceID did 为空必须在**取授权链接**阶段就拒绝。
func TestOAuthAuthURLRequiresDeviceID(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = newMemKV()
	svc := NewAuthService(d)
	ctx := context.Background()

	for _, did := range []string{"", "   ", "\t\n"} {
		_, e := svc.oauthAuthURL(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, did)
		if e == nil {
			t.Fatalf("did=%q 应被拒绝", did)
		}
		if e.BizCode != errcode.BizDeviceIDRequired {
			t.Fatalf("did=%q 应返回 20016，实际 %d", did, e.BizCode)
		}
	}
}

// TestOAuthAuthURLRejectsOverlongDeviceID 超长 did 必须拒绝（state 值会进 Redis）。
func TestOAuthAuthURLRejectsOverlongDeviceID(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = newMemKV()
	svc := NewAuthService(d)
	ctx := context.Background()

	overlong := strings.Repeat("a", oauthMaxDIDLen+1)
	_, e := svc.oauthAuthURL(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, overlong)
	if e == nil {
		t.Fatal("超长 did 应被拒绝")
	}
	if e.BizCode != errcode.BizInvalidParam {
		t.Fatalf("超长 did 应返回 10008，实际 %d", e.BizCode)
	}
}

// TestOAuthAuthURLAcceptsMaxLengthDeviceID 边界：正好 128 字节要放行。
func TestOAuthAuthURLAcceptsMaxLengthDeviceID(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = newMemKV()
	svc := NewAuthService(d)
	ctx := context.Background()

	maxLen := strings.Repeat("a", oauthMaxDIDLen)
	// 该用例只验「不被长度校验拦住」，配置缺失导致的错误是允许的（本测试没有配置 provider）。
	_, e := svc.oauthAuthURL(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, maxLen)
	if e != nil && e.BizCode == errcode.BizInvalidParam {
		t.Fatalf("128 字节应放行，不该是参数错误：%v", e)
	}
}

// TestOAuthStateRoundTripDeviceID 回调能取回发起时的 did（浏览器 302 无法带 header）。
func TestOAuthStateRoundTripDeviceID(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = newMemKV()
	svc := NewAuthService(d)
	ctx := context.Background()

	const did = "device-from-header"
	st, e := svc.issueOAuthState(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb, did)
	if e != nil {
		t.Fatalf("签发 state 失败: %v", e)
	}
	got, e2 := svc.verifyOAuthState(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb, st)
	if e2 != nil {
		t.Fatalf("校验 state 失败: %v", e2)
	}
	if got != did {
		t.Fatalf("回调取回的 did = %q, want %q", got, did)
	}
}

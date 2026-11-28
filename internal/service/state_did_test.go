package service

// 验证 OAuth 回调的 device-id 能从 state 取回。
//
// 为什么需要这条：OAuth 回调是**浏览器 302 跳转**（微信/QQ/GitHub 平台发起），
// 前端无法给这个请求加自定义 header，而 refresh_token 必须与设备绑定。
// 所以发起授权时把 did 存进 state（我们自己的随机串，平台原样回传），回调时取回。

import (
	"context"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
)

func TestOAuthStateCarriesDeviceID(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	kv := newMemKV()
	d.AuthTok = kv
	svc := NewAuthService(d)
	ctx := context.Background()

	// ① 发起授权时带 did → 回调能取回同一个值
	st1, e := svc.issueOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "dev-abc")
	if e != nil {
		t.Fatalf("签发失败: %v", e)
	}
	got, e2 := svc.verifyOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, st1)
	if e2 != nil {
		t.Fatalf("校验失败: %v", e2)
	}
	if got != "dev-abc" {
		t.Fatalf("deviceID = %q, want dev-abc", got)
	}

	// ② 不带 did → 存占位，取回空串（不报错，由调用方决定是否要求）
	st2, _ := svc.issueOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "")
	got2, e3 := svc.verifyOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, st2)
	if e3 != nil {
		t.Fatalf("校验失败: %v", e3)
	}
	if got2 != "" {
		t.Fatalf("deviceID = %q, want 空串", got2)
	}

	// ③ 一次性消费：同一个 state 再验必须失败（防重放）
	if _, e4 := svc.verifyOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, st2); e4 == nil {
		t.Fatal("state 应已被消费，第二次校验必须失败")
	}
}

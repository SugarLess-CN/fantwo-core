package oauthkit

// 第 1 层：真实响应 fixture 的解析断言（零外部依赖，CI 主力）。
//
// 与 fixtures_test.go 配套：常量在那边，这里只放断言。
// 这些用例防的是「代码在文档构造的样本上全绿、遇到真实字节流就崩」：
// 真实响应带 rid、带尾逗号、带 HTML WAF 页，形状远比文档脏。

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestRealWechatInvalidCodeMapsToCodeInvalid 真实微信 40029 响应必须映射 ErrCodeInvalid。
//
// 为什么重要：40029 是「引导用户重新授权」，而凭据错（40013/40125 → ErrCredentialBad）
// 是运维事故，两者处置相反。真实响应的 errcode 是 JSON 数字，曾被实现对成字符串类型，
// 改动解析层时这条最先红。
func TestRealWechatInvalidCodeMapsToCodeInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"web": realWechatWebInvalidCode,
		"app": realWechatAppInvalidCode,
	} {
		var resp wechatTokenResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("[%s] 真实响应应能解析: %v", name, err)
		}
		err := resp.err()
		if !errors.Is(err, ErrCodeInvalid) {
			t.Errorf("[%s] 真实 40029 应映射 ErrCodeInvalid，实际 %v", name, err)
		}
		// 反向断言：绝不能误判成「凭据错」—— 那会让运维去翻密钥，方向完全错。
		if errors.Is(err, ErrCredentialBad) {
			t.Errorf("[%s] 40029 不得误判为 ErrCredentialBad", name)
		}
	}
}

// TestRealQQMeIllegalTokenMapsToTokenExpired 真实 QQ /me 100013 必须映射 ErrTokenExpired。
//
// 100013 = access token is illegal → 用户重新授权（不是我们配错了）。
func TestRealQQMeIllegalTokenMapsToTokenExpired(t *testing.T) {
	var me qqMeResponse
	if err := json.Unmarshal(stripJSONP([]byte(realQQMeIllegalToken)), &me); err != nil {
		t.Fatalf("真实 JSONP 错误体应能剥壳后解析: %v", err)
	}
	code := int64(me.Error)
	if code != 100013 {
		t.Fatalf("解析出的 error 码应为 100013，实际 %d（stripJSONP 或 qqFlexInt 回归）", code)
	}
	err := qqCodeError(code, me.ErrorDescription)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("100013 应映射 ErrTokenExpired，实际 %v", err)
	}
	if errors.Is(err, ErrCredentialBad) {
		t.Errorf("100013 不得误判为 ErrCredentialBad")
	}
}

// TestRealQQUserInfoInvalidOpenIDRejected 真实 QQ userinfo `{"ret":-22,...}` 必须判失败。
//
// 防的 bug：QQ 业务错误 **HTTP 恒 200**，只看状态码会把「openid 非法」当成功，
// 返回一个昵称头像全空的 User —— 拿它建号会污染用户数据。
func TestRealQQUserInfoInvalidOpenIDRejected(t *testing.T) {
	var ui qqUserInfoResponse
	if err := json.Unmarshal([]byte(realQQUserInfoInvalidOpenID), &ui); err != nil {
		t.Fatalf("真实响应应能解析: %v", err)
	}
	ret := int64(ui.Ret)
	if ret == 0 {
		t.Fatalf("qqFlexInt 应解出 -22，实际 0（解析静默变 0 会把错误当成功）")
	}
	if err := qqCodeError(ret, ui.Msg); err == nil {
		t.Error("ret != 0 必须返回错误")
	}
	// -22 不在 QQ 公共返回码全表里 → 兜底 ErrUpstream，不得猜测性映射成用户可修复的错误。
	if !errors.Is(qqCodeError(ret, ui.Msg), ErrUpstream) {
		t.Errorf("未知 ret 应兜底 ErrUpstream，实际 %v", qqCodeError(ret, ui.Msg))
	}
}

// TestRealQQWAFBlockPageRejectedAsTokenResponse WAF 拦截页（HTML，HTTP 501）
// 必须被拒为 ErrUpstream，**绝不能**被当成「成功但没有 token」。
//
// 这是「能解析 ≠ 是平台的响应」的活体样本：
//
//	url.ParseQuery 对这一页返回 nil error（它 tolerant 得危险），
//	若不判「必须命中已知字段」，就会拿到空 payload → 最终报
//	"no access_token"，把「上游挂了」误导成「QQ 凭什么不给 token」。
func TestRealQQWAFBlockPageRejectedAsTokenResponse(t *testing.T) {
	payload, err := qqParseTokenBody([]byte(realQQWAFBlockPage))
	if err == nil {
		t.Fatalf("WAF HTML 页必须被拒，实际解析成功: payload=%+v", payload)
	}
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("WAF 拦截页应映射 ErrUpstream，实际 %v", err)
	}
	if payload != nil {
		t.Error("拒绝时不得返回 payload（否则调用方可能继续用空 token）")
	}
}

// TestRealQQWAFBlockPageRejectedAsMe /me 解析路径对同一 WAF 页也必须失败。
//
// 与上一条覆盖不同代码路径：/me 走 stripJSONP + json.Unmarshal，
// 而 stripJSONP 是「幂等剥壳」实现（不含 callback( 就当 JSON 解）——
// 要确认它不会把 HTML 页「剥」成一个能解的对象。
func TestRealQQWAFBlockPageRejectedAsMe(t *testing.T) {
	var me qqMeResponse
	if err := json.Unmarshal(stripJSONP([]byte(realQQWAFBlockPage)), &me); err == nil {
		t.Fatalf("WAF HTML 页不得被 /me 解析成功: %+v", me)
	}
}

// TestRealQQWAFBlockPageNotParseableAsQueryWithKnownKeys 反证：WAF 页「解析成功」
// 恰恰是危险的，必须靠「已知字段」判定兜住。
//
// 这条不是测产品代码，而是把「为什么会出问题」钉在测试里：
// 将来有人想简化 qqParseTokenBody、删掉 known 字段判定时，这里会提醒他
// url.ParseQuery 对 HTML 页是不报错的。
func TestRealQQWAFBlockPageNotParseableAsQueryWithKnownKeys(t *testing.T) {
	if _, err := qqParseTokenBody([]byte(realQQWAFBlockPage)); err == nil {
		t.Fatal("前提失效：当前实现已放行 WAF 页，请立刻检查 known 字段判定")
	}
	// 说明性断言：HTML 页里确实含 = 号（这就是 ParseQuery 容错坑的来源）。
	if !containsByte([]byte(realQQWAFBlockPage), '=') {
		t.Fatal("fixture 已失真（不再含 = 号），需重新抓取真实 WAF 页")
	}
}

// containsByte 判断字节串是否含某字节（避免为一条断言引入 bytes 依赖语义）。
func containsByte(b []byte, c byte) bool {
	for _, x := range b {
		if x == c {
			return true
		}
	}
	return false
}

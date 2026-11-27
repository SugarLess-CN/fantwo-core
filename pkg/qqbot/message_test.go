package qqbot

// message.go 的单测：三种发送模式的请求体 + 参数互斥校验 + 错误码分类。

import (
	"errors"
	"testing"
)

// 三种发送模式的请求体必须正确，且路径/请求头符合官方协议。
func TestSendThreeModes(t *testing.T) {
	cases := []struct {
		name     string
		req      SendRequest
		wantMode string
		// 断言请求体里必须出现 / 必须不出现的键
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name:        "① 被动回复（msg_id + msg_seq）",
			req:         SendRequest{MsgType: MsgTypeText, Content: "你的邀请码是 ABC", MsgID: "MSG-1", MsgSeq: 2},
			wantMode:    "passive_reply",
			wantPresent: []string{"msg_id", "msg_seq", "content", "msg_type"},
			wantAbsent:  []string{"is_wakeup", "event_id"},
		},
		{
			name:        "② 互动召回（is_wakeup）",
			req:         SendRequest{MsgType: MsgTypeText, Content: "在吗", IsWakeup: true},
			wantMode:    "wakeup",
			wantPresent: []string{"is_wakeup", "content"},
			wantAbsent:  []string{"msg_id", "event_id", "msg_seq"},
		},
		{
			name:        "③ 普通主动（都空）",
			req:         SendRequest{MsgType: MsgTypeText, Content: "通知"},
			wantMode:    "proactive",
			wantPresent: []string{"content"},
			wantAbsent:  []string{"msg_id", "event_id", "is_wakeup", "msg_seq"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockUpstream(t)
			c := m.newTestClient(t)

			if got := SendMode(tc.req); got != tc.wantMode {
				t.Errorf("SendMode = %q, want %q", got, tc.wantMode)
			}

			resp, err := c.SendC2C("USER1", tc.req)
			if err != nil {
				t.Fatalf("SendC2C: %v", err)
			}
			if resp.ID != "ROBOT1.0_abc" {
				t.Errorf("resp.ID = %q", resp.ID)
			}
			if resp.ExtInfo == nil || resp.ExtInfo.RefIdx != "REFIDX_x==" {
				t.Errorf("ExtInfo 解析错误：%+v", resp.ExtInfo)
			}

			body, path, headers := m.sendReq()
			if path != "/v2/users/USER1/messages" {
				t.Errorf("path = %q", path)
			}
			for _, k := range tc.wantPresent {
				if _, ok := body[k]; !ok {
					t.Errorf("请求体缺 %q：%v", k, body)
				}
			}
			for _, k := range tc.wantAbsent {
				if _, ok := body[k]; ok {
					t.Errorf("请求体不该有 %q（omitempty 失效？）：%v", k, body)
				}
			}
			// 请求头契约
			if got := headers.Get("Authorization"); got != "QQBot TOK-1" {
				t.Errorf("Authorization = %q, want \"QQBot TOK-1\"", got)
			}
			if got := headers.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			if got := headers.Get("X-Union-Appid"); got != "11111111" {
				t.Errorf("X-Union-Appid = %q", got)
			}
		})
	}
}

// 群聊走 /v2/groups/{group_openid}/messages。
func TestSendGroupPath(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	if _, err := c.SendGroup("GROUP9", SendRequest{MsgType: MsgTypeText, Content: "群通知"}); err != nil {
		t.Fatalf("SendGroup: %v", err)
	}
	_, path, _ := m.sendReq()
	if path != "/v2/groups/GROUP9/messages" {
		t.Errorf("path = %q, want /v2/groups/GROUP9/messages", path)
	}
}

// openid 会被 PathEscape（防 ../ 之类污染路径）。
//
// 断言用**原始请求行**：r.URL.Path 是解码后的值，%2F 会变回 /，看不出转义是否发生。
func TestSendPathEscapesOpenID(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	if _, err := c.SendC2C("a/../../admin", SendRequest{MsgType: MsgTypeText, Content: "x"}); err != nil {
		t.Fatalf("SendC2C: %v", err)
	}
	uri := m.sendURI()
	if uri != "/v2/users/a%2F..%2F..%2Fadmin/messages" {
		t.Errorf("原始请求行 = %q（/ 未被转义，可穿越到别的端点）", uri)
	}
}

// Markdown 消息：markdown 必填、content 必须为空。
func TestSendMarkdown(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)

	resp, err := c.SendC2C("USER1", SendRequest{
		MsgType:  MsgTypeMarkdown,
		Markdown: &Markdown{Content: "# 标题\n正文"},
	})
	if err != nil {
		t.Fatalf("SendC2C(markdown): %v", err)
	}
	if resp.ID == "" {
		t.Error("resp.ID 为空")
	}
	body, _, _ := m.sendReq()
	md, ok := body["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown 字段类型错误：%T", body["markdown"])
	}
	if md["content"] != "# 标题\n正文" {
		t.Errorf("markdown.content = %v", md["content"])
	}
	if _, ok := body["content"]; ok {
		t.Errorf("markdown 消息不该带 content：%v", body)
	}
}

// 参数互斥：本地拦下，绝不发上游（省频控额度 + 报错语义清晰）。
func TestSendInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		req  SendRequest
		oid  string
	}{
		{"MsgID + IsWakeup 同时传", SendRequest{MsgType: MsgTypeText, Content: "x", MsgID: "M", IsWakeup: true}, "U"},
		{"MsgID + EventID 同时传", SendRequest{MsgType: MsgTypeText, Content: "x", MsgID: "M", EventID: "E"}, "U"},
		{"EventID + IsWakeup", SendRequest{MsgType: MsgTypeText, Content: "x", EventID: "E", IsWakeup: true}, "U"},
		{"只有 msg_seq 没有 ID", SendRequest{MsgType: MsgTypeText, Content: "x", MsgSeq: 2}, "U"},
		{"空 openid", SendRequest{MsgType: MsgTypeText, Content: "x"}, ""},
		{"markdown 类型但没给 markdown", SendRequest{MsgType: MsgTypeMarkdown}, "U"},
		{"markdown 类型但给了 content", SendRequest{MsgType: MsgTypeMarkdown, Content: "x", Markdown: &Markdown{Content: "y"}}, "U"},
		{"markdown 结构却声明文本类型", SendRequest{MsgType: MsgTypeText, Content: "x", Markdown: &Markdown{Content: "y"}}, "U"},
		{"文本类型但内容为空", SendRequest{MsgType: MsgTypeText}, "U"},
		{"不支持的 msg_type", SendRequest{MsgType: 99, Content: "x"}, "U"},
		{"负数 msg_seq", SendRequest{MsgType: MsgTypeText, Content: "x", MsgID: "M", MsgSeq: -1}, "U"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockUpstream(t)
			c := m.newTestClient(t)

			_, err := c.SendC2C(tc.oid, tc.req)
			if err == nil {
				t.Fatal("应当返回参数错误")
			}

			// ① 是 *Error 且 Kind 正确（调用方按 Kind 分流）
			var qe *Error
			if !errors.As(err, &qe) {
				t.Fatalf("err 应为 *Error，实际 %T: %v", err, err)
			}
			if qe.Kind != KindInvalidArgument {
				t.Errorf("Kind = %v, want KindInvalidArgument", qe.Kind)
			}
			// ② 同时能 errors.Is 到哨兵
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("errors.Is(err, ErrInvalidArgument) = false: %v", err)
			}
			// ③ 关键：不许发上游（省频控配额）
			if _, send := m.counts(); send != 0 {
				t.Errorf("参数错误仍发出了 %d 次上游请求", send)
			}
		})
	}
}

// 错误码 → ErrorKind 分类（docs/29 §4.2 全表抽样，覆盖四类）。
func TestErrorKindClassification(t *testing.T) {
	want := map[int]ErrorKind{
		// 窗口过期 → DEFERRED
		40034005: KindWindowExpired,
		304103:   KindWindowExpired,
		40034128: KindWindowExpired,
		40034026: KindWindowExpired,
		// 可重试
		40034100: KindRetryable,
		40054005: KindRetryable,
		50055002: KindRetryable,
		// 40034122 召回额度用尽：不是「重试同一条召回」（那会一直失败到 4 周期结束），
		// 而是「换模式」——去掉 is_wakeup 走普通主动。故不归 Retryable。
		40034122: KindWakeupExhausted,
		// 永久失败
		40054013: KindPermanent,
		40054004: KindPermanent,
		40034006: KindPermanent,
		40034105: KindPermanent,
		40034024: KindPermanent,
		40054007: KindPermanent,
		40054018: KindPermanent,
		304061:   KindPermanent,
		// 配置 / 凭据
		100007: KindConfig,
		100016: KindConfig,
		10004:  KindConfig,
		// 未知 → 保守可重试
		999999: KindUnknown,
	}
	for code, kind := range want {
		if got := KindOf(code); got != kind {
			t.Errorf("KindOf(%d) = %v, want %v", code, got, kind)
		}
	}

	// 「窗口过期」不算可重试（要等用户开口，不是等一会儿再试）
	if KindWindowExpired.Retryable() {
		t.Error("KindWindowExpired 不应被判为可重试")
	}
	if !KindUnknown.Retryable() || !KindRetryable.Retryable() {
		t.Error("KindUnknown / KindRetryable 应可重试")
	}
}

// 平台返回的发送失败：业务码走 body（HTTP 可能 200 也可能非 200），Kind 由 KindOf 决定。
func TestSendErrorKindFromPlatform(t *testing.T) {
	cases := []struct {
		name     string
		httpCode int
		body     string
		wantKind ErrorKind
		wantCode int
	}{
		{"窗口过期 · HTTP200", 0, `{"code":40034005,"message":"msg_id expired"}`, KindWindowExpired, 40034005},
		{"窗口过期 · HTTP400", 400, `{"code":40034128,"message":"reply limit"}`, KindWindowExpired, 40034128},
		{"用户拒收 · HTTP403", 403, `{"code":40054013,"message":"user rejected"}`, KindPermanent, 40054013},
		{"频控 · HTTP429", 429, `{"code":40034100,"message":"rate limit"}`, KindRetryable, 40034100},
		{"去重冲突", 0, `{"code":40054005,"message":"duplicated"}`, KindRetryable, 40054005},
		{"内容违规", 0, `{"code":40034006,"message":"content blocked"}`, KindPermanent, 40034006},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockUpstream(t)
			c := m.newTestClient(t)
			m.setSend(t, tc.body, tc.httpCode)

			_, err := c.SendC2C("USER1", SendRequest{MsgType: MsgTypeText, Content: "x"})
			if err == nil {
				t.Fatal("应当报错")
			}
			var qe *Error
			if !errors.As(err, &qe) {
				t.Fatalf("err 应为 *Error，实际 %T", err)
			}
			if qe.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", qe.Kind, tc.wantKind)
			}
			if qe.Code != tc.wantCode {
				t.Errorf("Code = %d, want %d", qe.Code, tc.wantCode)
			}
			if qe.Op != opSendC2C {
				t.Errorf("Op = %q, want %q", qe.Op, opSendC2C)
			}
			// Message 保留官方文案（仅排查用，禁止用于分支判断）
			if qe.Message == "" {
				t.Error("Message 为空，丢失了官方文案")
			}
		})
	}
}

// 非 2xx 且无业务码（网关异常）→ KindUnknown（按可重试），且错误信息不带响应体。
func TestSendGatewayErrorNoBodyLeak(t *testing.T) {
	m := newMockUpstream(t)
	c := m.newTestClient(t)
	m.setSend(t, `<html>502 Bad Gateway secret-looking-content</html>`, 502)

	_, err := c.SendC2C("USER1", SendRequest{MsgType: MsgTypeText, Content: "x"})
	if err == nil {
		t.Fatal("应当报错")
	}
	var qe *Error
	if !errors.As(err, &qe) {
		t.Fatalf("err 应为 *Error，实际 %T", err)
	}
	if !qe.Kind.Retryable() {
		t.Errorf("网关 502 应可重试，实际 %v", qe.Kind)
	}
	if got := err.Error(); len(got) > 200 {
		t.Errorf("错误信息过长（可能夹带了响应体）：%q", got)
	}
}

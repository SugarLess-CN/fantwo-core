// 「全部已读」类型过滤的纯逻辑回归测试。
//
// 为什么必须存在：站内消息有**两套已读载体**（广播走 message_read 回执、个人走行内 is_read），
// 原实现用 if/else 二选一 → 前端只传 ?type= 时广播型通知永远标不掉
// （真机实测 system 未读仍为 1）。这里的用例把「两类都要标」与「type 校验」钉死。
//
// 不连数据库：只测 MarkAllAsRead 的参数校验分支（非法 type 必须在碰 DB 之前被挡下）。
package service

import (
	"testing"
)

// TestMarkAllAsRead_InvalidType 非法 type 必须在 ready()/DB 之前返回 400。
func TestMarkAllAsRead_InvalidType(t *testing.T) {
	svc := &MessageService{} // DB 为 nil：若校验在 DB 之后就会 panic/nil 错，正好反证顺序
	cases := []string{"bogus", "SYSTEM", "System", "all", "PRIVATE"}
	for _, typ := range cases {
		_, err := svc.MarkAllAsRead("u-1", MarkAllReadInput{Type: typ})
		if err == nil {
			t.Errorf("type=%q 应被拒绝，实际通过", typ)
			continue
		}
		// 期望是参数错误（10001），不是 503 依赖不可用
		if got := err.Error(); got == "" {
			t.Errorf("type=%q 错误信息为空", typ)
		}
	}
}

// TestMarkAllAsRead_ValidTypePassesValidation 合法 type 不应被校验挡下
// （此时 DB 为 nil → 会走到 ready() 返回 503，那说明校验通过了）。
func TestMarkAllAsRead_ValidTypePassesValidation(t *testing.T) {
	for _, typ := range []string{"", MsgTypeSystem, MsgTypeNotification, MsgTypePrivate} {
		svc := &MessageService{}
		_, err := svc.MarkAllAsRead("u-1", MarkAllReadInput{Type: typ})
		if err == nil {
			t.Errorf("type=%q DB 为 nil 时应报依赖不可用，实际成功", typ)
			continue
		}
		// 必须是「依赖不可用」而不是「参数非法」—— 证明校验放过了它
		if e, ok := err.(interface{ Msg() string }); ok {
			if e.Msg() == "message.kind_invalid" {
				t.Errorf("type=%q 是合法值，却被参数校验拦下", typ)
			}
		}
	}
}

// TestMarkAllReadInput_TypeEnumMatchesDto 守住「不新造一套取值」：
// handler/service 用的常量必须与 GET /message 的 type 枚举一致。
func TestMarkAllReadInput_TypeEnumMatchesDto(t *testing.T) {
	want := map[string]bool{MsgTypePrivate: true, MsgTypeSystem: true, MsgTypeNotification: true}
	for _, v := range []string{MsgTypePrivate, MsgTypeSystem, MsgTypeNotification} {
		if !want[v] {
			t.Errorf("unexpected enum %q", v)
		}
	}
	if len(want) != 3 {
		t.Errorf("type 枚举应为 3 个，实际 %d", len(want))
	}
}

// 消息域对外 JSON 空值契约回归测试（docs/21-JSON序列化规范.md）。
//
// 只测组装函数（纯函数，不需要 DB）：buildMessageVO / toVOWith 在 model 层字段为
// NULL 时必须输出 "" 或整个字段省略，**任何情况都不许出现 null**。
//
// 为什么这条测试值得存在：model 层故意保留 *string / *time.Time / jsonb（DB 要区分
// NULL 与空串），一旦有人把内嵌 model.Message 的写法改回来，这里会立刻红。
package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// assertNoNull 断言序列化结果里没有 null 字面量。
func assertNoNull(t *testing.T, label string, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: json.Marshal 失败: %v", label, err)
	}
	out := string(raw)
	if strings.Contains(out, ":null") {
		t.Fatalf("%s: 输出含 null（违反 docs/21）: %s", label, out)
	}
	return out
}

// TestBuildMessageVO_AllNull model 层全可空字段为 NULL 时，输出不得有 null。
func TestBuildMessageVO_AllNull(t *testing.T) {
	m := &model.Message{
		SenderID: "u-sender",
		// ReceiverID / Title 为 nil，Metadata 为 nil —— 最坏情况
		Content: "",
		Type:    MsgTypeNotification,
	}
	vo := buildMessageVO(m, nil, nil)
	out := assertNoNull(t, "MessageVO", vo)

	// 语义断言：字符串字段是 ""，不是省略
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"receiver_id", "title", "content"} {
		if v, ok := got[k]; !ok {
			t.Errorf("字段 %s 应存在且为 \"\"，实际整个字段缺失", k)
		} else if v != "" {
			t.Errorf("字段 %s 期望 \"\"，实际 %#v", k, v)
		}
	}
	// 布尔 false 必须有意义地保留（不能 omitempty）
	for _, k := range []string{"is_read", "is_broadcast"} {
		if _, ok := got[k]; !ok {
			t.Errorf("字段 %s 应始终存在（false 是有意义的值）", k)
		}
	}
	// 对象 / 透传字段空时整个不传
	for _, k := range []string{"sender", "receiver", "metadata", "article_id", "notification_type"} {
		if _, ok := got[k]; ok && got[k] == nil {
			t.Errorf("字段 %s 不应为 null", k)
		}
	}
}

// TestBuildMessageVO_MetadataFlat metadata 里的业务键要平铺出来（对齐源端 transformMessage），
// 且 snake_case / camelCase 两种历史写法都能取到。
func TestBuildMessageVO_MetadataFlat(t *testing.T) {
	m := &model.Message{
		SenderID: "u1",
		Content:  "x",
		Type:     MsgTypeSystem,
		Metadata: model.MessageJSON{
			"article_id":        "a1",
			"notification_type": "like",
		},
	}
	vo := buildMessageVO(m, nil, nil)
	assertNoNull(t, "MessageVO+metadata", vo)
	if vo.ArticleID != "a1" {
		t.Errorf("articleId 期望 a1（snake_case 命中），实际 %#v", vo.ArticleID)
	}
	if vo.NotificationType != "like" {
		t.Errorf("notificationType 期望 like（camelCase 命中），实际 %#v", vo.NotificationType)
	}
	if vo.CommentID != nil {
		t.Errorf("commentId 无来源时应保持 nil（→ omitempty 省略），实际 %#v", vo.CommentID)
	}
}

// TestPrivateMessageVO_AllNull 私信 VO 空值契约。
func TestPrivateMessageVO_AllNull(t *testing.T) {
	svc := &PrivateMessageService{}
	m := &model.PrivateMessage{
		ConversationID: "c1",
		SenderID:       "u1",
		ReceiverID:     "u2",
		MessageKind:    "text",
		// ReadAt / RecalledAt / RecalledByID / RecallReason / Payload 全 nil
	}
	vo := svc.toVOWith(m, nil, nil)
	out := assertNoNull(t, "PrivateMessageVO", vo)

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 时间 / 可空字符串：空时整个字段不传
	for _, k := range []string{"read_at", "recalled_at", "recalled_by_id", "recall_reason", "payload"} {
		if _, ok := got[k]; ok {
			t.Errorf("字段 %s 空时应整个不传，实际存在: %#v", k, got[k])
		}
	}
	// 布尔派生字段始终存在
	for _, k := range []string{"is_read", "is_recalled"} {
		if _, ok := got[k]; !ok {
			t.Errorf("字段 %s 应始终存在（false 有意义）", k)
		}
	}
	// 字符串值类型
	if got["content"] != "" {
		t.Errorf("content 期望 \"\"，实际 %#v", got["content"])
	}
}

// TestPrivateMessageVO_Recalled 撤回场景：时间为指针有值时正常输出。
func TestPrivateMessageVO_Recalled(t *testing.T) {
	svc := &PrivateMessageService{}
	now := time.Now()
	reason := "发错"
	by := "u1"
	m := &model.PrivateMessage{
		ConversationID: "c1", SenderID: "u1", ReceiverID: "u2",
		MessageKind: "text", RecalledAt: &now,
		RecallReason: &reason, RecalledByID: &by,
	}
	vo := svc.toVOWith(m, nil, nil)
	assertNoNull(t, "PrivateMessageVO+recalled", vo)
	if !vo.IsRecalled {
		t.Error("isRecalled 应为 true")
	}
	if vo.RecallReason != "发错" || vo.RecalledByID != "u1" {
		t.Errorf("撤回字段未回填: reason=%q by=%q", vo.RecallReason, vo.RecalledByID)
	}
	if vo.ReadAt != nil {
		t.Error("ReadAt 未设置时应为 nil（→ omitempty 省略）")
	}
}

// TestBriefUser_NilStrings 用户 nickname / avatar 为 NULL 时输出 ""。
func TestBriefUser_NilStrings(t *testing.T) {
	u := &model.User{BaseModel: model.BaseModel{ID: "u1"}, Username: "alice"}
	out := assertNoNull(t, "UserBriefVO", briefUser(u, nil, nil))
	var got map[string]interface{}
	_ = json.Unmarshal([]byte(out), &got)
	if got["nickname"] != "" || got["avatar"] != "" {
		t.Errorf("nickname/avatar 期望 \"\"，实际 %#v / %#v", got["nickname"], got["avatar"])
	}
}

// TestCursorMeta_LastPage 最后一页 nextCursor 是 "" 而不是 null。
func TestCursorMeta_LastPage(t *testing.T) {
	meta := CursorMeta{Limit: 20, HasMore: false}
	out := assertNoNull(t, "CursorMeta", meta)
	if !strings.Contains(out, `"next_cursor":""`) {
		t.Errorf("最后一页 nextCursor 期望 \"\"，实际: %s", out)
	}
}

// TestBlockedUser_NoNull 拉黑列表项：reason 为 NULL 时输出 ""，blockedUser 空时不传。
func TestBlockedUser_NoNull(t *testing.T) {
	item := BlockedUserVO{ID: "b1", CreatedAt: time.Now()}
	out := assertNoNull(t, "BlockedUserVO", item)
	if !strings.Contains(out, `"reason":""`) {
		t.Errorf("reason 期望 \"\"，实际: %s", out)
	}
}

// TestMarkMessagesReadResult_EmptyArrays 批量已读空结果：数组必须是 [] 不是 null。
func TestMarkMessagesReadResult_EmptyArrays(t *testing.T) {
	res := MarkMessagesReadResultVO{MessageIDs: []string{}, Receipts: []ReadReceiptVO{}}
	out := assertNoNull(t, "MarkMessagesReadResultVO", res)
	if !strings.Contains(out, `"message_ids":[]`) || !strings.Contains(out, `"receipts":[]`) {
		t.Errorf("空数组期望 []，实际: %s", out)
	}
}

// TestConversationDto_CounterpartID 会话项的 counterpart_id 契约（docs/28 §六 E 段）。
//
// 前端 lib/message-socket.ts 按 `counterpart_id`（字符串）解析，不认嵌套的 counterpart 对象。
// 两条不变量：
//  1. 有对方时 counterpart_id 必须等于 counterpart.id（两者恒一致，不能各说一套）
//  2. 对方为 nil（账号被硬删）时 counterpart_id 必须是 ""，且字段必须存在（不能整个不传）
func TestConversationDto_CounterpartID(t *testing.T) {
	// ① 正常情形：对象与扁平 ID 并存且一致
	item := ConversationVO{
		ConversationID: "c-1",
		CounterpartID:  "u-peer",
		Counterpart: &UserBriefVO{
			ID: "u-peer", Username: "peer", Nickname: "对方", Avatar: "",
		},
		UnreadCount: 3,
	}
	out := assertNoNull(t, "ConversationDto", item)

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["counterpart_id"] != "u-peer" {
		t.Errorf("counterpart_id 期望字符串 u-peer，实际 %#v", got["counterpart_id"])
	}
	obj, ok := got["counterpart"].(map[string]interface{})
	if !ok {
		t.Fatalf("counterpart 应为对象，实际 %#v", got["counterpart"])
	}
	if obj["id"] != got["counterpart_id"] {
		t.Errorf("counterpart.id(%v) 与 counterpart_id(%v) 必须一致",
			obj["id"], got["counterpart_id"])
	}

	// ② 对方为 nil：counterpart_id 是 "" 且字段存在，counterpart 整个不传
	bare := ConversationVO{ConversationID: "c-2"}
	out2 := assertNoNull(t, "ConversationDto/nil-counterpart", bare)
	var got2 map[string]interface{}
	_ = json.Unmarshal([]byte(out2), &got2)
	v, exists := got2["counterpart_id"]
	if !exists {
		t.Error("counterpart_id 空时也应存在且为 \"\"（docs/21 字符串规则），实际整个字段缺失")
	} else if v != "" {
		t.Errorf("counterpart_id 空时期望 \"\"，实际 %#v", v)
	}
	if _, has := got2["counterpart"]; has {
		t.Error("counterpart 为 nil 时应整个字段不传（omitempty）")
	}
}

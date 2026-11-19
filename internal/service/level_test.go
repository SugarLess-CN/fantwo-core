package service

import (
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// captureUserEvents 用独立总线捕获发布，返回事件→payload 表。
func captureUserEvents(t *testing.T, topics ...string) map[string][]interface{} {
	t.Helper()
	bus := event.NewBus()
	old := event.Default()
	event.SetDefault(bus)
	t.Cleanup(func() { event.SetDefault(old) })

	got := make(map[string][]interface{}, len(topics))
	for _, topic := range topics {
		got[topic] = nil
		topicCopy := topic
		bus.Subscribe(topicCopy, func(payload interface{}) {
			got[topicCopy] = append(got[topicCopy], payload)
		})
	}
	return got
}

// TestPublishDailyLogin 签到事件：主题正确、payload 为值类型且字段匹配（积分活动 DAILY_LOGIN 的触发源）。
func TestPublishDailyLogin(t *testing.T) {
	got := captureUserEvents(t, event.EventUserDailyLogin)
	publishDailyLogin("u-1", 7)

	list := got[event.EventUserDailyLogin]
	if len(list) != 1 {
		t.Fatalf("期望发布 1 次，实际 %d 次", len(list))
	}
	p, ok := list[0].(event.UserDailyLoginPayload)
	if !ok {
		t.Fatalf("payload 类型应为 event.UserDailyLoginPayload（值），实际 %T", list[0])
	}
	if p.UserID != "u-1" {
		t.Errorf("UserID 不符: %s", p.UserID)
	}
	if p.ConsecutiveDays != 7 {
		t.Errorf("ConsecutiveDays 不符: %d", p.ConsecutiveDays)
	}
}

// TestPublishFollowedBothDirections 关注：同时发布 user.followed 与 user.receivedFollow，字段互为镜像。
func TestPublishFollowedBothDirections(t *testing.T) {
	got := captureUserEvents(t, event.EventUserFollowed, event.EventUserReceivedFollow)
	publishFollowed("follower-A", "target-B")

	followed := got[event.EventUserFollowed]
	if len(followed) != 1 {
		t.Fatalf("user.followed 期望 1 次，实际 %d 次", len(followed))
	}
	fp, ok := followed[0].(event.UserFollowedPayload)
	if !ok {
		t.Fatalf("user.followed payload 类型不符: %T", followed[0])
	}
	if fp.UserID != "follower-A" || fp.TargetUserID != "target-B" {
		t.Errorf("user.followed 字段不符: %+v", fp)
	}

	received := got[event.EventUserReceivedFollow]
	if len(received) != 1 {
		t.Fatalf("user.receivedFollow 期望 1 次，实际 %d 次", len(received))
	}
	rp, ok := received[0].(event.UserReceivedFollowPayload)
	if !ok {
		t.Fatalf("user.receivedFollow payload 类型不符: %T", received[0])
	}
	if rp.UserID != "target-B" || rp.FollowerID != "follower-A" {
		t.Errorf("user.receivedFollow 字段不符: %+v", rp)
	}
}

// TestPublishProfileCompleted 资料补全事件用 GenericPayload（该事件无专用结构体）。
func TestPublishProfileCompleted(t *testing.T) {
	got := captureUserEvents(t, event.EventUserProfileCompleted)
	publishProfileCompleted("u-9")

	list := got[event.EventUserProfileCompleted]
	if len(list) != 1 {
		t.Fatalf("期望 1 次，实际 %d 次", len(list))
	}
	p, ok := list[0].(event.GenericPayload)
	if !ok {
		t.Fatalf("payload 应为 event.GenericPayload，实际 %T", list[0])
	}
	if p.UserID != "u-9" {
		t.Errorf("UserID 不符: %s", p.UserID)
	}
}

// TestPublishLevelUp 升级事件 payload 等级正确。
func TestPublishLevelUp(t *testing.T) {
	got := captureUserEvents(t, event.EventUserLevelUp)
	publishLevelUp("u-7", 3)

	list := got[event.EventUserLevelUp]
	if len(list) != 1 {
		t.Fatalf("期望 1 次，实际 %d 次", len(list))
	}
	p, ok := list[0].(event.UserLevelUpPayload)
	if !ok {
		t.Fatalf("payload 类型不符: %T", list[0])
	}
	if p.UserID != "u-7" || p.Level != 3 {
		t.Errorf("payload 字段不符: %+v", p)
	}
}

// TestPublishSkipsEmptyUserID 空用户 ID 不发布（避免订阅方推进到空账号）。
func TestPublishSkipsEmptyUserID(t *testing.T) {
	got := captureUserEvents(t, event.EventUserDailyLogin, event.EventUserFollowed,
		event.EventUserReceivedFollow, event.EventUserProfileCompleted, event.EventUserLevelUp)
	publishDailyLogin("", 1)
	publishFollowed("", "b")
	publishFollowed("a", "")
	publishProfileCompleted("")
	publishLevelUp("", 1)
	for topic, list := range got {
		if len(list) != 0 {
			t.Errorf("%s 不应发布，实际 %d 次", topic, len(list))
		}
	}
}

// TestCalculateLevel 等级门槛对齐原 LEVEL_CONFIG（0:0 / 1:100 / 2:300 / 3:600 / 4:1000 / 5:1500 / 6:2100）。
func TestCalculateLevel(t *testing.T) {
	cases := map[int]int{
		0: 0, 99: 0, 100: 1, 299: 1, 300: 2, 599: 2,
		600: 3, 1000: 4, 1499: 4, 1500: 5, 2099: 5, 2100: 6, 99999: 6,
	}
	for exp, want := range cases {
		if got := calculateLevel(exp); got != want {
			t.Errorf("经验 %d 期望 %d 级，实际 %d", exp, want, got)
		}
	}
}

// TestIsProfileComplete 资料补全判定（昵称 + 头像 + 简介齐备）。
func TestIsProfileComplete(t *testing.T) {
	nick, avatar, desc := "小画家", "https://a/b.png", "我是简介"
	full := &model.User{Nickname: &nick, Avatar: &avatar, Description: &desc}
	if !isProfileComplete(full) {
		t.Error("三项齐备应判为完整")
	}
	for name, u := range map[string]*model.User{
		"缺昵称": {Avatar: &avatar, Description: &desc},
		"缺头像": {Nickname: &nick, Description: &desc},
		"缺简介": {Nickname: &nick, Avatar: &avatar},
		"空串":  {Nickname: strptr(""), Avatar: strptr(""), Description: strptr("")},
		"nil": nil,
	} {
		if isProfileComplete(u) {
			t.Errorf("%s 应判为不完整", name)
		}
	}
}

// strptr 字符串指针辅助。
func strptr(s string) *string { return &s }

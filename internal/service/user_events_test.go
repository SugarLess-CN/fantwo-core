package service

import (
	"sync"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// TestUserEventSubscribersNoPanic 订阅注册与事件发布不 panic（DB/Redis 为 nil 的降级路径）。
func TestUserEventSubscribersNoPanic(t *testing.T) {
	bus := event.NewBus()
	old := event.Default()
	event.SetDefault(bus)
	defer event.SetDefault(old)

	d := NewDeps(nil, nil, nil, nil, nil)
	RegisterUserEventSubscribers(d)

	cases := []interface{}{
		event.ReportHandledPayload{Action: "BAN_USER", ReportedUserID: "u-1", HandlerID: "admin"},
		&event.ReportHandledPayload{Action: "NONE", ReportedUserID: "u-2"},
		map[string]interface{}{"action": "BAN_USER", "reportedUserId": "u-3"},
		event.InviteUsedPayload{InviterID: "u-4", InviteeID: "u-5", InviteCode: "ABC"},
		&event.InviteUsedPayload{InviterID: "u-6", InviteeID: "u-7"},
		map[string]interface{}{"inviter_id": "u-8", "invitee_id": "u-9", "invite_code": "XYZ"},
		"非法载荷",
		nil,
	}
	for _, p := range cases {
		bus.Publish(event.EventReportHandled, p)
		bus.Publish(event.EventInviteUsed, p)
	}
}

// TestRegisterUserEventSubscribersIdempotentGuard 装配层用 sync.Once 包裹时的语义验证。
func TestRegisterUserEventSubscribersIdempotentGuard(t *testing.T) {
	bus := event.NewBus()
	old := event.Default()
	event.SetDefault(bus)
	defer event.SetDefault(old)

	once := sync.Once{}
	d := NewDeps(nil, nil, nil, nil, nil)
	for i := 0; i < 3; i++ {
		once.Do(func() { RegisterUserEventSubscribers(d) })
	}
	// 重复调用不 panic 即通过（订阅只注册一次，避免事件重复处理）。
	bus.Publish(event.EventInviteUsed, event.InviteUsedPayload{InviteeID: "u-1"})
}

// TestWalletDeductRejectsInvalid 钱包扣款参数校验（DB 不可用/金额非法时返回错误而非 panic）。
func TestWalletDeductRejectsInvalid(t *testing.T) {
	w := NewUserWallet(NewDeps(nil, nil, nil, nil, nil))
	if err := w.DeductBalance("u-1", 10, "test", "测试"); err == nil {
		t.Error("DB 不可用时应返回错误")
	}
	if err := NewUserWallet(nil).DeductBalance("u-1", 10, "test", "测试"); err == nil {
		t.Error("nil 依赖应返回错误")
	}
}

// TestViewUserMasksSensitive 用户视图掩码手机/邮箱（对齐原 getProfile 语义）。
func TestViewUserMasksSensitive(t *testing.T) {
	phone := "13800138000"
	email := "someone@example.com"
	u := newTestUser("u-1", &phone, &email)
	v := ViewUser(u, true)
	if v.Phone == nil || *v.Phone != "138****8000" {
		t.Errorf("手机号掩码错误: %v", v.Phone)
	}
	if v.Email == nil || *v.Email != "s****@example.com" {
		t.Errorf("邮箱掩码错误: %v", v.Email)
	}
}

// newTestUser 测试用用户构造。
func newTestUser(id string, phone, email *string) *model.User {
	u := &model.User{Username: "tester", Phone: phone, Email: email, Status: model.UserStatusActive}
	u.ID = id
	return u
}

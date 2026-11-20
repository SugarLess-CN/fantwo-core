// Package event 事件总线测试。
//
// 防的 bug：
//   - 订阅顺序错乱：多个订阅者（成就 + 装饰品 + 通知）依赖注册顺序推进进度，
//     顺序乱了会导致进度计算取到旧值；
//   - 单个订阅者 panic 拖垮整条链：支付成功后要同时触发会员开通与邀请分成，
//     任一订阅者 panic 不能让其它订阅者收不到事件（钱的问题）；
//   - 无人订阅时静默丢弃：SubscriberCount 用于诊断「事件发了但没人收」；
//   - Default 单例被意外替换：各域通过 Default() 共享同一个总线，
//     若 SetDefault 后没恢复，其它包的测试会互相污染。
package event

import (
	"sync"
	"testing"
)

// TestPublish_SubscribersCalledInOrder 多订阅者按注册顺序调用。
// 顺序即业务语义：ops 域先记进度，通知域后读进度。
func TestPublish_SubscribersCalledInOrder(t *testing.T) {
	bus := NewBus()
	var order []string

	bus.Subscribe("order.paid", func(_ interface{}) { order = append(order, "first") })
	bus.Subscribe("order.paid", func(_ interface{}) { order = append(order, "second") })
	bus.Subscribe("order.paid", func(_ interface{}) { order = append(order, "third") })

	bus.Publish("order.paid", nil)

	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("订阅者调用次数：得到 %d，期望 %d（%v）", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("调用顺序[%d]：得到 %q，期望 %q（全部：%v）", i, order[i], want[i], order)
		}
	}
}

// TestPublish_PayloadPassthrough 载荷原样传递（各域依赖具体 struct 做断言）。
func TestPublish_PayloadPassthrough(t *testing.T) {
	bus := NewBus()
	type payload struct {
		OrderID string
		Amount  float64
	}
	sent := payload{OrderID: "o-1", Amount: 99.5}

	var got interface{}
	bus.Subscribe("order.paid", func(p interface{}) { got = p })
	bus.Publish("order.paid", sent)

	gotP, ok := got.(payload)
	if !ok {
		t.Fatalf("载荷类型被改变：%T", got)
	}
	if gotP != sent {
		t.Errorf("载荷内容被改变：得到 %+v，期望 %+v", gotP, sent)
	}
}

// TestPublish_SubscriberPanicIsolated 单个订阅者 panic 不影响其它订阅者。
//
// 这是最关键的一条：支付成功后链路上任一订阅者（比如邀请分成算错了空指针）
// panic，不能让后面的「会员开通」收不到事件。
func TestPublish_SubscriberPanicIsolated(t *testing.T) {
	bus := NewBus()
	var afterCalled bool

	bus.Subscribe("order.paid", func(_ interface{}) { panic("订阅者炸了") })
	bus.Subscribe("order.paid", func(_ interface{}) { afterCalled = true })

	// Publish 本身不能 panic（panic 必须被内部 recover 掉）
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Publish 把订阅者 panic 外抛了: %v", r)
		}
	}()
	bus.Publish("order.paid", nil)

	if !afterCalled {
		t.Error("panic 的订阅者阻断了后续订阅者（只 recover 不继续，链条会断）")
	}
}

// TestPublish_AllPanicStillReturns 全部订阅者都 panic 时 Publish 仍正常返回。
func TestPublish_AllPanicStillReturns(t *testing.T) {
	bus := NewBus()
	bus.Subscribe("x", func(_ interface{}) { panic("a") })
	bus.Subscribe("x", func(_ interface{}) { panic("b") })

	done := make(chan struct{})
	go func() {
		bus.Publish("x", nil)
		close(done)
	}()
	<-done // 不 hang 即通过
}

// TestPublish_NoSubscriber 无订阅者时静默丢弃（不 panic、不阻塞）。
func TestPublish_NoSubscriber(t *testing.T) {
	bus := NewBus()
	bus.Publish("nobody.listens", "payload") // 不应 panic
	if n := bus.SubscriberCount("nobody.listens"); n != 0 {
		t.Errorf("无订阅主题的 SubscriberCount 应为 0，得到 %d", n)
	}
}

// TestPublish_TopicIsolation 不同主题互不干扰（同名 payload 也不能串）。
func TestPublish_TopicIsolation(t *testing.T) {
	bus := NewBus()
	var aCalled, bCalled bool

	bus.Subscribe("topic.a", func(_ interface{}) { aCalled = true })
	bus.Subscribe("topic.b", func(_ interface{}) { bCalled = true })

	bus.Publish("topic.a", nil)

	if !aCalled {
		t.Error("订阅 topic.a 的处理器未被调用")
	}
	if bCalled {
		t.Error("发布 topic.a 却触发了 topic.b 的订阅者（主题隔离失效）")
	}
}

// TestSubscriberCount 订阅者计数（诊断「事件发了但没人收」）。
func TestSubscriberCount(t *testing.T) {
	bus := NewBus()
	if n := bus.SubscriberCount("e"); n != 0 {
		t.Errorf("初始应为 0，得到 %d", n)
	}

	bus.Subscribe("e", func(_ interface{}) {})
	if n := bus.SubscriberCount("e"); n != 1 {
		t.Errorf("注册 1 个后应为 1，得到 %d", n)
	}

	bus.Subscribe("e", func(_ interface{}) {})
	if n := bus.SubscriberCount("e"); n != 2 {
		t.Errorf("注册 2 个后应为 2，得到 %d", n)
	}

	// 其它主题不受影响
	if n := bus.SubscriberCount("other"); n != 0 {
		t.Errorf("其它主题应为 0，得到 %d", n)
	}
}

// TestDefault_SingletonAndReplace 全局单例与替换/恢复。
//
// 用 t.Cleanup 恢复原实例，避免本测试污染同进程内其它包的 Default() 断言。
func TestDefault_SingletonAndReplace(t *testing.T) {
	orig := Default()
	t.Cleanup(func() { SetDefault(orig) })

	if Default() == nil {
		t.Fatal("Default() 返回 nil")
	}
	if Default() != Default() {
		t.Fatal("Default() 两次返回不同实例（单例失效）")
	}

	replacement := NewBus()
	SetDefault(replacement)
	if Default() != replacement {
		t.Fatal("SetDefault 未生效")
	}

	// 替换后的总线共享同一份订阅表
	var called bool
	Default().Subscribe("k", func(_ interface{}) { called = true })
	Default().Publish("k", nil)
	if !called {
		t.Error("替换后的总线不接收订阅/发布")
	}
}

// TestPublish_SlowSubscriberDoesNotBlockOthers 阻塞型订阅者不阻断同步链的语义确认。
//
// 说明：本总线是**同步**调用（对齐 NestJS EventEmitter2 默认 emit），
// 所以慢订阅者会拖住整体耗时 —— 这个测试把这一约定钉死：
// 若将来改成异步，这里的顺序断言会失败，提醒作者同步更新注释与调用方假设。
func TestPublish_SlowSubscriberDoesNotBlockOthers(t *testing.T) {
	bus := NewBus()
	var order []string

	bus.Subscribe("t", func(_ interface{}) {
		order = append(order, "slow-start")
		order = append(order, "slow-end")
	})
	bus.Subscribe("t", func(_ interface{}) { order = append(order, "fast") })

	bus.Publish("t", nil)

	want := []string{"slow-start", "slow-end", "fast"}
	if len(order) != len(want) {
		t.Fatalf("得到 %v，期望 %v（同步语义被改成异步了？）", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("[%d] 得到 %q，期望 %q", i, order[i], want[i])
		}
	}
}

// TestSubscribe_ConcurrentSafe 并发订阅 + 发布无 data race（配合 -race 使用）。
//
// 防的是：Publish 持读锁期间被 Subscribe 写 map 引发 "concurrent map read and map write"。
// 实现里 Publish 先复制切片再释放锁，正是为此。
func TestSubscribe_ConcurrentSafe(t *testing.T) {
	bus := NewBus()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Subscribe("concurrent", func(_ interface{}) {})
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish("concurrent", nil)
		}()
	}
	wg.Wait()

	// 每次 Subscribe 都成功登记（Publish 期间的读锁不能吞掉写）
	if n := bus.SubscriberCount("concurrent"); n != 50 {
		t.Errorf("并发订阅丢失：得到 %d，期望 50", n)
	}
}

// TestSubscribe_DuringPublishNoDeadlock 发布过程中订阅不产生死锁。
// 防的是：Publish 持锁调用 handler 而 handler 内又 Subscribe 造成自锁。
func TestSubscribe_DuringPublishNoDeadlock(t *testing.T) {
	bus := NewBus()
	bus.Subscribe("self", func(_ interface{}) {
		bus.Subscribe("self", func(_ interface{}) {}) // 处理器内部再订阅
	})

	done := make(chan struct{})
	go func() {
		bus.Publish("self", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-make(chan struct{}): // 永不返回即为死锁
	}
}
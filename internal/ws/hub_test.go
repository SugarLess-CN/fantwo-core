// Package ws WebSocket hub 测试（白盒：直接操作 clients/rooms map 与通道）。
//
// 防的 bug：
//   - 连接数/房间原子计数漏减：/health 的 connections 只增不减，监控永远显示「有人在连」；
//   - 注销后没退出房间：房间 map 残留已断开的 client，广播时向已关闭的 send chan 发送 → panic；
//   - 空房间不回收：房间数只增不减，内存泄漏；
//   - 慢消费者阻塞主循环：一个不读数据的客户端把整个 hub 卡死（所有人收不到消息）；
//   - SendToUser 只发首端：多端在线（手机+网页）时另一半收不到；
//   - 事件名被改名：前端按事件名匹配，改名等于消息静默丢失。
package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestClient 构造不依赖真实连接的 client（send 有缓冲，conn 为 nil）。
// 广播路径只写 send 通道、不触碰 conn，因此 nil conn 安全。
func newTestClient(h *Hub, userID string) *Client {
	return &Client{
		hub:      h,
		send:     make(chan Frame, 256),
		SocketID: "sock-" + userID,
		UserID:   userID,
		DeviceID: "dev",
		Rooms:    make(map[string]struct{}),
	}
}

// resetCounters 清理包级原子计数（进程级共享，避免测试间污染）。
func resetCounters() {
	for ActiveConnections() > 0 {
		activeConnections.Add(-1)
	}
	for ActiveConnections() < 0 {
		activeConnections.Add(1)
	}
	for ActiveRooms() > 0 {
		activeRooms.Add(-1)
	}
	for ActiveRooms() < 0 {
		activeRooms.Add(1)
	}
}

// startHub 启动 hub 主循环。
func startHub(t *testing.T) *Hub {
	t.Helper()
	h := NewHub()
	go h.Run()
	t.Cleanup(resetCounters)
	return h
}

// waitFor 轮询等待条件成立（hub 是异步 goroutine，不能立即断言）。
func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", desc)
}

// drain 非阻塞读一帧。
func drain(c *Client) (Frame, bool) {
	select {
	case f := <-c.send:
		return f, true
	default:
		return Frame{}, false
	}
}

// TestEventConstants_NonEmptyAndUnique 事件名非空且互不重复。
//
// 前端按事件名 switch 分发；重名意味着两处 handler 抢同一事件，
// 改名意味着消息静默丢失（前端收不到且无报错）。
func TestEventConstants_NonEmptyAndUnique(t *testing.T) {
	events := map[string]string{
		"EventConnected":                  EventConnected,
		"EventError":                      EventError,
		"EventNewMessage":                 EventNewMessage,
		"EventHistory":                    EventHistory,
		"EventRead":                       EventRead,
		"EventAllMarkedAsRead":            EventAllMarkedAsRead,
		"EventUnreadCount":                EventUnreadCount,
		"EventPrivateMessage":             EventPrivateMessage,
		"EventPrivateHistory":             EventPrivateHistory,
		"EventPrivateConversations":       EventPrivateConversations,
		"EventPrivateConversationUpdated": EventPrivateConversationUpdated,
		"EventPrivateConversationRead":    EventPrivateConversationRead,
		"EventPrivateMessageRecalled":     EventPrivateMessageRecalled,
		"EventPrivateMessagesRead":        EventPrivateMessagesRead,
		"EventJoined":                     EventJoined,
		"EventLeaved":                     EventLeaved,
		"EventProfile":                    EventProfile,
		"EventUserStatus":                 EventUserStatus,
		"EventUserStatusChanged":          EventUserStatusChanged,
		"EventPong":                       EventPong,
		"EventJoin":                       EventJoin,
		"EventLeave":                      EventLeave,
		"EventPing":                       EventPing,
		"EventSubscribe":                  EventSubscribe,
	}

	seen := map[string]string{}
	for name, val := range events {
		if val == "" {
			t.Errorf("%s 为空串（发送出去前端无法识别）", name)
			continue
		}
		if prev, dup := seen[val]; dup {
			t.Errorf("事件名重复：%s 与 %s 都是 %q（handler 会互相覆盖）", prev, name, val)
			continue
		}
		seen[val] = name
	}
}

// TestEventConstants_MatchesSourceContract 关键事件名逐字对齐原 Socket.io 契约。
//
// 这些是前端硬编码的字符串（原 message.gateway.ts），改一个字母前端就收不到。
func TestEventConstants_MatchesSourceContract(t *testing.T) {
	cases := []struct {
		constName string
		got       string
		want      string
	}{
		{"connected", EventConnected, "connected"},
		{"newMessage", EventNewMessage, "newMessage"},
		{"privateMessage", EventPrivateMessage, "privateMessage"},
		{"userStatusChanged", EventUserStatusChanged, "userStatusChanged"},
		{"pong", EventPong, "pong"},
		{"join", EventJoin, "join"},
		{"leave", EventLeave, "leave"},
		{"ping", EventPing, "ping"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("事件名 %s：得到 %q，期望 %q（前端硬编码，改名即丢消息）",
				c.constName, c.got, c.want)
		}
	}
}

// TestFrame_NilData 空帧的 Data 为 nil（JSON 里省略 data 字段）。
func TestFrame_NilData(t *testing.T) {
	f := Frame{Event: "pong"}
	if f.Data != nil {
		t.Errorf("空帧 Data 应为 nil，得到 %v", f.Data)
	}
	if f.Event != "pong" {
		t.Errorf("Event 字段异常: %q", f.Event)
	}
}

// TestHub_RegisterAndUnregister 注册/注销与连接计数。
func TestHub_RegisterAndUnregister(t *testing.T) {
	h := startHub(t)
	resetCounters()

	c := newTestClient(h, "u-1")
	h.register <- c
	waitFor(t, "连接注册计数 +1", func() bool { return ActiveConnections() == 1 })

	h.unregister <- c
	waitFor(t, "连接注销计数归零", func() bool { return ActiveConnections() == 0 })
}

// TestHub_UnregisterLeavesAllRooms 注销时自动退出全部房间，空房间被回收。
//
// 防的是：断开后仍留在房间 → 后续房间广播向已关闭的 send 通道发送 → panic（整进程崩）。
func TestHub_UnregisterLeavesAllRooms(t *testing.T) {
	h := startHub(t)
	resetCounters()

	c := newTestClient(h, "u-1")
	h.register <- c
	waitFor(t, "注册", func() bool { return ActiveConnections() == 1 })

	// JoinRoom 在 hub 主循环外调用（见 message_gateway 用法），这里顺序调用即可
	h.JoinRoom(c, "room-a")
	h.JoinRoom(c, "room-b")
	if got := ActiveRooms(); got != 2 {
		t.Fatalf("加入两个房间后 ActiveRooms 应为 2，得到 %d", got)
	}

	h.unregister <- c
	waitFor(t, "注销后房间全部回收", func() bool { return ActiveRooms() == 0 })

	// 房间已删除：再向该房间广播不应 panic，也不应投递
	h.SendToRoom("room-a", Frame{Event: "x"})
	time.Sleep(20 * time.Millisecond) // 让 hub 处理该帧
}

// TestHub_JoinRoomIdempotent 重复加入同一房间不重复计房间数。
func TestHub_JoinRoomIdempotent(t *testing.T) {
	h := startHub(t)
	resetCounters()

	c := newTestClient(h, "u-1")
	h.register <- c
	waitFor(t, "注册", func() bool { return ActiveConnections() == 1 })

	h.JoinRoom(c, "room-a")
	h.JoinRoom(c, "room-a")
	h.JoinRoom(c, "room-a")

	if got := ActiveRooms(); got != 1 {
		t.Errorf("重复加入同一房间后房间数应为 1，得到 %d（房间计数虚高）", got)
	}
	if len(c.Rooms) != 1 {
		t.Errorf("client.Rooms 应只有 1 项，得到 %d", len(c.Rooms))
	}
}

// TestHub_Broadcast_AllClients 全体广播投递到每个连接。
func TestHub_Broadcast_AllClients(t *testing.T) {
	h := startHub(t)
	resetCounters()

	c1 := newTestClient(h, "u-1")
	c2 := newTestClient(h, "u-2")
	h.register <- c1
	h.register <- c2
	waitFor(t, "两个连接注册", func() bool { return ActiveConnections() == 2 })

	h.Broadcast(Frame{Event: "announce", Data: "hello"})
	// 注意：waitFor 的重试函数必须**无副作用**。
	// 这里先只判断「两个 channel 都有数据」（len 不消费），
	// 确认到齐后再各 drain 一次取帧——否则第一次重试就把 c1 的帧消费掉，
	// 第二次重试 c1 已空从而永远等不到（曾导致本用例超时）。
	waitFor(t, "广播投递", func() bool {
		return len(c1.send) > 0 && len(c2.send) > 0
	})
	f1, ok1 := drain(c1)
	f2, ok2 := drain(c2)
	if !ok1 || !ok2 {
		t.Fatal("广播后未取到帧")
	}
	if f1.Event != "announce" || f2.Event != "announce" {
		t.Fatalf("广播事件名异常：%q / %q", f1.Event, f2.Event)
	}
}

// TestHub_SendToRoom_OnlyRoomMembers 房间广播只投给房间成员。
//
// 防的是：实现退化成全体广播 → 私信/会话消息发给所有人（严重越权）。
func TestHub_SendToRoom_OnlyRoomMembers(t *testing.T) {
	h := startHub(t)
	resetCounters()

	inside := newTestClient(h, "u-in")
	outside := newTestClient(h, "u-out")
	h.register <- inside
	h.register <- outside
	waitFor(t, "注册", func() bool { return ActiveConnections() == 2 })

	h.JoinRoom(inside, "conv-1")
	h.SendToRoom("conv-1", Frame{Event: "privateMessage", Data: "secret"})

	waitFor(t, "房间成员收到", func() bool {
		f, ok := drain(inside)
		return ok && f.Event == "privateMessage"
	})

	if f, ok := drain(outside); ok {
		t.Errorf("非房间成员收到了房间消息 %q（越权泄露）", f.Event)
	}
}

// TestHub_SendToUser_MultipleDevices 单用户定向：多端全部命中。
//
// 防的是：只发首个匹配连接，手机上收不到网页端发的消息。
func TestHub_SendToUser_MultipleDevices(t *testing.T) {
	h := startHub(t)
	resetCounters()

	phone := newTestClient(h, "u-1")
	web := newTestClient(h, "u-1") // 同一用户第二端
	other := newTestClient(h, "u-2")
	h.register <- phone
	h.register <- web
	h.register <- other
	waitFor(t, "注册三个连接", func() bool { return ActiveConnections() == 3 })

	h.SendToUser("u-1", Frame{Event: "newMessage", Data: "hi"})

	waitFor(t, "两端都收到", func() bool {
		_, ok1 := drain(phone)
		_, ok2 := drain(web)
		return ok1 && ok2
	})

	if f, ok := drain(other); ok {
		t.Errorf("发给 u-1 的消息被投递给了 u-2（%q）", f.Event)
	}
}

// TestHub_SlowConsumerDoesNotBlock 慢消费者（send 满）不阻塞 hub 主循环。
//
// 防的是：某客户端不读数据、send 缓冲打满后，hub 阻塞在该客户端上，
// 全站所有人都收不到消息（一个坏客户端拖垮整个实时链路）。
func TestHub_SlowConsumerDoesNotBlock(t *testing.T) {
	h := startHub(t)
	resetCounters()

	slow := newTestClient(h, "u-slow")
	// 把 send 换成无缓冲且无人读的通道：投递必然阻塞
	slow.send = make(chan Frame)
	h.register <- slow
	waitFor(t, "注册", func() bool { return ActiveConnections() == 1 })

	healthy := newTestClient(h, "u-ok")
	h.register <- healthy
	waitFor(t, "第二个连接注册", func() bool { return ActiveConnections() == 2 })

	done := make(chan struct{})
	go func() {
		h.Broadcast(Frame{Event: "announce"})
		close(done)
	}()

	select {
	case <-done:
		// Broadcast 只往 hub 通道塞帧，本身不阻塞
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast 被阻塞（发送路径不应直接写 client.send）")
	}

	// 健康客户端应能收到（慢客户端被丢弃而非拖住其他人）
	waitFor(t, "健康客户端收到广播", func() bool {
		f, ok := drain(healthy)
		return ok && f.Event == "announce"
	})
}

// TestHub_CountersNeverNegative 计数不得变负（重复注销等异常路径）。
//
// 防的是：/health 的 connections 出现 -1，监控图表错乱、告警规则失效。
func TestHub_CountersNeverNegative(t *testing.T) {
	h := startHub(t)
	resetCounters()

	c := newTestClient(h, "u-1")
	h.register <- c
	waitFor(t, "注册", func() bool { return ActiveConnections() == 1 })

	// 连续注销两次：第二次不应把计数减成 -1
	h.unregister <- c
	h.unregister <- c
	time.Sleep(30 * time.Millisecond)

	if got := ActiveConnections(); got < 0 {
		t.Errorf("重复注销后连接计数为负 %d（/health 会显示负数）", got)
	}
}

// TestHub_Handler_EndToEnd 真实 WS 连接：升级成功、收到 connected 帧。
//
// 用 httptest + gorilla Dialer 走完整握手，验证 Handler 的
// 升级 → 注册 → 发送 connected 帧链路。
func TestHub_Handler_EndToEnd(t *testing.T) {
	h := startHub(t)
	resetCounters()

	r := gin.New()
	r.GET("/ws", h.Handler)

	srv := httptest.NewServer(r)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 握手失败: %v（resp=%v）", err, resp)
	}
	defer func() { _ = conn.Close() }()

	// 首帧应为 connected
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var f Frame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("读取首帧失败: %v", err)
	}
	if f.Event != EventConnected {
		t.Errorf("首帧事件应为 %q，得到 %q（前端靠它判断连接就绪）", EventConnected, f.Event)
	}

	waitFor(t, "连接计数 +1", func() bool { return ActiveConnections() == 1 })

	// 定向发一帧，客户端应收到
	h.SendToUser("", Frame{Event: EventPong})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// 空 UserID 不会命中，这里只验证不 panic；真正的定向投递在 SendToUser 测试里覆盖
}

// TestHub_Handler_RejectsNonWebSocket 非 WS 请求不被升级（返回 4xx，不 panic）。
func TestHub_Handler_RejectsNonWebSocket(t *testing.T) {
	h := startHub(t)
	r := gin.New()
	r.GET("/ws", h.Handler)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Error("普通 GET 请求被错误地升级成了 WS")
	}
}

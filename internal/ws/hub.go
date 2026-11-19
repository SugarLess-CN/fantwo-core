// Package ws WebSocket hub 骨架（gorilla/websocket，对应原 Socket.io /ws-message）。
//
// 事件名与 payload 迁移要求逐字节兼容（docs/01 §5：约 30 个事件、
// 多端在线 presence、已读回执、撤回、拉黑、未读数）。
// 本文件为骨架：连接管理 / 房间 / 广播接口 + 事件名占位，
// 业务事件处理在 message 模块批次落地。
package ws

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// 进程级连接/房间计数（供 /health 可观测读取，无锁）。
//
// 为什么用包级原子量而不是 Hub 字段查询：clients/rooms 两个 map 只能在 Run 循环内
// 安全访问（外部读会与 register/unregister 竞争），而 health 端点属于旁路，
// 不能因为抢锁拖慢主循环。原子计数在 Run 循环内维护，读侧永远无锁。
var (
	activeConnections atomic.Int64
	activeRooms       atomic.Int64
)

// ActiveConnections 当前进程内活跃 WS 连接总数。
func ActiveConnections() int64 { return activeConnections.Load() }

// ActiveRooms 当前进程内活跃房间总数。
func ActiveRooms() int64 { return activeRooms.Load() }

// EventName 事件名占位（对齐 message.gateway.ts 的事件面；payload 待业务批次定稿）。
const (
	// 服务端 → 客户端
	EventConnected                  = "connected"
	EventError                      = "error"
	EventNewMessage                 = "newMessage"
	EventHistory                    = "history"
	EventRead                       = "read"
	EventAllMarkedAsRead            = "allMarkedAsRead"
	EventUnreadCount                = "unreadCount"
	EventPrivateMessage             = "privateMessage"
	EventPrivateHistory             = "privateHistory"
	EventPrivateConversations       = "privateConversations"
	EventPrivateConversationUpdated = "privateConversationUpdated"
	EventPrivateConversationRead    = "privateConversationRead"
	EventPrivateMessageRecalled     = "privateMessageRecalled"
	EventPrivateMessagesRead        = "privateMessagesRead"
	EventJoined                     = "joined"
	EventLeaved                     = "leaved"
	EventProfile                    = "profile"
	EventUserStatus                 = "userStatus"
	EventUserStatusChanged          = "userStatusChanged"
	EventPong                       = "pong"

	// 客户端 → 服务端（处理方法占位）
	EventJoin      = "join"
	EventLeave     = "leave"
	EventPing      = "ping"
	EventSubscribe = "subscribeUserStatus"
)

// Frame JSON 事件帧：{event, data}（对应 socket.io 的 emit(event, payload)）。
type Frame struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data,omitempty"`
}

// Room 房间（原 socket.io room；presence 房间 + 私信会话房间）。
type Room struct {
	Name    string
	Clients map[*Client]struct{}
}

// Client 单个 WS 连接。
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Frame
	// SocketID 连接唯一标识（连接建立时生成 uuid，对齐源端 socket.id）。
	// 不能用 RemoteAddr：同 IP 多端（NAT / 多标签页）会得到相同地址，互相覆盖 presence 集合。
	SocketID string
	UserID   string
	DeviceID string // 多端在线（原 deviceId header 语义）
	Rooms    map[string]struct{}
}

// Hub 连接管理器：注册/注销、房间、广播。
type Hub struct {
	clients    map[*Client]struct{}
	rooms      map[string]*Room
	register   chan *Client
	unregister chan *Client
	broadcast  chan Frame
	// broadcastTo 定向广播（房间或单用户）
	broadcastTo chan AddressedFrame
}

// AddressedFrame 定向广播帧：ToRoom 非空投房间，否则投单用户。
type AddressedFrame struct {
	Room   string
	UserID string
	Frame  Frame
}

// NewHub 创建 hub（go hub.Run() 启动）。
func NewHub() *Hub {
	return &Hub{
		clients:     make(map[*Client]struct{}),
		rooms:       make(map[string]*Room),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		broadcast:   make(chan Frame, 256),
		broadcastTo: make(chan AddressedFrame, 256),
	}
}

// Run hub 主循环（每个连接的读写泵另行 goroutine）。
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}
			activeConnections.Add(1)
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				activeConnections.Add(-1)
				for name := range c.Rooms {
					h.leaveRoom(c, name)
				}
				close(c.send)
			}
		case f := <-h.broadcast:
			for c := range h.clients {
				select {
				case c.send <- f:
				default: // 发送缓冲满则丢弃（慢消费者），生产语义后续细化
				}
			}
		case af := <-h.broadcastTo:
			if af.Room != "" {
				if room, ok := h.rooms[af.Room]; ok {
					for c := range room.Clients {
						select {
						case c.send <- af.Frame:
						default:
						}
					}
				}
			} else if af.UserID != "" {
				for c := range h.clients {
					if c.UserID == af.UserID {
						select {
						case c.send <- af.Frame:
						default:
						}
					}
				}
			}
		}
	}
}

// JoinRoom 加入房间。
func (h *Hub) JoinRoom(c *Client, name string) {
	room, ok := h.rooms[name]
	if !ok {
		room = &Room{Name: name, Clients: make(map[*Client]struct{})}
		h.rooms[name] = room
		activeRooms.Add(1)
	}
	room.Clients[c] = struct{}{}
	c.Rooms[name] = struct{}{}
}

// leaveRoom 退出房间（房间空则回收）。
func (h *Hub) leaveRoom(c *Client, name string) {
	if room, ok := h.rooms[name]; ok {
		delete(room.Clients, c)
		if len(room.Clients) == 0 {
			delete(h.rooms, name)
			activeRooms.Add(-1)
		}
	}
	delete(c.Rooms, name)
}

// Broadcast 全体广播。
func (h *Hub) Broadcast(f Frame) { h.broadcast <- f }

// SendToRoom 房间广播。
func (h *Hub) SendToRoom(room string, f Frame) {
	h.broadcastTo <- AddressedFrame{Room: room, Frame: f}
}

// SendToUser 单用户定向（多端全部命中）。
func (h *Hub) SendToUser(userID string, f Frame) {
	h.broadcastTo <- AddressedFrame{UserID: userID, Frame: f}
}

// upgrader WS 升级器（鉴权在 handler 内做：token 从 auth/header/query 取，对齐原 gateway）。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // CORS 全开，对齐原仓库
}

// Handler WS 接入端点骨架（挂 /api/v1/ws-message；鉴权与事件处理在 message 批次完善）。
func (h *Hub) Handler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan Frame, 256),
		SocketID: uuid.NewString(),
		Rooms:    make(map[string]struct{}),
	}
	h.register <- client
	client.send <- Frame{Event: EventConnected, Data: nil}

	// 读写泵骨架：正式实现（事件分发 join/leave/sendMessage/...）在 message 批次
	go writePump(client)
	go readPump(client)
}

// readPump 读循环骨架。
func readPump(c *Client) {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	for {
		var f Frame
		if err := c.conn.ReadJSON(&f); err != nil {
			return
		}
		// TODO(message 批次): 按事件分发 join/leave/sendMessage/getHistory/...
		_ = f
	}
}

// writePump 写循环骨架。
func writePump(c *Client) {
	defer func() { _ = c.conn.Close() }()
	for f := range c.send {
		if err := c.conn.WriteJSON(f); err != nil {
			return
		}
	}
}

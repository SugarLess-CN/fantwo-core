// Package ws message 域 WebSocket 网关（替换原 Socket.io /ws-message namespace）。
//
// 为什么放在 ws 包内：Client 与 Hub 的注册/注销/房间成员字段都是包内私有，
// 只有同包代码才能建连接并把 client 挂进 hub。业务处理通过 MsgGateway 的
// 两个回调注入（Auth / Dispatch），ws 包因此**不 import service**，不存在循环依赖。
//
// 协议差异：原前端 socket.io emit(event, payload) ↔ on(event, ...)；
// 本实现帧格式为 {"event":"<name>","data":{...}}（hub.Frame），前端需按此改造。
//
// 鉴权（对齐源端 handleConnection）：token 依次取 handshake.auth.token →
// Authorization 头（可带 Bearer 前缀）→ query.token；校验失败推 error 帧后断开。
package ws

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MsgUser 连接态用户（Auth 回调返回，挂到连接上）。
// Permissions 来自 JWT claims（原 PermissionGuard 同样不查库），供广播类事件鉴权。
type MsgUser struct {
	ID          string
	Username    string
	Nickname    *string
	Avatar      *string
	Permissions []string
}

// HasPerm 是否持有任一权限点。
func (u MsgUser) HasPerm(perms ...string) bool {
	for _, req := range perms {
		for _, p := range u.Permissions {
			if p == req {
				return true
			}
		}
	}
	return false
}

// MsgAuthFunc token → 用户；返回 false 表示认证失败。
type MsgAuthFunc func(token string) (MsgUser, bool)

// MsgDispatchFunc 业务事件分发：event 为客户端事件名，data 为原始 JSON；
// 返回 (ack, errorCode, errorMsg)：ack 非 nil 时作为该事件的应答帧回给发起者
// （对应源端 @SubscribeMessage 的 return 值）。
type MsgDispatchFunc func(ctx *MsgContext, event string, data json.RawMessage) (ack interface{}, errCode string)

// MsgPresenceHook 在线状态钩子（由 service 层实现，ws 包不持有业务依赖）。
type MsgPresenceHook interface {
	// Connected 连接成功；返回 becameOnline 表示是否首次上线。
	Connected(userID, socketID, deviceID string) (becameOnline bool, payload UserStatusPayload)
	// Disconnected 断开；返回 becameOffline 表示是否全部端下线。
	Disconnected(socketID string) (becameOffline bool, userID string, payload UserStatusPayload)
	// Touch 心跳续期。
	Touch(userID, deviceID string)
	// Presence 查询在线状态。
	Presence(userID string) UserStatusPayload
}

// MsgGateway /ws-message 接入网关。
type MsgGateway struct {
	Hub      *Hub
	Auth     MsgAuthFunc
	Dispatch MsgDispatchFunc
	Presence MsgPresenceHook
}

// NewMsgGateway 构造网关（presence 可为 nil）。
func NewMsgGateway(hub *Hub, auth MsgAuthFunc, dispatch MsgDispatchFunc, presence MsgPresenceHook) *MsgGateway {
	return &MsgGateway{Hub: hub, Auth: auth, Dispatch: dispatch, Presence: presence}
}

// MsgContext 单次客户端事件的处理上下文（提供推送原语给 Dispatch 实现）。
type MsgContext struct {
	gateway *MsgGateway
	client  *Client
	User    MsgUser
}

// Emit 回帧给当前连接（对应 socket.on 收到的事件）。
func (x *MsgContext) Emit(event string, data interface{}) {
	select {
	case x.client.send <- Frame{Event: event, Data: data}:
	default: // 发送缓冲满则丢弃（慢消费者）
	}
}

// EmitError 回 error 帧（对齐源端 {message, code}）。
func (x *MsgContext) EmitError(code, message string) {
	x.Emit(EventError, ErrorPayload{Message: message, Code: code})
}

// SendToUser 定向推送（多端全部命中，等价 socket.io .to(uid).emit）。
func (x *MsgContext) SendToUser(userID, event string, data interface{}) {
	if x.gateway == nil || x.gateway.Hub == nil || userID == "" {
		return
	}
	x.gateway.Hub.SendToUser(userID, Frame{Event: event, Data: data})
}

// Broadcast 全体推送（等价 socket.io server.emit）。
func (x *MsgContext) Broadcast(event string, data interface{}) {
	if x.gateway == nil || x.gateway.Hub == nil {
		return
	}
	x.gateway.Hub.Broadcast(Frame{Event: event, Data: data})
}

// SendToRoom 房间推送（presence 房间用）。
func (x *MsgContext) SendToRoom(room, event string, data interface{}) {
	if x.gateway == nil || x.gateway.Hub == nil || room == "" {
		return
	}
	x.gateway.Hub.SendToRoom(room, Frame{Event: event, Data: data})
}

// SendToUser 网关级定向推送（供 service 的 MessagePublisher 适配器复用）。
func (g *MsgGateway) SendToUser(userID, event string, data interface{}) {
	if g == nil || g.Hub == nil || userID == "" {
		return
	}
	g.Hub.SendToUser(userID, Frame{Event: event, Data: data})
}

// Broadcast 网关级全体推送。
func (g *MsgGateway) Broadcast(event string, data interface{}) {
	if g == nil || g.Hub == nil {
		return
	}
	g.Hub.Broadcast(Frame{Event: event, Data: data})
}

// Handler GET /api/v1/ws-message 升级入口。
func (g *MsgGateway) Handler(c *gin.Context) {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"data": nil, "message": "Please log in first", "status_code": 401})
		return
	}
	user, ok := g.Auth(token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"data": nil, "message": "Invalid token", "status_code": 401})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	deviceID := c.GetHeader("Device-Id")
	client := &Client{
		hub:      g.Hub,
		conn:     conn,
		send:     make(chan Frame, 256),
		SocketID: uuid.NewString(), // 每连接唯一（替代 RemoteAddr，支持同 IP 多端）
		UserID:   user.ID,
		DeviceID: deviceID,
		Rooms:    make(map[string]struct{}),
	}
	g.Hub.register <- client

	// 自动加入用户专属房间（源端 client.join(String(user.id))）
	g.Hub.JoinRoom(client, UserRoom(user.ID))

	// 在线状态：首次上线时向 presence 房间广播 userStatusChanged
	if g.Presence != nil {
		becameOnline, payload := g.Presence.Connected(user.ID, client.SocketID, deviceID)
		if becameOnline {
			g.Hub.SendToRoom(PresenceRoom(user.ID), Frame{Event: EventUserStatusChanged, Data: payload})
		}
	}

	// connected 帧（源端只回 message + 四字段 user）
	client.send <- Frame{Event: EventConnected, Data: ConnectedPayload{
		Message: "连接成功",
		User: ConnectedUserInfo{
			ID: user.ID, Username: user.Username, Nickname: user.Nickname, Avatar: user.Avatar,
		},
	}}

	go g.writeLoop(client)
	go g.readLoop(client, user)
}

// readLoop 读循环 + 事件路由。
func (g *MsgGateway) readLoop(client *Client, user MsgUser) {
	defer func() {
		client.hub.unregister <- client
		_ = client.conn.Close()
		if g.Presence != nil {
			becameOffline, uid, payload := g.Presence.Disconnected(client.SocketID)
			if becameOffline {
				g.Hub.SendToRoom(PresenceRoom(uid), Frame{Event: EventUserStatusChanged, Data: payload})
			}
		}
	}()

	ctx := &MsgContext{gateway: g, client: client, User: user}

	for {
		var in InFrame
		if err := client.conn.ReadJSON(&in); err != nil {
			return
		}
		g.handle(ctx, in)
	}
}

// handle 单事件路由：连接类事件在此闭环，业务事件转交 Dispatch。
func (g *MsgGateway) handle(ctx *MsgContext, in InFrame) {
	client := ctx.client
	switch in.Event {
	case EventPing:
		if g.Presence != nil {
			g.Presence.Touch(ctx.User.ID, client.DeviceID)
		}
		ctx.Emit(EventPong, PongPayload{
			Message:   "pong",
			UserID:    ctx.User.ID,
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return

	case EventJoin:
		g.Hub.JoinRoom(client, UserRoom(ctx.User.ID))
		ctx.Emit(EventJoined, JoinedPayload{
			UserID: ctx.User.ID, Message: "成功加入房间", Room: UserRoom(ctx.User.ID),
		})
		return

	case EventLeave:
		// leaveRoom 是包内方法，直接调用（源端 client.leave(String(user.id))）
		g.Hub.leaveRoom(client, UserRoom(ctx.User.ID))
		ctx.Emit(EventLeaved, JoinedPayload{
			UserID: ctx.User.ID, Message: "成功离开房间", Room: UserRoom(ctx.User.ID),
		})
		return

	case EventGetProfile:
		ctx.Emit(EventProfile, ProfilePayload{
			ID: ctx.User.ID, Username: ctx.User.Username, Nickname: ctx.User.Nickname, Avatar: ctx.User.Avatar,
		})
		return

	case EventSubscribe:
		var in2 UserStatusInput
		_ = json.Unmarshal(in.Data, &in2)
		if in2.UserID == "" {
			ctx.EmitError(ErrCodeStatusUserIDRequired, "userId is required")
			return
		}
		g.Hub.JoinRoom(client, PresenceRoom(in2.UserID))
		var payload UserStatusPayload
		if g.Presence != nil {
			payload = g.Presence.Presence(in2.UserID)
		}
		ctx.Emit(EventUserStatus, payload)
		return

	case EventUnsubscribe:
		var in2 UserStatusInput
		_ = json.Unmarshal(in.Data, &in2)
		if in2.UserID == "" {
			ctx.EmitError(ErrCodeStatusUserIDRequired, "userId is required")
			return
		}
		g.Hub.leaveRoom(client, PresenceRoom(in2.UserID))
		return

	case EventGetUserStatus:
		var in2 UserStatusInput
		_ = json.Unmarshal(in.Data, &in2)
		if in2.UserID == "" {
			ctx.EmitError(ErrCodeStatusUserIDRequired, "userId is required")
			return
		}
		var payload UserStatusPayload
		if g.Presence != nil {
			payload = g.Presence.Presence(in2.UserID)
		}
		ctx.Emit(EventUserStatus, payload)
		return
	}

	// 业务事件：交给 handler 层注入的 Dispatch
	if g.Dispatch == nil {
		ctx.EmitError(ErrCodeUserNotFound, "事件分发未就绪")
		return
	}
	ack, errCode := g.Dispatch(ctx, in.Event, in.Data)
	if errCode != "" {
		ctx.EmitError(errCode, dispatchErrorText(errCode))
		return
	}
	// ack 由 Dispatch 通过 ctx.Emit 自行推送具体事件帧，这里不重复 emit
	_ = ack
}

// writeLoop 写循环。
func (g *MsgGateway) writeLoop(client *Client) {
	defer func() { _ = client.conn.Close() }()
	for f := range client.send {
		if err := client.conn.WriteJSON(f); err != nil {
			return
		}
	}
}

// InFrame 入站帧：data 保留原始 JSON 供各事件自行解析。
type InFrame struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// extractToken 按源端顺序取 token：query.token → Authorization（去 Bearer）→ auth header。
func extractToken(c *gin.Context) string {
	token := c.Query("token")
	if token == "" {
		token = c.GetHeader("Authorization")
	}
	if token == "" {
		token = c.GetHeader("Auth")
	}
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	return token
}

// dispatchErrorText 错误码 → 中文提示（对齐源端 error 帧 message 字段）。
func dispatchErrorText(code string) string {
	switch code {
	case ErrCodeNoPermission:
		return "无权限执行该操作"
	case ErrCodeMessageSendFail:
		return "消息发送失败"
	case ErrCodeHistoryFail:
		return "获取历史消息失败"
	case ErrCodeConversationsFail:
		return "获取私信会话失败"
	case ErrCodePrivateHistoryFail:
		return "获取私信历史失败"
	case ErrCodePrivateReadFail:
		return "批量标记私信已读失败"
	case ErrCodeConversationReadFail:
		return "标记会话已读失败"
	case ErrCodeRecallFail:
		return "撤回私信失败"
	case ErrCodeUnreadFail:
		return "获取未读计数失败"
	case ErrCodeMarkAllReadFail:
		return "标记所有消息为已读失败"
	case ErrCodeBatchFail:
		return "批量操作失败"
	case ErrCodeMarkReadFail:
		return "标记已读失败"
	default:
		return "操作失败"
	}
}

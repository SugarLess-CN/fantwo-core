// 消息域服务：用户在线状态（presence）。
//
// 对齐源端 tmp/picart/src/modules/message/message-presence.service.ts：
//   - Redis 存「用户 → 该用户的 socket 集合」，支持多端在线与多实例部署；
//   - socket → user 反向映射，断开时据此定位用户并判断是否所有端都下线；
//   - 键 TTL 300s（源端 PRESENCE_TTL），僵尸连接自动过期；
//   - 上线/下线只在「状态真正翻转」时广播 userStatusChanged。
//
// 键设计（与源端同名前缀）：
//
//	presence:user:{uid}      -> "sockA,sockB"   TTL 300s
//	presence:socket:{sid}    -> "{uid}"         TTL 300s
//	presence:lastseen:{uid}  -> unix 毫秒       无 TTL（下线后仍可查最后在线）
//
// 房间名对齐源端 getPresenceRoom：presence:user:{uid}（见 ws.PresenceRoom）。
package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	presenceUserPrefix     = "presence:user:"
	presenceSocketPrefix   = "presence:socket:"
	presenceLastSeenPrefix = "presence:lastseen:"
	// PresenceTTL 僵尸连接自动清理窗口（对齐源端 PRESENCE_TTL = 300 秒）。
	PresenceTTL = 300 * time.Second
)

// MessagePresenceService 在线状态服务（Redis 不可用时降级为「全部离线」）。
type MessagePresenceService struct {
	DB    *gorm.DB
	Redis *redis.Client
}

// NewMessagePresenceService 构造。
func NewMessagePresenceService(db *gorm.DB, rdb *redis.Client) *MessagePresenceService {
	return &MessagePresenceService{DB: db, Redis: rdb}
}

// available Redis 可用性。
func (s *MessagePresenceService) available() bool {
	return s != nil && s.Redis != nil
}

// ctx 短超时上下文（presence 属旁路能力，不能拖慢主链路）。
func (s *MessagePresenceService) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// HandleConnected 处理连接：登记 socket、记录最后活跃、返回是否首次上线。
func (s *MessagePresenceService) HandleConnected(userID, socketID, deviceID string) (bool, ws.UserStatusPayload) {
	becameOnline := true
	if s.available() {
		ctx, cancel := s.ctx()
		defer cancel()
		userKey := presenceUserPrefix + userID
		socketKey := presenceSocketPrefix + socketID

		sockets, _ := s.readSockets(ctx, userKey)
		becameOnline = len(sockets) == 0
		sockets[socketID] = struct{}{}
		_ = s.Redis.Set(ctx, userKey, joinSockets(sockets), PresenceTTL).Err()
		_ = s.Redis.Set(ctx, socketKey, userID, PresenceTTL).Err()
	}

	lastSeen := s.touchActivity(userID, deviceID)
	return becameOnline, ws.UserStatusPayload{
		UserID:     userID,
		IsOnline:   true,
		LastSeenAt: &lastSeen,
	}
}

// HandleDisconnected 处理断开：摘除 socket，返回是否所有端都下线。
//
// 返回 (becameOffline, userID, payload)：socket 未登记过时 becameOffline=false 且 userID 为空。
func (s *MessagePresenceService) HandleDisconnected(socketID string) (bool, string, ws.UserStatusPayload) {
	if !s.available() {
		return false, "", ws.UserStatusPayload{}
	}
	ctx, cancel := s.ctx()
	defer cancel()

	socketKey := presenceSocketPrefix + socketID
	userID, err := s.Redis.Get(ctx, socketKey).Result()
	if err != nil || userID == "" {
		return false, "", ws.UserStatusPayload{}
	}
	_ = s.Redis.Del(ctx, socketKey).Err()

	userKey := presenceUserPrefix + userID
	sockets, err := s.readSockets(ctx, userKey)
	if err != nil || len(sockets) == 0 {
		return false, userID, ws.UserStatusPayload{}
	}
	delete(sockets, socketID)

	if len(sockets) > 0 {
		// 还有其他端在线：刷新集合，不广播下线
		_ = s.Redis.Set(ctx, userKey, joinSockets(sockets), PresenceTTL).Err()
		return false, userID, ws.UserStatusPayload{}
	}

	_ = s.Redis.Del(ctx, userKey).Err()
	lastSeen := s.touchActivity(userID, "")
	return true, userID, ws.UserStatusPayload{
		UserID:     userID,
		IsOnline:   false,
		LastSeenAt: &lastSeen,
	}
}

// Touch 心跳续期（对齐源端 touchOnlineUser：仅在已在线时续 TTL）。
func (s *MessagePresenceService) Touch(userID, deviceID string) {
	if s.available() {
		ctx, cancel := s.ctx()
		defer cancel()
		userKey := presenceUserPrefix + userID
		if raw, err := s.Redis.Get(ctx, userKey).Result(); err == nil && raw != "" {
			_ = s.Redis.Set(ctx, userKey, raw, PresenceTTL).Err()
		}
	}
	_ = s.touchActivity(userID, deviceID)
}

// IsOnline 是否在线（任一端在线即为在线）。
func (s *MessagePresenceService) IsOnline(userID string) bool {
	if !s.available() {
		return false
	}
	ctx, cancel := s.ctx()
	defer cancel()
	sockets, err := s.readSockets(ctx, presenceUserPrefix+userID)
	if err != nil {
		return false
	}
	return len(sockets) > 0
}

// Presence 查询某人状态（对齐源端 getUserPresence：在线状态 + 最后活跃时间）。
func (s *MessagePresenceService) Presence(userID string) ws.UserStatusPayload {
	online := s.IsOnline(userID)
	last := s.lastSeen(userID)
	return ws.UserStatusPayload{UserID: userID, IsOnline: online, LastSeenAt: last}
}

// readSockets 读用户 socket 集合（逗号分隔）。
func (s *MessagePresenceService) readSockets(ctx context.Context, key string) (map[string]struct{}, error) {
	raw, err := s.Redis.Get(ctx, key).Result()
	out := make(map[string]struct{})
	if err != nil {
		if err == redis.Nil {
			return out, nil
		}
		return out, err
	}
	for _, sid := range strings.Split(raw, ",") {
		if sid != "" {
			out[sid] = struct{}{}
		}
	}
	return out, nil
}

// joinSockets 集合序列化。
func joinSockets(sockets map[string]struct{}) string {
	parts := make([]string, 0, len(sockets))
	for sid := range sockets {
		parts = append(parts, sid)
	}
	return strings.Join(parts, ",")
}

// touchActivity 刷新用户活跃时间：Redis 存 lastSeen + 顺带更新 user.last_active_at。
//
// 对齐源端 userService.touchUserActivity(userId, deviceId)：只动 last_active_at 一列，
// 不改用户其它字段。DB 不可用时静默跳过。
func (s *MessagePresenceService) touchActivity(userID, deviceID string) string {
	nowT := time.Now()
	if s.available() {
		ctx, cancel := s.ctx()
		defer cancel()
		_ = s.Redis.Set(ctx, presenceLastSeenPrefix+userID, nowT.UnixMilli(), 0).Err()
	}
	_ = deviceID // 设备维度的活跃统计属 user 域（user_device），消息域只关心用户级时间戳
	if s.DB != nil && userID != "" {
		_ = s.DB.Model(&model.User{}).Where("id = ?", userID).
			UpdateColumn("last_active_at", nowT).Error
	}
	return nowT.Format(time.RFC3339)
}

// lastSeen 最后在线时间：优先 Redis，其次 users.last_active_at，都没有则 nil。
func (s *MessagePresenceService) lastSeen(userID string) *string {
	if s.available() {
		ctx, cancel := s.ctx()
		defer cancel()
		if raw, err := s.Redis.Get(ctx, presenceLastSeenPrefix+userID).Result(); err == nil && raw != "" {
			if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
				str := time.UnixMilli(ms).Format(time.RFC3339)
				return &str
			}
		}
	}
	if s.DB != nil && userID != "" {
		var u model.User
		if err := s.DB.Select("last_active_at").Where("id = ?", userID).First(&u).Error; err == nil {
			if u.LastActiveAt != nil {
				str := u.LastActiveAt.Format(time.RFC3339)
				return &str
			}
		}
	}
	return nil
}

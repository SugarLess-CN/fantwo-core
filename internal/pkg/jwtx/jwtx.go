// Package jwtx JWT 签发与解析（golang-jwt/jwt/v5）。
//
// 对齐原仓库 passport-jwt 行为：
//   - HS256 + JWT_SECRET；
//   - claims 携带 userID、roles、permissions（原 PermissionGuard 不查库，
//     直接用 JWT 内的 roles[].permissions[].name 比对——保持同一语义）；
//   - deviceId 支持多端在线（原 user-device 体系）。
package jwtx

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 自定义 claims。
type Claims struct {
	UserID      string   `json:"uid"`
	DeviceID    string   `json:"did,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"perms,omitempty"`
	// PermVer 权限版本号：签发时 Redis user:{id}:permver 的快照。
	//
	// ⚠️ 2026-11-28 起**不再作为拒绝请求的依据**：权限已改为从数据库实时读取
	// （middleware/permsource.go），用户无需重登即可拿到新权限。
	// 保留该字段是为了留一个单调递增的版本戳（将来要恢复「强制重登」可直接用）。
	// 见 middleware/authz.go 的第 3 条说明。
	PermVer int64 `json:"pv,omitempty"`
	jwt.RegisteredClaims
}

// Signer 签发器（由 config 构造，main 注入到依赖方）。
type Signer struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewSigner 构造签发器；expires/refresh 传 0 时使用默认（1h / 7d）。
func NewSigner(secret, issuer string, expires, refresh time.Duration) *Signer {
	if expires <= 0 {
		expires = time.Hour
	}
	if refresh <= 0 {
		refresh = 7 * 24 * time.Hour
	}
	return &Signer{
		secret:     []byte(secret),
		issuer:     issuer,
		accessTTL:  expires,
		refreshTTL: refresh,
	}
}

// AccessTTL / RefreshTTL 有效期读取。
func (s *Signer) AccessTTL() time.Duration  { return s.accessTTL }
func (s *Signer) RefreshTTL() time.Duration { return s.refreshTTL }

// Generate 签发 token。tokenType: "access" / "refresh"（写入 claims 角色）。
func (s *Signer) Generate(userID string, roles, permissions []string, deviceID, tokenType string) (string, error) {
	ttl := s.accessTTL
	if tokenType == "refresh" {
		ttl = s.refreshTTL
	}
	now := time.Now()
	claims := Claims{
		UserID:      userID,
		DeviceID:    deviceID,
		Roles:       roles,
		Permissions: permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// GenerateWithPermVer 签发携带权限版本号的 token（T-05b：perm_ver 方案）。
// tokenType 语义同 Generate；permVer 为当前 Redis user:{id}:permver 值。
func (s *Signer) GenerateWithPermVer(userID string, roles, permissions []string, deviceID, tokenType string, permVer int64) (string, error) {
	ttl := s.accessTTL
	if tokenType == "refresh" {
		ttl = s.refreshTTL
	}
	now := time.Now()
	claims := Claims{
		UserID:      userID,
		DeviceID:    deviceID,
		Roles:       roles,
		Permissions: permissions,
		PermVer:     permVer,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Remaining 返回 token 剩余有效期（黑名单 TTL 用；已过期返回 0）。
func (s *Signer) Remaining(c *Claims) time.Duration {
	if c == nil || c.ExpiresAt == nil {
		return 0
	}
	d := time.Until(c.ExpiresAt.Time)
	if d < 0 {
		return 0
	}
	return d
}

// Parse 解析并校验 token，返回 claims。
// 错误语义：token 过期/签名错误/格式错误均返回 error（中间件统一按 401 处理）。
func (s *Signer) Parse(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("非法的签名算法")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("token 无效")
	}
	return claims, nil
}

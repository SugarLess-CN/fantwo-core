// Package service 认证服务：登录 / 注册 / 刷新 / 登出 / 全端登出 / 验证码。
//
// 对齐原 user.service.ts 的认证语义：
//   - 账号（含 @ 邮箱 / 用户名）+ 密码登录，bcrypt 校验；
//   - 签发 access + refresh（refresh 哈希落 user_device，支持多端）；
//   - 登出：设备删除 + access token 黑名单（TTL=剩余生命）；
//   - 全端登出：logout_all 时间戳 + 清全部设备；
//   - 验证码：Redis 存 6 位码（10 分钟），1 分钟防重发；
//     短信/邮件发送经 provider 接口（默认 logger 实现，见 verifycode.go）。
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"go.uber.org/zap"
	"math/big"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AuthService 认证业务。
type AuthService struct {
	d     *Deps
	Codes *VerifyCodeStore
}

// NewAuthService 构造。
func NewAuthService(d *Deps) *AuthService {
	return &AuthService{d: d, Codes: NewVerifyCodeStore(d.Redis)}
}

// hashPassword bcrypt 哈希（cost 10，对齐原 saltRounds=10）。
func hashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 10)
	return string(b), err
}

// checkPassword bcrypt 比对。
func checkPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// LoginInput 登录入参。
type LoginInput struct {
	Account  string
	Password string
	DeviceID string
}

// Login 用户登录：账号密码 → 双 token + 设备登记。
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	if in.Account == "" || in.Password == "" || in.DeviceID == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	user, err := s.d.Users.FindByAccount(s.d.DB, in.Account)
	if IsNotFound(err) {
		return nil, "", "", errcode.ErrUserNotExist
	}
	if err != nil {
		s.d.Logger.Error("login find user", zapErr(err))
		return nil, "", "", errcode.ErrInternal
	}
	if !checkPassword(user.Password, in.Password) {
		return nil, "", "", errcode.ErrPasswordError
	}
	if user.Status == model.UserStatusBanned {
		return nil, "", "", errcode.ErrAccountBanned
	}
	token, refresh, e := s.issueTokens(ctx, user, in.DeviceID)
	if e != nil {
		return nil, "", "", e
	}
	return user, token, refresh, nil
}

// RegisterInput 注册入参。
type RegisterInput struct {
	Username string
	Nickname string
	Password string
	Email    string
	Phone    string
}

// Register 用户注册：唯一性校验 → 建用户 + 默认角色。
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*model.User, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Username == "" || in.Password == "" {
		return nil, errcode.ErrInvalidParam
	}
	if dup, err := s.d.Users.ExistsUsername(s.d.DB, in.Username, ""); err != nil {
		s.d.Logger.Error("register check username", zapErr(err))
		return nil, errcode.ErrInternal
	} else if dup {
		return nil, errcode.ErrUsernameExists
	}
	if in.Email != "" {
		if dup, err := s.d.Users.ExistsEmail(s.d.DB, in.Email, ""); err != nil {
			return nil, errcode.ErrInternal
		} else if dup {
			return nil, errcode.ErrEmailExists
		}
	}
	if in.Phone != "" {
		if dup, err := s.d.Users.ExistsPhone(s.d.DB, in.Phone, ""); err != nil {
			return nil, errcode.ErrInternal
		} else if dup {
			return nil, errcode.ErrPhoneExists
		}
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	user := &model.User{
		Username: in.Username,
		Nickname: nilIfEmpty(in.Nickname),
		Password: hash,
		Email:    nilIfEmpty(in.Email),
		Phone:    nilIfEmpty(in.Phone),
		Status:   model.UserStatusActive,
	}
	// 默认挂普通用户角色（对齐原注册流程初始化 user 角色）。
	if role, err := s.d.Roles.FindByName(s.d.DB, model.RoleNameUser); err == nil {
		user.Roles = []model.Role{*role}
	}
	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.d.Users.Create(tx, user); err != nil {
			return err
		}
		return s.d.Users.CreateProfile(tx, &model.UserProfile{UserID: user.ID})
	}); err != nil {
		s.d.Logger.Error("register create", zapErr(err))
		return nil, errcode.ErrInternal
	}
	return user, nil
}

// RefreshInput 刷新入参。
type RefreshInput struct {
	RefreshToken string
	DeviceID     string
}

// RefreshToken 刷新 access token：refresh 验签 + 设备哈希比对。
func (s *AuthService) RefreshToken(ctx context.Context, in RefreshInput) (string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return "", e
	}
	if in.RefreshToken == "" || in.DeviceID == "" {
		return "", errcode.ErrInvalidParam
	}
	claims, err := s.d.Signer.Parse(in.RefreshToken)
	if err != nil {
		return "", errcode.ErrRefreshTokenInvalid
	}
	if authtoken.Blacklisted(ctx, s.d.Redis, in.RefreshToken) {
		return "", errcode.ErrRefreshTokenInvalid
	}
	if at := authtoken.LogoutAllAt(ctx, s.d.Redis, claims.UserID); !at.IsZero() &&
		claims.IssuedAt != nil && claims.IssuedAt.Time.Before(at.Add(time.Second)) {
		return "", errcode.ErrRefreshTokenInvalid
	}
	device, err := s.d.Users.FindDevice(s.d.DB, claims.UserID, in.DeviceID)
	if err != nil || device.HashedRefreshToken == nil ||
		!checkPassword(*device.HashedRefreshToken, in.RefreshToken) {
		return "", errcode.ErrRefreshTokenInvalid
	}
	user, err := s.d.Users.FindByID(s.d.DB, claims.UserID)
	if err != nil {
		return "", errcode.ErrUserNotExist
	}
	token, e := s.accessToken(ctx, user, in.DeviceID)
	if e != nil {
		return "", e
	}
	_ = s.d.Users.TouchDevice(s.d.DB, device.ID)
	return token, nil
}

// Logout 单端登出：删设备 + access/refresh 进黑名单（TTL=剩余生命）。
func (s *AuthService) Logout(ctx context.Context, userID, token, deviceID string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if claims, err := s.d.Signer.Parse(token); err == nil {
		_ = authtoken.BlacklistAdd(ctx, s.d.Redis, token, s.d.Signer.Remaining(claims))
	}
	if deviceID != "" {
		if err := s.d.Users.DeleteDevice(s.d.DB, userID, deviceID); err != nil {
			s.d.Logger.Error("logout delete device", zapErr(err))
		}
	}
	return nil
}

// LogoutAll 全端登出：logout_all 时间戳 + 清全部设备。
func (s *AuthService) LogoutAll(ctx context.Context, userID string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if err := authtoken.SetLogoutAll(ctx, s.d.Redis, userID, time.Now()); err != nil {
		s.d.Logger.Error("logout-all set", zapErr(err))
		return errcode.ErrInternal
	}
	if err := s.d.Users.DeleteAllDevices(s.d.DB, userID); err != nil {
		s.d.Logger.Error("logout-all devices", zapErr(err))
	}
	return nil
}

// SendCodeInput 验证码发送入参。
type SendCodeInput struct {
	Target string // 邮箱或手机号
	Scene  string // register / reset_password / bind_phone
}

// SendCode 发送验证码（1 分钟防重发，10 分钟有效）。
func (s *AuthService) SendCode(ctx context.Context, in SendCodeInput) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if in.Target == "" || in.Scene == "" {
		return errcode.ErrInvalidParam
	}
	if err := s.Codes.Send(ctx, in.Target, in.Scene); err != nil {
		switch err {
		case errTooFrequent:
			return errcode.ErrVerifyCodeTooOften
		default:
			s.d.Logger.Error("send code", zapErr(err))
			return errcode.ErrInternal
		}
	}
	return nil
}

// VerifyCode 校验验证码（一次性，验后即焚）。
func (s *AuthService) VerifyCode(ctx context.Context, target, scene, code string) *errcode.Err {
	if ok, err := s.Codes.Verify(ctx, target, scene, code); err != nil {
		return errcode.ErrInternal
	} else if !ok {
		return errcode.ErrVerifyCodeInvalid
	}
	return nil
}

// ChangePassword 修改密码（验原密码）。
func (s *AuthService) ChangePassword(ctx context.Context, userID, oldPwd, newPwd string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if oldPwd == "" || newPwd == "" {
		return errcode.ErrInvalidParam
	}
	user, err := s.d.Users.FindPlainByID(s.d.DB, userID)
	if err != nil {
		return errcode.ErrUserNotExist
	}
	if !checkPassword(user.Password, oldPwd) {
		return errcode.ErrPasswordError
	}
	hash, err := hashPassword(newPwd)
	if err != nil {
		return errcode.ErrInternal
	}
	if err := s.d.Users.UpdateFields(s.d.DB, userID, map[string]interface{}{"password": hash}); err != nil {
		return errcode.ErrInternal
	}
	// 改密后全端登出（安全语义）。
	_ = authtoken.SetLogoutAll(ctx, s.d.Redis, userID, time.Now())
	return nil
}

// ResetPassword 忘记密码：邮箱验证码重置。
func (s *AuthService) ResetPassword(ctx context.Context, email, code, newPwd string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if email == "" || code == "" || newPwd == "" {
		return errcode.ErrInvalidParam
	}
	user, err := s.d.Users.FindByEmail(s.d.DB, email)
	if IsNotFound(err) {
		return errcode.ErrUserNotExist
	}
	if err != nil {
		return errcode.ErrInternal
	}
	if ok, err := s.Codes.Verify(ctx, email, "reset_password", code); err != nil {
		return errcode.ErrInternal
	} else if !ok {
		return errcode.ErrVerifyCodeInvalid
	}
	hash, err := hashPassword(newPwd)
	if err != nil {
		return errcode.ErrInternal
	}
	if err := s.d.Users.UpdateFields(s.d.DB, user.ID, map[string]interface{}{"password": hash}); err != nil {
		return errcode.ErrInternal
	}
	_ = authtoken.SetLogoutAll(ctx, s.d.Redis, user.ID, time.Now())
	return nil
}

// issueTokens 签发 access + refresh 并登记设备。
func (s *AuthService) issueTokens(ctx context.Context, user *model.User, deviceID string) (string, string, *errcode.Err) {
	access, e := s.accessToken(ctx, user, deviceID)
	if e != nil {
		return "", "", e
	}
	refresh, err := s.d.Signer.GenerateWithPermVer(user.ID, roleNames(user), permNames(user), deviceID, "refresh", currentPermVer(ctx, s.d.Redis, user.ID))
	if err != nil {
		s.d.Logger.Error("issue refresh", zapErr(err))
		return "", "", errcode.ErrInternal
	}
	hashed, err := hashPassword(refresh)
	if err != nil {
		return "", "", errcode.ErrInternal
	}
	rt := refresh
	now := time.Now()
	if err := s.d.Users.UpsertDevice(s.d.DB, &model.UserDevice{
		UserID: user.ID, DeviceID: deviceID,
		RefreshToken: &rt, HashedRefreshToken: &hashed,
		LoginAt: &now, LastActiveAt: &now,
	}); err != nil {
		s.d.Logger.Error("upsert device", zapErr(err))
		return "", "", errcode.ErrInternal
	}
	_ = s.d.Users.UpdateFields(s.d.DB, user.ID, map[string]interface{}{"last_login_at": now})
	return access, refresh, nil
}

// accessToken 签发 access token。
func (s *AuthService) accessToken(ctx context.Context, user *model.User, deviceID string) (string, *errcode.Err) {
	token, err := s.d.Signer.GenerateWithPermVer(user.ID, roleNames(user), permNames(user), deviceID, "access", currentPermVer(ctx, s.d.Redis, user.ID))
	if err != nil {
		s.d.Logger.Error("issue access", zapErr(err))
		return "", errcode.ErrInternal
	}
	return token, nil
}

// roleNames / permNames 取角色名与权限名集合。
func roleNames(u *model.User) []string {
	names := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		names = append(names, r.Name)
	}
	return names
}

func permNames(u *model.User) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, r := range u.Roles {
		for _, p := range r.Permissions {
			if _, ok := seen[p.Name]; !ok {
				seen[p.Name] = struct{}{}
				out = append(out, p.Name)
			}
		}
	}
	return out
}

// currentPermVer 当前权限版本号（签发时快照，登出/改密后 bump 使旧 token 失效）。
func currentPermVer(ctx context.Context, rdb *redis.Client, userID string) int64 {
	return authtoken.PermVer(ctx, rdb, userID)
}

// genCode 6 位数字验证码。
func genCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", 100000+int(n.Int64()))
}

// zapErr error 包装（zap.Error 便捷）。
func zapErr(err error) zap.Field { return zap.Error(err) }

// nilIfEmpty 空串转 nil（对应 TypeORM nullable 字段）。
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SmsLogin 短信验证码登录：校验验证码 → 已注册则直接登录，未注册自动建号。
// 对齐原「手机号登录」语义扩展（原仓库仅账号密码，T-05b 任务书 §1 要求补齐）。
func (s *AuthService) SmsLogin(ctx context.Context, phone, code, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	if phone == "" || code == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	ok, err := s.Codes.Verify(ctx, phone, "login", code)
	if err != nil {
		return nil, "", "", errcode.ErrInternal
	}
	if !ok {
		return nil, "", "", errcode.ErrVerifyCodeInvalid
	}
	user, err := s.d.Users.FindByPhone(s.d.DB, phone)
	if IsNotFound(err) {
		pwd, herr := hashPassword(genCode() + genCode())
		if herr != nil {
			return nil, "", "", errcode.ErrInternal
		}
		user = &model.User{
			Username: "u" + phone + genCode()[:2],
			Password: pwd,
			Phone:    &phone,
			Status:   model.UserStatusActive,
		}
		if role, rerr := s.d.Roles.FindByName(s.d.DB, model.RoleNameUser); rerr == nil {
			user.Roles = []model.Role{*role}
		}
		if cerr := s.d.Users.Create(s.d.DB, user); cerr != nil {
			s.d.Logger.Error("sms login auto register", zapErr(cerr))
			return nil, "", "", errcode.ErrInternal
		}
		_ = s.d.Users.CreateProfile(s.d.DB, &model.UserProfile{UserID: user.ID})
	} else if err != nil {
		return nil, "", "", errcode.ErrInternal
	}
	if user.Status == model.UserStatusBanned {
		return nil, "", "", errcode.ErrAccountBanned
	}
	if deviceID == "" {
		deviceID = "sms-" + phone
	}
	token, refresh, e := s.issueTokens(ctx, user, deviceID)
	if e != nil {
		return nil, "", "", e
	}
	return user, token, refresh, nil
}

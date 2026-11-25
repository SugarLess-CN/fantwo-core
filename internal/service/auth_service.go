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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"go.uber.org/zap"
	"math/big"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
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

// Deps 只读暴露依赖组（T-37e：绑定管理 handler 在 AuthHandler 上现建
// OAuthService，需要拿到同一份 Deps —— 无状态共享，只读访问器足够）。
func (s *AuthService) Deps() *Deps { return s.d }

// hashPassword bcrypt 哈希（cost 10，对齐原 saltRounds=10）。
// 注意：bcrypt 输入上限 72 字节，仅用于用户密码；token 哈希必须用 hashToken。
func hashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 10)
	return string(b), err
}

// checkPassword bcrypt 比对。
func checkPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// hashToken 哈希 JWT refresh token。
//
// 不能用 bcrypt：refresh token 是数百字节的 JWT，超出 bcrypt 的 72 字节输入上限，
// 会直接报错（曾导致登录返回 500）。token 本身是高熵随机串，用 SHA-256 足够，
// 且无需慢哈希。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// checkTokenHash 常量时间比对 token 哈希。
func checkTokenHash(hash, token string) bool {
	want := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1
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
	if in.Account == "" || in.Password == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	// device-id 单独判：它是**请求头**，不是 body 字段。缺失时客户端需要知道
	// 「去补哪个头」，而不是笼统的「Invalid request parameter」。
	// 与 RefreshToken（handler/auth.go:118 已用 ErrDeviceIDRequired）保持同一错误码口径。
	//
	// 历史问题：这三项原本合并成一个 ErrInvalidParam（10008），
	// 导致 Apifox 里 Device-Id 传空字符串时只看到「参数错误」，完全不知道是缺头。
	if in.DeviceID == "" {
		return nil, "", "", errcode.ErrDeviceIDRequired
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
	// 密码长度校验：对齐原仓库 create-user.dto.ts @MinLength(6)（按 rune 计数）。
	if utf8.RuneCountInString(in.Password) < 6 {
		return nil, errcode.ErrInvalidParam.WithKey(i18n.AuthPasswordTooShort)
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
	if authtoken.Blacklisted(ctx, s.d.AuthTok, in.RefreshToken) {
		return "", errcode.ErrRefreshTokenInvalid
	}
	if at := authtoken.LogoutAllAt(ctx, s.d.AuthTok, claims.UserID); !at.IsZero() &&
		claims.IssuedAt != nil && claims.IssuedAt.Time.Before(at.Add(time.Second)) {
		return "", errcode.ErrRefreshTokenInvalid
	}
	device, err := s.d.Users.FindDevice(s.d.DB, claims.UserID, in.DeviceID)
	if err != nil || device.HashedRefreshToken == nil ||
		!checkTokenHash(*device.HashedRefreshToken, in.RefreshToken) {
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
		_ = authtoken.BlacklistAdd(ctx, s.d.AuthTok, token, s.d.Signer.Remaining(claims))
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
	if err := authtoken.SetLogoutAll(ctx, s.d.AuthTok, userID, time.Now()); err != nil {
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
//
// 投递策略（T-15：可配置即生效、未配置即降级）：
//   - target 含 @ → 邮件（SMTP 未配置则只写 Redis + 打 log）；
//   - 否则视为手机号 → 短信（无启用渠道则只写 Redis + 打 log，即开发模式）。
func (s *AuthService) SendCode(ctx context.Context, in SendCodeInput) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if in.Target == "" || in.Scene == "" {
		return errcode.ErrInvalidParam
	}
	code, err := s.Codes.Send(ctx, in.Target, in.Scene)
	if err != nil {
		switch err {
		case errTooFrequent:
			return errcode.ErrVerifyCodeTooOften
		default:
			s.d.Logger.Error("send code", zapErr(err))
			return errcode.ErrInternal
		}
	}

	if isEmailTarget(in.Target) {
		mailer := NewMailer(s.d)
		if !mailer.Configured() {
			s.d.Logger.Warn(fmt.Sprintf("[开发模式] 邮件未配置，验证码 target=%s scene=%s code=%s", in.Target, in.Scene, code))
			return nil
		}
		subject, body := verifyCodeMail(in.Scene, code)
		if err := mailer.Send(in.Target, subject, body); err != nil {
			s.d.Logger.Error("邮件发送失败 target=" + in.Target + " err=" + err.Error())
			return errcode.ErrInternal.WithKey(i18n.AuthCaptchaSendFailed)
		}
		return nil
	}

	sms := NewSMSSender(s.d)
	if !sms.Configured() {
		s.d.Logger.Warn(fmt.Sprintf("[开发模式] 短信渠道未配置，验证码 phone=%s scene=%s code=%s", maskPhone(in.Target), in.Scene, code))
		return nil
	}
	if err := sms.Send(ctx, in.Target, code, in.Scene); err != nil {
		s.d.Logger.Error("短信发送失败 phone=" + maskPhone(in.Target) + " err=" + err.Error())
		return errcode.ErrInternal.WithKey(i18n.AuthCaptchaSendFailed)
	}
	return nil
}

// isEmailTarget 目标是否邮箱（含 @）。
func isEmailTarget(target string) bool { return strings.Contains(target, "@") }

// verifyCodeMail 验证码邮件标题与正文。
func verifyCodeMail(scene, code string) (string, string) {
	title := "验证码"
	switch scene {
	case "reset_password":
		title = "重置密码验证码"
	case "bind_phone":
		title = "绑定手机验证码"
	case "login":
		title = "登录验证码"
	}
	body := fmt.Sprintf(`<div style="font-family:sans-serif;padding:24px">
<h2>%s</h2>
<p>您的验证码是：</p>
<p style="font-size:28px;font-weight:bold;letter-spacing:6px">%s</p>
<p>验证码 10 分钟内有效，请勿泄露给他人。</p>
</div>`, title, code)
	return "[PicArt] " + title, body
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
	// 新密码长度校验：对齐原仓库 change-password.dto.ts @MinLength(8)（按 rune 计数）。
	if utf8.RuneCountInString(newPwd) < 8 {
		return errcode.ErrInvalidParam.WithKey(i18n.AuthNewPasswordTooShort)
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
	_ = authtoken.SetLogoutAll(ctx, s.d.AuthTok, userID, time.Now())
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
	// 新密码长度校验：与 ChangePassword 同口径（@MinLength(8)，按 rune 计数）。
	if utf8.RuneCountInString(newPwd) < 8 {
		return errcode.ErrInvalidParam.WithKey(i18n.AuthNewPasswordTooShort)
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
	_ = authtoken.SetLogoutAll(ctx, s.d.AuthTok, user.ID, time.Now())
	return nil
}

// issueTokens 签发 access + refresh 并登记设备。
func (s *AuthService) issueTokens(ctx context.Context, user *model.User, deviceID string) (string, string, *errcode.Err) {
	access, e := s.accessToken(ctx, user, deviceID)
	if e != nil {
		return "", "", e
	}
	refresh, err := s.d.Signer.GenerateWithPermVer(user.ID, roleNames(user), permNames(user), deviceID, "refresh", currentPermVer(ctx, s.d.AuthTok, user.ID))
	if err != nil {
		s.d.Logger.Error("issue refresh", zapErr(err))
		return "", "", errcode.ErrInternal
	}
	hashed := hashToken(refresh)
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
	token, err := s.d.Signer.GenerateWithPermVer(user.ID, roleNames(user), permNames(user), deviceID, "access", currentPermVer(ctx, s.d.AuthTok, user.ID))
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
func currentPermVer(ctx context.Context, st authtoken.Store, userID string) int64 {
	return authtoken.PermVer(ctx, st, userID)
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

// ---------------------------------------------------------------------------
// 第三方登录编排（T-37c：全部走 pkg/oauthkit）
//
// 分工：协议交互在 oauthkit（拼 URL / 换 token / 拉 userinfo / 校验 token），
// 账号归属在 OAuthService.FindOrCreateByOAuth，本段只做「state + 流程串联 + 双 token」。
//
// 硬约束（docs/14 §7.1.2）：
//   - 禁令 1/2：日志只打 oauthErrClass(err) 与 provider/channel，不打 Token/User，
//     也不打 err.Error()（SDK 错误文本可能夹带上游响应体）。
//   - 禁令 3：参数先本地校验（空 code / 空 state / 空 token 用 ErrInvalidArgument）。
//   - 禁令 5：QQ app 的 token 必须先 ValidateToken，绝不手工置 Verified。
// ---------------------------------------------------------------------------

// oauthStateTTL state 在 Redis 的有效期（一次授权流程足够）。
const oauthStateTTL = 10 * time.Minute

// oauthStatePrefix state 键前缀：oauth:<provider>:<channel>:state:
//
// 按 (provider, channel) 分开，避免「微信 web 的 state 被拿去换 QQ 的 code」这类
// 跨渠道重放；GitHub 只有 web 一个渠道，同样落在 oauth:github:web:state: 下。
func oauthStatePrefix(provider, channel string) string {
	return "oauth:" + provider + ":" + channel + ":state:"
}

// oauthProvider 按 (provider, channel) 构造 SDK 实例。
//
// 配置读取：oauthkit.ConfigKeys 给出该组合需要的 5 个键名（不在此处硬编码键名），
// 值从 s.d.Cfg 读（配置服务已实现 DB 优先 + env 兜底），未配置齐全返回 20026。
func (s *AuthService) oauthProvider(provider, channel string) (oauthkit.Provider, *errcode.Err) {
	if s.d.Cfg == nil {
		return nil, errOAuthNotConfigured
	}
	keyID, keySecret, keyRedirect, keyScope, keySuccess := oauthkit.ConfigKeys(provider, channel)
	if keyID == "" {
		// (provider, channel) 组合不受支持 —— 接线错误，不是配置事故。
		s.d.Logger.Error("oauth unsupported provider/channel",
			zap.String("provider", provider), zap.String("channel", channel))
		return nil, oauthFailed()
	}
	cfg := oauthkit.Config{
		ClientID:     strings.TrimSpace(s.d.Cfg.GetString(keyID, "")),
		ClientSecret: readOptionalConfig(s.d.Cfg, keySecret),
		RedirectURI:  readOptionalConfig(s.d.Cfg, keyRedirect),
		Scope:        readOptionalConfig(s.d.Cfg, keyScope),
	}
	if keySuccess != "" {
		cfg.SuccessRedirectURL = readOptionalConfig(s.d.Cfg, keySuccess)
	}
	p, err := oauthkit.New(provider, channel, cfg)
	if err != nil {
		// New 只在 provider/channel 组合不支持或 github scope 为空时失败。
		s.d.Logger.Error("oauth build provider", zap.String("provider", provider),
			zap.String("channel", channel), zapErr(err))
		return nil, oauthFailed()
	}
	if !p.Configured() {
		return nil, errOAuthNotConfigured
	}
	return p, nil
}

// readOptionalConfig 读取可能为空的配置键（键名为空时直接返回空串）。
func readOptionalConfig(cfg ConfigReader, key string) string {
	if key == "" || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.GetString(key, ""))
}

// oauthSuccessURL 登录成功后前端跳转地址（未配置回落 "/"）。
func (s *AuthService) oauthSuccessURL(provider, channel string) string {
	_, _, _, _, keySuccess := oauthkit.ConfigKeys(provider, channel)
	if u := readOptionalConfig(s.d.Cfg, keySuccess); u != "" {
		return u
	}
	return "/"
}

// issueOAuthState 生成 state 并写入 Redis（**fail-closed**）。
//
// 旧实现（github_oauth.go:91-95）在 Set 失败时只打一条 warn 就继续放行，
// 回调时 VerifyState 又因为「Redis 不可用 → return true」放行 —— 两道防线同时失效，
// 等于 CSRF 防护完全不存在（docs/14 §6.4 记录的缺陷）。
// 新实现：state 写不进去就不发授权链接，直接 503 让前端稍后重试。
func (s *AuthService) issueOAuthState(ctx context.Context, provider, channel string) (string, *errcode.Err) {
	if s.d.AuthTok == nil {
		s.d.Logger.Error("oauth state store unavailable (AuthTok nil), refuse to start oauth flow",
			zap.String("provider", provider), zap.String("channel", channel))
		return "", errcode.ErrDependencyUnavailable
	}
	state, err := randomState()
	if err != nil {
		s.d.Logger.Error("oauth generate state", zapErr(err))
		return "", errcode.ErrInternal
	}
	if err := s.d.AuthTok.Set(ctx, oauthStatePrefix(provider, channel)+state, 1, oauthStateTTL); err != nil {
		s.d.Logger.Error("oauth persist state failed, refuse to start oauth flow",
			zap.String("provider", provider), zap.String("channel", channel), zapErr(err))
		return "", errcode.ErrDependencyUnavailable
	}
	return state, nil
}

// verifyOAuthState 校验并消费 state（**fail-closed**）。
//
// 三种结果严格区分：
//   - state 为空 → 400 参数错误（禁令 3：调用方没传，重试无用）
//   - Redis 不可用 / 读写报错 → 503（依赖故障，可重试；**绝不放行**）
//   - 未命中 / 已用过 → 400 AuthOAuthFailed（过期或被重放）
func (s *AuthService) verifyOAuthState(ctx context.Context, provider, channel, state string) *errcode.Err {
	if strings.TrimSpace(state) == "" {
		return errcode.ErrInvalidParam
	}
	if s.d.AuthTok == nil {
		s.d.Logger.Error("oauth state store unavailable (AuthTok nil), refuse to verify state",
			zap.String("provider", provider), zap.String("channel", channel))
		return errcode.ErrDependencyUnavailable
	}
	key := oauthStatePrefix(provider, channel) + state
	n, err := s.d.AuthTok.Exists(ctx, key)
	if err != nil {
		s.d.Logger.Error("oauth verify state failed, refuse the callback",
			zap.String("provider", provider), zap.String("channel", channel), zapErr(err))
		return errcode.ErrDependencyUnavailable
	}
	if n == 0 {
		// state 未命中 = 链接已过期 / 已用过 / 伪造 —— 用户可自行修复（重新发起登录），
		// 必须与「上游失败」区分开：上游失败要等，state 失效要重来（T-37f）。
		// 注意仍然是 400 而不是 401：这不是本站认证态失效，是本次登录参数失效。
		return errcode.ErrOAuthStateInvalid
	}
	// 一次性消费：删不掉也当失败处理（否则同一 state 可被重放换 token）。
	if err := s.d.AuthTok.Del(ctx, key); err != nil {
		s.d.Logger.Error("oauth consume state failed, refuse the callback",
			zap.String("provider", provider), zap.String("channel", channel), zapErr(err))
		return errcode.ErrDependencyUnavailable
	}
	return nil
}

// oauthAuthURL 取授权链接 + 签发 state（web 渠道通用）。
func (s *AuthService) oauthAuthURL(ctx context.Context, provider, channel string) (*dto.OAuthURLDto, *errcode.Err) {
	p, e := s.oauthProvider(provider, channel)
	if e != nil {
		return nil, e
	}
	state, e := s.issueOAuthState(ctx, provider, channel)
	if e != nil {
		return nil, e
	}
	u, err := p.AuthURL(state)
	if err != nil {
		// AuthURL 的失败一般是配置缺项（redirect_uri 未配）或渠道不支持，均非用户可修。
		s.d.Logger.Error("oauth build auth url",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		if e := mapOAuthErr(err); e != nil && e.BizCode == errcode.BizInvalidParam {
			return nil, e
		}
		return nil, oauthFailed()
	}
	return &dto.OAuthURLDto{URL: u, State: state}, nil
}

// oauthWebCallback web 渠道回调（微信 / QQ / GitHub 共用）。
//
// 顺序固定：校验 state（fail-closed）→ Exchange(code) → UserInfo → 账号归属 → 双 token。
func (s *AuthService) oauthWebCallback(ctx context.Context, provider, channel, code, state, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	// 禁令 3：空 code 先本地拦掉，别白耗一次上游配额。
	if strings.TrimSpace(code) == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	p, e := s.oauthProvider(provider, channel)
	if e != nil {
		return nil, "", "", e
	}
	if e := s.verifyOAuthState(ctx, provider, channel, state); e != nil {
		return nil, "", "", e
	}

	tok, err := p.Exchange(ctx, code)
	if err != nil {
		s.d.Logger.Error("oauth exchange failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	u, err := p.UserInfo(ctx, tok)
	if err != nil {
		// 禁令 2：只打分类标签，不打 err.Error()（可能夹带上游响应体）。
		s.d.Logger.Error("oauth userinfo failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	return s.finishOAuthLogin(ctx, tok, u, deviceID)
}

// oauthAppLoginByCode app 渠道「交 code」登录（微信移动应用）。
//
// 微信 App 交的是 code（客户端 SDK 换不到 token，后端自己换），因此**不需要**
// ValidateToken —— 这一点与 QQ app 相反，不许为「接口统一」而抹平（docs/14 §8.5 问题 2）。
// App 渠道没有 state（客户端不参与浏览器跳转），CSRF 面由 code 一次性 + 服务端换 token 覆盖。
func (s *AuthService) oauthAppLoginByCode(ctx context.Context, provider, channel, code, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	if strings.TrimSpace(code) == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	p, e := s.oauthProvider(provider, channel)
	if e != nil {
		return nil, "", "", e
	}
	tok, err := p.Exchange(ctx, code)
	if err != nil {
		s.d.Logger.Error("oauth app exchange failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	u, err := p.UserInfo(ctx, tok)
	if err != nil {
		s.d.Logger.Error("oauth app userinfo failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	return s.finishOAuthLogin(ctx, tok, u, deviceID)
}

// oauthAppLoginByToken app 渠道「交 token」登录（QQ 移动应用）。
//
// 安全要点（禁令 5）：access_token 与 openid 都来自客户端，可信度为零 ——
// **必须** ValidateToken（内部调 oauth2.0/me 并比对 openid 一致性）后才允许 UserInfo。
// 绝不手工 `tok.Verified = true`：那等于把伪造防护整条拆掉。
func (s *AuthService) oauthAppLoginByToken(ctx context.Context, provider, channel, accessToken, openID, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	accessToken = strings.TrimSpace(accessToken)
	openID = strings.TrimSpace(openID)
	if accessToken == "" || openID == "" {
		return nil, "", "", errcode.ErrInvalidParam
	}
	p, e := s.oauthProvider(provider, channel)
	if e != nil {
		return nil, "", "", e
	}
	tok, err := p.ValidateToken(ctx, &oauthkit.Token{
		AccessToken: accessToken,
		OpenID:      openID,
		Channel:     channel,
		Verified:    false, // 必须由 ValidateToken 置 true，见函数注释
	})
	if err != nil {
		s.d.Logger.Error("oauth app validate token failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	u, err := p.UserInfo(ctx, tok)
	if err != nil {
		s.d.Logger.Error("oauth app userinfo failed",
			zap.String("provider", provider), zap.String("channel", channel),
			zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	return s.finishOAuthLogin(ctx, tok, u, deviceID)
}

// finishOAuthLogin 收尾：账号归属 → 封禁判断 → 双 token。
func (s *AuthService) finishOAuthLogin(ctx context.Context, tok *oauthkit.Token, u *oauthkit.User, deviceID string) (*model.User, string, string, *errcode.Err) {
	user, e := NewOAuthService(s.d).FindOrCreateByOAuth(ctx, tok, u)
	if e != nil {
		return nil, "", "", e
	}
	if user.Status == model.UserStatusBanned {
		return nil, "", "", errcode.ErrAccountBanned
	}
	if strings.TrimSpace(deviceID) == "" {
		// 与旧实现一致：第三方登录没有 device-id 头时用「平台-openid」占位，
		// 保证同一账号在不同平台的会话互不覆盖。
		deviceID = strings.ToLower(u.Provider) + "-" + u.ID
	}
	token, refresh, e := s.issueTokens(ctx, user, deviceID)
	if e != nil {
		return nil, "", "", e
	}
	return user, token, refresh, nil
}

// GithubAuthURL 生成 GitHub 授权链接（state 随机生成并存入 Redis）。
func (s *AuthService) GithubAuthURL(ctx context.Context) (*dto.OAuthURLDto, *errcode.Err) {
	return s.oauthAuthURL(ctx, oauthkit.ProviderGitHub, oauthkit.ChannelWeb)
}

// GithubCallback GitHub 登录（SPA 模式：用 code 换 token 并签发本站双 token）。
//
// 账号归属（按 github_id / 邮箱合并 / 自动建号）已统一收敛到
// OAuthService.FindOrCreateByOAuth，本方法只负责流程串联。
func (s *AuthService) GithubCallback(ctx context.Context, code, state, deviceID string) (*model.User, string, string, *errcode.Err) {
	return s.oauthWebCallback(ctx, oauthkit.ProviderGitHub, oauthkit.ChannelWeb, code, state, deviceID)
}

// GithubRedirectURL 重定向模式：返回应 302 的前端地址（成功带 token，失败带 error）。
func (s *AuthService) GithubRedirectURL(ctx context.Context, code, state, oauthError, deviceID string) string {
	return s.oauthRedirectURL(ctx, oauthkit.ProviderGitHub, oauthkit.ChannelWeb, code, state, oauthError, deviceID)
}

// oauthRedirectURL 重定向模式通用实现（微信 / QQ / GitHub web 共用）。
//
// 成功 URL 来自配置（<provider>_<channel>_success_redirect_url），未配置回落 "/"。
func (s *AuthService) oauthRedirectURL(ctx context.Context, provider, channel, code, state, oauthError, deviceID string) string {
	base := s.oauthSuccessURL(provider, channel)
	// 用户在授权页点了拒绝：上游会带 error 回来，不需要再打接口。
	if oauthError != "" {
		return base + "?error=" + url.QueryEscape(oauthError)
	}
	_, token, _, e := s.oauthWebCallback(ctx, provider, channel, code, state, deviceID)
	if e != nil {
		return base + "?error=" + url.QueryEscape(e.Msg())
	}
	return base + "?token=" + url.QueryEscape(token)
}

// ---- 微信 ----

// WechatWebAuthURL 微信网站应用（PC 扫码）授权链接。
func (s *AuthService) WechatWebAuthURL(ctx context.Context) (*dto.OAuthURLDto, *errcode.Err) {
	return s.oauthAuthURL(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb)
}

// WechatWebCallback 微信网站应用回调（state 前缀 oauth:wechat:web:state:）。
func (s *AuthService) WechatWebCallback(ctx context.Context, code, state, deviceID string) (*model.User, string, string, *errcode.Err) {
	return s.oauthWebCallback(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, code, state, deviceID)
}

// WechatWebRedirectURL 微信网站应用重定向模式。
func (s *AuthService) WechatWebRedirectURL(ctx context.Context, code, state, oauthError, deviceID string) string {
	return s.oauthRedirectURL(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, code, state, oauthError, deviceID)
}

// WechatAppLogin 微信移动应用登录：客户端交 code，后端用 appid+secret 换 token。
//
// 无 state（App 不走浏览器跳转）；不调 ValidateToken（token 是后端自己换的，天然可信）。
func (s *AuthService) WechatAppLogin(ctx context.Context, code, deviceID string) (*model.User, string, string, *errcode.Err) {
	return s.oauthAppLoginByCode(ctx, oauthkit.ProviderWechat, oauthkit.ChannelApp, code, deviceID)
}

// ---- QQ ----

// QQWebAuthURL QQ 网站应用授权链接。
func (s *AuthService) QQWebAuthURL(ctx context.Context) (*dto.OAuthURLDto, *errcode.Err) {
	return s.oauthAuthURL(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb)
}

// QQWebCallback QQ 网站应用回调（state 前缀 oauth:qq:web:state:）。
func (s *AuthService) QQWebCallback(ctx context.Context, code, state, deviceID string) (*model.User, string, string, *errcode.Err) {
	return s.oauthWebCallback(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb, code, state, deviceID)
}

// QQWebRedirectURL QQ 网站应用重定向模式。
func (s *AuthService) QQWebRedirectURL(ctx context.Context, code, state, oauthError, deviceID string) string {
	return s.oauthRedirectURL(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb, code, state, oauthError, deviceID)
}

// QQAppLogin QQ 移动应用登录：客户端上报 access_token + openid。
//
// 两者都可伪造 → 必须 ValidateToken（禁令 5）。客户端上报的 expires_in 一律不采信，
// 因此这里直接手工构造 Token 而不走 ParseQQAppLoginPayload（那份 JSON 里的
// expires_in 对后端登录没有任何用处，少一个被污染的字段）。
func (s *AuthService) QQAppLogin(ctx context.Context, accessToken, openID, deviceID string) (*model.User, string, string, *errcode.Err) {
	return s.oauthAppLoginByToken(ctx, oauthkit.ProviderQQ, oauthkit.ChannelApp, accessToken, openID, deviceID)
}

// QQAppLoginPayload QQ 移动应用登录（原始回调 JSON 形态）。
//
// 客户端直接上报 SDK onComplete 的 JSONObject 时走这里：expires_in 是字符串
// "7776000"、msg 拼成 "sucess"，解析口径统一由 oauthkit.ParseQQAppLoginPayload 负责。
// 解析出的 Verified 恒为 false，仍必须 ValidateToken。
func (s *AuthService) QQAppLoginPayload(ctx context.Context, raw []byte, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	if len(raw) == 0 {
		return nil, "", "", errcode.ErrInvalidParam
	}
	p, e := s.oauthProvider(oauthkit.ProviderQQ, oauthkit.ChannelApp)
	if e != nil {
		return nil, "", "", e
	}
	tok, err := oauthkit.ParseQQAppLoginPayload(raw)
	if err != nil {
		s.d.Logger.Error("oauth qq app parse payload failed", zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	tok, err = p.ValidateToken(ctx, tok)
	if err != nil {
		s.d.Logger.Error("oauth qq app validate token failed", zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	u, err := p.UserInfo(ctx, tok)
	if err != nil {
		s.d.Logger.Error("oauth qq app userinfo failed", zap.String("class", oauthErrClass(err)))
		return nil, "", "", mapOAuthErr(err)
	}
	return s.finishOAuthLogin(ctx, tok, u, deviceID)
}

// randomState 生成 OAuth state（16 字节 hex）。
func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// findUserOrErr 取用户并转统一错误。
func (s *AuthService) findUserOrErr(id string) (*model.User, *errcode.Err) {
	user, err := s.d.Users.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return user, nil
}

// uniqueUsername 生成唯一用户名：
//   - base 为空时用 fallback；
//   - 候选冲突则追加 "-gh" + suffix()，最多尝试 tries 次（避免无限循环）；
//   - tries 次仍冲突时返回最后一个候选（交由 DB 唯一索引兜底，宁可插入失败也不死循环）。
func uniqueUsername(base, fallback string, exists func(string) (bool, error),
	suffix func() string, tries int) (string, error) {
	cand := base
	if cand == "" {
		cand = fallback
	}
	if tries <= 0 {
		tries = 1
	}
	for i := 0; i < tries; i++ {
		dup, err := exists(cand)
		if err != nil {
			return "", err
		}
		if !dup {
			return cand, nil
		}
		cand = base
		if cand == "" {
			cand = fallback
		}
		cand = cand + "-gh" + suffix()
	}
	return cand, nil
}

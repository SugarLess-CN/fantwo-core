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
			return errcode.ErrInternal.With("验证码发送失败，请稍后重试")
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
		return errcode.ErrInternal.With("验证码发送失败，请稍后重试")
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

// GithubAuthURL 生成 GitHub 授权链接（state 随机生成并存入 Redis）。
func (s *AuthService) GithubAuthURL(ctx context.Context) (map[string]interface{}, *errcode.Err) {
	oauth := NewGithubOAuth(s.d)
	if !oauth.Configured() {
		return nil, errcode.ErrGithubOAuthNotConfigured
	}
	state, err := randomState()
	if err != nil {
		return nil, errcode.ErrInternal
	}
	u, err := oauth.AuthorizeURL(ctx, state)
	if err != nil {
		s.d.Logger.Error("github authorize url", zapErr(err))
		return nil, errcode.ErrGithubOAuthNotConfigured
	}
	return map[string]interface{}{"url": u, "state": state}, nil
}

// GithubCallback GitHub 登录（SPA 模式：用 code 换 token 并签发本站双 token）。
// 按 github_id 找用户；找不到时按邮箱合并绑定；仍无则自动建号（随机密码，不可密码登录）。
func (s *AuthService) GithubCallback(ctx context.Context, code, state, deviceID string) (*model.User, string, string, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, "", "", e
	}
	oauth := NewGithubOAuth(s.d)
	if !oauth.Configured() {
		return nil, "", "", errcode.ErrGithubOAuthNotConfigured
	}
	if !oauth.VerifyState(ctx, state) {
		return nil, "", "", errcode.ErrInvalidParam.With("state 校验失败或已过期")
	}
	profile, err := oauth.Exchange(ctx, code)
	if err != nil {
		s.d.Logger.Error("github exchange", zapErr(err))
		return nil, "", "", errcode.ErrInvalidParam.With("GitHub 授权失败：" + err.Error())
	}
	user, e := s.findOrCreateGithubUser(ctx, profile)
	if e != nil {
		return nil, "", "", e
	}
	if user.Status == model.UserStatusBanned {
		return nil, "", "", errcode.ErrAccountBanned
	}
	if deviceID == "" {
		deviceID = "github-" + profile.ID
	}
	token, refresh, e := s.issueTokens(ctx, user, deviceID)
	if e != nil {
		return nil, "", "", e
	}
	return user, token, refresh, nil
}

// GithubRedirectURL 重定向模式：返回应 302 的前端地址（成功带 token，失败带 error）。
func (s *AuthService) GithubRedirectURL(ctx context.Context, code, state, oauthError, deviceID string) string {
	oauth := NewGithubOAuth(s.d)
	base := "/"
	if oauth.Configured() {
		base = oauth.SuccessRedirectURL()
	}
	if oauthError != "" {
		return base + "?error=" + url.QueryEscape(oauthError)
	}
	_, token, _, e := s.GithubCallback(ctx, code, state, deviceID)
	if e != nil {
		return base + "?error=" + url.QueryEscape(e.Msg)
	}
	return base + "?token=" + url.QueryEscape(token)
}

// findOrCreateGithubUser 按 github_id / 邮箱匹配，必要时建号。
func (s *AuthService) findOrCreateGithubUser(ctx context.Context, p *GithubProfile) (*model.User, *errcode.Err) {
	if p.ID == "" {
		return nil, errcode.ErrInvalidParam.With("GitHub 未返回用户 ID")
	}
	if user, err := s.d.Users.FindByGithubID(s.d.DB, p.ID); err == nil {
		// 已有绑定：刷新资料
		fields := map[string]interface{}{"github_username": p.Login}
		if p.AvatarURL != "" {
			fields["avatar"] = p.AvatarURL
		}
		_ = s.d.Users.UpdateFields(s.d.DB, user.ID, fields)
		return s.d.Users.FindByID(s.d.DB, user.ID)
	} else if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	// 邮箱合并：老账号补绑 github_id
	if p.Email != "" {
		if existing, err := s.d.Users.FindByEmail(s.d.DB, p.Email); err == nil {
			_ = s.d.Users.UpdateFields(s.d.DB, existing.ID, map[string]interface{}{
				"github_id": p.ID, "github_username": p.Login,
			})
			return s.d.Users.FindByID(s.d.DB, existing.ID)
		} else if !IsNotFound(err) {
			return nil, errcode.ErrInternal
		}
	}
	// 建号：用户名唯一化（github 登录名 → 冲突则追加 -ghN）
	username := p.Login
	if username == "" {
		username = "gh" + p.ID
	}
	for i := 0; i < 5; i++ {
		dup, err := s.d.Users.ExistsUsername(s.d.DB, username, "")
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if !dup {
			break
		}
		username = fmt.Sprintf("%s-gh%s", p.Login, genCode()[:3])
	}
	pwd, err := hashPassword(genCode() + genCode())
	if err != nil {
		return nil, errcode.ErrInternal
	}
	var nickname *string
	if p.Name != "" {
		nickname = &p.Name
	}
	user := &model.User{
		Username: username, Nickname: nickname, Password: pwd,
		Status: model.UserStatusActive, GithubID: &p.ID, GithubUsername: &p.Login,
	}
	if p.Email != "" {
		user.Email = &p.Email
	}
	if p.AvatarURL != "" {
		user.Avatar = &p.AvatarURL
	}
	if role, rerr := s.d.Roles.FindByName(s.d.DB, model.RoleNameUser); rerr == nil {
		user.Roles = []model.Role{*role}
	}
	if err := s.d.Users.Create(s.d.DB, user); err != nil {
		s.d.Logger.Error("github register", zapErr(err))
		return nil, errcode.ErrInternal
	}
	_ = s.d.Users.CreateProfile(s.d.DB, &model.UserProfile{UserID: user.ID})
	return s.d.Users.FindByID(s.d.DB, user.ID)
}

// randomState 生成 OAuth state（16 字节 hex）。
func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

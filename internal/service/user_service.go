// Package service 用户管理服务（profile / 用户 CRUD / 配置 / 会员 / 收藏）。
//
// 对齐原 user.service.ts：
//   - getProfile    手机/邮箱掩码 + isMember + 认证标识 + 自动签到；
//   - updateUser    资料字段局部更新（仅本人或管理员）；
//   - removeUser    删除用户（管理员权限点 user:delete）；
//   - getUserConfig / updateUserConfig / updateNotificationSettings；
//   - findAllUsers  分页 + keyword；
//   - checkAndUpdateMembershipStatus / batchCheckMembershipStatus；
//   - profile 域「我的收藏」= article_favorites 表（与内容域共用，见 model.ArticleFavorite）。
package service

import (
	"context"
	"strings"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"gorm.io/gorm"
)

// UserService 用户管理业务。
type UserService struct {
	d *Deps
}

// NewUserService 构造。
func NewUserService(d *Deps) *UserService { return &UserService{d: d} }

// UserView 用户对外视图（camelCase，对齐原前端契约）。
// UserView 用户对外视图。
//
// **别名到 dto.UserDto**（2026-11-25 空值规范改造）：
// 结构定义统一放 `internal/dto/`，service/handler 层只引用不重新定义 ——
// 否则 dto 与 service 两处定义会漂移，swagger 与真实响应也对不上。
//
// 字段类型规则见 docs/21-JSON序列化规范.md：
// 字符串值类型（空 ""）、数组非 nil（空 []）、时间与嵌套对象 omitempty（空不传）。
type UserView = dto.UserDto

// RoleBrief 角色简述（别名到 dto.RoleBriefDto，理由同上）。
type RoleBrief = dto.RoleBriefDto

// buildUserView 组装用户视图（masked=true 时按原语义掩码手机/邮箱）。
//
// 空值转换规则见 docs/21-JSON序列化规范.md：
//   - 字符串：*string → string（nil 变 ""），对外不出现 null
//   - 时间：*time.Time → *string（nil 保持 nil，靠 omitempty 整个字段不传）
//   - 数组：Roles 显式初始化，空时序列化成 [] 而不是 null
func buildUserView(u *model.User, masked bool, isMember bool) *UserView {
	v := &UserView{
		ID:          u.ID,
		Username:    u.Username,
		Nickname:    strOrEmpty(u.Nickname),
		Avatar:      strOrEmpty(u.Avatar),
		Description: strOrEmpty(u.Description),
		Background:  strOrEmpty(u.Background),
		Address:     strOrEmpty(u.Address),
		Gender:      u.Gender,
		Status:      u.Status,
		// 时间用 RFC3339；nil 时保持 nil → omitempty 不传该字段
		BirthDate:           timePtrRFC3339(u.BirthDate),
		Email:               strOrEmpty(u.Email),
		Phone:               strOrEmpty(u.Phone),
		ArticleCount:        u.ArticleCount,
		FollowerCount:       u.FollowerCount,
		FollowingCount:      u.FollowingCount,
		Likes:               u.Likes,
		Level:               u.Level,
		Experience:          u.Experience,
		Wallet:              u.Wallet,
		Points:              u.Points,
		MembershipLevel:     u.MembershipLevel,
		MembershipLevelName: u.MembershipLevelName,
		MembershipStatus:    u.MembershipStatus,
		MembershipEndDate:   timePtrRFC3339(u.MembershipEndDate),
		MyInviteCode:        strOrEmpty(u.MyInviteCode),
		InviteEarnings:      u.InviteEarnings,
		InviteCount:         u.InviteCount,
		CreatedAt:           u.CreatedAt.Format(time.RFC3339),
		IsMember:            isMember,
	}
	if masked {
		// 掩码后是确定值；原值为 nil 时 maskPhone("") 会返回 ""，仍是字符串不是 null
		v.Phone = maskPhone(v.Phone)
		v.Email = maskEmail(v.Email)
	}
	// 数组必须显式初始化为空切片，否则 nil 会序列化成 null
	v.Roles = make([]RoleBrief, 0, len(u.Roles))
	for _, r := range u.Roles {
		dn := ""
		if r.DisplayName != nil {
			dn = *r.DisplayName
		}
		v.Roles = append(v.Roles, RoleBrief{ID: r.ID, Name: r.Name, DisplayName: dn})
	}
	return v
}

// timePtrRFC3339 时间指针 → RFC3339 字符串指针；nil 保持 nil。
//
// 为什么对外是 string 而不是 time.Time：
// dto 层的 CreatedAt 也是 string（swagger 里 format:"date-time"），保持一致。
// nil 时返回 nil，配合 `json:",omitempty"` 让字段整个不出现 —— 时间没有"空"的自然表示
// （传 "" 会让前端 new Date("") 得到 Invalid Date，比不传更糟）。
func timePtrRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// maskPhone 138****8000。空串原样返回（避免把 nil 变成一个假号码）。
func maskPhone(p string) string {
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

// maskEmail a****@domain。空串原样返回。
func maskEmail(e string) string {
	if e == "" {
		return ""
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		return e
	}
	name, domain := e[:at], e[at+1:]
	if len(name) <= 1 {
		return name + "****@" + domain
	}
	return name[:1] + "****@" + domain
}

// isMembershipActive 会员是否有效（对齐原 checkMembershipStatus）。
func isMembershipActive(u *model.User) bool {
	if u.MembershipStatus != model.MembershipStatusActive || u.MembershipEndDate == nil {
		return false
	}
	return u.MembershipEndDate.After(time.Now())
}

// Profile 当前用户信息（含认证/会员/自动签到）。
// Profile 当前用户信息（GET /user/profile）。
//
// 返回 dto.UserProfileDto（= UserDto + certification）而不是手写 map：
// 手写 map 无法被 swag 解析（swagger 里 data 只能是空对象），也容易漏字段。
func (s *UserService) Profile(ctx context.Context, userID string) (*dto.UserProfileDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	user, err := s.d.Users.FindByID(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	base := buildUserView(user, true, isMembershipActive(user))
	base.Config = s.configOf(ctx, userID)
	out := &dto.UserProfileDto{UserDto: *base}
	if cert, err := s.d.Users.FindApprovedCert(s.d.DB, userID); err == nil {
		out.Certification = certDto(cert)
	}
	// 自动签到（原 getProfile 副作用）：失败不影响主流程。
	_, _ = s.Sign(ctx, userID, true)
	return out, nil
}

// UpdateProfileInput 资料更新入参（指针语义：nil 表示不改）。
type UpdateProfileInput struct {
	Nickname    *string `json:"nickname"`
	Avatar      *string `json:"avatar"`
	Description *string `json:"description"`
	Background  *string `json:"background"`
	Address     *string `json:"address"`
	Gender      *string `json:"gender"`
	BirthDate   *string `json:"birth_date"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
}

// UpdateProfile 更新当前用户资料（仅本人，管理员改他人走 UpdateUser）。
func (s *UserService) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	// 更新前快照：用于判定「资料是否本次补齐」（只在 不完整 → 完整 跃迁时发一次事件）。
	before, err := s.d.Users.FindPlainByID(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	wasComplete := isProfileComplete(before)
	fields := map[string]interface{}{}
	if in.Nickname != nil && *in.Nickname != "" {
		fields["nickname"] = *in.Nickname
	}
	if in.Avatar != nil {
		fields["avatar"] = *in.Avatar
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.Background != nil {
		fields["background"] = *in.Background
	}
	if in.Address != nil {
		fields["address"] = *in.Address
	}
	if in.Gender != nil && *in.Gender != "" {
		fields["gender"] = *in.Gender
	}
	if in.BirthDate != nil && *in.BirthDate != "" {
		t, err := time.Parse("2006-01-02", *in.BirthDate)
		if err != nil {
			return nil, errcode.ErrInvalidParam
		}
		fields["birth_date"] = t
	}
	if in.Email != nil && *in.Email != "" {
		dup, err := s.d.Users.ExistsEmail(s.d.DB, *in.Email, userID)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if dup {
			return nil, errcode.ErrEmailExists
		}
		fields["email"] = *in.Email
	}
	if in.Phone != nil && *in.Phone != "" {
		dup, err := s.d.Users.ExistsPhone(s.d.DB, *in.Phone, userID)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if dup {
			return nil, errcode.ErrPhoneExists
		}
		fields["phone"] = *in.Phone
	}
	if len(fields) == 0 {
		return nil, errcode.ErrInvalidParam
	}
	if err := s.d.Users.UpdateFields(s.d.DB, userID, fields); err != nil {
		return nil, errcode.ErrInternal
	}
	user, err := s.d.Users.FindByID(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	// 资料由「不完整」变为「完整」→ 发布 user.profileCompleted（成就 PROFILE_COMPLETED）。
	if !wasComplete && isProfileComplete(user) {
		publishProfileCompleted(userID)
	}
	return buildUserView(user, true, isMembershipActive(user)), nil
}

// UpdateContactInput 修改联系方式入参（docs/28 §二 A4）。
//
// 一次只能改一项：验证码是按 (target, scene) 签发的，一个 code 命不中两个 target。
type UpdateContactInput struct {
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	VerificationCode string `json:"verification_code"`
}

// UpdateContact 修改邮箱 / 手机号（PATCH /user/profile/contact）。
//
// 改造前该端点用通用 UpdateProfileInput，注释写着「带验证码校验」，**实现里一个字都没有**
// —— 任何持 token 者可不带验证码把账号邮箱/手机号改成自己的，是账号接管面。
//
// 验证码场景必须与签发端点一致，否则验不过：
//   - 改邮箱 → scene "verification"（POST /user/email/verification 的默认 scene）
//   - 改手机 → scene "bind_phone"（POST /user/profile/phone/code 的 scene）
func (s *UserService) UpdateContact(ctx context.Context, userID string, in UpdateContactInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	// 一次改一项 + 验证码必填。两个都传时无法用一个 code 验两个目标，直接拒。
	if in.VerificationCode == "" || (in.Email == "" && in.Phone == "") ||
		(in.Email != "" && in.Phone != "") {
		return nil, errcode.ErrInvalidParam
	}
	codes := NewVerifyCodeStore(s.d.Redis)
	fields := map[string]interface{}{}
	if in.Email != "" {
		if dup, err := s.d.Users.ExistsEmail(s.d.DB, in.Email, userID); err != nil {
			return nil, errcode.ErrInternal
		} else if dup {
			return nil, errcode.ErrEmailExists
		}
		ok, err := codes.Verify(ctx, in.Email, "verification", in.VerificationCode)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if !ok {
			return nil, errcode.ErrVerifyCodeInvalid
		}
		fields["email"] = in.Email
	}
	if in.Phone != "" {
		if dup, err := s.d.Users.ExistsPhone(s.d.DB, in.Phone, userID); err != nil {
			return nil, errcode.ErrInternal
		} else if dup {
			return nil, errcode.ErrPhoneExists
		}
		ok, err := codes.Verify(ctx, in.Phone, "bind_phone", in.VerificationCode)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if !ok {
			return nil, errcode.ErrVerifyCodeInvalid
		}
		fields["phone"] = in.Phone
	}
	if err := s.d.Users.UpdateFields(s.d.DB, userID, fields); err != nil {
		return nil, errcode.ErrInternal
	}
	user, err := s.d.Users.FindByID(s.d.DB, userID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	view := buildUserView(user, true, isMembershipActive(user))
	view.Config = s.configOf(ctx, userID)
	return view, nil
}

// BindPhoneInput 绑定手机号入参。
type BindPhoneInput struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// BindPhone 绑定手机号（需短信验证码）。
func (s *UserService) BindPhone(ctx context.Context, userID string, in BindPhoneInput) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if in.Phone == "" || in.Code == "" {
		return errcode.ErrInvalidParam
	}
	if dup, err := s.d.Users.ExistsPhone(s.d.DB, in.Phone, userID); err != nil {
		return errcode.ErrInternal
	} else if dup {
		return errcode.ErrPhoneExists
	}
	codes := NewVerifyCodeStore(s.d.Redis)
	ok, err := codes.Verify(ctx, in.Phone, "bind_phone", in.Code)
	if err != nil {
		return errcode.ErrInternal
	}
	if !ok {
		return errcode.ErrVerifyCodeInvalid
	}
	if err := s.d.Users.UpdateFields(s.d.DB, userID, map[string]interface{}{"phone": in.Phone}); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// FindUser 用户详情（admin/他人视角按可见性返回）。
//
// 详情类接口：带上 config（docs/28 §四：列表 N 个用户各带 28 字段配置会让响应体爆炸，
// 所以只有详情填）。
func (s *UserService) FindUser(ctx context.Context, id string) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	user, err := s.d.Users.FindByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	view := buildUserView(user, true, isMembershipActive(user))
	view.Config = s.configOf(ctx, id)
	return view, nil
}

// configOf 详情类响应用的配置块（失败返回 nil —— 配置读不到不该让资料接口整体失败）。
//
// 列表接口**不要调用它**：UserDto 被 GET /user、关注/粉丝列表复用，
// 每行带 28 字段配置会让响应体按 N 倍膨胀（docs/28 §四）。
func (s *UserService) configOf(ctx context.Context, userID string) *dto.UserConfigDto {
	cfg, e := s.GetConfig(ctx, userID)
	if e != nil {
		s.d.Logger.Warn("load user config for view failed user=" + userID)
		return nil
	}
	return cfg
}

// ListUsersInput 用户列表入参。
type ListUsersInput struct {
	Page    int
	Limit   int
	Keyword string
}

// ListUsers 用户分页列表。
func (s *UserService) ListUsers(ctx context.Context, in ListUsersInput) ([]*UserView, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit := normPage(in.Page, in.Limit)
	users, total, err := s.d.Users.List(s.d.DB, page, limit, in.Keyword)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	views := make([]*UserView, 0, len(users))
	for i := range users {
		views = append(views, buildUserView(&users[i], true, isMembershipActive(&users[i])))
	}
	return views, total, nil
}

// CreateUserInput 管理员创建用户入参。
type CreateUserInput struct {
	Username   string   `json:"username"`
	Nickname   string   `json:"nickname"`
	Password   string   `json:"password"`
	Email      string   `json:"email"`
	Phone      string   `json:"phone"`
	RoleIDs    []string `json:"role_ids"`
	Wallet     float64  `json:"wallet"`
	InviteCode string   `json:"invite_code"`
	// VerificationCode 邮箱验证码（可空）：**非空才校验**，与 RegisterInput 同语义。
	VerificationCode string `json:"verification_code"`
}

// CreateUser 管理员创建用户（可指定角色/初始余额）。
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if in.Username == "" || in.Password == "" {
		return nil, errcode.ErrInvalidParam
	}
	if dup, err := s.d.Users.ExistsUsername(s.d.DB, in.Username, ""); err != nil {
		return nil, errcode.ErrInternal
	} else if dup {
		return nil, errcode.ErrUsernameExists
	}
	auth := NewAuthService(s.d)
	user, e := auth.Register(ctx, RegisterInput{
		Username: in.Username, Nickname: in.Nickname, Password: in.Password,
		Email: in.Email, Phone: in.Phone,
		// 邀请码与验证码必须显式转发（T-39a 教训：漏了这一段 = DTO 加了也没用）
		InviteCode:       in.InviteCode,
		VerificationCode: in.VerificationCode,
	})
	if e != nil {
		return nil, e
	}
	if len(in.RoleIDs) > 0 {
		roles, err := s.d.Roles.FindByIDs(s.d.DB, in.RoleIDs)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		for _, r := range roles {
			_ = s.d.Roles.AssignRoleToUser(s.d.DB, user.ID, r.ID)
		}
	}
	if in.Wallet > 0 {
		_ = s.d.Users.UpdateFields(s.d.DB, user.ID, map[string]interface{}{"wallet": in.Wallet})
	}
	user, err := s.d.Users.FindByID(s.d.DB, user.ID)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	return buildUserView(user, false, isMembershipActive(user)), nil
}

// UpdateUserInput 管理员更新用户入参（20 字段，指针语义 nil 不改）。
//
// 与 dto.UpdateUserRequest 逐字段对齐。改造前只有 9 个资料字段 ——
// **封禁 / 会员调整 / 密码重置这些管理端真正要用的能力都写不了**（docs/28 §八）。
type UpdateUserInput struct {
	// 身份 3
	Username  *string `json:"username"`
	Password  *string `json:"password"`
	BirthDate *string `json:"birth_date"`
	// 资料 6
	Nickname    *string `json:"nickname"`
	Avatar      *string `json:"avatar"`
	Description *string `json:"description"`
	Background  *string `json:"background"`
	Address     *string `json:"address"`
	Gender      *string `json:"gender"`
	// 管理 3
	RoleIDs []string `json:"role_ids"`
	Wallet  *float64 `json:"wallet"`
	Status  *string  `json:"status"`
	// 邀请 1
	InviteCode *string `json:"invite_code"`
	// 封禁 2
	Banned    *string `json:"banned"`
	BanReason *string `json:"ban_reason"`
	// 会员 5
	MembershipLevel     *int    `json:"membership_level"`
	MembershipLevelName *string `json:"membership_level_name"`
	MembershipStatus    *string `json:"membership_status"`
	MembershipStartDate *string `json:"membership_start_date"`
	MembershipEndDate   *string `json:"membership_end_date"`
}

// parseFlexTime 宽松时间解析：RFC3339 优先，退化到 date-only。
//
// 管理端入参既有 "2026-11-27T10:00:00Z"（JS 的 ISO 串）也有 "2026-11-27"。
func parseFlexTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", v)
}

// UpdateUser 管理员更新用户（资料 + 角色 + 状态）。
func (s *UserService) UpdateUser(ctx context.Context, id string, in UpdateUserInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	before, err := s.d.Users.FindPlainByID(s.d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	wasComplete := isProfileComplete(before)
	fields := map[string]interface{}{}
	// ---- 身份 3 ----
	if in.Username != nil && *in.Username != "" {
		dup, err := s.d.Users.ExistsUsername(s.d.DB, *in.Username, id)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		if dup {
			return nil, errcode.ErrUsernameExists
		}
		fields["username"] = *in.Username
	}
	if in.Password != nil && *in.Password != "" {
		// 管理员重置密码：绝不能明文落库。与注册走同一个 hashPassword（bcrypt cost 10）。
		hash, err := hashPassword(*in.Password)
		if err != nil {
			return nil, errcode.ErrInternal
		}
		fields["password"] = hash
	}
	if in.BirthDate != nil && *in.BirthDate != "" {
		t, err := time.Parse("2006-01-02", *in.BirthDate)
		if err != nil {
			return nil, errcode.ErrInvalidParam
		}
		fields["birth_date"] = t
	}
	// ---- 资料 6 ----
	if in.Nickname != nil {
		fields["nickname"] = *in.Nickname
	}
	if in.Avatar != nil {
		fields["avatar"] = *in.Avatar
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.Background != nil {
		fields["background"] = *in.Background
	}
	if in.Address != nil {
		fields["address"] = *in.Address
	}
	if in.Gender != nil && *in.Gender != "" {
		fields["gender"] = *in.Gender
	}
	// ---- 管理 3 ----
	if in.Wallet != nil {
		fields["wallet"] = *in.Wallet
	}
	if in.Status != nil && *in.Status != "" {
		fields["status"] = *in.Status
	}
	if in.InviteCode != nil {
		fields["invite_code"] = *in.InviteCode
	}
	// ---- 封禁 / 解封 2 ----
	// banned 传非空 = 封禁（顺带写 status + ban_reason）；传空串 = 解封（banned 置 NULL + status 回 ACTIVE）。
	// ban_reason 允许单独改（不动封禁状态），所以两段分开写。
	var revokeAllAt time.Time
	if in.Banned != nil {
		if *in.Banned == "" {
			fields["banned"] = nil // gorm map 写 nil → SQL NULL
			if in.Status == nil {
				fields["status"] = model.UserStatusActive
			}
		} else {
			t, perr := parseFlexTime(*in.Banned)
			if perr != nil {
				return nil, errcode.ErrInvalidParam
			}
			fields["banned"] = t
			// 撤 token 的时刻必须 >= 现在，否则封禁形同虚设：
			// 中间件判 `token.iat < logoutAllAt` 才拒。管理员若传一个**过去**的时间
			// （如「2020-01-01 就该封了」这种补录语义），现有 token 的 iat 全都晚于它
			// → status 写成了 BANNED 但 token 照样能用。实测复现过。
			revokeAllAt = t
			if now := time.Now(); revokeAllAt.Before(now) {
				revokeAllAt = now
			}
			if in.Status == nil {
				fields["status"] = model.UserStatusBanned
			}
			if in.BanReason != nil {
				fields["ban_reason"] = *in.BanReason
			}
		}
	} else if in.BanReason != nil {
		fields["ban_reason"] = *in.BanReason
	}
	// ---- 会员 5 ----
	if in.MembershipLevel != nil {
		fields["membership_level"] = *in.MembershipLevel
	}
	if in.MembershipLevelName != nil {
		fields["membership_level_name"] = *in.MembershipLevelName
	}
	if in.MembershipStatus != nil && *in.MembershipStatus != "" {
		fields["membership_status"] = *in.MembershipStatus
	}
	for k, v := range map[string]*string{
		"membership_start_date": in.MembershipStartDate,
		"membership_end_date":   in.MembershipEndDate,
	} {
		if v == nil {
			continue
		}
		if *v == "" {
			fields[k] = nil // 传空串 = 清空该时间
			continue
		}
		t, perr := parseFlexTime(*v)
		if perr != nil {
			return nil, errcode.ErrInvalidParam
		}
		fields[k] = t
	}
	// 一个字段都没有 → 空请求体是客户端 bug，静默成功会让管理员以为改过了。
	if len(fields) == 0 && len(in.RoleIDs) == 0 {
		return nil, errcode.ErrInvalidParam
	}
	if len(fields) > 0 {
		if err := s.d.Users.UpdateFields(s.d.DB, id, fields); err != nil {
			return nil, errcode.ErrInternal
		}
	}
	// 封禁落库成功后才撤 token：顺序反过来会出现「token 已撤、封禁没写进去」的窗口。
	if !revokeAllAt.IsZero() {
		if err := authtoken.SetLogoutAll(ctx, s.d.AuthTok, id, revokeAllAt); err != nil {
			s.d.Logger.Warn("ban revoke tokens failed user=" + id)
		}
	}
	if len(in.RoleIDs) > 0 {
		if err := s.d.DB.Exec("DELETE FROM user_roles WHERE user_id = ?", id).Error; err != nil {
			return nil, errcode.ErrInternal
		}
		for _, rid := range in.RoleIDs {
			if err := s.d.Roles.AssignRoleToUser(s.d.DB, id, rid); err != nil {
				return nil, errcode.ErrInternal
			}
		}
		// 角色变更 → 权限版本递增，令旧 token 失效。
		authtokenBump(ctx, s.d, []string{id})
	}
	user, err := s.d.Users.FindByID(s.d.DB, id)
	if err != nil {
		return nil, errcode.ErrInternal
	}
	// 管理员补齐资料同样算「资料补全」，与本人更新保持一致的事件语义。
	if !wasComplete && isProfileComplete(user) {
		publishProfileCompleted(id)
	}
	view := buildUserView(user, true, isMembershipActive(user))
	view.Config = s.configOf(ctx, id) // 详情类返回，与 GET /user/{id} 一致
	return view, nil
}

// RemoveUser 删除用户（软删）。
func (s *UserService) RemoveUser(ctx context.Context, id string) *errcode.Err {
	if e := s.d.ready(); e != nil {
		return e
	}
	if err := s.d.DB.Delete(&model.User{}, "id = ?", id).Error; err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// GetConfig 当前用户配置（无则建默认）。
//
// 返回 dto.UserConfigDto 而不是 *model.UserProfile：
// 直接透传 model 会让数据库模型变成对外契约（remark 是 *string → 响应里出现 null，
// 且将来给表加列会自动泄漏到 API）。转换层是必要的隔离。
func (s *UserService) GetConfig(ctx context.Context, userID string) (*dto.UserConfigDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	p, err := s.d.Users.FindProfile(s.d.DB, userID)
	if err != nil {
		if !IsNotFound(err) {
			return nil, errcode.ErrInternal
		}
		p = &model.UserProfile{UserID: userID}
		if err := s.d.Users.CreateProfile(s.d.DB, p); err != nil {
			return nil, errcode.ErrInternal
		}
	}
	return userConfigDto(p), nil
}

// userConfigDto model.UserProfile → dto.UserConfigDto（空值规则见 docs/21）。
func userConfigDto(p *model.UserProfile) *dto.UserConfigDto {
	return &dto.UserConfigDto{
		// 三个审计字段：声明了就必须填，否则是假契约（前端拿到 "" 会误判为无效记录）。
		// 时间统一 RFC3339，与 toUserDto 同口径。
		ID:                        p.ID,
		CreatedAt:                 p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                 p.UpdatedAt.Format(time.RFC3339),
		UserID:                    p.UserID,
		ArticleCommissionRate:     p.ArticleCommissionRate,
		MembershipCommissionRate:  p.MembershipCommissionRate,
		ProductCommissionRate:     p.ProductCommissionRate,
		ServiceCommissionRate:     p.ServiceCommissionRate,
		EnableCustomCommission:    p.EnableCustomCommission,
		EnableSystemNotification:  p.EnableSystemNotification,
		EnableCommentNotification: p.EnableCommentNotification,
		EnableLikeNotification:    p.EnableLikeNotification,
		EnableFollowNotification:  p.EnableFollowNotification,
		EnableMessageNotification: p.EnableMessageNotification,
		EnableOrderNotification:   p.EnableOrderNotification,
		EnablePaymentNotification: p.EnablePaymentNotification,
		EnableInviteNotification:  p.EnableInviteNotification,
		EnableEmailNotification:   p.EnableEmailNotification,
		EnableSmsNotification:     p.EnableSmsNotification,
		EnablePushNotification:    p.EnablePushNotification,
		HideFavorites:             p.HideFavorites,
		HideComments:              p.HideComments,
		HideCollections:           p.HideCollections,
		HideFollowers:             p.HideFollowers,
		HideFollowings:            p.HideFollowings,
		HideTags:                  p.HideTags,
		WatermarkEnabled:          p.WatermarkEnabled,
		Remark:                    strOrEmpty(p.Remark),
	}
}

// UpdateConfigInput 配置更新入参（指针语义，nil 不改）。
//
// 24 个可写字段 = 响应 dto.UserConfigDto 的 28 字段减去 4 个只读字段
// （id / created_at / updated_at / user_id）。改造前只有 5 个佣金字段 →
// 前端「能读不能写」：设置页拉到 28 项，保存只能提交 5 项（docs/28 §三 B2）。
type UpdateConfigInput struct {
	// 佣金 5
	ArticleCommissionRate    *float64 `json:"article_commission_rate"`
	MembershipCommissionRate *float64 `json:"membership_commission_rate"`
	ProductCommissionRate    *float64 `json:"product_commission_rate"`
	ServiceCommissionRate    *float64 `json:"service_commission_rate"`
	EnableCustomCommission   *bool    `json:"enable_custom_commission"`

	// 通知 11
	EnableSystemNotification  *bool `json:"enable_system_notification"`
	EnableCommentNotification *bool `json:"enable_comment_notification"`
	EnableLikeNotification    *bool `json:"enable_like_notification"`
	EnableFollowNotification  *bool `json:"enable_follow_notification"`
	EnableMessageNotification *bool `json:"enable_message_notification"`
	EnableOrderNotification   *bool `json:"enable_order_notification"`
	EnablePaymentNotification *bool `json:"enable_payment_notification"`
	EnableInviteNotification  *bool `json:"enable_invite_notification"`
	EnableEmailNotification   *bool `json:"enable_email_notification"`
	EnableSmsNotification     *bool `json:"enable_sms_notification"`
	EnablePushNotification    *bool `json:"enable_push_notification"`

	// 隐私 6
	HideFavorites   *bool `json:"hide_favorites"`
	HideComments    *bool `json:"hide_comments"`
	HideCollections *bool `json:"hide_collections"`
	HideFollowers   *bool `json:"hide_followers"`
	HideFollowings  *bool `json:"hide_followings"`
	HideTags        *bool `json:"hide_tags"`

	// 其他 2
	WatermarkEnabled *bool   `json:"watermark_enabled"`
	Remark           *string `json:"remark"`
}

// UpdateConfig 更新用户配置（PATCH /user/config，全 24 字段统一入口）。
//
// 保留「一个字段都没传 → ErrInvalidParam」：空请求体是客户端 bug，
// 静默成功会让前端以为保存了。
func (s *UserService) UpdateConfig(ctx context.Context, userID string, in UpdateConfigInput) *errcode.Err {
	if _, e := s.GetConfig(ctx, userID); e != nil {
		return e
	}
	fields := configFields(in)
	if len(fields) == 0 {
		return errcode.ErrInvalidParam
	}
	if err := s.d.Users.UpdateProfile(s.d.DB, userID, fields); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// configFields 入参 → gorm 列名映射（只映射非 nil）。
//
// 逐个手写而不是反射：反射能少 40 行，但把「字段名 → 列名」的对应关系藏起来，
// 拼错列名时 gorm 会静默忽略该字段（UpdateProfile 走 map，未知键不报错）——
// 设置页保存了但没生效，是最难查的一类 bug。手写至少能 grep 到。
func configFields(in UpdateConfigInput) map[string]interface{} {
	f := map[string]interface{}{}
	setF := func(k string, v *float64) {
		if v != nil {
			f[k] = *v
		}
	}
	setB := func(k string, v *bool) {
		if v != nil {
			f[k] = *v
		}
	}
	// 佣金
	setF("article_commission_rate", in.ArticleCommissionRate)
	setF("membership_commission_rate", in.MembershipCommissionRate)
	setF("product_commission_rate", in.ProductCommissionRate)
	setF("service_commission_rate", in.ServiceCommissionRate)
	setB("enable_custom_commission", in.EnableCustomCommission)
	// 通知
	setB("enable_system_notification", in.EnableSystemNotification)
	setB("enable_comment_notification", in.EnableCommentNotification)
	setB("enable_like_notification", in.EnableLikeNotification)
	setB("enable_follow_notification", in.EnableFollowNotification)
	setB("enable_message_notification", in.EnableMessageNotification)
	setB("enable_order_notification", in.EnableOrderNotification)
	setB("enable_payment_notification", in.EnablePaymentNotification)
	setB("enable_invite_notification", in.EnableInviteNotification)
	setB("enable_email_notification", in.EnableEmailNotification)
	setB("enable_sms_notification", in.EnableSmsNotification)
	setB("enable_push_notification", in.EnablePushNotification)
	// 隐私
	setB("hide_favorites", in.HideFavorites)
	setB("hide_comments", in.HideComments)
	setB("hide_collections", in.HideCollections)
	setB("hide_followers", in.HideFollowers)
	setB("hide_followings", in.HideFollowings)
	setB("hide_tags", in.HideTags)
	// 其他
	setB("watermark_enabled", in.WatermarkEnabled)
	if in.Remark != nil {
		f["remark"] = *in.Remark
	}
	return f
}

// NotificationInput 通知开关入参。
type NotificationInput struct {
	EnableSystemNotification  *bool `json:"enable_system_notification"`
	EnableCommentNotification *bool `json:"enable_comment_notification"`
	EnableLikeNotification    *bool `json:"enable_like_notification"`
	EnableFollowNotification  *bool `json:"enable_follow_notification"`
	EnableMessageNotification *bool `json:"enable_message_notification"`
	EnableOrderNotification   *bool `json:"enable_order_notification"`
	EnablePaymentNotification *bool `json:"enable_payment_notification"`
	EnableInviteNotification  *bool `json:"enable_invite_notification"`
	EnableEmailNotification   *bool `json:"enable_email_notification"`
	EnableSmsNotification     *bool `json:"enable_sms_notification"`
	EnablePushNotification    *bool `json:"enable_push_notification"`
}

// UpdateNotifications 更新通知设置。
func (s *UserService) UpdateNotifications(ctx context.Context, userID string, in NotificationInput) *errcode.Err {
	if _, e := s.GetConfig(ctx, userID); e != nil {
		return e
	}
	pairs := []struct {
		key string
		val *bool
	}{
		{"enable_system_notification", in.EnableSystemNotification},
		{"enable_comment_notification", in.EnableCommentNotification},
		{"enable_like_notification", in.EnableLikeNotification},
		{"enable_follow_notification", in.EnableFollowNotification},
		{"enable_message_notification", in.EnableMessageNotification},
		{"enable_order_notification", in.EnableOrderNotification},
		{"enable_payment_notification", in.EnablePaymentNotification},
		{"enable_invite_notification", in.EnableInviteNotification},
		{"enable_email_notification", in.EnableEmailNotification},
		{"enable_sms_notification", in.EnableSmsNotification},
		{"enable_push_notification", in.EnablePushNotification},
	}
	fields := map[string]interface{}{}
	for _, p := range pairs {
		if p.val != nil {
			fields[p.key] = *p.val
		}
	}
	if len(fields) == 0 {
		return errcode.ErrInvalidParam
	}
	if err := s.d.Users.UpdateProfile(s.d.DB, userID, fields); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// CheckMembership 检查并更新单用户会员状态（到期自动转 INACTIVE）。
func (s *UserService) CheckMembership(ctx context.Context, userID string) (*dto.MembershipCheckDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	user, err := s.d.Users.FindPlainByID(s.d.DB, userID)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	active := isMembershipActive(user)
	if !active && user.MembershipStatus == model.MembershipStatusActive {
		_ = s.d.Users.UpdateFields(s.d.DB, userID, map[string]interface{}{
			"membership_status":     model.MembershipStatusInactive,
			"membership_level":      0,
			"membership_level_name": "普通用户",
		})
	}
	return &dto.MembershipCheckDto{UserID: userID, IsMember: active}, nil
}

// BatchCheckMembership 批量检查会员状态。
func (s *UserService) BatchCheckMembership(ctx context.Context) (*dto.BatchMembershipCheckDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	now := time.Now()
	tx := s.d.DB.Model(&model.User{}).
		Where("membership_status = ? AND membership_end_date IS NOT NULL AND membership_end_date < ?",
			model.MembershipStatusActive, now).
		Updates(map[string]interface{}{
			"membership_status":     model.MembershipStatusInactive,
			"membership_level":      0,
			"membership_level_name": "普通用户",
		})
	if tx.Error != nil {
		return nil, errcode.ErrInternal
	}
	return &dto.BatchMembershipCheckDto{Updated: tx.RowsAffected}, nil
}

// FavoriteItem 收藏项（文章简要）。
type FavoriteItem struct {
	ArticleID   string    `json:"article_id"`
	Title       string    `json:"title"`
	Cover       *string   `json:"cover"`
	Summary     *string   `json:"summary"`
	AuthorID    *string   `json:"author_id"`
	FavoritedAt time.Time `json:"favorited_at"`
}

// ListFavorites 我的收藏（article_favorites 分页）。
func (s *UserService) ListFavorites(ctx context.Context, userID string, page, limit int) ([]FavoriteItem, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	var total int64
	base := s.d.DB.Model(&model.ArticleFavorite{}).Where("user_id = ?", userID)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, errcode.ErrInternal
	}
	var favs []model.ArticleFavorite
	if err := s.d.DB.Where("user_id = ?", userID).
		Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).
		Find(&favs).Error; err != nil {
		return nil, 0, errcode.ErrInternal
	}
	items := make([]FavoriteItem, 0, len(favs))
	if len(favs) == 0 {
		return items, total, nil
	}
	ids := make([]string, 0, len(favs))
	for _, f := range favs {
		ids = append(ids, f.ArticleID)
	}
	var articles []model.Article
	_ = s.d.DB.Where("id IN ?", ids).Find(&articles).Error
	byID := make(map[string]model.Article, len(articles))
	for _, a := range articles {
		byID[a.ID] = a
	}
	for _, f := range favs {
		item := FavoriteItem{ArticleID: f.ArticleID, FavoritedAt: f.CreatedAt}
		if a, ok := byID[f.ArticleID]; ok {
			item.Title = a.Title
			item.Cover = a.Cover
			item.Summary = a.Summary
			item.AuthorID = a.AuthorID
		}
		items = append(items, item)
	}
	return items, total, nil
}

// FavoriteDetail 单条收藏详情（不存在返回 nil）。
func (s *UserService) FavoriteDetail(ctx context.Context, userID, articleID string) (*dto.FavoriteDetailDto, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	var fav model.ArticleFavorite
	err := s.d.DB.Where("user_id = ? AND article_id = ?", userID, articleID).First(&fav).Error
	if IsNotFound(err) {
		return &dto.FavoriteDetailDto{Favorited: false}, nil
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	var article model.Article
	if err := s.d.DB.Where("id = ?", articleID).First(&article).Error; err != nil && !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	return &dto.FavoriteDetailDto{
		Favorited: true, FavoritedAt: fav.CreatedAt.Format(time.RFC3339),
		Article: &dto.FavoriteItemDto{
			ArticleID: articleID, Title: article.Title,
			Cover: strOrEmpty(article.Cover), Summary: strOrEmpty(article.Summary),
			AuthorID: strOrEmpty(article.AuthorID), FavoritedAt: fav.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

// authtokenBump 批量递增权限版本（权限/角色变更后令旧 token 失效）。
func authtokenBump(ctx context.Context, d *Deps, userIDs []string) {
	if d.Redis == nil || len(userIDs) == 0 {
		return
	}
	for _, id := range userIDs {
		authtokenBumpOne(ctx, d, id)
	}
}

// var _ 保证 gorm 与 middleware 引用不被优化掉（跨包类型在别处使用）。
var (
	_ = gorm.ErrRecordNotFound
	_ = middleware.CtxUserKey
)

// ViewUser 对外暴露用户视图构造（handler 层拼装响应用；masked 控制手机/邮箱掩码）。
func ViewUser(u *model.User, masked bool) *UserView {
	return buildUserView(u, masked, isMembershipActive(u))
}

// Deps 暴露共享依赖（handler 层构造其它 service 时复用同一套依赖）。
func (s *UserService) Deps() *Deps { return s.d }

// IsMember 会员是否有效（对外暴露给 handler 拼装响应）。
func IsMember(u *model.User) bool { return isMembershipActive(u) }

// LoginResult 组装登录成功的响应（用户字段**扁平展开**，不嵌套在 user 里）。
//
// 所有登录入口（账密 / 短信 / 微信 web+app / QQ web+app / GitHub）都必须走这里。
// 不要在 handler 里手写 gin.H —— 之前 4 处手写导致「dto 声明一份、运行时另一份」，
// 字段加漏了编译期发现不了（docs/21 §四之三 契约漂移）。
func LoginResult(u *model.User, token, refresh string) *dto.LoginResultDto {
	return &dto.LoginResultDto{UserDto: *ViewUser(u, true), Token: token, RefreshToken: refresh}
}

// ---------- 用户搜索（GET /search/users，公开） ----------

// ListUsersForSearch 用户搜索（公开端点）。
//
// 与 ListUsers 的三点差异（都不改 ListUsers，避免影响管理端）：
//  1. 返回 dto.UserSearchItemDto —— **不含 email/phone/wallet** 等敏感字段
//     （UserDto 面向管理端，直接拿来给公开搜索 = 把全站用户联系方式暴露给任何人）；
//  2. 填 is_followed（搜索卡片要显示「已关注」态，前端原本只能等用户点一次才准）；
//  3. 只在登录时查关注关系；游客（actor 为空）跳过，省一次查询。
func (s *UserService) ListUsersForSearch(ctx context.Context, actor Actor, page, limit int, keyword string) ([]*dto.UserSearchItemDto, int64, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, 0, e
	}
	page, limit = normPage(page, limit)
	users, total, err := s.d.Users.List(s.d.DB, page, limit, keyword)
	if err != nil {
		return nil, 0, errcode.ErrInternal
	}
	out := make([]*dto.UserSearchItemDto, 0, len(users))
	if len(users) == 0 {
		return out, total, nil
	}

	// 关注关系：一次查全量（不按用户逐个查，避免 N+1）。
	following := map[string]struct{}{}
	if !actor.Anonymous() {
		if ids, err := s.followingIDs(ctx, actor.UserID); err == nil {
			for _, id := range ids {
				following[id] = struct{}{}
			}
		}
	}

	for i := range users {
		u := &users[i]
		nick := strOrEmpty(u.Nickname)
		if nick == "" {
			nick = u.Username
		}
		_, isFollowed := following[u.ID]
		out = append(out, &dto.UserSearchItemDto{
			ID:             u.ID,
			Username:       u.Username,
			Nickname:       nick,
			Avatar:         strOrEmpty(u.Avatar),
			Description:    strOrEmpty(u.Description),
			ArticleCount:   u.ArticleCount,
			FollowerCount:  u.FollowerCount,
			FollowingCount: u.FollowingCount,
			Level:          u.Level,
			IsMember:       isMembershipActive(u),
			IsFollowed:     isFollowed,
			CreatedAt:      u.CreatedAt.Format(time.RFC3339),
		})
	}
	return out, total, nil
}

// followingIDs 当前用户关注的人（空 userID 返回空集）。
func (s *UserService) followingIDs(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	return s.d.Users.FollowingIDs(s.d.DB, userID)
}

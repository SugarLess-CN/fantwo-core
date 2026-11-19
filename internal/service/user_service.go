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

	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
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
type UserView struct {
	ID                  string      `json:"id"`
	Username            string      `json:"username"`
	Nickname            *string     `json:"nickname"`
	Avatar              *string     `json:"avatar"`
	Description         *string     `json:"description"`
	Background          *string     `json:"background"`
	Address             *string     `json:"address"`
	Gender              string      `json:"gender"`
	BirthDate           *time.Time  `json:"birthDate"`
	Email               *string     `json:"email"`
	Phone               *string     `json:"phone"`
	Status              string      `json:"status"`
	ArticleCount        int         `json:"articleCount"`
	FollowerCount       int         `json:"followerCount"`
	FollowingCount      int         `json:"followingCount"`
	Likes               int         `json:"likes"`
	Level               int         `json:"level"`
	Experience          int         `json:"experience"`
	Wallet              float64     `json:"wallet"`
	Points              int         `json:"points"`
	MembershipLevel     int         `json:"membershipLevel"`
	MembershipLevelName string      `json:"membershipLevelName"`
	MembershipStatus    string      `json:"membershipStatus"`
	MembershipEndDate   *time.Time  `json:"membershipEndDate"`
	MyInviteCode        *string     `json:"myInviteCode"`
	InviteEarnings      float64     `json:"inviteEarnings"`
	InviteCount         int         `json:"inviteCount"`
	CreatedAt           time.Time   `json:"createdAt"`
	Roles               []RoleBrief `json:"roles,omitempty"`
	IsMember            bool        `json:"isMember"`
	Certification       interface{} `json:"certification,omitempty"`
}

// RoleBrief 角色简述。
type RoleBrief struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// buildUserView 组装用户视图（masked=true 时按原语义掩码手机/邮箱）。
func buildUserView(u *model.User, masked bool, isMember bool) *UserView {
	v := &UserView{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar,
		Description: u.Description, Background: u.Background, Address: u.Address,
		Gender: u.Gender, BirthDate: u.BirthDate, Email: u.Email, Phone: u.Phone,
		Status: u.Status, ArticleCount: u.ArticleCount, FollowerCount: u.FollowerCount,
		FollowingCount: u.FollowingCount, Likes: u.Likes, Level: u.Level,
		Experience: u.Experience, Wallet: u.Wallet, Points: u.Points,
		MembershipLevel: u.MembershipLevel, MembershipLevelName: u.MembershipLevelName,
		MembershipStatus: u.MembershipStatus, MembershipEndDate: u.MembershipEndDate,
		MyInviteCode: u.MyInviteCode, InviteEarnings: u.InviteEarnings,
		InviteCount: u.InviteCount, CreatedAt: u.CreatedAt, IsMember: isMember,
	}
	if masked {
		if u.Phone != nil {
			p := maskPhone(*u.Phone)
			v.Phone = &p
		}
		if u.Email != nil {
			e := maskEmail(*u.Email)
			v.Email = &e
		}
	}
	for _, r := range u.Roles {
		dn := ""
		if r.DisplayName != nil {
			dn = *r.DisplayName
		}
		v.Roles = append(v.Roles, RoleBrief{ID: r.ID, Name: r.Name, DisplayName: dn})
	}
	return v
}

// maskPhone 138****8000。
func maskPhone(p string) string {
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

// maskEmail a****@domain。
func maskEmail(e string) string {
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
func (s *UserService) Profile(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	view := buildUserView(user, true, isMembershipActive(user))
	if cert, err := s.d.Users.FindApprovedCert(s.d.DB, userID); err == nil {
		view.Certification = certView(cert)
	}
	// 自动签到（原 getProfile 副作用）：失败不影响主流程。
	_, _ = s.Sign(ctx, userID, true)
	out := toMap(view)
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
	BirthDate   *string `json:"birthDate"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
}

// UpdateProfile 更新当前用户资料（仅本人，管理员改他人走 UpdateUser）。
func (s *UserService) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
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
	return buildUserView(user, true, isMembershipActive(user)), nil
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
	return buildUserView(user, true, isMembershipActive(user)), nil
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
	RoleIDs    []string `json:"roleIds"`
	Wallet     float64  `json:"wallet"`
	InviteCode string   `json:"inviteCode"`
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

// UpdateUserInput 管理员更新用户入参。
type UpdateUserInput struct {
	Nickname    *string  `json:"nickname"`
	Avatar      *string  `json:"avatar"`
	Description *string  `json:"description"`
	Background  *string  `json:"background"`
	Address     *string  `json:"address"`
	Gender      *string  `json:"gender"`
	RoleIDs     []string `json:"roleIds"`
	Wallet      *float64 `json:"wallet"`
	Status      *string  `json:"status"`
}

// UpdateUser 管理员更新用户（资料 + 角色 + 状态）。
func (s *UserService) UpdateUser(ctx context.Context, id string, in UpdateUserInput) (*UserView, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if _, err := s.d.Users.FindPlainByID(s.d.DB, id); IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	fields := map[string]interface{}{}
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
	if in.Wallet != nil {
		fields["wallet"] = *in.Wallet
	}
	if in.Status != nil && *in.Status != "" {
		fields["status"] = *in.Status
	}
	if len(fields) > 0 {
		if err := s.d.Users.UpdateFields(s.d.DB, id, fields); err != nil {
			return nil, errcode.ErrInternal
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
	return buildUserView(user, true, isMembershipActive(user)), nil
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
func (s *UserService) GetConfig(ctx context.Context, userID string) (*model.UserProfile, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	p, err := s.d.Users.FindProfile(s.d.DB, userID)
	if err == nil {
		return p, nil
	}
	if !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	p = &model.UserProfile{UserID: userID}
	if err := s.d.Users.CreateProfile(s.d.DB, p); err != nil {
		return nil, errcode.ErrInternal
	}
	return p, nil
}

// UpdateConfigInput 配置更新（指针语义，nil 不改）。
type UpdateConfigInput struct {
	ArticleCommissionRate    *float64 `json:"articleCommissionRate"`
	MembershipCommissionRate *float64 `json:"membershipCommissionRate"`
	ProductCommissionRate    *float64 `json:"productCommissionRate"`
	ServiceCommissionRate    *float64 `json:"serviceCommissionRate"`
	EnableCustomCommission   *bool    `json:"enableCustomCommission"`
}

// UpdateConfig 更新抽成配置。
func (s *UserService) UpdateConfig(ctx context.Context, userID string, in UpdateConfigInput) *errcode.Err {
	if _, e := s.GetConfig(ctx, userID); e != nil {
		return e
	}
	fields := commissionFields(in.ArticleCommissionRate, in.MembershipCommissionRate,
		in.ProductCommissionRate, in.ServiceCommissionRate, in.EnableCustomCommission)
	if len(fields) == 0 {
		return errcode.ErrInvalidParam
	}
	if err := s.d.Users.UpdateProfile(s.d.DB, userID, fields); err != nil {
		return errcode.ErrInternal
	}
	return nil
}

// commissionFields 抽成字段映射。
func commissionFields(a, m, p, sv *float64, enable *bool) map[string]interface{} {
	f := map[string]interface{}{}
	if a != nil {
		f["article_commission_rate"] = *a
	}
	if m != nil {
		f["membership_commission_rate"] = *m
	}
	if p != nil {
		f["product_commission_rate"] = *p
	}
	if sv != nil {
		f["service_commission_rate"] = *sv
	}
	if enable != nil {
		f["enable_custom_commission"] = *enable
	}
	return f
}

// NotificationInput 通知开关入参。
type NotificationInput struct {
	EnableSystemNotification  *bool `json:"enableSystemNotification"`
	EnableCommentNotification *bool `json:"enableCommentNotification"`
	EnableLikeNotification    *bool `json:"enableLikeNotification"`
	EnableFollowNotification  *bool `json:"enableFollowNotification"`
	EnableMessageNotification *bool `json:"enableMessageNotification"`
	EnableOrderNotification   *bool `json:"enableOrderNotification"`
	EnablePaymentNotification *bool `json:"enablePaymentNotification"`
	EnableInviteNotification  *bool `json:"enableInviteNotification"`
	EnableEmailNotification   *bool `json:"enableEmailNotification"`
	EnableSmsNotification     *bool `json:"enableSmsNotification"`
	EnablePushNotification    *bool `json:"enablePushNotification"`
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
func (s *UserService) CheckMembership(ctx context.Context, userID string) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"userId": userID, "isMember": active}, nil
}

// BatchCheckMembership 批量检查会员状态。
func (s *UserService) BatchCheckMembership(ctx context.Context) (map[string]interface{}, *errcode.Err) {
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
	return map[string]interface{}{"updated": tx.RowsAffected}, nil
}

// FavoriteItem 收藏项（文章简要）。
type FavoriteItem struct {
	ArticleID   string    `json:"articleId"`
	Title       string    `json:"title"`
	Cover       *string   `json:"cover"`
	Summary     *string   `json:"summary"`
	AuthorID    *string   `json:"authorId"`
	FavoritedAt time.Time `json:"favoritedAt"`
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
func (s *UserService) FavoriteDetail(ctx context.Context, userID, articleID string) (map[string]interface{}, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	var fav model.ArticleFavorite
	err := s.d.DB.Where("user_id = ? AND article_id = ?", userID, articleID).First(&fav).Error
	if IsNotFound(err) {
		return map[string]interface{}{"favorited": false}, nil
	}
	if err != nil {
		return nil, errcode.ErrInternal
	}
	var article model.Article
	if err := s.d.DB.Where("id = ?", articleID).First(&article).Error; err != nil && !IsNotFound(err) {
		return nil, errcode.ErrInternal
	}
	return map[string]interface{}{
		"favorited":   true,
		"favoritedAt": fav.CreatedAt,
		"article": FavoriteItem{
			ArticleID: articleID, Title: article.Title, Cover: article.Cover,
			Summary: article.Summary, AuthorID: article.AuthorID, FavoritedAt: fav.CreatedAt,
		},
	}, nil
}

// toMap UserView → map（便于追加 certification 等可选字段）。
func toMap(v *UserView) map[string]interface{} {
	out := map[string]interface{}{
		"id": v.ID, "username": v.Username, "nickname": v.Nickname,
		"avatar": v.Avatar, "description": v.Description, "background": v.Background,
		"address": v.Address, "gender": v.Gender, "birthDate": v.BirthDate,
		"email": v.Email, "phone": v.Phone, "status": v.Status,
		"articleCount": v.ArticleCount, "followerCount": v.FollowerCount,
		"followingCount": v.FollowingCount, "likes": v.Likes, "level": v.Level,
		"experience": v.Experience, "wallet": v.Wallet, "points": v.Points,
		"membershipLevel": v.MembershipLevel, "membershipLevelName": v.MembershipLevelName,
		"membershipStatus": v.MembershipStatus, "membershipEndDate": v.MembershipEndDate,
		"myInviteCode": v.MyInviteCode, "inviteEarnings": v.InviteEarnings,
		"inviteCount": v.InviteCount, "createdAt": v.CreatedAt,
		"isMember": v.IsMember, "roles": v.Roles,
	}
	if v.Certification != nil {
		out["certification"] = v.Certification
	}
	return out
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

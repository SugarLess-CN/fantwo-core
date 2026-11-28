// Package repository 数据访问层：用户域仓储。
//
// 约定（docs/04 §2）：只做 GORM 读写与分页，不做业务判断；
// 方法第一个参数统一为 *gorm.DB（可为事务 tx），便于 service 编排事务。
package repository

import (
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// UserRepo 用户/设备/签到/关注/注销数据访问。
type UserRepo struct{}

// NewUserRepo 构造。
func NewUserRepo() *UserRepo { return &UserRepo{} }

// userPreload 用户详情统一预加载链（对齐原 relations:
// roles / roles.permissions / config / userDecorations）。
func userPreload(db *gorm.DB) *gorm.DB {
	return db.Preload("Roles").Preload("Roles.Permissions")
}

// Create 创建用户。
func (r *UserRepo) Create(db *gorm.DB, u *model.User) error {
	return db.Create(u).Error
}

// ExistsNickID 该号码是否已被他人占用（nick_id 不唯一，这是**可选**的业务校验）。
//
// 只在配置 nick_id_allow_duplicate=false 时被调用；
// excludeID 用于「改自己时不算自己占着」。
func (r *UserRepo) ExistsNickID(db *gorm.DB, nickID int64, excludeID string) (bool, error) {
	var n int64
	q := db.Model(&model.User{}).Where("nick_id = ?", nickID)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

// NextNickID 从序列取下一个对外展示号码（user.nick_id）。
//
// 必须走 SEQUENCE 而不是 max(nick_id)+1：并发注册时 `max+1` 会读到同一个 max，
// 两个用户拿到同一个号；`nextval` 是原子且不依赖事务隔离级别。
//
// 序列在 migrations.ensureNickIDSequence 里创建（START WITH 10000）。
func (r *UserRepo) NextNickID(db *gorm.DB) (int64, error) {
	var v int64
	err := db.Raw(`SELECT nextval('user_nick_id_seq')`).Scan(&v).Error
	return v, err
}

// Save 全量保存。
func (r *UserRepo) Save(db *gorm.DB, u *model.User) error { return db.Save(u).Error }

// UpdateFields 局部更新用户字段。
func (r *UserRepo) UpdateFields(db *gorm.DB, id string, fields map[string]interface{}) error {
	return db.Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

// FindByID 按主键取用户（带 roles/permissions 预加载）。
func (r *UserRepo) FindByID(db *gorm.DB, id string) (*model.User, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var u model.User
	err := userPreload(db).Where("id = ?", id).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindPlainByID 按主键取用户（不预加载，性能敏感路径用）。
func (r *UserRepo) FindPlainByID(db *gorm.DB, id string) (*model.User, error) {
	var u model.User
	if err := db.Where("id = ?", id).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByAccount 账号查询：含 @ 视为邮箱，否则视为用户名（对齐原 validateUser）。
func (r *UserRepo) FindByAccount(db *gorm.DB, account string) (*model.User, error) {
	q := userPreload(db)
	if isEmailAccount(account) {
		q = q.Where("email = ?", account)
	} else {
		q = q.Where("username = ?", account)
	}
	var u model.User
	if err := q.First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByUsername 按用户名查。
func (r *UserRepo) FindByUsername(db *gorm.DB, username string) (*model.User, error) {
	var u model.User
	if err := userPreload(db).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByEmail 按邮箱查。
func (r *UserRepo) FindByEmail(db *gorm.DB, email string) (*model.User, error) {
	var u model.User
	if err := userPreload(db).Where("email = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByPhone 按手机号查。
func (r *UserRepo) FindByPhone(db *gorm.DB, phone string) (*model.User, error) {
	var u model.User
	if err := userPreload(db).Where("phone = ?", phone).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByGithubID 按 GitHub 用户 ID 查。
func (r *UserRepo) FindByGithubID(db *gorm.DB, githubID string) (*model.User, error) {
	var u model.User
	if err := userPreload(db).Where("github_id = ?", githubID).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// ExistsUsername 用户名是否占用（excludeID 非空时排除自身）。
// CountAll 统计用户总数（软删不计）。
//
// 用途：注册时判断「是不是第一个用户」—— 第一个用户自动授予 super-admin，
// 否则全新部署后没有任何人能进管理后台（必须先手动改库才能管理，体验很差）。
func (r *UserRepo) CountAll(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&model.User{}).Count(&n).Error
	return n, err
}

// ExistsUsername 用户名是否已占用（username 列 varchar(64) + uniqueIndex）。
func (r *UserRepo) ExistsUsername(db *gorm.DB, username, excludeID string) (bool, error) {
	var n int64
	q := db.Model(&model.User{}).Where("username = ?", username)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

// ExistsNickname 昵称是否占用（昵称列在 user 表上是 varchar(64) + uniqueIndex；
// 第三方登录建号时昵称重名率极高，必须与 username 一样做唯一化，否则必然撞索引）。
func (r *UserRepo) ExistsNickname(db *gorm.DB, nickname, excludeID string) (bool, error) {
	var n int64
	q := db.Model(&model.User{}).Where("nickname = ?", nickname)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

// ExistsEmail 邮箱是否占用。
func (r *UserRepo) ExistsEmail(db *gorm.DB, email, excludeID string) (bool, error) {
	var n int64
	q := db.Model(&model.User{}).Where("email = ?", email)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

// ExistsPhone 手机号是否占用。
func (r *UserRepo) ExistsPhone(db *gorm.DB, phone, excludeID string) (bool, error) {
	var n int64
	q := db.Model(&model.User{}).Where("phone = ?", phone)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

// List 用户分页列表（keyword 命中 username/nickname/email）。
func (r *UserRepo) List(db *gorm.DB, page, limit int, keyword string) ([]model.User, int64, error) {
	var (
		list  []model.User
		total int64
	)
	q := db.Model(&model.User{})
	if keyword != "" {
		like := "%" + keyword + "%"
		// nick_id 是 bigint，LIKE 需要先转 text；用户按号码片段搜索是常见操作（如「1」）。
		q = q.Where("username LIKE ? OR nickname LIKE ? OR email LIKE ? OR CAST(nick_id AS TEXT) LIKE ?",
			like, like, like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := userPreload(q.Order("created_at DESC")).
		Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListByIDs 批量按 ID 取（关注列表等）。
func (r *UserRepo) ListByIDs(db *gorm.DB, ids []string, page, limit int, keyword string) ([]model.User, int64, error) {
	var (
		list  []model.User
		total int64
	)
	if len(ids) == 0 {
		return list, 0, nil
	}
	q := db.Model(&model.User{}).Where("id IN ?", ids)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("username LIKE ? OR nickname LIKE ? OR CAST(nick_id AS TEXT) LIKE ?",
			like, like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := userPreload(q.Order("created_at DESC")).
		Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// IncrCounter 原子增减计数器（follower_count/following_count/article_count/likes）。
// delta 可为负；下限钳制在 0（GREATEST）。
func (r *UserRepo) IncrCounter(db *gorm.DB, id, column string, delta int) error {
	return db.Model(&model.User{}).Where("id = ?", id).
		Update(column, gorm.Expr("GREATEST("+column+" + ?, 0)", delta)).Error
}

// IncrWallet 原子增减钱包余额，余额不足则不更新（返回 RowsAffected==0）。
func (r *UserRepo) IncrWallet(db *gorm.DB, id string, delta float64) (int64, error) {
	tx := db.Model(&model.User{}).
		Where("id = ? AND wallet + ? >= 0", id, delta).
		Update("wallet", gorm.Expr("wallet + ?", delta))
	return tx.RowsAffected, tx.Error
}

// ---------- 设备（多端在线 / refresh / 登出黑名单） ----------

// FindDevice 按 (userId, deviceId) 取设备记录。
func (r *UserRepo) FindDevice(db *gorm.DB, userID, deviceID string) (*model.UserDevice, error) {
	var d model.UserDevice
	err := db.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FindDeviceByDeviceID 仅按 deviceId 取（原 refreshToken 语义：deviceId 全局唯一匹配）。
func (r *UserRepo) FindDeviceByDeviceID(db *gorm.DB, deviceID string) (*model.UserDevice, error) {
	var d model.UserDevice
	err := db.Where("device_id = ?", deviceID).Order("last_active_at DESC").First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpsertDevice 登录写入设备记录（存在则更新）。
func (r *UserRepo) UpsertDevice(db *gorm.DB, d *model.UserDevice) error {
	existing, err := r.FindDevice(db, d.UserID, d.DeviceID)
	if err == nil {
		d.ID = existing.ID
		d.CreatedAt = existing.CreatedAt
		return db.Model(&model.UserDevice{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"device_type":          d.DeviceType,
			"device_name":          d.DeviceName,
			"refresh_token":        d.RefreshToken,
			"hashed_refresh_token": d.HashedRefreshToken,
			"login_at":             d.LoginAt,
			"last_active_at":       d.LastActiveAt,
		}).Error
	}
	return db.Create(d).Error
}

// TouchDevice 更新设备活跃时间。
func (r *UserRepo) TouchDevice(db *gorm.DB, id string) error {
	now := time.Now()
	return db.Model(&model.UserDevice{}).Where("id = ?", id).
		Update("last_active_at", now).Error
}

// DeleteDevice 删除单设备（登出）。
func (r *UserRepo) DeleteDevice(db *gorm.DB, userID, deviceID string) error {
	return db.Where("user_id = ? AND device_id = ?", userID, deviceID).
		Delete(&model.UserDevice{}).Error
}

// DeleteAllDevices 删除用户全部设备（全端登出）。
func (r *UserRepo) DeleteAllDevices(db *gorm.DB, userID string) error {
	return db.Where("user_id = ?", userID).Delete(&model.UserDevice{}).Error
}

// ListDevices 用户设备列表。
func (r *UserRepo) ListDevices(db *gorm.DB, userID string) ([]model.UserDevice, error) {
	var list []model.UserDevice
	err := db.Where("user_id = ?", userID).Order("last_active_at DESC").Find(&list).Error
	return list, err
}

// ---------- 签到 ----------

// FindSignInByDate 查某用户某日签到记录。
func (r *UserRepo) FindSignInByDate(db *gorm.DB, userID string, day time.Time) (*model.UserSignIn, error) {
	var s model.UserSignIn
	err := db.Where("user_id = ? AND sign_in_date = ?", userID, day).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSignIn 写签到记录。
func (r *UserRepo) CreateSignIn(db *gorm.DB, s *model.UserSignIn) error {
	return db.Create(s).Error
}

// ListSignIns 签到记录（近 N 天，倒序）。
func (r *UserRepo) ListSignIns(db *gorm.DB, userID string, since time.Time) ([]model.UserSignIn, error) {
	var list []model.UserSignIn
	err := db.Where("user_id = ? AND sign_in_date >= ?", userID, since).
		Order("sign_in_date DESC").Find(&list).Error
	return list, err
}

// CountSignIns 总签到天数。
func (r *UserRepo) CountSignIns(db *gorm.DB, userID string) (int64, error) {
	var n int64
	err := db.Model(&model.UserSignIn{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// LatestSignIn 最近一次签到。
func (r *UserRepo) LatestSignIn(db *gorm.DB, userID string) (*model.UserSignIn, error) {
	var s model.UserSignIn
	err := db.Where("user_id = ?", userID).Order("sign_in_date DESC").First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------- 关注关系 ----------

// Follow 建立关注关系（幂等）。
func (r *UserRepo) Follow(db *gorm.DB, followerID, followingID string) (bool, error) {
	var n int64
	if err := db.Model(&model.UserFollowing{}).
		Where("follower_id = ? AND following_id = ?", followerID, followingID).
		Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	return true, db.Create(&model.UserFollowing{FollowerID: followerID, FollowingID: followingID}).Error
}

// Unfollow 解除关注关系（返回是否有删除）。
func (r *UserRepo) Unfollow(db *gorm.DB, followerID, followingID string) (bool, error) {
	tx := db.Where("follower_id = ? AND following_id = ?", followerID, followingID).
		Delete(&model.UserFollowing{})
	return tx.RowsAffected > 0, tx.Error
}

// IsFollowing 是否已关注。
func (r *UserRepo) IsFollowing(db *gorm.DB, followerID, followingID string) (bool, error) {
	var n int64
	err := db.Model(&model.UserFollowing{}).
		Where("follower_id = ? AND following_id = ?", followerID, followingID).
		Count(&n).Error
	return n > 0, err
}

// FollowerIDs 某用户的粉丝 ID 列表。
func (r *UserRepo) FollowerIDs(db *gorm.DB, userID string) ([]string, error) {
	var ids []string
	err := db.Model(&model.UserFollowing{}).
		Where("following_id = ?", userID).Pluck("follower_id", &ids).Error
	return ids, err
}

// FollowingIDs 某用户关注的 ID 列表。
func (r *UserRepo) FollowingIDs(db *gorm.DB, userID string) ([]string, error) {
	var ids []string
	err := db.Model(&model.UserFollowing{}).
		Where("follower_id = ?", userID).Pluck("following_id", &ids).Error
	return ids, err
}

// CountFollowers 粉丝数。
func (r *UserRepo) CountFollowers(db *gorm.DB, userID string) (int64, error) {
	var n int64
	err := db.Model(&model.UserFollowing{}).Where("following_id = ?", userID).Count(&n).Error
	return n, err
}

// CountFollowing 关注数。
func (r *UserRepo) CountFollowing(db *gorm.DB, userID string) (int64, error) {
	var n int64
	err := db.Model(&model.UserFollowing{}).Where("follower_id = ?", userID).Count(&n).Error
	return n, err
}

// ---------- 拉黑 ----------

// BlockedIDs 我拉黑的用户 ID 列表。
func (r *UserRepo) BlockedIDs(db *gorm.DB, userID string) ([]string, error) {
	var ids []string
	err := db.Model(&model.UserBlock{}).Where("user_id = ?", userID).
		Pluck("blocked_user_id", &ids).Error
	return ids, err
}

// IsBlocked 我是否拉黑了对方。
func (r *UserRepo) IsBlocked(db *gorm.DB, userID, targetID string) (bool, error) {
	var n int64
	err := db.Model(&model.UserBlock{}).
		Where("user_id = ? AND blocked_user_id = ?", userID, targetID).Count(&n).Error
	return n > 0, err
}

// ---------- 用户配置 ----------

// FindProfile 取用户配置（无则 nil）。
func (r *UserRepo) FindProfile(db *gorm.DB, userID string) (*model.UserProfile, error) {
	var p model.UserProfile
	err := db.Where("user_id = ?", userID).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateProfile 建默认配置。
func (r *UserRepo) CreateProfile(db *gorm.DB, p *model.UserProfile) error {
	return db.Create(p).Error
}

// UpdateProfile 局部更新配置。
func (r *UserRepo) UpdateProfile(db *gorm.DB, userID string, fields map[string]interface{}) error {
	return db.Model(&model.UserProfile{}).Where("user_id = ?", userID).Updates(fields).Error
}

// ---------- 账号注销 ----------

// FindDeletion 取注销记录。
func (r *UserRepo) FindDeletion(db *gorm.DB, userID string) (*model.AccountDeletion, error) {
	var d model.AccountDeletion
	err := db.Where("user_id = ?", userID).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpsertDeletion 写入/更新注销记录。
func (r *UserRepo) UpsertDeletion(db *gorm.DB, d *model.AccountDeletion) error {
	existing, err := r.FindDeletion(db, d.UserID)
	if err == nil {
		d.ID = existing.ID
		return db.Model(&model.AccountDeletion{}).Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"status":       d.Status,
				"reason":       d.Reason,
				"scheduled_at": d.ScheduledAt,
				"canceled_at":  d.CanceledAt,
				"executed_at":  d.ExecutedAt,
			}).Error
	}
	return db.Create(d).Error
}

// DueDeletions 到期应执行的注销记录（冷静期结束）。
func (r *UserRepo) DueDeletions(db *gorm.DB, now time.Time) ([]model.AccountDeletion, error) {
	var list []model.AccountDeletion
	err := db.Where("status = ? AND scheduled_at <= ?", model.DeletionStatusPending, now).
		Find(&list).Error
	return list, err
}

// ---------- 认证 ----------

// FindLatestCert 用户最新认证记录。
func (r *UserRepo) FindLatestCert(db *gorm.DB, userID string) (*model.UserCertification, error) {
	var c model.UserCertification
	err := db.Where("user_id = ?", userID).Order("created_at DESC").First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindApprovedCert 用户已通过的认证。
// ListCertsByUser 某用户的全部认证记录（新→旧）。
//
// 与 FindLatestCert 的区别：Latest 只取最近一条给 /status，
// List 给 /user/certification/me（"我的认证记录"是列表语义，MCP 文档如此定义）。
func (r *UserRepo) ListCertsByUser(db *gorm.DB, userID string) ([]model.UserCertification, error) {
	var out []model.UserCertification
	err := db.Where("user_id = ?", userID).
		Order("created_at DESC").Find(&out).Error
	return out, err
}

func (r *UserRepo) FindApprovedCert(db *gorm.DB, userID string) (*model.UserCertification, error) {
	var c model.UserCertification
	err := db.Where("user_id = ? AND status = ?", userID, model.CertStatusApproved).
		Order("reviewed_at DESC").First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// FindCertByID 按 ID 取认证记录。
func (r *UserRepo) FindCertByID(db *gorm.DB, id string) (*model.UserCertification, error) {
	var c model.UserCertification
	if err := db.Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCert 提交认证申请。
func (r *UserRepo) CreateCert(db *gorm.DB, c *model.UserCertification) error {
	return db.Create(c).Error
}

// UpdateCert 更新认证记录。
func (r *UserRepo) UpdateCert(db *gorm.DB, id string, fields map[string]interface{}) error {
	return db.Model(&model.UserCertification{}).Where("id = ?", id).Updates(fields).Error
}

// ListCerts 认证申请分页（status/type 过滤）。
func (r *UserRepo) ListCerts(db *gorm.DB, page, limit int, status, certType string) ([]model.UserCertification, int64, error) {
	var (
		list  []model.UserCertification
		total int64
	)
	q := db.Model(&model.UserCertification{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if certType != "" {
		q = q.Where("type = ?", certType)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

// isEmailAccount 判定账号形态（含 @ 即邮箱），对齐原 validateUser。
func isEmailAccount(account string) bool {
	for _, r := range account {
		if r == '@' {
			return true
		}
	}
	return false
}

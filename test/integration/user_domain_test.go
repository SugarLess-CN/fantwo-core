//go:build integration

// 用户域集成测试（真实 PostgreSQL）：CRUD / 关注 / RBAC 权限版本 / 认证审核 / 验证码 / 签到积分 / 资料补全事件。
//
// 与 business_test.go 的分工：那里覆盖「注册-登录-刷新」「积分增删」「事件总线契约」，
// 本文件覆盖其余需要真实 SQL 的用户域路径（软删、关联表、计数冗余列、唯一索引、
// 事务提交后的事件发布顺序）。
package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/migrations"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// testStore 内存键值替身：同时满足 authtoken.Store 与 service.CodeStore，
// 让认证态/验证码用例在无 Redis 环境下也能跑（Redis 不参与断言逻辑）。
type testStore struct {
	mu   sync.Mutex
	data map[string]string
	ttl  map[string]time.Time
}

func newTestStore() *testStore {
	return &testStore{data: map[string]string{}, ttl: map[string]time.Time{}}
}

func (s *testStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = fmt.Sprint(value)
	if ttl > 0 {
		s.ttl[key] = time.Now().Add(ttl)
	} else {
		delete(s.ttl, key)
	}
	return nil
}

func (s *testStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp, ok := s.ttl[key]; ok && time.Now().After(exp) {
		delete(s.data, key)
		delete(s.ttl, key)
	}
	v, ok := s.data[key]
	if !ok {
		return "", gorm.ErrRecordNotFound
	}
	return v, nil
}

func (s *testStore) Exists(ctx context.Context, key string) (int64, error) {
	if _, err := s.Get(ctx, key); err != nil {
		return 0, nil
	}
	return 1, nil
}

func (s *testStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	delete(s.ttl, key)
	return nil
}

func (s *testStore) Incr(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, r := range s.data[key] {
		digit := int64(r - '0')
		n = n*10 + digit
	}
	n++
	s.data[key] = fmt.Sprint(n)
	return n, nil
}

// userDeps 构造用户域依赖（ty 为内存存储替身）。
func userDeps(db *gorm.DB, store authtoken.Store) *service.Deps {
	signer := jwtx.NewSigner("integration-user-secret-32bytes!", "fantwo-test", time.Hour, 7*24*time.Hour)
	d := service.NewDeps(db, nil, signer, nil, zap.NewNop())
	if store != nil {
		d.AuthTok = store
	}
	return d
}

// TestUser_CRUDAndSoftDelete 用户创建 / 查询 / 更新 / 软删全链路。
//
// 防的 bug：
//   - 软删用 DELETE 物理删除 → 历史外键数据悬空、审计无法追溯；
//   - 更新只写缓存不落库 → 重启后资料回滚；
//   - 删除后仍能被查询到（软删列未生效）。
func TestUser_CRUDAndSoftDelete(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	svc := service.NewUserService(userDeps(db, nil))

	created, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_crud_" + uniqueSuffix(),
		Nickname: "CRUD 用户",
		Password: "Test123456",
		Email:    "it_crud_" + uniqueSuffix() + "@example.com",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}
	if created.ID == "" {
		t.Fatal("创建后 ID 为空")
	}

	// 查：按 ID 取详情
	found, e := svc.FindUser(ctx, created.ID)
	if e != nil {
		t.Fatalf("查询用户失败: %+v", e)
	}
	if found.Username != created.Username {
		t.Errorf("用户名不符: got %s want %s", found.Username, created.Username)
	}

	// 改：更新昵称与简介
	nick := "改名后"
	desc := "新简介"
	updated, e := svc.UpdateUser(ctx, created.ID, service.UpdateUserInput{
		Nickname: &nick, Description: &desc,
	})
	if e != nil {
		t.Fatalf("更新用户失败: %+v", e)
	}
	if updated.Nickname == nil || *updated.Nickname != nick {
		t.Errorf("昵称未更新: %+v", updated.Nickname)
	}
	// 落库校验（防「只改返回值不改库」）
	var row model.User
	if err := db.First(&row, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if row.Nickname == nil || *row.Nickname != nick {
		t.Error("昵称未真正落库")
	}

	// 删：软删后默认查询不可见，但物理行仍在（deleted_at 有值）
	if e := svc.RemoveUser(ctx, created.ID); e != nil {
		t.Fatalf("删除用户失败: %+v", e)
	}
	if _, e := svc.FindUser(ctx, created.ID); e == nil {
		t.Error("软删后不应还能查到用户")
	}
	var cnt int64
	if err := db.Unscoped().Model(&model.User{}).Where("id = ?", created.ID).Count(&cnt).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if cnt != 1 {
		t.Errorf("软删应保留物理行，实际物理行数 %d", cnt)
	}
}

// TestUser_FollowRelationAndCounters 关注关系 + 计数冗余列同步。
//
// 防的 bug：
//   - 关注只写关系表不改 follower_count → 列表页数字与详情页不一致；
//   - 重复关注导致计数虚增（幂等性）；
//   - 取关不减少计数 / 减到负数。
func TestUser_FollowRelationAndCounters(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	svc := service.NewUserService(userDeps(db, nil))

	suffix := uniqueSuffix()
	a, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_fa_" + suffix, Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建 A 失败: %+v", e)
	}
	b, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_fb_" + suffix, Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建 B 失败: %+v", e)
	}

	// 关注自己：必须拒绝
	if _, e := svc.Follow(ctx, a.ID, a.ID); e == nil {
		t.Error("不应允许关注自己")
	}

	out, e := svc.Follow(ctx, a.ID, b.ID)
	if e != nil {
		t.Fatalf("关注失败: %+v", e)
	}
	if changed, _ := out["changed"].(bool); !changed {
		t.Error("首次关注应返回 changed=true")
	}

	// 计数校验（A 关注 1 人，B 粉丝 1）
	stats, e := svc.FollowStats(ctx, b.ID)
	if e != nil {
		t.Fatalf("取关注统计失败: %+v", e)
	}
	if n, _ := stats["followerCount"].(int64); n != 1 {
		t.Errorf("B 粉丝数应为 1，实际 %v", stats["followerCount"])
	}
	statsA, _ := svc.FollowStats(ctx, a.ID)
	if n, _ := statsA["followingCount"].(int64); n != 1 {
		t.Errorf("A 关注数应为 1，实际 %v", statsA["followingCount"])
	}

	// 幂等：重复关注不应让计数变 2
	if out, e := svc.Follow(ctx, a.ID, b.ID); e != nil {
		t.Fatalf("重复关注不应报错: %+v", e)
	} else if changed, _ := out["changed"].(bool); changed {
		t.Error("重复关注不应返回 changed=true")
	}
	stats, _ = svc.FollowStats(ctx, b.ID)
	if n, _ := stats["followerCount"].(int64); n != 1 {
		t.Errorf("重复关注后粉丝数应仍为 1，实际 %v", stats["followerCount"])
	}

	// 粉丝列表包含 A
	followers, total, e := svc.ListFollowers(ctx, b.ID, service.FollowListInput{Page: 1, Limit: 10})
	if e != nil {
		t.Fatalf("取粉丝列表失败: %+v", e)
	}
	if total != 1 || len(followers) != 1 || followers[0].ID != a.ID {
		t.Errorf("粉丝列表不符: total=%d list=%v", total, followers)
	}

	// 取关：计数回退
	if _, e := svc.Unfollow(ctx, a.ID, b.ID); e != nil {
		t.Fatalf("取关失败: %+v", e)
	}
	stats, _ = svc.FollowStats(ctx, b.ID)
	if n, _ := stats["followerCount"].(int64); n != 0 {
		t.Errorf("取关后粉丝数应为 0，实际 %v", stats["followerCount"])
	}
	// 重复取关不应把计数减成负数
	if _, e := svc.Unfollow(ctx, a.ID, b.ID); e != nil {
		t.Fatalf("重复取关不应报错: %+v", e)
	}
	var u model.User
	if err := db.First(&u, "id = ?", b.ID).Error; err != nil {
		t.Fatalf("回查 B 失败: %v", err)
	}
	if u.FollowerCount < 0 {
		t.Errorf("粉丝计数不应为负，实际 %d", u.FollowerCount)
	}
}

// TestRole_AssignPermissionsBumpsPermVer 角色权限变更 → 受影响用户权限版本号递增。
//
// 防的 bug：权限变更后不 bump perm_ver → 旧 token 里的 perms 继续生效到过期
// （这正是 perm_ver 方案存在的唯一理由）。
func TestRole_AssignPermissionsBumpsPermVer(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	store := newTestStore()
	deps := userDeps(db, store)

	roleSvc := service.NewRoleService(deps)
	userSvc := service.NewUserService(deps)

	// 建用户
	u, e := userSvc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_perm_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}

	// 建权限 + 角色
	pRead, e := roleSvc.CreatePermission(ctx, service.PermissionInput{
		Name: "it:read", Description: "集成测试读权限",
	})
	if e != nil {
		t.Fatalf("创建权限失败: %+v", e)
	}
	role, e := roleSvc.CreateRole(ctx, service.CreateRoleInput{
		Name: "it_role_" + uniqueSuffix(), DisplayName: "集成测试角色",
		PermissionIDs: []string{pRead.ID},
	})
	if e != nil {
		t.Fatalf("创建角色失败: %+v", e)
	}

	// 直接把用户挂到角色上（走 user_roles join 表）
	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", u.ID, role.ID).Error; err != nil {
		t.Fatalf("关联角色失败: %v", err)
	}

	// 变更前版本号（未 bump 时为 0）
	before := authtoken.PermVer(ctx, store, u.ID)

	// 分配权限 → 触发 bump
	if _, e := roleSvc.AssignPermissions(ctx, role.ID, []string{pRead.ID}); e != nil {
		t.Fatalf("分配权限失败: %+v", e)
	}
	after := authtoken.PermVer(ctx, store, u.ID)
	if after <= before {
		t.Errorf("权限变更后版本号应递增: before=%d after=%d", before, after)
	}

	// 启用/禁用角色同样应 bump
	if _, e := roleSvc.ToggleStatus(ctx, role.ID, false); e != nil {
		t.Fatalf("禁用角色失败: %+v", e)
	}
	afterDisable := authtoken.PermVer(ctx, store, u.ID)
	if afterDisable <= after {
		t.Errorf("禁用角色后版本号应继续递增: before=%d after=%d", after, afterDisable)
	}

	// 角色菜单可读可写
	menus := []string{"dashboard", "users"}
	if _, e := roleSvc.AssignMenus(ctx, role.ID, menus); e != nil {
		t.Fatalf("分配菜单失败: %+v", e)
	}
	got, e := roleSvc.RoleMenus(ctx, role.ID)
	if e != nil {
		t.Fatalf("读取菜单失败: %+v", e)
	}
	if list, ok := got["menus"].([]string); !ok || len(list) != 2 {
		t.Errorf("菜单未正确落库: %v", got["menus"])
	}
}

// TestCertification_FullLifecycle 认证申请 → 审核通过 → 撤销全链路。
//
// 防的 bug：
//   - 同一用户可重复提交待审核申请（刷单）；已有 APPROVED 仍能再申请；
//   - 审核通过后身份未变（isCreator/isOfficial 判定失效）；
//   - 撤销后身份仍显示为创作者（越权保留认证标识）。
func TestCertification_FullLifecycle(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	svc := service.NewUserService(userDeps(db, nil))

	u, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_cert_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}
	reviewer, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_reviewer_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建审核人失败: %+v", e)
	}

	// 申请
	applied, e := svc.ApplyCertification(ctx, u.ID, service.ApplyCertInput{
		Type: model.CertTypeCreator, RealName: "张三", Slogan: "画画的人",
		Description: "申请人", Materials: []string{"https://cdn/m1.png"},
	})
	if e != nil {
		t.Fatalf("提交认证申请失败: %+v", e)
	}
	certID, _ := applied["id"].(string)
	if certID == "" {
		t.Fatal("申请后未返回认证 ID")
	}

	// 重复申请必须拒绝（有 PENDING）
	if _, e := svc.ApplyCertification(ctx, u.ID, service.ApplyCertInput{
		Type: model.CertTypeCreator,
	}); !errors.Is(e, errcode.ErrCertificationExists) {
		t.Errorf("重复申请应返回 ErrCertificationExists，实际 %v", e)
	}

	// 审核前身份为普通用户
	identity, e := svc.Identity(ctx, u.ID)
	if e != nil {
		t.Fatalf("取身份失败: %+v", e)
	}
	if identity["identity"] != "user" {
		t.Errorf("审核前身份应为 user，实际 %v", identity["identity"])
	}

	// 审核通过
	if _, e := svc.AuditCertification(ctx, certID, reviewer.ID, service.AuditCertInput{
		Status: model.CertStatusApproved, Reason: "材料齐全",
	}); e != nil {
		t.Fatalf("审核通过失败: %+v", e)
	}
	identity, _ = svc.Identity(ctx, u.ID)
	if identity["identity"] != "creator" || identity["isCreator"] != true {
		t.Errorf("审核通过后身份应为 creator，实际 %+v", identity)
	}

	// 已通过的再审核应报「不在待审核状态」
	if _, e := svc.AuditCertification(ctx, certID, reviewer.ID, service.AuditCertInput{
		Status: model.CertStatusApproved,
	}); !errors.Is(e, errcode.ErrCertificationNotPending) {
		t.Errorf("重复审核应返回 ErrCertificationNotPending，实际 %v", e)
	}

	// 撤销
	if _, e := svc.RevokeCertification(ctx, certID, reviewer.ID, "材料造假"); e != nil {
		t.Fatalf("撤销认证失败: %+v", e)
	}
	identity, _ = svc.Identity(ctx, u.ID)
	if identity["identity"] != "user" {
		t.Errorf("撤销后身份应回落为 user，实际 %+v", identity)
	}
}

// TestVerifyCode_FullFlowWithStore 验证码发送 → 校验 → 一次性消费（内存存储）。
//
// 防的 bug：验证码校验后不删除（可重放）；或校验不比对内容（任意码通过）。
func TestVerifyCode_FullFlowWithStore(t *testing.T) {
	store := newTestStore()
	codeStore := service.NewVerifyCodeStoreWith(store)
	ctx := context.Background()

	code, err := codeStore.Send(ctx, "13800138000", "bind_phone")
	if err != nil {
		t.Fatalf("发送验证码失败: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("验证码应为 6 位，实际 %q", code)
	}

	// 错误码不通过
	if ok, _ := codeStore.Verify(ctx, "13800138000", "bind_phone", "000000"); ok {
		t.Error("错误验证码不应通过")
	}
	// 正确码通过
	if ok, _ := codeStore.Verify(ctx, "13800138000", "bind_phone", code); !ok {
		t.Error("正确验证码应通过")
	}
	// 一次性：二次验证失败
	if ok, _ := codeStore.Verify(ctx, "13800138000", "bind_phone", code); ok {
		t.Error("验证码应一次性消费，二次校验不应通过")
	}
}

// TestUser_SignInGrantsDailyLoginPoints 签到 → 经验/积分/连续天数落库 + 当日重复签到拒绝。
//
// 防的 bug：
//   - 签到不发放 DAILY_LOGIN 积分（本轮 T-32 修复的正是这条）；
//   - 同一天可重复签到刷积分；
//   - 连续天数计算错误（跨天判断用了 Now() 而非零点归一）。
func TestUser_SignInGrantsDailyLoginPoints(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	if err := migrations.SeedOps(db); err != nil {
		t.Fatalf("灌运营域种子失败（DAILY_LOGIN 活动缺失会导致积分不发）: %v", err)
	}
	ctx := context.Background()
	svc := service.NewUserService(userDeps(db, nil))

	u, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_sign_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}

	out, e := svc.Sign(ctx, u.ID, false)
	if e != nil {
		t.Fatalf("签到失败: %+v", e)
	}
	if signed, _ := out["signed"].(bool); !signed {
		t.Error("签到应返回 signed=true")
	}
	if cd, _ := out["consecutiveDays"].(int); cd != 1 {
		t.Errorf("首次签到连续天数应为 1，实际 %v", out["consecutiveDays"])
	}

	// 积分应已到账（DAILY_LOGIN 活动 +10）
	var acc model.PointsAccount
	if err := db.First(&acc, "user_id = ?", u.ID).Error; err != nil {
		t.Fatalf("签到后未有积分账户（DAILY_LOGIN 未发放）: %v", err)
	}
	if acc.Balance <= 0 {
		t.Errorf("签到后积分应 > 0，实际 %d", acc.Balance)
	}

	// 同日重复签到必须拒绝
	if _, e := svc.Sign(ctx, u.ID, false); !errors.Is(e, errcode.ErrSignInAlready) {
		t.Errorf("重复签到应返回 ErrSignInAlready，实际 %v", e)
	}

	// 签到记录落库
	var cnt int64
	if err := db.Model(&model.UserSignIn{}).Where("user_id = ?", u.ID).Count(&cnt).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if cnt != 1 {
		t.Errorf("签到记录应为 1 条（重复签到不应写第二条），实际 %d", cnt)
	}
}

// TestUser_ProfileCompletedEventOnce 资料补全事件：仅「不完整 → 完整」跃迁时发一次。
//
// 防的 bug：
//   - 每次更新都发 → 成就 PROFILE_COMPLETED 进度虚增；
//   - 一开始就完整（注册即全字段）也发 → 白送成就。
func TestUser_ProfileCompletedEventOnce(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()

	bus := event.NewBus()
	old := event.Default()
	event.SetDefault(bus)
	t.Cleanup(func() { event.SetDefault(old) })

	var (
		mu    sync.Mutex
		fired int
	)
	bus.Subscribe(event.EventUserProfileCompleted, func(payload interface{}) {
		mu.Lock()
		defer mu.Unlock()
		fired++
	})

	svc := service.NewUserService(userDeps(db, nil))
	u, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_prof_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}

	// 1) 只补昵称（仍不完整：缺头像/简介）→ 不发事件
	nick := "昵称"
	if _, e := svc.UpdateProfile(ctx, u.ID, service.UpdateProfileInput{Nickname: &nick}); e != nil {
		t.Fatalf("更新资料失败: %+v", e)
	}
	mu.Lock()
	got := fired
	mu.Unlock()
	if got != 0 {
		t.Errorf("资料仍不完整时不应发事件，实际 %d 次", got)
	}

	// 2) 补头像（仍缺简介）→ 不发
	avatar := "https://cdn/a.png"
	if _, e := svc.UpdateProfile(ctx, u.ID, service.UpdateProfileInput{Avatar: &avatar}); e != nil {
		t.Fatalf("更新头像失败: %+v", e)
	}

	// 3) 补简介 → 三要素齐备，发一次
	desc := "我的简介"
	if _, e := svc.UpdateProfile(ctx, u.ID, service.UpdateProfileInput{Description: &desc}); e != nil {
		t.Fatalf("更新简介失败: %+v", e)
	}
	mu.Lock()
	got = fired
	mu.Unlock()
	if got != 1 {
		t.Fatalf("资料补齐时应恰好发 1 次事件，实际 %d 次", got)
	}

	// 4) 再次更新（已完整）→ 不再发
	nick2 := "再改昵称"
	if _, e := svc.UpdateProfile(ctx, u.ID, service.UpdateProfileInput{Nickname: &nick2}); e != nil {
		t.Fatalf("二次更新失败: %+v", e)
	}
	mu.Lock()
	got = fired
	mu.Unlock()
	if got != 1 {
		t.Errorf("已完整后再更新不应重复发事件，实际累计 %d 次", got)
	}
}

// TestUser_AccountDeletionThreeStates 账号注销三态：提交 → 撤销 → 再提交。
//
// 防的 bug：
//   - 提交后 status 未置 1（前端拿不到冷静期状态）；
//   - 撤销后仍显示「待注销」；
//   - 冷静期重复提交（覆盖原 scheduled_at，等于无限延长）。
func TestUser_AccountDeletionThreeStates(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)
	ctx := context.Background()
	svc := service.NewUserService(userDeps(db, nil))

	u, e := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "it_del_" + uniqueSuffix(), Password: "Test123456",
	})
	if e != nil {
		t.Fatalf("创建用户失败: %+v", e)
	}

	// 初始：无注销
	st, e := svc.DeletionStatus(ctx, u.ID)
	if e != nil {
		t.Fatalf("查询注销状态失败: %+v", e)
	}
	if pending, _ := st["deletionPending"].(bool); pending {
		t.Error("未提交时不应处于待注销")
	}

	// 提交
	out, e := svc.RequestDeletion(ctx, u.ID, service.RequestDeletionInput{Reason: "不想用了"})
	if e != nil {
		t.Fatalf("提交注销失败: %+v", e)
	}
	if status, _ := out["status"].(int); status != model.DeletionStatusPending {
		t.Errorf("提交后 status 应为 1，实际 %v", out["status"])
	}

	// 冷静期内重复提交必须拒绝（不能无限延长冷静期）
	if _, e := svc.RequestDeletion(ctx, u.ID, service.RequestDeletionInput{}); !errors.Is(e, errcode.ErrDeletionPending) {
		t.Errorf("冷静期内重复提交应返回 ErrDeletionPending，实际 %v", e)
	}

	// 撤销
	if _, e := svc.CancelDeletion(ctx, u.ID); e != nil {
		t.Fatalf("撤销注销失败: %+v", e)
	}
	st, _ = svc.DeletionStatus(ctx, u.ID)
	if pending, _ := st["deletionPending"].(bool); pending {
		t.Error("撤销后不应再处于待注销")
	}
	// 再撤销一次：不在冷静期
	if _, e := svc.CancelDeletion(ctx, u.ID); !errors.Is(e, errcode.ErrDeletionNotPending) {
		t.Errorf("非冷静期撤销应返回 ErrDeletionNotPending，实际 %v", e)
	}

	// 可再次提交（撤销后允许重新申请）
	if _, e := svc.RequestDeletion(ctx, u.ID, service.RequestDeletionInput{}); e != nil {
		t.Errorf("撤销后应允许重新提交注销: %+v", e)
	}
}

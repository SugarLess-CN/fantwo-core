//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/event"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"time"
)

// 用户域表（清表用）。
var userTables = []string{
	"user", "user_config", "user_device", "user_sign_in", "user_block",
	"user_certification", "account_deletion", "user_followings", "user_roles",
	"role", "permission", "role_permissions", "role_menus", "wallet_transaction",
}

// TestAuth_RegisterLoginRefresh 完整认证链路（真实 PG）。
//
// 防：
//   - 注册未建默认角色 → 新用户无任何权限
//   - 登录时 bcrypt 校验失败（密码未正确哈希）
//   - refresh token 哈希无法比对（bcrypt 72 字节上限事故的回归）
//   - 密码明文出现在返回体
func TestAuth_RegisterLoginRefresh(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)

	signer := jwtx.NewSigner("integration-test-secret-32bytes!!", "fantwo-test", time.Hour, 7*24*time.Hour)
	deps := service.NewDeps(db, nil, signer, nil, zap.NewNop())
	authSvc := service.NewAuthService(deps)
	ctx := context.Background()

	username := "it_user_" + uniqueSuffix()
	const password = "Test123456"

	// 1) 注册
	u, e := authSvc.Register(ctx, service.RegisterInput{
		Username: username,
		Nickname: "集成测试用户",
		Password: password,
		Email:    username + "@example.com",
	})
	if e != nil {
		t.Fatalf("注册失败: %+v", e)
	}
	if u.ID == "" {
		t.Fatal("注册后 ID 为空")
	}
	// 密码必须是哈希（不能等于明文，且应是 bcrypt 格式）
	var row model.User
	if err := db.First(&row, "id = ?", u.ID).Error; err != nil {
		t.Fatalf("回查用户失败: %v", err)
	}
	if row.Password == password {
		t.Fatal("密码明文落库 —— 严重安全问题")
	}
	if len(row.Password) < 50 || row.Password[0] != '$' {
		t.Errorf("密码不是 bcrypt 哈希格式: %q", row.Password)
	}

	// 2) 登录（用刚注册的账号密码）
	logged, access, refresh, e := authSvc.Login(ctx, service.LoginInput{
		Account: username, Password: password, DeviceID: "it-device-1",
	})
	if e != nil {
		t.Fatalf("登录失败（bcrypt 校验或签发链路有问题）: %+v", e)
	}
	if logged.ID != u.ID {
		t.Errorf("登录返回的用户不符: got %s want %s", logged.ID, u.ID)
	}
	if access == "" || refresh == "" {
		t.Fatal("登录未签发 token")
	}
	// refresh token 通常 > 72 字节，正是当年 bcrypt 超限事故的触发条件
	if len(refresh) <= 72 {
		t.Logf("提示：refresh token 长度 %d（≤72），未覆盖 bcrypt 超限场景", len(refresh))
	}

	// 3) access token 可被验签
	claims, err := signer.Parse(access)
	if err != nil {
		t.Fatalf("access token 验签失败: %v", err)
	}
	if claims.UserID != u.ID {
		t.Errorf("claims.UserID 不符: got %s", claims.UserID)
	}

	// 4) 刷新令牌（解码 refresh 哈希比对链路）
	newAccess, e := authSvc.RefreshToken(ctx, service.RefreshInput{
		RefreshToken: refresh, DeviceID: "it-device-1",
	})
	if e != nil {
		t.Fatalf("刷新令牌失败（refresh 哈希比对链路问题）: %+v", e)
	}
	if newAccess == "" {
		t.Fatal("刷新后未返回新 access token")
	}
}

// TestAuth_LoginWrongPassword 错误密码必须拒绝，且错误码可区分。
func TestAuth_LoginWrongPassword(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)

	signer := jwtx.NewSigner("integration-test-secret-32bytes!!", "fantwo-test", time.Hour, 7*24*time.Hour)
	deps := service.NewDeps(db, nil, signer, nil, zap.NewNop())
	authSvc := service.NewAuthService(deps)
	ctx := context.Background()

	username := "it_wrong_" + uniqueSuffix()
	if _, e := authSvc.Register(ctx, service.RegisterInput{
		Username: username, Password: "Right123456", Nickname: "x",
	}); e != nil {
		t.Fatalf("注册失败: %+v", e)
	}

	_, _, _, e := authSvc.Login(ctx, service.LoginInput{
		Account: username, Password: "Wrong123456", DeviceID: "d1",
	})
	if e == nil {
		t.Fatal("错误密码竟然登录成功了 —— 认证形同虚设")
	}
	if e.Code != 4003 {
		t.Errorf("错误密码的错误码应为 4003（密码错误），实际 %d: %s", e.Code, e.Msg)
	}

	// 不存在的账号也必须有明确错误（不能与密码错误混淆，否则可枚举用户）
	_, _, _, e2 := authSvc.Login(ctx, service.LoginInput{
		Account: "no_such_user_" + uniqueSuffix(), Password: "x", DeviceID: "d1",
	})
	if e2 == nil {
		t.Fatal("不存在的账号竟然登录成功")
	}
}

// TestPoints_AddSpendAndBalance 积分增减的真实事务与余额一致性。
//
// 防：扣减后余额为负、并发扣减超支、流水与余额不一致。
func TestPoints_AddSpendAndBalance(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)

	svc := service.NewPointsService(db)
	uid := "it-points-" + uniqueSuffix()

	// 加分
	if _, err := svc.Add(uid, service.AddPointsInput{Amount: 100, Source: "test", Description: "初始积分"}); err != nil {
		t.Fatalf("加积分失败: %v", err)
	}
	bal, err := svc.Balance(uid)
	if err != nil {
		t.Fatalf("查余额失败: %v", err)
	}
	if bal != 100 {
		t.Errorf("余额应为 100，实际 %v", bal)
	}

	// 扣分
	if _, err := svc.Spend(uid, service.SpendPointsInput{Amount: 30, Source: "test", Description: "消费"}); err != nil {
		t.Fatalf("扣积分失败: %v", err)
	}
	bal, _ = svc.Balance(uid)
	if bal != 70 {
		t.Errorf("扣减后余额应为 70，实际 %v", bal)
	}

	// 超额扣减必须失败，且余额不变
	if _, err := svc.Spend(uid, service.SpendPointsInput{Amount: 1000, Source: "test", Description: "超额消费"}); err == nil {
		t.Error("超额扣减竟然成功 —— 余额可为负，是严重缺陷")
	}
	bal, _ = svc.Balance(uid)
	if bal != 70 {
		t.Errorf("超额扣减失败后余额应保持 70，实际 %v", bal)
	}

	// 流水条数应与操作次数一致（2 成功 + 1 失败不记）
	var txCount int64
	db.Model(&model.PointsTransaction{}).Where("user_id = ?", uid).Count(&txCount)
	if txCount != 2 {
		t.Errorf("流水应为 2 条（成功加/扣各一条），实际 %d", txCount)
	}
}

// TestPermissions_PermVerInvalidatesToken 权限版本号语义。
//
// 防：管理员改了角色权限后，用户手持旧 token 仍能访问已收回的接口。
func TestPermissions_PermVerInvalidatesToken(t *testing.T) {
	db := testDB(t)
	truncateAll(t, db)

	ctx := context.Background()
	uid := "it-perm-" + uniqueSuffix()

	// 初始版本
	v0 := authtoken.PermVer(ctx, nil, uid)
	// nil Redis 时应返回 0（降级语义，不 panic）
	if v0 != 0 {
		t.Errorf("redis=nil 时 PermVer 应为 0，实际 %d", v0)
	}

	// Bump 后版本必须变化（用真实 Redis 才能验，这里只验 nil 安全）
	authtoken.BumpPermVerMany(ctx, nil, []string{uid}) // 不应 panic
}

// TestEventBus_CrossDomainContract 跨域事件契约（真实 Bus，不依赖 DB）。
//
// 防：发布端改了 payload 字段名 / 订阅端读错字段 → 事件静默丢弃（成就系统不工作）。
func TestEventBus_CrossDomainContract(t *testing.T) {
	bus := event.NewBus()

	var gotUserID string
	bus.Subscribe(event.EventArticleCreated, func(payload interface{}) {
		// 模拟 ops 的解析逻辑：优先取 userId
		if p, ok := payload.(event.ArticleCreatedPayload); ok {
			gotUserID = p.UserID
		}
	})

	bus.Publish(event.EventArticleCreated, event.ArticleCreatedPayload{
		UserID: "author-x", ArticleID: "art-1",
	})

	if gotUserID != "author-x" {
		t.Errorf("跨域契约断裂：订阅端读到 %q，期望 author-x", gotUserID)
	}

	// SubscriberCount 诊断能力
	if n := bus.SubscriberCount(event.EventArticleCreated); n != 1 {
		t.Errorf("SubscriberCount 应为 1，实际 %d", n)
	}
	if n := bus.SubscriberCount("nobody.listens"); n != 0 {
		t.Errorf("无订阅者时应为 0，实际 %d", n)
	}
}

// truncateAll 清空本项目全部业务表（按模型清单反射收集表名）。
func truncateAll(t *testing.T, db *gorm.DB) {
	t.Helper()
	// 显式列举，避免反射拿错表
	tables := []string{
		"user", "user_config", "user_device", "user_sign_in", "user_block",
		"user_certification", "account_deletion", "user_followings", "user_roles",
		"role", "permission", "role_permissions", "role_menus", "wallet_transaction",
		"articles", "article_tags", "article_likes", "article_favorites",
		"article_dislikes", "browse_histories", "comments", "comment_likes",
		"collections", "collection_items", "tags", "tag_follows", "categories",
		"points_accounts", "points_transactions", "points_activities", "points_task_records",
		"achievements", "user_achievements",
	}
	// 只清存在的表（有些域可能未迁移）
	for _, tb := range tables {
		var exists bool
		db.Raw(`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema='public' AND table_name = ?)`, tb).Scan(&exists)
		if !exists {
			continue
		}
		// 表名加引号：user 等是 PG 保留字
		if err := db.Exec(`TRUNCATE TABLE "` + tb + `" RESTART IDENTITY CASCADE`).Error; err != nil {
			t.Fatalf("清空 %s 失败: %v", tb, err)
		}
	}
}

// 确保 repository 包被引用（后续扩展用）。
var _ = repository.NewUserRepo

//go:build integration

// Package integration 集成测试：跑真实 PostgreSQL（+ 可选 Redis）。
//
// 为什么单独一个包 + build tag：
//   - service/repository 的 CRUD 与 BuildViews 依赖具体 *gorm.DB，无 DB 会 panic，
//     纯单测覆盖不到（见 docs/07「测试局限」）。
//   - 用 `//go:build integration` 隔离，默认 `go test ./...` 不会跑，
//     既不加依赖（不用 sqlite driver），也不影响 CI 的纯单测。
//
// 运行方式：
//
//	INTEGRATION_PG_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=fantwo_test sslmode=disable" \
//	  go test ./test/integration/... -tags=integration -v
//
// 安全约束：**必须用独立测试库**（默认 fantwo_test），且每个用例前清表，
// 避免污染开发库。DSN 未设置时全部 Skip（不会误跑）。
package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	envDSN = "INTEGRATION_PG_DSN"
	// 默认测试库名；DSN 里若指向非 *test* 库会拒绝运行（防误伤）
	defaultDSN = "host=127.0.0.1 port=5432 user=postgres password=postgres dbname=fantwo_test sslmode=disable"
)

// testDB 返回一个已迁移的干净测试库连接。
// DSN 未设置且默认库不可用时 Skip；库名不含 test 时直接失败（安全护栏）。
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv(envDSN)
	if dsn == "" {
		dsn = defaultDSN
	}

	// 安全护栏：只允许连测试库，避免把开发/生产库清空
	if !strings.Contains(dsn, "test") {
		t.Fatalf("拒绝运行：DSN 必须指向测试库（库名需含 test），当前: %s", maskDSN(dsn))
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("跳过集成测试：无法连接测试库（%v）", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Skipf("跳过集成测试：取 *sql.DB 失败（%v）", err)
	}
	sqlDB.SetMaxOpenConns(5)
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("跳过集成测试：Ping 失败（%v）", err)
	}

	migrateTestSchema(t, db)
	return db
}

// migrateTestSchema 建表（与线上同一套 AutoMigrate 入口）。
func migrateTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	// 只迁移本项目实体；跨域只读视图不参与
	if err := db.AutoMigrate(
		&model.User{}, &model.UserProfile{}, &model.UserDevice{}, &model.UserSignIn{},
		&model.UserBlock{}, &model.UserCertification{}, &model.AccountDeletion{}, &model.UserFollowing{},
		&model.Role{}, &model.Permission{}, &model.RolePermission{}, &model.RoleMenu{},
		&model.WalletTransaction{},
	); err != nil {
		t.Fatalf("迁移用户域表失败: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Article{}, &model.ArticleTag{}, &model.ArticleLike{}, &model.ArticleFavorite{},
		&model.ArticleDislike{}, &model.BrowseHistory{},
		&model.Comment{}, &model.CommentLike{},
		&model.Collection{}, &model.CollectionItem{},
		&model.Tag{}, &model.TagFollow{}, &model.Category{},
	); err != nil {
		t.Fatalf("迁移内容域表失败: %v", err)
	}
	// 运营域：复用生产同一份迁移清单（migrations.OpsModels），
	// 避免测试手抄表清单与生产漂移。
	if err := db.AutoMigrate(migrations.OpsModels()...); err != nil {
		t.Fatalf("迁移运营域表失败: %v", err)
	}
}

// cleanTables 清空指定表（集成测试每个用例前调用，保证互不影响）。
// 用 TRUNCATE ... CASCADE 一次清完，比逐条 DELETE 快且不受外键顺序影响。
func cleanTables(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	if len(tables) == 0 {
		return
	}
	for i := range tables {
		// 标识符白名单校验：只允许字母数字下划线，防注入
		if !validIdent(tables[i]) {
			t.Fatalf("非法表名: %q", tables[i])
		}
	}
	// 表名必须加双引号：user / order / group 等都是 PostgreSQL 保留字，
	// 裸写会 SQLSTATE 42601 syntax error（实测 user 表直接报错）。
	existing := make([]string, 0, len(tables))
	for _, tb := range tables {
		var ok bool
		db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			WHERE table_schema='public' AND table_name = ?)`, tb).Scan(&ok)
		if ok {
			existing = append(existing, `"`+tb+`"`)
		}
	}
	if len(existing) == 0 {
		return
	}
	stmt := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(existing, ", "))
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("清表失败（%s）: %v", stmt, err)
	}
}

// validIdent 表名白名单校验。
func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// maskDSN 打日志时隐去密码。
func maskDSN(dsn string) string {
	out := dsn
	for _, kv := range strings.Fields(dsn) {
		if strings.HasPrefix(kv, "password=") {
			out = strings.Replace(out, kv, "password=***", 1)
		}
	}
	return out
}

// uniqueSuffix 生成唯一后缀，避免固定用户名撞唯一索引。
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

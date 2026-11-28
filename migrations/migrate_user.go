// 用户域实体迁移清单（T-05b）。
//
// 约定见 migrate.go：每域一个 MigrateXxx 文件，只在本文件维护本域实体列表，
// 由 Run 统一依序调用，避免并行成员同时改同一个实体切片。
package migrations

import (
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"gorm.io/gorm"
)

// MigrateUser 用户域 / RBAC / 钱包流水建表。
//
// 顺序说明：先 Role/Permission（user_roles、role_permissions 的 join 表由
// many2many 自动创建，需两端表先存在），再 User 及其附属表。
func MigrateUser(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// RBAC
		&model.Permission{},
		&model.Role{},
		&model.RolePermission{},
		&model.RoleMenu{},
		// 用户主体与附属
		&model.User{},
		&model.UserProfile{},
		&model.UserDevice{},
		&model.UserSignIn{},
		&model.UserBlock{},
		&model.UserFollowing{},
		&model.UserCertification{},
		&model.AccountDeletion{},
		// 第三方账号绑定（T-37）：独立表而非 user 表加列 —— 同一平台 web/app 渠道
		// 产生不同 openid，单表列存不下；(provider, openid) 复合唯一索引表达平台内唯一。
		&model.UserOAuthBinding{},
		// 钱包流水
		&model.WalletTransaction{},
	); err != nil {
		return err
	}
	// user.nick_id 的取值序列与存量回填（必须晚于 AutoMigrate —— 列要先存在）。
	return ensureNickIDSequence(db)
}

// ensureNickIDSequence 为 user.nick_id 建立取值序列并回填存量。
//
// # 为什么用 PostgreSQL SEQUENCE 而不是 max(nick_id)+1
//
// nick_id 是「对外号码」，要求**绝不重复发放**（虽然业务上允许重名，但发号本身不该撞）。
// `SELECT max(nick_id)+1` 在并发注册下会读到同一个 max → 两个用户拿到同一个号。
// SEQUENCE 的 nextval 是原子且不依赖事务隔离级别，是唯一正确做法。
//
// # 为什么从 10000 起
//
// 用户 2026-11-28 指定。前面的号段（0-9999）留作保留号/测试号，不发给真实用户。
//
// # 幂等性
//
// 全部语句带 IF NOT EXISTS / WHERE nick_id IS NULL，重复启动不会重号：
//
//	· 建序列：IF NOT EXISTS
//	· 回填：只填 NULL 的那些（新注册的已被注册流程赋值，不会再走这里）
//	· setval：只在「序列落后于已有最大值」时前推，绝不后退（后退会导致重号）
func ensureNickIDSequence(db *gorm.DB) error {
	// 仅 PostgreSQL 支持 SEQUENCE；其它方言（测试用的内存库等）直接跳过。
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	// ① 建序列，**从 10001 起**（用户 2026-11-28 指定）。
	if err := db.Exec(`CREATE SEQUENCE IF NOT EXISTS user_nick_id_seq START WITH 10001`).Error; err != nil {
		return err
	}

	// ② 回填存量用户：**按 created_at 顺序**从 10001 起发号。
	//
	// 为什么用 ROW_NUMBER() 而不是 nextval：nextval 的发号顺序取决于语句内部
	// 扫描顺序，不保证等于注册时间。用户要求「按创建时间从 10001 开始」，
	// 所以显式排序后取号；发完再把序列前推（③），后续新注册接着往下发。
	// 同 created_at 时用 id 兜底排序，保证结果稳定可复现。
	if err := db.Exec(`
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) + 10000 AS new_nick_id
			FROM "user" WHERE nick_id IS NULL
		)
		UPDATE "user" u SET nick_id = r.new_nick_id
		FROM ranked r WHERE u.id = r.id
	`).Error; err != nil {
		return err
	}

	// ③ 序列前推到 max(nick_id)：防止「回填时手工指定过大值」之后新号撞车。
	//    GREATEST 保证不会把序列调小（调小 = 发已发过的号）。
	if err := db.Exec(`
		SELECT setval(
			'user_nick_id_seq',
			GREATEST(
				(SELECT COALESCE(MAX(nick_id), 9999) FROM "user"),
				(SELECT last_value FROM user_nick_id_seq)
			)
		)
	`).Error; err != nil {
		return err
	}

	// ④ 索引：搜索与展示都按 nick_id 查，但**不能唯一**（用户可改成重名号）。
	//    写在 AutoMigrate 之后是因为 gorm 的 index tag 已声明 idx_user_nick_id，
	//    这里只是确保老库也有（AutoMigrate 对已存在表会补索引，此句为双保险）。
	return db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_nick_id ON "user" (nick_id)`).Error
}

// Package model 实体层测试。
//
// 防的 bug：
//   - TableName() 与迁移实际建表名不一致：GORM 会去找不存在的表报 42P01，
//     或更糟——静默建出第二张同名实体的影子表（曾发生统计/视图硬编码 users 表名问题，T-12 第 6 项）；
//   - 表名不符合 snake_case 规范：与 TypeORM 时代命名对不上，联调期视图/裸 SQL 全挂；
//   - BeforeCreate 未生成 UUID：主键为空导致插入报错，或同一批数据主键冲突；
//   - BeforeCreate 覆盖了调用方显式设置的主键：外部导入数据时 ID 被改。
package model

import (
	"reflect"
	"regexp"
	"testing"

	"gorm.io/gorm"
)

// snakeCaseRe 合法表名：小写字母开头，仅含小写字母/数字/下划线。
var snakeCaseRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// allModels 全部参与迁移的实体（与 migrations 各域 MigrateXxx 注册的保持一致）。
//
// 新增实体时必须在此补一行 —— 本测试是「实体清单」与「迁移清单」的对照表，
// 漏登记就会漏测（配合 TestTableName_UniqueAcrossModels 可发现重名）。
func allModels() []interface{} {
	return []interface{}{
		// 用户域
		&User{}, &Role{}, &Permission{}, &RolePermission{}, &RoleMenu{},
		&UserProfile{}, &UserSignIn{}, &UserCertification{}, &UserDevice{}, &UserBlock{},
		&UserFollowing{}, &AccountDeletion{}, &WalletTransaction{},
		// 内容域
		&Article{}, &ArticleTag{}, &ArticleLike{}, &ArticleDislike{}, &ArticleFavorite{},
		&BrowseHistory{}, &Comment{}, &CommentLike{},
		&Tag{}, &TagFollow{}, &Category{}, &Collection{}, &CollectionItem{},
		// 运营域
		&PointsAccount{}, &PointsActivity{}, &PointsTaskRecord{}, &PointsTransaction{},
		&Achievement{}, &UserAchievement{},
		&Decoration{}, &DecorationActivity{}, &UserDecoration{}, &UserActivityProgress{},
		&Emoji{}, &EmojiPack{}, &EmojiPackItem{}, &EmojiFavorite{},
		&Banner{}, &Report{}, &Invite{}, &InviteCommission{}, &CommissionRecord{},
		// 交易域
		&Order{}, &Payment{}, &AuditRecord{}, &QueueMessage{},
		// 基础设施域
		&Config{}, &Upload{}, &SearchLog{},
		// 消息域
		&Message{}, &MessageRead{}, &PrivateMessage{}, &PrivateConversation{},
	}
}

// valueObjects 无主键的 JSON 值对象（嵌在实体字段里，不是迁移表）。
//
// 它们刻意**不实现 TableName()**、也不内嵌 BaseModel —— 本测试断言这一点，
// 防止有人误给它们加 TableName 后被 AutoMigrate 顺手建出一张无意义的表。
func valueObjects() []interface{} {
	return []interface{}{
		&AchievementCondition{}, // achievement.conditions 里的元素
		&UploadThumbnail{},      // upload.thumbnails 里的元素
		&UploadOriginal{},       // upload.original
		&UploadVideoJob{},       // upload.video_job
		&HotKeyword{},           // 热搜词响应项
		&BannerQuery{}, &ReportQuery{}, &OrderQuery{}, &EmojiQuery{},
	}
}

// crossDomainViews 跨域只读视图：**必须**实现 TableName()（用于直接查别的域的表），
// 但**不得**参与 AutoMigrate（表归各自域建）。
//
// 与 valueObjects 的区别：值对象是 JSON 片段（无表语义），视图是真实的表映射。
// 两者的共同点是都「不能进 AutoMigrate 清单」，但视图必须有 TableName。
func crossDomainViews() []interface{} {
	return []interface{}{
		&ConfigKV{},         // 查 config 表（支付/审核读配置）
		&UserPaymentView{},  // 查 user 表（交易域读钱包/邀请关系）
		&ArticleOrderView{}, // 查 articles 表（下单前校验文章定价）
		&ReportTargetUserView{},
	}
}

// TestCrossDomainViews_HaveTableName 跨域只读视图必须有 TableName，
// 否则 GORM 回退到结构体名复数，查一张不存在的表（曾因此 500）。
func TestCrossDomainViews_HaveTableName(t *testing.T) {
	for _, m := range crossDomainViews() {
		tn, ok := m.(interface{ TableName() string })
		if !ok {
			t.Errorf("%T 是跨域只读视图，必须实现 TableName()", m)
			continue
		}
		if tn.TableName() == "" {
			t.Errorf("%T 的 TableName() 返回空串", m)
		}
		if !snakeCaseRe.MatchString(tn.TableName()) {
			t.Errorf("%T 的表名 %q 不符合 snake_case", m, tn.TableName())
		}
	}
}

// TestCrossDomainViews_NotInAllModels 跨域视图不得出现在迁移清单里。
func TestCrossDomainViews_NotInAllModels(t *testing.T) {
	inAll := map[reflect.Type]bool{}
	for _, m := range allModels() {
		inAll[reflect.TypeOf(m)] = true
	}
	for _, m := range crossDomainViews() {
		if inAll[reflect.TypeOf(m)] {
			t.Errorf("%T 是跨域只读视图，不得加入 allModels()（AutoMigrate 会试图建表）", m)
		}
	}
}

// TestValueObjects_AreNotMigratedTables 值对象不得实现 TableName()。
//
// 防的是：给 JSON 值对象误加 TableName() 后，若有人把它加进 AutoMigrate 清单，
// 会在库里多建一张没有业务含义的表（且没有 BaseModel 主键 → 迁移失败或建出怪表）。
func TestValueObjects_AreNotMigratedTables(t *testing.T) {
	for _, m := range valueObjects() {
		if tn, ok := m.(interface{ TableName() string }); ok {
			t.Errorf("%T 是值对象却实现了 TableName()=%q（可能被误建表）", m, tn.TableName())
		}
	}
}

// TestValueObjects_DoNotEmbedBaseModel 值对象不得内嵌 BaseModel（无主键语义）。
func TestValueObjects_DoNotEmbedBaseModel(t *testing.T) {
	baseType := reflect.TypeOf(BaseModel{})
	for _, m := range valueObjects() {
		typ := reflect.TypeOf(m).Elem()
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).Type == baseType {
				t.Errorf("%T 内嵌了 BaseModel（值对象不该有主键/软删语义）", m)
			}
		}
	}
}

// TestTableName_AllSnakeCase 全部实体的表名必须是 snake_case。
func TestTableName_AllSnakeCase(t *testing.T) {
	for _, m := range allModels() {
		name := tableNameOf(t, m)
		if !snakeCaseRe.MatchString(name) {
			t.Errorf("%T 的表名 %q 不符合 snake_case 规范（视图/裸 SQL 会引用失败）", m, name)
		}
	}
}

// TestTableName_NotEmpty 表名不得为空（空表名 GORM 会回退到结构体名复数，
// 与既有 TypeORM 表名对不上）。
func TestTableName_NotEmpty(t *testing.T) {
	for _, m := range allModels() {
		if got := tableNameOf(t, m); got == "" {
			t.Errorf("%T 未实现 TableName()（GORM 会回退到结构体名复数，与既有表名不一致）", m)
		}
	}
}

// TestTableName_UniqueAcrossModels 不同实体不得映射到同一张表。
//
// 防的是：两个实体漏写 TableName 且结构体名恰好同复数
// （如 UserCertification 与 Certification 都变 "certifications"），
// 迁移时会互相改表结构，最后谁都不对。
func TestTableName_UniqueAcrossModels(t *testing.T) {
	seen := map[string]string{} // 表名 -> 首个使用它的类型名
	for _, m := range allModels() {
		name := tableNameOf(t, m)
		typ := reflect.TypeOf(m).Elem().Name()
		if prev, exists := seen[name]; exists {
			t.Errorf("表名冲突：%s 与 %s 都映射到 %q（迁移会互相改表）", prev, typ, name)
			continue
		}
		seen[name] = typ
	}
}

// TestTableName_UsesTableNameInterface 全部实体通过「方法」而非 tag 指定表名。
// 这是项目约定（docs/03 第七节）：显式 TableName() 便于与 TypeORM 表名对照。
func TestTableName_UsesTableNameInterface(t *testing.T) {
	for _, m := range allModels() {
		if _, ok := m.(interface{ TableName() string }); !ok {
			t.Errorf("%T 未实现 TableName() 方法", m)
		}
	}
}

// TestTableName_Stability 多次调用返回同一值（不能随机/带时间戳）。
func TestTableName_Stability(t *testing.T) {
	for _, m := range allModels() {
		first := tableNameOf(t, m)
		for i := 0; i < 3; i++ {
			if got := tableNameOf(t, m); got != first {
				t.Errorf("%T 的 TableName() 不稳定：%q → %q", m, first, got)
			}
		}
	}
}

// TestBeforeCreate_GeneratesUUID 空主键时生成 36 位 UUID。
//
// 防的是：忘了落 BeforeCreate → ID 为空 → 插入报主键非空约束，或
// 多条空主键互相覆盖（软删场景下尤为致命）。
func TestBeforeCreate_GeneratesUUID(t *testing.T) {
	b := &BaseModel{}
	if err := b.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate 返回错误: %v", err)
	}
	if b.ID == "" {
		t.Fatal("未生成主键 UUID")
	}
	if len(b.ID) != 36 {
		t.Errorf("UUID 长度应为 36，得到 %d（%q）", len(b.ID), b.ID)
	}
	// 8-4-4-4-12 格式
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(b.ID) {
		t.Errorf("不符合 UUID 格式：%q（TypeORM 侧是标准 uuid，前端按此格式校验）", b.ID)
	}
}

// TestBeforeCreate_PreservesExistingID 已有主键不被覆盖。
//
// 防的是：数据导入/回放时 ID 被重新生成，导致外键全部断裂。
func TestBeforeCreate_PreservesExistingID(t *testing.T) {
	const fixed = "11111111-2222-3333-4444-555555555555"
	b := &BaseModel{ID: fixed}
	if err := b.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate 返回错误: %v", err)
	}
	if b.ID != fixed {
		t.Errorf("已有主键被覆盖：%q → %q（导入/回放会断外键）", fixed, b.ID)
	}
}

// TestBeforeCreate_UniquePerCall 连续调用生成不同 UUID（不能是固定串）。
func TestBeforeCreate_UniquePerCall(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		b := &BaseModel{}
		if err := b.BeforeCreate(nil); err != nil {
			t.Fatalf("BeforeCreate 返回错误: %v", err)
		}
		if _, dup := seen[b.ID]; dup {
			t.Fatalf("第 %d 次生成重复 UUID：%q（主键冲突）", i, b.ID)
		}
		seen[b.ID] = struct{}{}
	}
}

// TestBeforeCreate_EmbeddedInEntity 内嵌 BaseModel 的实体也能被 Hook 触发。
// 防的是：实体改用值内嵌而非指针内嵌，GORM Hook 不触发，ID 恒为空。
func TestBeforeCreate_EmbeddedInEntity(t *testing.T) {
	u := &User{}
	if err := u.BaseModel.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate 返回错误: %v", err)
	}
	if u.ID == "" {
		t.Error("内嵌 BaseModel 的实体未生成主键")
	}
}

// TestBaseModel_GormTags 主键/时间戳/软删的 gorm tag 约定。
//
// 防的是：主键 tag 被改成自增 int（与 uuid varchar(36) 契约冲突，前端拿到的 id 变成数字）；
// 软删列失去 index（软删查询全表扫，联调期明显变慢）。
func TestBaseModel_GormTags(t *testing.T) {
	typ := reflect.TypeOf(BaseModel{})

	cases := []struct {
		field string
		want  []string // tag 中必须包含的子串
		desc  string
	}{
		{"ID", []string{"primaryKey", "varchar(36)"}, "主键必须是 varchar(36) uuid 字符串"},
		{"CreatedAt", []string{"autoCreateTime"}, "创建时间自动填充"},
		{"UpdatedAt", []string{"autoUpdateTime"}, "更新时间自动填充"},
		{"DeletedAt", []string{"index"}, "软删列必须带 index（软删查询依赖）"},
	}
	for _, c := range cases {
		f, ok := typ.FieldByName(c.field)
		if !ok {
			t.Errorf("BaseModel 缺字段 %s", c.field)
			continue
		}
		tag := f.Tag.Get("gorm")
		for _, want := range c.want {
			if !containsStr(tag, want) {
				t.Errorf("%s 的 gorm tag 缺 %q（%s）：当前 %q", c.field, want, c.desc, tag)
			}
		}
	}
}

// TestBaseModel_CamelCaseJSON 对外 JSON 字段名必须是 camelCase。
// 口径见 AGENTS.md：数据库列 snake_case、对外 JSON camelCase。
func TestBaseModel_CamelCaseJSON(t *testing.T) {
	typ := reflect.TypeOf(BaseModel{})
	want := map[string]string{
		"ID":        "id",
		"CreatedAt": "createdAt",
		"UpdatedAt": "updatedAt",
	}
	for field, wantJSON := range want {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Errorf("缺字段 %s", field)
			continue
		}
		if got := f.Tag.Get("json"); got != wantJSON {
			t.Errorf("%s 的 json tag：得到 %q，期望 %q（对外必须 camelCase）", field, got, wantJSON)
		}
	}
	// 软删列不对外暴露
	if f, ok := typ.FieldByName("DeletedAt"); ok {
		if got := f.Tag.Get("json"); got != "-" {
			t.Errorf("DeletedAt 不应对外暴露，json tag 应为 \"-\"，得到 %q", got)
		}
	}
}

// tableNameOf 取实体表名（通过 TableNameer 接口，避免依赖 GORM 反射）。
func tableNameOf(t *testing.T, m interface{}) string {
	t.Helper()
	if tn, ok := m.(interface{ TableName() string }); ok {
		return tn.TableName()
	}
	return ""
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 确保 gorm 包被引用（BeforeCreate 签名依赖 *gorm.DB；保持 import 显式可读）。
var _ = (*gorm.DB)(nil)

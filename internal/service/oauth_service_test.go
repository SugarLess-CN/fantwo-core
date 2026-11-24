// OAuthService service 层单测（T-37d）。
//
// 覆盖面：
//   - FindOrCreateByOAuth 查找顺序（禁令 4 回归防线：unionid 优先、空串不查询、
//     openid 不带 channel、GitHub email 合并、未命中建号）
//   - mapOAuthErr 全量映射表（SDK 哨兵 → errcode 业务码）
//   - truncateRunes 边界（中文 3 字节 / emoji 4 字节 / 恰好 / 超长 / 空 / max<=0）
//   - uniqueOAuthNickname（重名加后缀 / 空昵称回退 / 超长截断 / DB 抖动兜底）
//   - oauthErrClass 全量分类（哨兵 → 日志标签）
//   - state 一次性消费与渠道隔离（wechat:web 与 qq:web 互不串用）
//
// 测试基础设施说明：go.mod 无 sqlite / sqlmock（归 lead 独占管理，禁止 go get），
// 因此 OAuthService 的 DB 依赖抽成 oauthDataAccess 接口（生产实现 oauthGormAccess
// 行为不变），测试注入内存桩 fakeOAuthAccess。state 用 provider_test.go 的 memKV。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/oauthkit"
	"gorm.io/gorm"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// 内存桩：oauthDataAccess 的内存实现（无 DB）
// ---------------------------------------------------------------------------

// fakeOAuthAccess 内存数据访问桩：模拟绑定表 + 用户表的最小语义。
//
// createOAuthUserTx() 返回 true 是 createOAuthUser 判定「桩路径」的标记
// （生产 gorm 实现不带此方法，走单事务分支）。
type fakeOAuthAccess struct {
	bindings []*model.UserOAuthBinding // 模拟 user_oauth_binding 表
	users    []*model.User             // 模拟 user 表（未软删）
	// softDeletedEmails 软删账号仍占用的 email（模拟唯一索引含已删行）
	softDeletedEmails map[string]bool
	// existingUsernames / existingNicknames 既有占用（模拟唯一索引）
	existingUsernames map[string]bool
	existingNicknames map[string]bool
	// failNext 指定下一次调用应报错的接口名（故障注入），消费即清空
	failNext string
	// calls 每个接口的调用次数（验证「unionid 命中后不再查 openid」这类顺序断言）
	calls map[string]int
	seq   int // 自增 ID 序号
}

func newFakeOAuthAccess() *fakeOAuthAccess {
	return &fakeOAuthAccess{
		softDeletedEmails: map[string]bool{},
		existingUsernames: map[string]bool{},
		existingNicknames: map[string]bool{},
		calls:             map[string]int{},
	}
}

func (f *fakeOAuthAccess) tick(op string) error {
	f.calls[op]++
	if f.failNext == op {
		f.failNext = ""
		return fmt.Errorf("injected failure: %s", op)
	}
	return nil
}

func (f *fakeOAuthAccess) nextID() string {
	f.seq++
	return fmt.Sprintf("fake-%06d", f.seq)
}

func (f *fakeOAuthAccess) FindBindingByUnionID(provider, unionID string) (*model.UserOAuthBinding, error) {
	if err := f.tick("FindBindingByUnionID"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(unionID) == "" {
		return nil, nil // 禁令 4 双保险：空串不查询
	}
	for _, b := range f.bindings {
		if b.Provider == provider && b.UnionID != nil && *b.UnionID == unionID {
			return b, nil
		}
	}
	return nil, nil
}

func (f *fakeOAuthAccess) FindBindingByOpenID(provider, openID string) (*model.UserOAuthBinding, error) {
	if err := f.tick("FindBindingByOpenID"); err != nil {
		return nil, err
	}
	for _, b := range f.bindings {
		if b.Provider == provider && b.OpenID == openID {
			return b, nil
		}
	}
	return nil, nil
}

func (f *fakeOAuthAccess) CreateBinding(b *model.UserOAuthBinding) error {
	if err := f.tick("CreateBinding"); err != nil {
		return err
	}
	if b.ID == "" {
		b.ID = f.nextID()
	}
	f.bindings = append(f.bindings, b)
	return nil
}

func (f *fakeOAuthAccess) RefreshBindingSnapshot(b *model.UserOAuthBinding, fields map[string]interface{}) error {
	if err := f.tick("RefreshBindingSnapshot"); err != nil {
		return err // 失败不影响登录（调用方只 warn），但桩内仍返回错误供验证
	}
	if v, ok := fields["channel"].(string); ok {
		b.Channel = v
	}
	if v, ok := fields["nickname"].(string); ok {
		b.Nickname = v
	}
	if v, ok := fields["avatar_url"].(string); ok {
		b.AvatarURL = v
	}
	if v, ok := fields["unionid"].(string); ok {
		u := v
		b.UnionID = &u
	}
	return nil
}

func (f *fakeOAuthAccess) EmailTakenIncludingDeleted(email string) (bool, error) {
	if err := f.tick("EmailTakenIncludingDeleted"); err != nil {
		return false, err
	}
	if f.softDeletedEmails[email] {
		return true, nil
	}
	for _, u := range f.users {
		if u.Email != nil && *u.Email == email {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeOAuthAccess) FindUserByID(id string) (*model.User, error) {
	if err := f.tick("FindUserByID"); err != nil {
		return nil, err
	}
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthAccess) ExistsUsername(username, _ string) (bool, error) {
	if err := f.tick("ExistsUsername"); err != nil {
		return false, err
	}
	if f.existingUsernames[username] {
		return true, nil
	}
	for _, u := range f.users {
		if u.Username == username {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeOAuthAccess) ExistsNickname(nickname, _ string) (bool, error) {
	if err := f.tick("ExistsNickname"); err != nil {
		return false, err
	}
	if f.existingNicknames[nickname] {
		return true, nil
	}
	for _, u := range f.users {
		if u.Nickname != nil && *u.Nickname == nickname {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeOAuthAccess) FindUserByEmail(email string) (*model.User, error) {
	if err := f.tick("FindUserByEmail"); err != nil {
		return nil, err
	}
	for _, u := range f.users {
		if u.Email != nil && *u.Email == email {
			return u, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthAccess) UpdateUserFields(id string, fields map[string]interface{}) error {
	if err := f.tick("UpdateUserFields"); err != nil {
		return err
	}
	for _, u := range f.users {
		if u.ID == id {
			if v, ok := fields["github_id"].(string); ok {
				u.GithubID = &v
			}
			if v, ok := fields["github_username"].(string); ok {
				u.GithubUsername = &v
			}
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeOAuthAccess) FindRoleByName(name string) (*model.Role, error) {
	if err := f.tick("FindRoleByName"); err != nil {
		return nil, err
	}
	return &model.Role{Name: name}, nil
}

func (f *fakeOAuthAccess) CreateUser(u *model.User) error {
	if err := f.tick("CreateUser"); err != nil {
		return err
	}
	if u.ID == "" {
		u.ID = f.nextID()
	}
	f.existingUsernames[u.Username] = true
	if u.Nickname != nil {
		f.existingNicknames[*u.Nickname] = true
	}
	f.users = append(f.users, u)
	return nil
}

func (f *fakeOAuthAccess) CreateProfile(string) error { return f.tick("CreateProfile") }

// createOAuthUserTx 桩路径标记：让 createOAuthUser 走非事务逐条落库分支。
func (f *fakeOAuthAccess) createOAuthUserTx() bool { return true }

// newFakeOAuthService 组装带内存桩的 OAuthService（不触碰真实 DB / Redis）。
func newFakeOAuthService(acc *fakeOAuthAccess) *OAuthService {
	d := NewDeps(nil, nil, nil, nil, nil) // Logger=Nop / Users/Roles 空仓储 / Cfg 降级
	s := NewOAuthService(d)
	s.data = acc
	return s
}

// ---------------------------------------------------------------------------
// 任务点 1：FindOrCreateByOAuth 查找顺序（禁令 4 回归防线）
// ---------------------------------------------------------------------------

// seedBinding 快捷造一条绑定（带 user）。
func seedBinding(f *fakeOAuthAccess, provider, channel, openid, unionid, userID string) *model.UserOAuthBinding {
	b := &model.UserOAuthBinding{
		BaseModel: model.BaseModel{ID: f.nextID()},
		UserID:    userID, Provider: provider,
		Channel: channel, OpenID: openid,
	}
	if unionid != "" {
		u := unionid
		b.UnionID = &u
	}
	f.bindings = append(f.bindings, b)
	f.users = append(f.users, &model.User{BaseModel: model.BaseModel{ID: userID}, Username: "u-" + userID, Status: model.UserStatusActive})
	return b
}

func oauthUser(provider, id, unionid string) *oauthkit.User {
	return &oauthkit.User{Provider: provider, ID: id, UnionID: unionid, Nickname: "小明", AvatarURL: "https://a/x.png"}
}

// TestFindOrCreateUnionIDHitReusesUser unionid 命中 → 复用该 user，
// 不再查 openid、不建新号（禁令 4 的核心防线）。
func TestFindOrCreateUnionIDHitReusesUser(t *testing.T) {
	f := newFakeOAuthAccess()
	uid := seedBinding(f, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "openid-web", "unionid-1", "user-A").UserID
	s := newFakeOAuthService(f)

	// App 渠道、openid 不同、unionid 相同 → 必须复用 user-A（同一人 PC + App 不分裂）。
	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelApp}, oauthUser(oauthkit.ProviderWechat, "openid-app", "unionid-1"))
	if e != nil {
		t.Fatalf("unionid 命中应复用用户: %v", e)
	}
	if u.ID != uid {
		t.Errorf("应复用 user-A(%s)，实际 %s", uid, u.ID)
	}
	if f.calls["FindBindingByOpenID"] != 1 {
		// 全程只应有 ensureBinding 里的 1 次 openid 查（补 app 绑定前的存在性检查）；
		// 查找阶段因 unionid 命中直接跳过，不再消耗 openid 查询。
		t.Errorf("unionid 命中后 openid 查询应仅 ensureBinding 的 1 次，实际 %d", f.calls["FindBindingByOpenID"])
	}
	if len(f.users) != 1 {
		t.Errorf("不应建新号（users=%d）", len(f.users))
	}
	// App 渠道登录后应补一条 app 渠道绑定。
	appBindings := 0
	for _, b := range f.bindings {
		if b.Channel == oauthkit.ChannelApp {
			appBindings++
			if b.UserID != uid {
				t.Errorf("app 绑定应指向 user-A，实际 %s", b.UserID)
			}
			if b.OpenID != "openid-app" {
				t.Errorf("app 绑定 openid 应为 openid-app，实际 %s", b.OpenID)
			}
		}
	}
	if appBindings != 1 {
		t.Errorf("应补一条 app 渠道绑定，实际 %d 条", appBindings)
	}
}

// TestFindOrCreateUnionIDEmptySkipsQuery unionid 空（GitHub / QQ 未申请权限）→
// 跳过 unionid 查询，直接 openid。
func TestFindOrCreateUnionIDEmptySkipsQuery(t *testing.T) {
	f := newFakeOAuthAccess()
	uid := seedBinding(f, oauthkit.ProviderGitHub, oauthkit.ChannelWeb, "gh-123", "", "user-G").UserID
	s := newFakeOAuthService(f)

	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderGitHub, "gh-123", ""))
	if e != nil {
		t.Fatalf("openid 命中应复用用户: %v", e)
	}
	if u.ID != uid {
		t.Errorf("应复用 user-G(%s)，实际 %s", uid, u.ID)
	}
	if f.calls["FindBindingByUnionID"] != 0 {
		t.Errorf("unionid 为空时不得发起 unionid 查询（实际 %d 次）", f.calls["FindBindingByUnionID"])
	}
	if f.calls["FindBindingByOpenID"] != 2 {
		// 查找阶段 1 次 + ensureBinding 补绑定前的存在性检查 1 次。
		t.Errorf("openid 查询应为查找 1 次 + ensureBinding 1 次 = 2，实际 %d", f.calls["FindBindingByOpenID"])
	}
}

// TestFindOrCreateBothMissCreatesNewUser unionid 空 + openid 未命中 → 建新号。
// 这条是「同一人不会分裂成两个账号」的基石的反面：新身份必须落新号 + 新绑定。
func TestFindOrCreateBothMissCreatesNewUser(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)

	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderGitHub, "gh-new", ""))
	if e != nil {
		t.Fatalf("未命中应建新号: %v", e)
	}
	if u.ID == "" || u.Username == "" {
		t.Fatalf("新号应有 ID 与 username：%+v", u)
	}
	if len(f.users) != 1 || len(f.bindings) != 1 {
		t.Fatalf("应恰好 1 user + 1 binding，实际 users=%d bindings=%d", len(f.users), len(f.bindings))
	}
	if f.bindings[0].Provider != oauthkit.ProviderGitHub || f.bindings[0].Channel != oauthkit.ChannelWeb {
		t.Errorf("绑定 provider/channel 不符: %+v", f.bindings[0])
	}
	if f.bindings[0].OpenID != "gh-new" || f.bindings[0].UserID != u.ID {
		t.Errorf("绑定 openid/userId 不符: %+v", f.bindings[0])
	}
}

// TestFindOrCreateOpenIDCrossChannelReuses 同一平台 web 建过号、app 再登（openid 不同、
// unionid 也未返回）→ 按 openid 不带 channel 的语义不应命中（openid 是渠道内标识）。
// 但 web/app 用同一 openid 字符串是 QQ 未申请 unionid 权限时的真实形态：
// 只要 (provider, openid) 命中就复用同一 user —— 不带 channel 过滤正是为此。
func TestFindOrCreateOpenIDCrossChannelReuses(t *testing.T) {
	f := newFakeOAuthAccess()
	uid := seedBinding(f, oauthkit.ProviderQQ, oauthkit.ChannelWeb, "qq-openid-1", "", "user-Q").UserID
	s := newFakeOAuthService(f)

	// app 渠道、openid 相同字符串（QQ 例外形态）→ 复用 user-Q 并补 app 绑定。
	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelApp}, oauthUser(oauthkit.ProviderQQ, "qq-openid-1", ""))
	if e != nil {
		t.Fatalf("openid 命中应复用用户: %v", e)
	}
	if u.ID != uid {
		t.Errorf("应复用 user-Q(%s)，实际 %s", uid, u.ID)
	}
	if len(f.users) != 1 {
		t.Errorf("不应建新号（users=%d）", len(f.users))
	}
	appBindings := 0
	for _, b := range f.bindings {
		if b.Channel == oauthkit.ChannelApp {
			appBindings++
			if b.UserID != uid {
				t.Errorf("app 绑定应指向 user-Q，实际 %s", b.UserID)
			}
		}
	}
	if appBindings != 1 {
		t.Errorf("应补一条 app 渠道绑定，实际 %d 条", appBindings)
	}
}

// TestFindOrCreateDifferentProviderDoesNotCross 平台隔离：微信的 openid 不该命中 QQ 的绑定。
func TestFindOrCreateDifferentProviderDoesNotCross(t *testing.T) {
	f := newFakeOAuthAccess()
	seedBinding(f, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "shared-openid", "", "user-W")
	s := newFakeOAuthService(f)

	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderQQ, "shared-openid", ""))
	if e != nil {
		t.Fatalf("QQ 登录应建新号: %v", e)
	}
	if u.ID == "user-W" {
		t.Error("QQ 的 openid 不得命中微信绑定（平台必须隔离）")
	}
	if len(f.users) != 2 {
		t.Errorf("QQ 应建新号（users=%d）", len(f.users))
	}
}

// TestFindOrCreateGithubEmailMerge GitHub email 合并：无绑定历史但邮箱已有账号 → 归并。
func TestFindOrCreateGithubEmailMerge(t *testing.T) {
	f := newFakeOAuthAccess()
	f.users = append(f.users, &model.User{
		BaseModel: model.BaseModel{ID: "user-mail"},
		Username:  "localguy", Email: strPtr("dev@example.com"), Status: model.UserStatusActive,
	})
	s := newFakeOAuthService(f)

	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb},
		&oauthkit.User{Provider: oauthkit.ProviderGitHub, ID: "gh-42", Email: "dev@example.com", Nickname: "dev"})
	if e != nil {
		t.Fatalf("GitHub email 合并失败: %v", e)
	}
	if u.ID != "user-mail" {
		t.Errorf("应合并到 user-mail，实际 %s", u.ID)
	}
	if len(f.users) != 1 {
		t.Errorf("不应建新号（users=%d）", len(f.users))
	}
	// GitHub 兼容字段应回写。
	if f.users[0].GithubID == nil || *f.users[0].GithubID != "gh-42" {
		t.Errorf("github_id 应回写 gh-42，实际 %v", f.users[0].GithubID)
	}
}

// TestFindOrCreateGithubEmailNotUsedForOtherProviders email 合并只属于 GitHub 特例。
func TestFindOrCreateGithubEmailNotUsedForOtherProviders(t *testing.T) {
	f := newFakeOAuthAccess()
	f.users = append(f.users, &model.User{
		BaseModel: model.BaseModel{ID: "user-mail"},
		Username:  "localguy", Email: strPtr("dev@example.com"), Status: model.UserStatusActive,
	})
	s := newFakeOAuthService(f)

	// 微信同邮箱 → 不合并，建新号（openid 才是微信的身份键）。
	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb},
		&oauthkit.User{Provider: oauthkit.ProviderWechat, ID: "wx-1", Email: "dev@example.com", Nickname: "wx"})
	if e != nil {
		t.Fatalf("微信登录失败: %v", e)
	}
	if u.ID == "user-mail" {
		t.Error("非 GitHub 平台不得按 email 合并")
	}
}

// TestFindOrCreateGithubSoftDeletedEmailNotRevived 软删账号占用的 email 不参与合并、
// 建号时也不回填 email（防 23505）。
func TestFindOrCreateGithubSoftDeletedEmailNotRevived(t *testing.T) {
	f := newFakeOAuthAccess()
	f.softDeletedEmails["dead@example.com"] = true
	s := newFakeOAuthService(f)

	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb},
		&oauthkit.User{Provider: oauthkit.ProviderGitHub, ID: "gh-9", Email: "dead@example.com", Nickname: "nine"})
	if e != nil {
		t.Fatalf("软删邮箱场景登录失败: %v", e)
	}
	if u.Email != nil {
		t.Errorf("被软删账号占用的 email 不得回填新号，实际 %s", *u.Email)
	}
	if len(f.users) != 1 {
		t.Fatalf("应建 1 个新号（users=%d）", len(f.users))
	}
}

// TestFindOrCreateBindingConflictFollowsExisting openid 已被另一个 user 绑定
// （unionid 与既有绑定指向不同的 user —— 数据异常 / 并发建号）→ 以既有绑定为准，
// 返回的 user 必须与落库绑定一致。
func TestFindOrCreateBindingConflictFollowsExisting(t *testing.T) {
	f := newFakeOAuthAccess()
	// user-B 已占住 (wechat, openid-shared)；user-A 持有 unionid（异常数据）。
	seedBinding(f, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "openid-shared", "", "user-B")
	seedBinding(f, oauthkit.ProviderWechat, oauthkit.ChannelApp, "openid-other", "unionid-x", "user-A")
	s := newFakeOAuthService(f)

	// 同一 unionid 的用户这次用 openid-shared 登录：unionid 命中 user-A，
	// 但 openid 已属 user-B —— uk_oauth_provider_openid 是硬约束，跟绑定走。
	u, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelApp}, oauthUser(oauthkit.ProviderWechat, "openid-shared", "unionid-x"))
	if e != nil {
		t.Fatalf("绑定冲突场景登录失败: %v", e)
	}
	if u.ID != "user-B" {
		t.Errorf("应以既有绑定为准返回 user-B，实际 %s", u.ID)
	}
}

// TestFindOrCreateInputValidation 入参边界：nil User / 空 provider / 空 openid。
func TestFindOrCreateInputValidation(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	ctx := context.Background()

	if _, e := s.FindOrCreateByOAuth(ctx, nil, nil); e == nil || e.BizCode != errcode.BizInvalidParam {
		t.Errorf("nil User 应返回 10008，实际 %v", e)
	}
	if _, e := s.FindOrCreateByOAuth(ctx, nil, oauthUser("", "oid", "")); e == nil || e.BizCode != errcode.BizInvalidParam {
		t.Errorf("空 provider 应返回 10008，实际 %v", e)
	}
	if _, e := s.FindOrCreateByOAuth(ctx, nil, oauthUser(oauthkit.ProviderWechat, "  ", "")); e == nil || e.BizCode != errcode.BizInvalidParam {
		t.Errorf("空 openid 应返回 10008，实际 %v", e)
	}
	// tok 为 nil 时渠道回落 web。
	if _, e := s.FindOrCreateByOAuth(ctx, nil, oauthUser(oauthkit.ProviderGitHub, "gh-nil", "")); e != nil {
		t.Fatalf("tok=nil 应按 web 渠道正常登录: %v", e)
	}
	if f.bindings[0].Channel != oauthkit.ChannelWeb {
		t.Errorf("tok=nil 应落 web 渠道绑定，实际 %s", f.bindings[0].Channel)
	}
	// unionid 从 token 回退：User 无 unionid、Token 有 → 用 token 的。
	f2 := newFakeOAuthAccess()
	uid := seedBinding(f2, oauthkit.ProviderWechat, oauthkit.ChannelWeb, "openid-web", "unionid-tok", "user-T").UserID
	s2 := newFakeOAuthService(f2)
	u, e := s2.FindOrCreateByOAuth(ctx,
		&oauthkit.Token{Channel: oauthkit.ChannelApp, UnionID: "unionid-tok"},
		oauthUser(oauthkit.ProviderWechat, "openid-app", ""))
	if e != nil {
		t.Fatalf("token unionid 回退失败: %v", e)
	}
	if u.ID != uid {
		t.Errorf("token 里的 unionid 应参与查找并复用 user-T，实际 %s", u.ID)
	}
}

// TestFindOrCreateDependencyFailure 故障注入：查绑定报错 → 500（不降级、不放行）。
func TestFindOrCreateDependencyFailure(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	// unionid 查询故障。
	f.failNext = "FindBindingByUnionID"
	if _, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderWechat, "wx-1", "uni-1")); e == nil || e.BizCode != errcode.BizInternal {
		t.Errorf("unionid 查询故障应返回 500，实际 %v", e)
	}
	// openid 查询故障。
	f.failNext = "FindBindingByOpenID"
	if _, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderGitHub, "gh-1", "")); e == nil || e.BizCode != errcode.BizInternal {
		t.Errorf("openid 查询故障应返回 500，实际 %v", e)
	}
	// 建号故障。
	f.failNext = "CreateUser"
	if _, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderGitHub, "gh-2", "")); e == nil || e.BizCode != errcode.BizInternal {
		t.Errorf("建号故障应返回 500，实际 %v", e)
	}
}

// TestFindOrCreateNilDBReadyGate DB 不可用（生产 Deps 未接库）→ 503 ready 门。
func TestFindOrCreateNilDBReadyGate(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	s := NewOAuthService(d) // 未注入桩 → accessor 会回落 gorm，但 ready() 先挡
	if _, e := s.FindOrCreateByOAuth(context.Background(),
		&oauthkit.Token{Channel: oauthkit.ChannelWeb}, oauthUser(oauthkit.ProviderGitHub, "gh-1", "")); e == nil || e.BizCode != errcode.BizDependencyDown {
		t.Errorf("DB 为 nil 应返回 503，实际 %v", e)
	}
}

// ---------------------------------------------------------------------------
// 任务点 2：mapOAuthErr 全量映射表
// ---------------------------------------------------------------------------

// TestMapOAuthErrFullMapping 每个 SDK 哨兵 → 期望业务码与文案 key（一行一条）。
func TestMapOAuthErrFullMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		bizCode int
		key     i18nKey
	}{
		{"not_configured", oauthkit.ErrNotConfigured, errcode.BizOAuthNotConfigured, i18n.AuthOAuthNotConfigured},
		{"invalid_argument", oauthkit.ErrInvalidArgument, errcode.BizInvalidParam, i18n.CommonInvalidParameter},
		{"code_invalid", oauthkit.ErrCodeInvalid, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"credential_bad", oauthkit.ErrCredentialBad, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"token_expired", oauthkit.ErrTokenExpired, errcode.BizUnauthorized, i18n.AuthOAuthReauthRequired},
		{"refresh_dead", oauthkit.ErrRefreshDead, errcode.BizUnauthorized, i18n.AuthOAuthReauthRequired},
		{"token_mismatch", oauthkit.ErrTokenMismatch, errcode.BizForbidden, i18n.AuthOAuthTokenForged},
		{"token_not_verified", oauthkit.ErrTokenNotVerified, errcode.BizForbidden, i18n.AuthOAuthTokenForged},
		{"scope_missing", oauthkit.ErrScopeMissing, errcode.BizInvalidParam, i18n.AuthOAuthScopeMissing},
		{"email_missing", oauthkit.ErrEmailMissing, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"authurl_unsupported", oauthkit.ErrAuthURLUnsupported, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"exchange_unsupported", oauthkit.ErrExchangeUnsupported, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"refresh_unsupported", oauthkit.ErrRefreshUnsupported, errcode.BizInvalidParam, i18n.AuthOAuthFailed},
		{"upstream", oauthkit.ErrUpstream, errcode.BizInternal, i18n.CommonInternal},
		{"wrapped_upstream", fmt.Errorf("wrap: %w", oauthkit.ErrUpstream), errcode.BizInternal, i18n.CommonInternal},
		{"unknown", errors.New("some random error"), errcode.BizInternal, i18n.CommonInternal},
	}
	for _, c := range cases {
		e := mapOAuthErr(c.err)
		if e == nil {
			t.Errorf("%s: 映射结果为 nil", c.name)
			continue
		}
		if e.BizCode != c.bizCode {
			t.Errorf("%s: BizCode=%d, want %d", c.name, e.BizCode, c.bizCode)
		}
		if e.Key != c.key {
			t.Errorf("%s: Key=%s, want %s", c.name, e.Key, c.key)
		}
	}
}

// ---------------------------------------------------------------------------
// 任务点 3：truncateRunes 边界
// ---------------------------------------------------------------------------

// TestTruncateRunes 中文 3 字节 / emoji 4 字节 / 恰好 / 超长 / 空串 / max<=0。
func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"empty", "", 10, ""},
		{"max_zero", "abc", 0, ""},
		{"max_negative", "abc", -1, ""},
		{"ascii_exact", "hello", 5, "hello"},
		{"ascii_shorter", "hello", 10, "hello"},
		{"ascii_truncate", "hello", 3, "hel"},
		{"chinese_exact", "中文昵称", 4, "中文昵称"}, // 4 rune 恰好等于 max
		{"chinese_truncate", "中文昵称", 2, "中文"},
		{"chinese_bytes_vs_runes", strings.Repeat("中", 100), 85, strings.Repeat("中", 85)},
		{"emoji_exact", "😀😀", 2, "😀😀"},
		{"emoji_truncate", "😀😀😀", 2, "😀😀"},
		{"mixed", "a中😀b", 3, "a中😀"},
	}
	for _, c := range cases {
		got := truncateRunes(c.in, c.max)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		// 任何输出必须是合法 UTF-8（防 22021 的本质要求）。
		if got != "" && !utf8.ValidString(got) {
			t.Errorf("%s: 输出非法 UTF-8 %q", c.name, got)
		}
		// 按 rune 计数不超 max。
		if n := len([]rune(got)); n > c.max && c.max > 0 {
			t.Errorf("%s: rune 数 %d 超过 max %d", c.name, n, c.max)
		}
	}
}

// ---------------------------------------------------------------------------
// 任务点 4：uniqueOAuthNickname
// ---------------------------------------------------------------------------

// TestUniqueOAuthNicknameFallbackEmpty 空昵称回退 "<provider>用户"。
func TestUniqueOAuthNicknameFallbackEmpty(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	got := s.uniqueOAuthNickname(oauthkit.ProviderWechat, "   ")
	if got == nil || *got != "wechat用户" {
		t.Errorf("空昵称应回退 wechat用户，实际 %v", got)
	}
}

// TestUniqueOAuthNicknamePassThrough 昵称可用 → 原样返回。
func TestUniqueOAuthNicknamePassThrough(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	got := s.uniqueOAuthNickname(oauthkit.ProviderQQ, "小明")
	if got == nil || *got != "小明" {
		t.Errorf("昵称应原样返回，实际 %v", got)
	}
}

// TestUniqueOAuthNicknameDupAddsSuffix 重名时追加 "-xxx" 短后缀且不超列宽 64。
func TestUniqueOAuthNicknameDupAddsSuffix(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	f.existingNicknames["小明"] = true // 第一候选被占用
	got := s.uniqueOAuthNickname(oauthkit.ProviderQQ, "小明")
	if got == nil {
		t.Fatal("重名场景应返回候选")
	}
	if !strings.HasPrefix(*got, "小明-") {
		t.Errorf("重名应加 -xxx 后缀，实际 %q", *got)
	}
	if len([]rune(*got)) > 64 {
		t.Errorf("昵称超列宽 64：%q", *got)
	}
}

// TestUniqueOAuthNicknameTruncatesTo59 超长昵称先截到 59（64-5）再判断。
func TestUniqueOAuthNicknameTruncatesTo59(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	long := strings.Repeat("长", 80) // 80 rune，超 59
	got := s.uniqueOAuthNickname(oauthkit.ProviderWechat, long)
	if got == nil {
		t.Fatal("超长昵称应返回候选")
	}
	if len([]rune(*got)) > 64 {
		t.Errorf("超长昵称截断后仍超 64：%d", len([]rune(*got)))
	}
	if !strings.HasPrefix(*got, strings.Repeat("长", 59)) {
		t.Errorf("应截到 59 个 rune，实际 %d", len([]rune(*got)))
	}
}

// TestUniqueOAuthNicknameDupCheckFailure DB 抖动 → 交唯一索引兜底，仍返回候选不阻断。
func TestUniqueOAuthNicknameDupCheckFailure(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	f.failNext = "ExistsNickname"
	got := s.uniqueOAuthNickname(oauthkit.ProviderQQ, "抖动昵称")
	if got == nil || *got != "抖动昵称" {
		t.Errorf("查询失败应回落候选原样返回，实际 %v", got)
	}
}

// TestUniqueOAuthUsernameFallback sanitize 后为空 → 回落 "<provider>-<openid 前 8 位>"。
func TestUniqueOAuthUsernameFallback(t *testing.T) {
	if got := shortOAuthID("1234567890abcdef"); got != "12345678" {
		t.Errorf("shortOAuthID 应取前 8 位，实际 %q", got)
	}
	if got := shortOAuthID("short"); got != "short" {
		t.Errorf("不足 8 位应原样返回，实际 %q", got)
	}
	if got := sanitizeUsername("小 明!@#"); got != "" {
		t.Errorf("纯中文/符号 sanitize 后应为空，实际 %q", got)
	}
	if got := sanitizeUsername("  Dev_Name-99  "); got != "dev_name-99" {
		t.Errorf("合法字符应保留并小写，实际 %q", got)
	}
	if got := sanitizeUsername(strings.Repeat("a", 40)); got != strings.Repeat("a", 32) {
		t.Errorf("username 应截到 32，实际 %d", len(got))
	}
}

// TestUniqueOAuthUsernameDupAddsSuffix username 重名 → fallback + 3 位后缀。
func TestUniqueOAuthUsernameDupAddsSuffix(t *testing.T) {
	f := newFakeOAuthAccess()
	s := newFakeOAuthService(f)
	f.existingUsernames["xiaoming"] = true
	got, err := s.uniqueOAuthUsername("qq", "openid-12345678", "XiaoMing")
	if err != nil {
		t.Fatalf("uniqueOAuthUsername 失败: %v", err)
	}
	if got != "xiaoming-qq-123" && !strings.HasPrefix(got, "xiaoming-qq-") && !strings.HasPrefix(got, "qq-openid-1-") {
		// 第一候选 xiaoming 被占，循环后候选为 fallback-xxx
		// （fallback = "qq-" + shortOAuthID("openid-12345678") = "qq-openid-1"）。
		t.Errorf("重名候选形态不符: %q", got)
	}
}

// ---------------------------------------------------------------------------
// 任务点 5：oauthErrClass 全量分类
// ---------------------------------------------------------------------------

// TestOAuthErrClassFullCoverage 每个哨兵 → 期望日志标签；未知错误 → unknown。
func TestOAuthErrClassFullCoverage(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{oauthkit.ErrNotConfigured, "not_configured"},
		{oauthkit.ErrInvalidArgument, "invalid_argument"},
		{oauthkit.ErrAuthURLUnsupported, "authurl_unsupported"},
		{oauthkit.ErrExchangeUnsupported, "exchange_unsupported"},
		{oauthkit.ErrRefreshUnsupported, "refresh_unsupported"},
		{oauthkit.ErrTokenNotVerified, "token_not_verified"},
		{oauthkit.ErrTokenMismatch, "token_mismatch"},
		{oauthkit.ErrCodeInvalid, "code_invalid"},
		{oauthkit.ErrCredentialBad, "credential_bad"},
		{oauthkit.ErrTokenExpired, "token_expired"},
		{oauthkit.ErrRefreshDead, "refresh_dead"},
		{oauthkit.ErrScopeMissing, "scope_missing"},
		{oauthkit.ErrEmailMissing, "email_missing"},
		{oauthkit.ErrUpstream, "upstream"},
		{fmt.Errorf("wrap: %w", oauthkit.ErrTokenExpired), "token_expired"},
		{errors.New("mystery"), "unknown"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		if got := oauthErrClass(c.err); got != c.want {
			t.Errorf("oauthErrClass(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 任务点 6：state 一次性消费 + 渠道隔离（provider_test.go 的 GitHub 用例之外，
// 补微信/QQ 渠道隔离）
// ---------------------------------------------------------------------------

// TestOAuthStateChannelIsolation 微信/QQ state 键按 (provider, channel) 隔离：
// wechat:web:state:X 与 qq:web:state:X 互不串用；web 签发的 state 也不能用于 app。
func TestOAuthStateChannelIsolation(t *testing.T) {
	kv := newMemKV()
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = kv
	svc := NewAuthService(d)
	ctx := context.Background()

	// ① 前缀字面量隔离（双保险：不依赖行为验证的字符串形态）。
	if got := oauthStatePrefix(oauthkit.ProviderWechat, oauthkit.ChannelWeb); got != "oauth:wechat:web:state:" {
		t.Errorf("wechat web 前缀不符: %q", got)
	}
	if got := oauthStatePrefix(oauthkit.ProviderQQ, oauthkit.ChannelWeb); got != "oauth:qq:web:state:" {
		t.Errorf("qq web 前缀不符: %q", got)
	}

	// ② 微信 web 签发的 state → QQ web 校验必须拒绝。
	stateWx, e := svc.issueOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb)
	if e != nil {
		t.Fatalf("签发微信 state 失败: %v", e)
	}
	if !kv.has("oauth:wechat:web:state:" + stateWx) {
		t.Fatal("微信 state 未按 wechat:web 前缀落库")
	}
	if e := svc.verifyOAuthState(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb, stateWx); e == nil {
		t.Error("微信 state 用于 QQ 渠道必须拒绝")
	}
	// 拒绝不得消费原 state：键仍在（真正的消费放在 ④ 验证一次性）。
	if !kv.has("oauth:wechat:web:state:" + stateWx) {
		t.Error("跨渠道拒绝不得误删原 state")
	}

	// ③ QQ web 签发 → 微信校验拒绝；web 签发 → app 校验拒绝。
	stateQQ, e := svc.issueOAuthState(ctx, oauthkit.ProviderQQ, oauthkit.ChannelWeb)
	if e != nil {
		t.Fatalf("签发 QQ state 失败: %v", e)
	}
	if e := svc.verifyOAuthState(ctx, oauthkit.ProviderWechat, oauthkit.ChannelWeb, stateQQ); e == nil {
		t.Error("QQ state 用于微信渠道必须拒绝")
	}
	if e := svc.verifyOAuthState(ctx, oauthkit.ProviderQQ, oauthkit.ChannelApp, stateQQ); e == nil {
		t.Error("web 签发的 state 用于 app 渠道必须拒绝")
	}

	// ④ 各渠道正确配对：一次成功消费后重放必须失败（一次性）。
	for _, pair := range []struct{ provider, channel, state string }{
		{oauthkit.ProviderWechat, oauthkit.ChannelWeb, stateWx},
		{oauthkit.ProviderQQ, oauthkit.ChannelWeb, stateQQ},
	} {
		if e := svc.verifyOAuthState(ctx, pair.provider, pair.channel, pair.state); e != nil {
			t.Fatalf("%s/%s 首次校验应通过: %v", pair.provider, pair.channel, e)
		}
		if e := svc.verifyOAuthState(ctx, pair.provider, pair.channel, pair.state); e == nil {
			t.Errorf("%s/%s 同一 state 二次校验应失败（一次性）", pair.provider, pair.channel)
		}
	}
}

// TestOAuthStateWechatAppNoState App 渠道无 state 语义：verify 空 state → 10008。
func TestOAuthStateWechatAppNoState(t *testing.T) {
	d := NewDeps(nil, nil, nil, nil, nil)
	d.AuthTok = newMemKV()
	svc := NewAuthService(d)
	if e := svc.verifyOAuthState(context.Background(), oauthkit.ProviderWechat, oauthkit.ChannelApp, ""); e == nil ||
		e.BizCode != errcode.BizInvalidParam {
		t.Errorf("空 state 应返回 10008，实际 %v", e)
	}
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

// i18nKey 测试可读别名（errcode.Err.Key 的类型），便于用例表内直接引用常量。
type i18nKey = i18n.Key

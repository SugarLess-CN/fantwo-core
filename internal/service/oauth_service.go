// 第三方账号绑定服务（T-37c）。
//
// 职责边界：
//   - 本文件只做「把 (provider, openid / unionid) 映射到本站 user」这件事，
//     协议交互（拼 URL / 换 token / 拉 userinfo / 校验 token）全部在
//     internal/pkg/oauthkit，不在这里重复实现。
//   - 本文件不签发 token、不碰 Redis、不碰 gin.Context。
//
// # 硬约束来源
//
// docs/14-第三方登录调研.md §7.1.2 接线禁令清单，本文件直接相关的三条：
//
//	禁令 1：不得把 Token / User 整体打进日志（Token.Raw 含明文 token）。
//	        本文件日志只出现 provider / channel / openid 之外的**分类标签**，
//	        见 oauthErrClass。
//	禁令 2：错误信息不夹带响应体片段 —— 因此日志里不出现 err.Error()，
//	        只出现 oauthErrClass(err) 与 oauthkit 哨兵名。
//	禁令 4：Token.UnionID 空串必须先判空再查询（空串不是 NULL，
//	        在 SQL 里会互相匹配，把不同的人并成同一账号）。
//
// # 查找顺序（写错会让同一人 PC + App 变成两个账号）
//
//	① provider + unionid（unionid 非空时才查）
//	② provider + openid（**不带 channel 过滤**：微信 web 与 app 是两个 appid，
//	   同一用户两渠道 openid 不同，带 channel 会漏）
//	③ GitHub 特例：email 非空时按 email 合并已有账号（保留旧 findOrCreateGithubUser 行为）
//	④ 都未命中 → 建新 user + 建 binding
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/oauthkit"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// i18n key（T-37c 报告已列出，待 lead 在 internal/pkg/i18n/{keys.go,en.go} 补齐后
// 把这三行替换成 i18n.AuthOAuthReauthRequired / AuthOAuthTokenForged / AuthOAuthScopeMissing）。
//
// 之所以先在这里写字面量：跨文件引用尚未存在的常量会直接编译不过，
// 而 key 的字符串形态已按 i18n 命名规范（<domain>.<subject>.<result>）预先对齐，
// lead 补齐文案后只改这三行的右值即可，业务代码零改动。
// 三个 OAuth 专属文案 key（由 lead 在 i18n 包里定义，此处只引用）。
//
// 早期为了避开与 lead 并行改 i18n 的冲突，曾写成裸字面量 —— key 一旦改名
// 就会与 i18n 表静默分叉（编译不报错，运行时 message 退化成 key 原文）。
// 现在改为引用常量，改名会在编译期暴露。
const (
	keyOAuthReauthRequired = i18n.AuthOAuthReauthRequired
	keyOAuthTokenForged    = i18n.AuthOAuthTokenForged
	keyOAuthScopeMissing   = i18n.AuthOAuthScopeMissing
)

// errOAuthNotConfigured 「第三方登录未配置」的统一出口。
//
// 语义早已通用（业务码 20026 / i18n.AuthOAuthNotConfigured），但 errcode 里的
// 变量名仍带 Github 前缀。errcode 归 lead 独占（并行冲突面），因此本文件用
// 别名承接：lead 把 errcode 里的名字改成 ErrOAuthNotConfigured 后，
// 只需把这一行改为指向新名字，service 各处引用不用动。
var errOAuthNotConfigured = errcode.ErrGithubOAuthNotConfigured

// oauthFailed 第三方登录通用失败（400 / 10008 + AuthOAuthFailed）。
//
// 用于「参数本身合法，但登录无法完成」的场景；不含任何上游响应体片段（禁令 2）。
func oauthFailed() *errcode.Err {
	return errcode.ErrInvalidParam.WithKey(i18n.AuthOAuthFailed)
}

// OAuthService 第三方账号绑定服务。
type OAuthService struct {
	d *Deps
}

// NewOAuthService 构造。
func NewOAuthService(d *Deps) *OAuthService { return &OAuthService{d: d} }

// FindOrCreateByOAuth 按第三方身份查找或创建本站用户，并保证绑定关系落库。
//
// tok 提供渠道与 unionid（Token 里没有 Provider 字段，平台取自 u.Provider）；
// u 提供 openid / 昵称 / 头像 / email。
//
// 返回的用户由调用方继续做封禁判断与双 token 签发 —— 本函数不做登录态。
func (s *OAuthService) FindOrCreateByOAuth(ctx context.Context, tok *oauthkit.Token, u *oauthkit.User) (*model.User, *errcode.Err) {
	if e := s.d.ready(); e != nil {
		return nil, e
	}
	if u == nil {
		return nil, oauthFailed()
	}
	provider := strings.ToLower(strings.TrimSpace(u.Provider))
	openID := strings.TrimSpace(u.ID)
	// openid 是绑定主键，缺了就没有任何可靠身份可落库（空 ID 建号等于批量制造垃圾账号）。
	if provider == "" || openID == "" {
		return nil, oauthFailed()
	}
	channel := oauthkit.ChannelWeb
	if tok != nil {
		if c := strings.TrimSpace(tok.Channel); c != "" {
			channel = c
		}
	}
	// unionid：优先用 UserInfo 归一化后的值，退回 token 里的值。
	unionID := strings.TrimSpace(u.UnionID)
	if unionID == "" && tok != nil {
		unionID = strings.TrimSpace(tok.UnionID)
	}

	// ① unionid → ② openid → ③ GitHub email 合并。
	userID := ""
	if unionID != "" { // 禁令 4：空串一律不参与查询
		b, err := s.findBindingByUnionID(provider, unionID)
		if err != nil {
			s.d.Logger.Error("oauth find binding by unionid", zap.String("provider", provider), zapErr(err))
			return nil, errcode.ErrInternal
		}
		if b != nil {
			userID = b.UserID
		}
	}
	if userID == "" {
		b, err := s.findBindingByOpenID(provider, openID)
		if err != nil {
			s.d.Logger.Error("oauth find binding by openid", zap.String("provider", provider), zapErr(err))
			return nil, errcode.ErrInternal
		}
		if b != nil {
			userID = b.UserID
		}
	}
	if userID == "" && provider == oauthkit.ProviderGitHub {
		if email := strings.TrimSpace(u.Email); email != "" {
			existing, err := s.d.Users.FindByEmail(s.d.DB, email)
			if err == nil {
				userID = existing.ID
			} else if !IsNotFound(err) {
				s.d.Logger.Error("oauth find user by email", zapErr(err))
				return nil, errcode.ErrInternal
			}
		}
	}

	// ④ 已有归属：补绑定 + 刷新资料快照，不改 users 表业务字段（GitHub 兼容字段除外）。
	if userID != "" {
		finalID, e := s.ensureBinding(provider, channel, openID, unionID, u, userID)
		if e != nil {
			return nil, e
		}
		if provider == oauthkit.ProviderGitHub {
			s.touchGithubCompatFields(finalID, openID, u)
		}
		return findUserOrErrByID(s.d, finalID)
	}

	return s.createOAuthUser(provider, channel, openID, unionID, u)
}

// findBindingByUnionID 按 provider + unionid 查绑定。
//
// 调用方必须自行保证 unionID 非空（禁令 4）；这里再挡一次是为了防止
// 将来有人「顺手」在别处复用本函数并漏掉判空。
func (s *OAuthService) findBindingByUnionID(provider, unionID string) (*model.UserOAuthBinding, error) {
	if strings.TrimSpace(unionID) == "" {
		return nil, nil
	}
	return s.firstBinding("provider = ? AND unionid = ?", provider, unionID)
}

// findBindingByOpenID 按 provider + openid 查绑定。
//
// **故意不带 channel 过滤**：微信「网站应用」与「移动应用」是两个独立 appid，
// 同一用户在两渠道的 openid 不同、unionid 相同；带 channel 会让跨端登录漏查，
// 而 (provider, openid) 本身有唯一索引，字符串形态天然不跨渠道重号。
func (s *OAuthService) findBindingByOpenID(provider, openID string) (*model.UserOAuthBinding, error) {
	return s.firstBinding("provider = ? AND openid = ?", provider, openID)
}

// firstBinding 取第一条匹配绑定；不存在返回 (nil, nil)（把 gorm 的 not found 归一掉）。
func (s *OAuthService) firstBinding(cond string, args ...interface{}) (*model.UserOAuthBinding, error) {
	var b model.UserOAuthBinding
	err := s.d.DB.Where(cond, args...).First(&b).Error
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// ensureBinding 保证 (provider, openid) 这条绑定存在并指向 userID。
//
// 返回最终采用的 userID：若该 openid 已被另一个 user 绑定（数据异常 / 并发建号），
// 以既有绑定为准 —— uk_oauth_provider_openid 是硬约束，跟着唯一索引走才不会
// 让「同一次登录返回的 user 与落库的绑定」对不上。
func (s *OAuthService) ensureBinding(provider, channel, openID, unionID string, u *oauthkit.User, userID string) (string, *errcode.Err) {
	if existing, err := s.findBindingByOpenID(provider, openID); err != nil {
		s.d.Logger.Error("oauth check binding", zap.String("provider", provider), zapErr(err))
		return "", errcode.ErrInternal
	} else if existing != nil {
		if existing.UserID != userID {
			// 不打 openid 全值（禁令 2）；user id 是本站内部标识，可打。
			s.d.Logger.Warn("oauth binding conflict: openid already bound to another user, follow existing binding",
				zap.String("provider", provider), zap.String("userId", existing.UserID))
			userID = existing.UserID
		}
		s.refreshBindingSnapshot(existing, channel, unionID, u)
		return userID, nil
	}

	b := &model.UserOAuthBinding{
		UserID:    userID,
		Provider:  provider,
		Channel:   channel,
		OpenID:    openID,
		Nickname:  u.Nickname,
		AvatarURL: u.AvatarURL,
	}
	if unionID != "" {
		unionID := unionID
		b.UnionID = &unionID
	}
	if err := s.d.DB.Create(b).Error; err != nil {
		// 并发下短窗口内可能被另一个请求抢先插入同一个 (provider, openid)：
		// 回查一次，命中即复用，不把并发登录变成 500。
		if again, ferr := s.findBindingByOpenID(provider, openID); ferr == nil && again != nil {
			return again.UserID, nil
		}
		s.d.Logger.Error("oauth create binding", zap.String("provider", provider), zapErr(err))
		return "", errcode.ErrInternal
	}
	return userID, nil
}

// refreshBindingSnapshot 刷新昵称/头像快照与首次见到的 unionid。
//
// 快照仅用于展示，失败不影响登录（不能因为昵称写不进去就把人挡在门外）。
func (s *OAuthService) refreshBindingSnapshot(b *model.UserOAuthBinding, channel, unionID string, u *oauthkit.User) {
	fields := map[string]interface{}{
		"channel":    channel,
		"nickname":   u.Nickname,
		"avatar_url": u.AvatarURL,
	}
	// 之前没拿到 unionid（微信 web 常见），这次拿到了就补上 —— 多端打通依赖它。
	if unionID != "" && (b.UnionID == nil || *b.UnionID == "") {
		fields["unionid"] = unionID
	}
	if err := s.d.DB.Model(&model.UserOAuthBinding{}).Where("id = ?", b.ID).Updates(fields).Error; err != nil {
		s.d.Logger.Warn("oauth refresh binding snapshot failed", zap.String("provider", b.Provider), zapErr(err))
	}
}

// createOAuthUser 建新用户 + 建绑定 + 建 profile（单事务）。
func (s *OAuthService) createOAuthUser(provider, channel, openID, unionID string, u *oauthkit.User) (*model.User, *errcode.Err) {
	username, err := s.uniqueOAuthUsername(provider, openID, u.Nickname)
	if err != nil {
		s.d.Logger.Error("oauth unique username", zapErr(err))
		return nil, errcode.ErrInternal
	}
	pwd, err := hashPassword(genCode() + genCode())
	if err != nil {
		return nil, errcode.ErrInternal
	}

	user := &model.User{
		Username: username,
		// nickname 在 user 表上是 varchar(64) **且带 uniqueIndex**（model/user.go:53）。
		// 第三方昵称重名率极高（"微信用户" / "小明"），直接落库必然撞唯一索引 →
		// 建号失败把登录打成 500。故与 username 一样做唯一化 + 截断。
		Nickname: s.uniqueOAuthNickname(provider, u.Nickname),
		Password: pwd, // 随机密码：第三方登录账号不可用密码登录
		Status:   model.UserStatusActive,
	}
	// email 只有 GitHub 会返回；且必须绕过软删账号占用唯一索引的坑（见下）。
	if email := strings.TrimSpace(u.Email); email != "" {
		taken, terr := s.emailTakenIncludingDeleted(email)
		if terr != nil {
			s.d.Logger.Error("oauth check email", zapErr(terr))
			return nil, errcode.ErrInternal
		}
		if !taken {
			user.Email = &email
		} else {
			// 软删账号仍占着 user.email 的唯一索引，硬写会 SQLSTATE 23505 直接把登录打成 500。
			// 安全边界：只认「未被软删」的账号做合并，软删账号不复活、不接管，
			// 本条第三方账号就以无邮箱形态建号。
			s.d.Logger.Info("oauth register without email: address held by a soft-deleted account",
				zap.String("provider", provider))
		}
	}
	if avatar := strings.TrimSpace(u.AvatarURL); avatar != "" {
		user.Avatar = &avatar // user.avatar 是 text，无需截断
	}
	// GitHub 兼容字段：旧实现一直在写，保留兼容（新绑定表才是权威来源）。
	if provider == oauthkit.ProviderGitHub {
		id := openID
		user.GithubID = &id
		if nick := strings.TrimSpace(u.Nickname); nick != "" {
			truncated := truncateRunes(nick, 255) // github_username varchar(255)
			user.GithubUsername = &truncated
		}
	}
	if role, rerr := s.d.Roles.FindByName(s.d.DB, model.RoleNameUser); rerr == nil {
		user.Roles = []model.Role{*role}
	}

	// 绑定表列宽：nickname varchar(255) / avatar_url varchar(512)。
	// PostgreSQL 对 varchar(n) 超长是**报错**（SQLSTATE 22001）而不是截断，
	// 微信 headimgurl 带参数时轻易破 512，故一律先截断再落库。
	binding := &model.UserOAuthBinding{
		Provider:  provider,
		Channel:   channel,
		OpenID:    openID,
		Nickname:  truncateRunes(u.Nickname, 255),
		AvatarURL: truncateRunes(u.AvatarURL, 512),
	}
	if unionID != "" {
		uid := unionID
		binding.UnionID = &uid
	}

	if err := s.d.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.d.Users.Create(tx, user); err != nil {
			return err
		}
		binding.UserID = user.ID
		if err := tx.Create(binding).Error; err != nil {
			return err
		}
		return s.d.Users.CreateProfile(tx, &model.UserProfile{UserID: user.ID})
	}); err != nil {
		s.d.Logger.Error("oauth register", zap.String("provider", provider), zapErr(err))
		return nil, errcode.ErrInternal
	}
	return findUserOrErrByID(s.d, user.ID)
}

// emailTakenIncludingDeleted email 是否被占用（**含软删记录**）。
//
// 与 UserRepo.ExistsEmail 的区别：后者走 gorm 默认软删作用域，看不见已删账号，
// 但 PostgreSQL 的 uniqueIndex 仍然包含已删行 —— 只用前者会漏判，插入时撞 23505。
func (s *OAuthService) emailTakenIncludingDeleted(email string) (bool, error) {
	var n int64
	err := s.d.DB.Unscoped().Model(&model.User{}).Where("email = ?", email).Count(&n).Error
	return n > 0, err
}

// touchGithubCompatFields 维护 user 表的 github_id / github_username（兼容旧契约）。
//
// 失败只记日志：绑定表已是权威来源，这些列是给旧接口/旧数据看的冗余信息。
func (s *OAuthService) touchGithubCompatFields(userID, githubID string, u *oauthkit.User) {
	fields := map[string]interface{}{"github_id": githubID}
	if nick := strings.TrimSpace(u.Nickname); nick != "" {
		fields["github_username"] = nick
	}
	if err := s.d.Users.UpdateFields(s.d.DB, userID, fields); err != nil {
		// 撞 github_id 唯一索引（脏数据）时不能让登录失败。
		s.d.Logger.Warn("oauth touch github compat fields failed", zap.String("userId", userID), zapErr(err))
	}
}

// uniqueOAuthUsername 生成未占用的用户名。
//
// base 取平台昵称并 sanitize（昵称可能含中文 / 空格 / emoji，而 username 是
// 面向登录的标识）；sanitize 后为空时回落到 "<provider>-<openid 前 8 位>"。
func (s *OAuthService) uniqueOAuthUsername(provider, openID, nickname string) (string, error) {
	base := sanitizeUsername(nickname)
	fallback := provider + "-" + shortOAuthID(openID)
	cand := base
	if cand == "" {
		cand = fallback
	}
	for i := 0; i < 5; i++ {
		dup, err := s.d.Users.ExistsUsername(s.d.DB, cand, "")
		if err != nil {
			return "", err
		}
		if !dup {
			return cand, nil
		}
		cand = fallback + "-" + genCode()[:3]
	}
	// 5 次仍冲突：交给唯一索引兜底（宁可插入失败返回 500，也不死循环）。
	return cand, nil
}

// sanitizeUsername 只保留字母 / 数字 / 下划线 / 连字符，长度截到 32。
func sanitizeUsername(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
		if b.Len() >= 32 {
			break
		}
	}
	return b.String()
}

// shortOAuthID 取 openid 前 8 位（仅用于生成用户名，不进日志）。
func shortOAuthID(openID string) string {
	if len(openID) > 8 {
		return openID[:8]
	}
	return openID
}

// findUserOrErrByID 取用户并转统一错误（AuthService / OAuthService 共用）。
func findUserOrErrByID(d *Deps, id string) (*model.User, *errcode.Err) {
	user, err := d.Users.FindByID(d.DB, id)
	if IsNotFound(err) {
		return nil, errcode.ErrUserNotExist
	}
	if err != nil {
		d.Logger.Error("oauth find user", zapErr(err))
		return nil, errcode.ErrInternal
	}
	return user, nil
}

// mapOAuthErr 把 oauthkit 哨兵错误映射成本项目 errcode。
//
// 映射表见任务书 T-37c §4；要点：
//   - ErrInvalidArgument = 我的错、重试无用 → 10008 参数错误（不要误判成「重新授权」）
//   - ErrTokenExpired / ErrRefreshDead → 401 + reauth 文案（引导用户重新授权）
//   - ErrTokenMismatch / ErrTokenNotVerified → 403 + forged 文案（可能是伪造，不是用户操作失误）
//   - ErrUpstream → 500，细节只进日志不进响应
func mapOAuthErr(err error) *errcode.Err {
	switch {
	case errors.Is(err, oauthkit.ErrNotConfigured):
		return errOAuthNotConfigured
	case errors.Is(err, oauthkit.ErrInvalidArgument):
		return errcode.ErrInvalidParam
	case errors.Is(err, oauthkit.ErrCodeInvalid), errors.Is(err, oauthkit.ErrCredentialBad):
		return oauthFailed()
	case errors.Is(err, oauthkit.ErrTokenExpired), errors.Is(err, oauthkit.ErrRefreshDead):
		return errcode.ErrUnauthorized.WithKey(keyOAuthReauthRequired)
	case errors.Is(err, oauthkit.ErrTokenMismatch), errors.Is(err, oauthkit.ErrTokenNotVerified):
		return errcode.ErrForbidden.WithKey(keyOAuthTokenForged)
	case errors.Is(err, oauthkit.ErrScopeMissing):
		return errcode.ErrInvalidParam.WithKey(keyOAuthScopeMissing)
	case errors.Is(err, oauthkit.ErrEmailMissing):
		// 非错误：GitHub 用户可能没邮箱 → 建号时 email 留空（由调用方容忍，见 completeOAuthLogin）。
		return oauthFailed()
	case errors.Is(err, oauthkit.ErrAuthURLUnsupported),
		errors.Is(err, oauthkit.ErrExchangeUnsupported),
		errors.Is(err, oauthkit.ErrRefreshUnsupported):
		// 渠道能力缺失属于接线错误（例如 QQ app 不该走 Exchange），不是用户能修的。
		return oauthFailed()
	default:
		// ErrUpstream 与一切未知错误：对外统一内部错误，细节进日志。
		return errcode.ErrInternal
	}
}

// tolerableOAuthErr 这些错误在登录主流程里可以降级容忍（拿不到邮箱仍可建号）。
func tolerableOAuthErr(err error) bool {
	return errors.Is(err, oauthkit.ErrEmailMissing) || errors.Is(err, oauthkit.ErrScopeMissing)
}

// oauthErrClass 把 SDK 错误归类成一个短标签，**专供日志**。
//
// 禁令 1 / 禁令 2：SDK 的错误文本可能夹带上游响应体片段（含 token 明文），
// 因此日志里不打 err.Error()，只打这个分类标签。
func oauthErrClass(err error) string {
	sentinels := []struct {
		name string
		err  error
	}{
		{"not_configured", oauthkit.ErrNotConfigured},
		{"invalid_argument", oauthkit.ErrInvalidArgument},
		{"authurl_unsupported", oauthkit.ErrAuthURLUnsupported},
		{"exchange_unsupported", oauthkit.ErrExchangeUnsupported},
		{"refresh_unsupported", oauthkit.ErrRefreshUnsupported},
		{"token_not_verified", oauthkit.ErrTokenNotVerified},
		{"token_mismatch", oauthkit.ErrTokenMismatch},
		{"code_invalid", oauthkit.ErrCodeInvalid},
		{"credential_bad", oauthkit.ErrCredentialBad},
		{"token_expired", oauthkit.ErrTokenExpired},
		{"refresh_dead", oauthkit.ErrRefreshDead},
		{"scope_missing", oauthkit.ErrScopeMissing},
		{"email_missing", oauthkit.ErrEmailMissing},
		{"upstream", oauthkit.ErrUpstream},
	}
	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return s.name
		}
	}
	return "unknown"
}

// truncateRunes 按「字符数」截断到 max 个 rune。
//
// 为什么不用 s[:max]：byte 切片切在多字节字符中间会产生非法 UTF-8，
// PostgreSQL 对非法 UTF-8 直接报错（SQLSTATE 22021）。中文昵称 3 字节/字，
// 按字节截 255 只能存 85 个汉字 —— 按 rune 截才是「人能看懂的长度」。
func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// uniqueOAuthNickname 生成不撞唯一索引的昵称。
//
// user.nickname 是 varchar(64) + uniqueIndex（model/user.go:53），而第三方昵称
// 重名率极高（「微信用户」「小明」），直接落库必然撞索引 → 建号失败把登录打成 500。
//
// 策略与 uniqueOAuthUsername 同构但更宽松：昵称允许中文 / emoji / 空格，
// 只在超长时截断、在重名时追加短后缀。
//
// 刻意不做脏词过滤：内容审核是独立链路（audit 域），在登录建号路径上做过滤
// 会让「审核误判 = 用户登不进来」，且审核是异步的（当前还没有正文可审）。
func (s *OAuthService) uniqueOAuthNickname(provider, nickname string) *string {
	const colWidth = 64
	// 留 5 个字符给重名后缀（"-xxx"）。
	base := truncateRunes(strings.TrimSpace(nickname), colWidth-5)
	if base == "" {
		base = provider + "用户"
	}
	nick := base
	for i := 0; i < 5; i++ {
		dup, err := s.d.Users.ExistsNickname(s.d.DB, nick, "")
		if err != nil {
			// 查询失败不阻断建号：交给唯一索引兜底。理由：一次 DB 抖动不该让用户
			// 登不进来；若真撞索引，插入会报错并返回 500（可观测），
			// 而「静默建一个无昵称账号」是不可观测的脏数据。
			s.d.Logger.Warn("oauth nickname dup check failed, falling back to unique index",
				zap.String("provider", provider), zapErr(err))
			return &nick
		}
		if !dup {
			return &nick
		}
		suffix := genCode()[:3]
		nick = truncateRunes(base, colWidth-len(suffix)-1) + "-" + suffix
	}
	// 5 次仍冲突：交给唯一索引兜底（宁可插入失败返回 500，也不死循环）。
	return &nick
}

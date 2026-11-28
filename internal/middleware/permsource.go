package middleware

// 实时权限读取（2026-11-28 用户要求）。
//
// # 为什么改
//
// 之前权限存在 JWT 的 `perms` 声明里，中间件只做内存比对、不查库。
// 后果：管理员给某人加了角色，**那个人的旧 token 里权限还是老的**，
// 必须等 token 过期或强制重新登录才生效（我们靠 perm_ver 让他 401 重登，
// 本质是「把权限同步问题转嫁给用户」）。
//
// 现在改为：判定时**从数据库实时读**权限，JWT 只负责「你是谁」。
//
// # 为什么还要缓存
//
// 每个请求都 JOIN 三张表太贵（permission ⋈ role_permissions ⋈ role ⋈ user_roles）。
// 所以加一层进程内缓存，TTL 很短（默认 5 秒）+ 按用户粒度：
//   · 权限变更后最多 5 秒生效（可接受，比「重登才生效」好得多）
//   · 同一个用户的连续请求只查一次库
//   · 进程内缓存不跨实例 —— 多副本部署时各副本独立刷新，最长 5 秒收敛
//
// 想立即生效可以调 InvalidatePermissionCache（角色/权限变更接口里已经调了）。

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
)

// permissionCacheTTL 权限缓存有效期。
//
// 5 秒是权衡结果：更短则高 QPS 下仍会频繁查库，更长则权限变更生效变慢。
// 变更接口会主动失效缓存，所以这里只是「兜底刷新周期」。
const permissionCacheTTL = 5 * time.Second

// permCache 进程内权限缓存（按 userID）。
type permCache struct {
	mu    sync.RWMutex
	items map[string]permEntry
}

type permEntry struct {
	perms []string
	roles []string
	exp   time.Time
}

func newPermCache() *permCache { return &permCache{items: make(map[string]permEntry)} }

func (c *permCache) get(userID string) ([]string, []string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[userID]
	if !ok || time.Now().After(e.exp) {
		return nil, nil, false
	}
	return e.perms, e.roles, true
}

func (c *permCache) set(userID string, perms, roles []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 简单的容量保护：条目太多时直接清空重建，避免无限增长（正常规模远达不到）
	if len(c.items) > 10000 {
		c.items = make(map[string]permEntry)
	}
	c.items[userID] = permEntry{perms: perms, roles: roles, exp: time.Now().Add(permissionCacheTTL)}
}

func (c *permCache) invalidate(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, userID)
}

// globalPermSource 进程级单例。
//
// 为什么需要全局：service 层（角色变更、给用户改角色）要知道权限缓存的存在才能失效它，
// 而它拿不到 router 构造的那个实例。用一个包级变量把两者连起来。
// 副作用可控 —— 全进程只该有一个权限来源，多实例反而不一致。
var globalPermSource *PermissionSource

// GlobalPermissionSource 取进程级实例（可能为 nil，测试/降级场景）。
func GlobalPermissionSource() *PermissionSource { return globalPermSource }

// InvalidateUserPermissions 让某用户的权限缓存失效（service 层调用）。
//
// service 包不方便依赖 middleware 的具体实例，这里提供包级函数做门面。
// 实例为 nil（未接 router）时静默返回，不影响业务。
func InvalidateUserPermissions(userID string) {
	if globalPermSource != nil {
		globalPermSource.Invalidate(userID)
	}
}

// InvalidateUsersPermissions 批量失效。
func InvalidateUsersPermissions(userIDs []string) {
	if globalPermSource != nil {
		globalPermSource.InvalidateMany(userIDs)
	}
}

// PermissionSource 实时权限来源（生产实现：数据库；测试可注入桩）。
type PermissionSource struct {
	db     *gorm.DB
	roles  *repository.RoleRepo
	log    *zap.Logger
	cache  *permCache
	enable bool
}

// NewPermissionSource 构造实时权限源。
//
// db 为 nil（降级启动 / 测试）时自动禁用实时查询，
// 调用方会回退到 JWT 里的快照 —— 保证降级场景仍可用，不会因为查不到库就全站 403。
func NewPermissionSource(db *gorm.DB, roles *repository.RoleRepo, log *zap.Logger) *PermissionSource {
	src := &PermissionSource{
		db:     db,
		roles:  roles,
		log:    log,
		cache:  newPermCache(),
		enable: db != nil && roles != nil,
	}
	globalPermSource = src
	return src
}

// Permissions 取某用户**当前**的权限点。第二个返回值表示是否成功读到。
//
// 读失败返回 (nil, false)，调用方应回退到 JWT 快照 ——
// 权限查询失败不该让整个站点 403（那是可用性事故，比权限滞后严重得多）。
func (p *PermissionSource) Permissions(userID string) ([]string, bool) {
	if !p.enable || userID == "" {
		return nil, false
	}
	if perms, _, ok := p.cache.get(userID); ok {
		return perms, true
	}
	perms, err := p.roles.UserPermissionNames(p.db, userID)
	if err != nil {
		if p.log != nil {
			p.log.Warn("load live permissions failed, fallback to token snapshot: " + err.Error())
		}
		return nil, false
	}
	roles, _ := p.roles.UserRoleNames(p.db, userID)
	p.cache.set(userID, perms, roles)
	return perms, true
}

// Roles 取某用户**当前**的角色名（未命中缓存则一并刷新权限）。
func (p *PermissionSource) Roles(userID string) ([]string, bool) {
	if !p.enable || userID == "" {
		return nil, false
	}
	if _, roles, ok := p.cache.get(userID); ok {
		return roles, true
	}
	// 复用 Permissions 的加载逻辑，避免重复查库
	if _, ok := p.Permissions(userID); !ok {
		return nil, false
	}
	_, roles, ok := p.cache.get(userID)
	return roles, ok
}

// Invalidate 让某用户的权限缓存立刻失效。
//
// 调用时机：给用户改角色、改角色权限、禁用/启用角色。
// 调了之后下一次请求就会重新查库（不用等 TTL）。
func (p *PermissionSource) Invalidate(userID string) {
	if p == nil || p.cache == nil {
		return
	}
	p.cache.invalidate(userID)
}

// InvalidateMany 批量失效（角色权限变更会波及该角色下所有用户）。
func (p *PermissionSource) InvalidateMany(userIDs []string) {
	for _, id := range userIDs {
		p.Invalidate(id)
	}
}

// ---- gin context 接线 ----

// CtxPermSource 权限来源在 gin context 里的键名。
const CtxPermSource = "dsh:perm-source"

// SetPermissionSource 把权限来源挂到 engine 上（router 启动时调一次）。
//
// 实现方式是注册一个全局中间件：它把 source 放进每个请求的 context，
// 后续 RequirePermissions / RequireAnyPermission 从 context 取。
// 没有这层也能跑 —— currentPermissions 取不到 source 时会回退到 JWT 快照。
func SetPermissionSource(r *gin.Engine, src *PermissionSource) {
	if src == nil {
		return
	}
	r.Use(func(c *gin.Context) {
		c.Set(CtxPermSource, src)
		c.Next()
	})
}

// currentPermissions 取当前请求用户的**实时**权限。
//
// 返回的 second 值表示「是否来自实时库」：
//   - true  → 从 PermissionSource 读到（或缓存命中）
//   - false → 降级用了 JWT 快照，或没有任何权限
//
// 调用方目前都不关心这个布尔值（只关心权限集合），保留它是为了将来
// 需要「权限可能过期」的场景（例如敏感操作要求实时校验）能区分。
func currentPermissions(c *gin.Context, claims *jwtx.Claims) ([]string, bool) {
	if v, ok := c.Get(CtxPermSource); ok {
		if src, ok2 := v.(*PermissionSource); ok2 && src != nil {
			if perms, fresh := src.Permissions(claims.UserID); fresh {
				return perms, true
			}
		}
	}
	// 回退：JWT 里的快照（降级 / 测试 / 查库失败）
	return claims.Permissions, false
}

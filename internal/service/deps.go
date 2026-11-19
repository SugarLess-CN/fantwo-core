// Package service 业务逻辑层：用户域（认证 / 用户 / RBAC）。
//
// 分层约定见 docs/04 §2：service 不接触 gin.Context（只吃 context.Context），
// 事务用 db.Transaction 显式开启，队列投递走 asynq client。
package service

import (
	"context"
	"errors"
	"time"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/repository"
	"github.com/SugarLess-CN/fantwo-core/internal/service/infra"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Deps 用户域 service 共享依赖。
type Deps struct {
	DB     *gorm.DB
	Redis  *redis.Client
	Signer *jwtx.Signer
	Queue  *asynq.Client
	Logger *zap.Logger
	Users  *repository.UserRepo
	Roles  *repository.RoleRepo
	Perms  *repository.PermissionRepo
	// Cfg 系统配置读取（复用基础设施域 ConfigService：进程内缓存 → Redis → DB）。
	// DB/Redis 为 nil 时 GetString 返回默认值，正好构成「未配置即降级」语义。
	Cfg *infra.ConfigService
	// DBUnavailable 降级提示：DB 为 nil 时业务端点返回 503。
}

// NewDeps 构造共享依赖（DB/Redis 可为 nil，服务以降级模式运行）。
func NewDeps(db *gorm.DB, rdb *redis.Client, signer *jwtx.Signer, queue *asynq.Client, logger *zap.Logger) *Deps {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Deps{
		DB:     db,
		Redis:  rdb,
		Signer: signer,
		Queue:  queue,
		Logger: logger,
		Users:  repository.NewUserRepo(),
		Roles:  repository.NewRoleRepo(),
		Perms:  repository.NewPermissionRepo(),
		Cfg:    infra.NewConfigService(db, rdb),
	}
}

// ready 校验数据库可用（骨架懒连接：DB 为 nil 时端点返回 503）。
func (d *Deps) ready() *errcode.Err {
	if d == nil || d.DB == nil {
		return errcode.ErrDependencyUnavailable
	}
	return nil
}

// IsNotFound gorm 记录不存在判定。
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// 账号注销冷静期（对齐任务书 §2：7 天冷静期任务入 asynq 延迟队列）。
const AccountDeletionCoolingDays = 7

// TypeAccountDeletion 账号注销到期执行任务类型。
const TypeAccountDeletion = "account:deletion:execute"

// now UTC 时间（数据库统一时区语义：本地 Asia/Shanghai 由 DSN 指定，代码用 time.Now）。
func now() time.Time { return time.Now() }

// dayStart 当天零点（签到日期归一用）。
func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// bgCtx 队列/后台操作用 context（与请求生命周期解耦）。
func bgCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

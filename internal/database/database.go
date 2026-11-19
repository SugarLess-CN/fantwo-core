// Package database 负责初始化 GORM（PostgreSQL）与 go-redis 客户端。
//
// 目标库为 PostgreSQL（用户 2026-11-19 指定；不需兼容存量数据，全新建库）。
// 骨架约定：两者均为「懒连接」——本机没有 PostgreSQL/Redis 时只打 warning 不 panic，
// 保证 main 能起、health 能通；业务端点在连接不可用时各自返回 5xx。
package database

import (
	"fmt"
	"log"

	"github.com/SugarLess-CN/fantwo-core/internal/config"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// InitPostgres 初始化 GORM PostgreSQL 连接。
// DisableAutomaticPing=true 使连接真正懒建立（首次查询才握手），
// 因此即使 PostgreSQL 不存在，服务也能正常启动。
// 失败时返回 (nil, err)，调用方打 warning 后继续。
func InitPostgres(cfg *config.PostgresConfig) (*gorm.DB, error) {
	if cfg.Database == "" {
		return nil, fmt.Errorf("postgres.database 未配置")
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableAutomaticPing: true, // 懒连接：不主动握手
		Logger:               gormlogger.Default.LogMode(parseGormLevel(cfg.LogLevel)),
	})
	if err != nil {
		return nil, fmt.Errorf("gorm 初始化失败: %w", err)
	}
	return db, nil
}

// MustInitPostgres 同 InitPostgres，但失败仅打 warning 并返回 nil（骨架懒启动语义）。
func MustInitPostgres(cfg *config.PostgresConfig) *gorm.DB {
	db, err := InitPostgres(cfg)
	if err != nil {
		log.Printf("[WARN] PostgreSQL 初始化失败（服务将以降级模式启动，业务端点不可用）: %v", err)
		return nil
	}
	return db
}

// InitRedis 初始化 go-redis 客户端。
// go-redis 本身懒连接（Ping 才握手），配置缺失/格式错误时返回 error。
func InitRedis(cfg *config.RedisConfig) (*redis.Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis.addr 未配置")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return rdb, nil
}

// MustInitRedis 同 InitRedis，但失败仅打 warning 并返回 nil。
func MustInitRedis(cfg *config.RedisConfig) *redis.Client {
	rdb, err := InitRedis(cfg)
	if err != nil {
		log.Printf("[WARN] Redis 初始化失败（队列/缓存功能降级）: %v", err)
		return nil
	}
	return rdb
}

func parseGormLevel(s string) gormlogger.LogLevel {
	switch s {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

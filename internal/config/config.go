// Package config 基于 viper 实现配置加载：
// 优先级：环境变量 > config.yaml > 内置默认值。
// 环境变量映射规则：层级键 db.host 对应 DB_HOST（点号转下划线，无前缀，
// 对齐原 NestJS 仓库的环境变量命名习惯）。
package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config 应用全局配置根。
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Postgres PostgresConfig `mapstructure:"postgres"`
	Redis    RedisConfig    `mapstructure:"redis"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Log      LogConfig      `mapstructure:"log"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	// Port 监听端口，默认 3000（对齐原仓库 PORT 默认值）。
	Port int `mapstructure:"port"`
	// Mode gin 运行模式：debug / release / test。
	Mode string `mapstructure:"mode"`
}

// PostgresConfig PostgreSQL 连接配置（2026-11-19 选型更新：目标库 PostgreSQL）。
type PostgresConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
	// SSLMode disable / require / verify-full 等，默认 disable。
	SSLMode string `mapstructure:"ssl_mode"`
	// LogLevel gorm 日志级别：silent / error / warn / info。
	LogLevel string `mapstructure:"log_level"`
}

// RedisConfig Redis 连接配置（对应原 REDIS_URL / REDIS_PASSWORD）。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// JWTConfig JWT 签发配置（对应原 JWT_SECRET / JWT_EXPIRES_IN）。
type JWTConfig struct {
	Secret             string `mapstructure:"secret"`
	ExpiresIn          string `mapstructure:"expires_in"`           // access token 有效期，默认 1h
	RefreshExpiresIn   string `mapstructure:"refresh_expires_in"`   // refresh token 有效期，默认 7d
	Issuer             string `mapstructure:"issuer"`
}

// LogConfig 日志配置。
type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug / info / warn / error
	Format string `mapstructure:"format"` // json / console
}

// Load 加载配置：读取 config.yaml（存在时），再以环境变量覆盖。
// config.yaml 缺失不报错（骨架要求：仅凭环境变量/默认值也能启动）。
func Load() (*Config, error) {
	v := viper.New()

	// 内置默认值
	v.SetDefault("server.port", 3000)
	v.SetDefault("server.mode", "debug")
	v.SetDefault("postgres.host", "127.0.0.1")
	v.SetDefault("postgres.port", 5432)
	v.SetDefault("postgres.ssl_mode", "disable")
	v.SetDefault("postgres.log_level", "warn")
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("jwt.expires_in", "1h")
	v.SetDefault("jwt.refresh_expires_in", "168h")
	v.SetDefault("jwt.issuer", "fantwo-core")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "console")

	// config.yaml（可缺失；config.example.yaml 为入库模板）
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// 找不到配置文件：继续走环境变量与默认值
	}

	// 环境变量覆盖：db.host -> DB_HOST
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// WorkDir 返回当前工作目录（日志与排错用）。
func WorkDir() string {
	dir, _ := os.Getwd()
	return dir
}

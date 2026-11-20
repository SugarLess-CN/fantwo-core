// Package config 测试：Load 的默认值、环境变量覆盖、点号转下划线规则。
//
// 防的核心 bug（历史事故，见 AGENTS.md「陷阱」与 infra.go 注释）：
//
//	viper 的 Unmarshal 只遍历 AllSettings()，而 AutomaticEnv **不会**把
//	「既没有 SetDefault、也不在配置文件里」的键加入 AllSettings。
//	后果：postgres.database / postgres.user / postgres.password 这类键
//	即使设了环境变量也被静默忽略，服务连不上库却毫无报错。
//	本次联调就踩到了这个坑（T-12）。
//
// 因此下面 TestLoad_EnvOverridesEveryDefaultedKey 逐个键验证「env 能覆盖」，
// 任何人删掉某行 SetDefault 都会立刻让测试变红。
package config

import (
	"os"
	"testing"
)

// chdirTemp 切到空临时目录，隔离工作区里的真实 config.yaml。
// 不隔离的话，本地 config.yaml（已在 .gitignore）会让默认值断言随机失败。
func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("取当前工作目录失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换到临时目录失败: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("恢复工作目录失败: %v", err)
		}
	})
}

// TestLoad_Defaults 无配置文件、无环境变量时的内置默认值。
func TestLoad_Defaults(t *testing.T) {
	chdirTemp(t)
	// 清掉可能从外部继承的同名环境变量，保证只测内置默认值
	for _, k := range []string{"SERVER_PORT", "SERVER_MODE", "POSTGRES_HOST", "POSTGRES_PORT",
		"POSTGRES_SSL_MODE", "REDIS_ADDR", "REDIS_DB", "JWT_ISSUER", "JWT_EXPIRES_IN",
		"JWT_REFRESH_EXPIRES_IN", "LOG_LEVEL", "LOG_FORMAT", "SWAGGER_ENABLED"} {
		t.Setenv(k, "")
	}
	// 空字符串会被 viper 当成「未设置」，此处仅防止真实环境值泄入

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失败（配置文件缺失不应报错）: %v", err)
	}

	cases := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"server.port 默认 3000", cfg.Server.Port, 3000},
		{"postgres.port 默认 5432", cfg.Postgres.Port, 5432},
		{"postgres.host 默认本机", cfg.Postgres.Host, "127.0.0.1"},
		{"postgres.ssl_mode 默认 disable", cfg.Postgres.SSLMode, "disable"},
		{"redis.addr 默认本机", cfg.Redis.Addr, "127.0.0.1:6379"},
		{"redis.db 默认 0", cfg.Redis.DB, 0},
		{"jwt.expires_in 默认 1h", cfg.JWT.ExpiresIn, "1h"},
		{"jwt.refresh_expires_in 默认 168h", cfg.JWT.RefreshExpiresIn, "168h"},
		{"jwt.issuer 默认 fantwo-core", cfg.JWT.Issuer, "fantwo-core"},
		{"log.level 默认 info", cfg.Log.Level, "info"},
		{"log.format 默认 console", cfg.Log.Format, "console"},
		{"swagger.enabled 默认关闭", cfg.Swagger.Enabled, false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: 得到 %v，期望 %v", c.name, c.got, c.want)
		}
	}
}

// TestLoad_EnvOverridesEveryDefaultedKey 逐个键验证环境变量覆盖生效。
//
// 每个 case 对应 config.go 里的一行 v.SetDefault(...)；
// 删掉那行 SetDefault，对应 case 立即失败（env 值读不到，回落到零值/默认值）。
func TestLoad_EnvOverridesEveryDefaultedKey(t *testing.T) {
	cases := []struct {
		env   string // 环境变量名（点号转下划线）
		value string
		check func(*Config) (interface{}, interface{}) // got, want
		desc  string
	}{
		{"SERVER_PORT", "18080",
			func(c *Config) (interface{}, interface{}) { return c.Server.Port, 18080 },
			"server.port → SERVER_PORT（int 解析）"},
		{"SERVER_MODE", "release",
			func(c *Config) (interface{}, interface{}) { return c.Server.Mode, "release" },
			"server.mode → SERVER_MODE"},
		{"POSTGRES_HOST", "db.internal",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.Host, "db.internal" },
			"postgres.host → POSTGRES_HOST"},
		{"POSTGRES_PORT", "6543",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.Port, 6543 },
			"postgres.port → POSTGRES_PORT"},
		{"POSTGRES_USER", "picart_user",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.User, "picart_user" },
			"postgres.user → POSTGRES_USER（曾因缺 SetDefault 完全失效）"},
		{"POSTGRES_PASSWORD", "s3cret",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.Password, "s3cret" },
			"postgres.password → POSTGRES_PASSWORD（曾因缺 SetDefault 完全失效）"},
		{"POSTGRES_DATABASE", "picart_db",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.Database, "picart_db" },
			"postgres.database → POSTGRES_DATABASE（曾因缺 SetDefault 完全失效）"},
		{"POSTGRES_SSL_MODE", "require",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.SSLMode, "require" },
			"多段键的点号转下划线：postgres.ssl_mode → POSTGRES_SSL_MODE"},
		{"POSTGRES_LOG_LEVEL", "info",
			func(c *Config) (interface{}, interface{}) { return c.Postgres.LogLevel, "info" },
			"postgres.log_level → POSTGRES_LOG_LEVEL"},
		{"REDIS_ADDR", "redis.internal:6380",
			func(c *Config) (interface{}, interface{}) { return c.Redis.Addr, "redis.internal:6380" },
			"redis.addr → REDIS_ADDR"},
		{"REDIS_PASSWORD", "redispw",
			func(c *Config) (interface{}, interface{}) { return c.Redis.Password, "redispw" },
			"redis.password → REDIS_PASSWORD"},
		{"REDIS_DB", "3",
			func(c *Config) (interface{}, interface{}) { return c.Redis.DB, 3 },
			"redis.db → REDIS_DB（int 解析）"},
		{"JWT_SECRET", "topsecret",
			func(c *Config) (interface{}, interface{}) { return c.JWT.Secret, "topsecret" },
			"jwt.secret → JWT_SECRET"},
		{"JWT_EXPIRES_IN", "30m",
			func(c *Config) (interface{}, interface{}) { return c.JWT.ExpiresIn, "30m" },
			"jwt.expires_in → JWT_EXPIRES_IN"},
		{"JWT_REFRESH_EXPIRES_IN", "72h",
			func(c *Config) (interface{}, interface{}) { return c.JWT.RefreshExpiresIn, "72h" },
			"jwt.refresh_expires_in → JWT_REFRESH_EXPIRES_IN"},
		{"JWT_ISSUER", "custom-issuer",
			func(c *Config) (interface{}, interface{}) { return c.JWT.Issuer, "custom-issuer" },
			"jwt.issuer → JWT_ISSUER"},
		{"LOG_LEVEL", "debug",
			func(c *Config) (interface{}, interface{}) { return c.Log.Level, "debug" },
			"log.level → LOG_LEVEL"},
		{"LOG_FORMAT", "json",
			func(c *Config) (interface{}, interface{}) { return c.Log.Format, "json" },
			"log.format → LOG_FORMAT"},
		{"SWAGGER_ENABLED", "true",
			func(c *Config) (interface{}, interface{}) { return c.Swagger.Enabled, true },
			"swagger.enabled → SWAGGER_ENABLED（bool 解析）"},
	}

	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			chdirTemp(t)
			t.Setenv(c.env, c.value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load 失败: %v", err)
			}
			got, want := c.check(cfg)
			if got != want {
				t.Errorf("%s：得到 %v，期望 %v（检查 config.go 是否漏了 SetDefault）", c.desc, got, want)
			}
		})
	}
}

// TestLoad_ConfigFileOverriddenByEnv 优先级：环境变量 > config.yaml > 内置默认值。
func TestLoad_ConfigFileOverriddenByEnv(t *testing.T) {
	chdirTemp(t)

	yaml := []byte("server:\n  port: 7777\npostgres:\n  host: file-host\n")
	if err := os.WriteFile("config.yaml", yaml, 0o644); err != nil {
		t.Fatalf("写 config.yaml 失败: %v", err)
	}

	// 1) 只有文件：读文件值
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("配置文件 server.port 未生效：得到 %d，期望 7777", cfg.Server.Port)
	}
	if cfg.Postgres.Host != "file-host" {
		t.Errorf("配置文件 postgres.host 未生效：得到 %q", cfg.Postgres.Host)
	}

	// 2) 文件 + 环境变量：env 覆盖文件
	t.Setenv("SERVER_PORT", "8888")
	t.Setenv("POSTGRES_HOST", "env-host")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.Port != 8888 {
		t.Errorf("环境变量未覆盖配置文件：得到 %d，期望 8888", cfg.Server.Port)
	}
	if cfg.Postgres.Host != "env-host" {
		t.Errorf("环境变量未覆盖配置文件：得到 %q，期望 env-host", cfg.Postgres.Host)
	}
}

// TestLoad_MissingConfigFileIsNotError 配置文件缺失只走 env/默认值，不报错。
// 防的是：有人把 ReadInConfig 的错误当致命错返回，导致裸机（无 config.yaml）起不来。
func TestLoad_MissingConfigFileIsNotError(t *testing.T) {
	chdirTemp(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("config.yaml 缺失时不应报错，实际: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load 返回 nil 配置")
	}
}

// TestWorkDir 返回非空路径（日志与排错依赖它）。
func TestWorkDir(t *testing.T) {
	if WorkDir() == "" {
		t.Fatal("WorkDir 返回空串")
	}
}

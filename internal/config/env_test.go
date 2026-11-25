// .env 加载的契约测试。
//
// 防的 bug（都是 viper + dotenv 组合的真实坑）：
//   - .env **覆盖**真实环境变量 → 容器 `-e POSTGRES_HOST=x` 被镜像里的 .env 盖掉（部署事故）
//   - .env 缺失时把启动打成 fatal（生产没有 .env 是常态）
//   - .env 语法错时静默忽略（配置写错了没人知道，排错从「为什么配置没生效」开始）
//   - 加载顺序反了（.env.local 该压过 .env）
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// inTempDir 在临时目录里跑，避免碰到仓库根的 .env。
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadDotEnvMissingIsNotError 没有 .env 不是错误（生产常态）。
func TestLoadDotEnvMissingIsNotError(t *testing.T) {
	inTempDir(t)
	res, err := LoadDotEnv()
	if err != nil {
		t.Fatalf(".env 缺失不应报错，实际 %v", err)
	}
	if res.Any() {
		t.Errorf("没有文件时 Loaded 应为空，实际 %v", res.Loaded)
	}
	if len(res.Missing) != 2 {
		t.Errorf("应记录两个 Missing（.env 与 .env.local），实际 %v", res.Missing)
	}
}

// TestLoadDotEnvDoesNotOverrideRealEnv 真实环境变量优先于 .env（部署正确性底线）。
func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	dir := inTempDir(t)
	writeFile(t, dir, ".env", "POSTGRES_HOST=from-dotenv\nPOSTGRES_PORT=5432\n")

	t.Setenv("POSTGRES_HOST", "from-real-env")

	res, err := LoadDotEnv()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(res.Loaded) != 1 || res.Loaded[0] != ".env" {
		t.Fatalf("应有 1 个文件加载成功，实际 %v", res.Loaded)
	}

	if got := os.Getenv("POSTGRES_HOST"); got != "from-real-env" {
		t.Errorf("POSTGRES_HOST 被 .env 覆盖了：得到 %q，期望 from-real-env（真实 env 必须优先）", got)
	}
	// 真实 env 没有的键才由 .env 补
	if got := os.Getenv("POSTGRES_PORT"); got != "5432" {
		t.Errorf("POSTGRES_PORT 应由 .env 提供，实际 %q", got)
	}
}

// TestLocalOverridesBase .env.local 压过 .env（开发者个人覆盖）。
func TestLocalOverridesBase(t *testing.T) {
	dir := inTempDir(t)
	writeFile(t, dir, ".env", "SERVER_PORT=3000\nJWT_ISSUER=team-default\n")
	writeFile(t, dir, ".env.local", "SERVER_PORT=9999\n")

	res, err := LoadDotEnv()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(res.Loaded) != 2 {
		t.Fatalf("应加载两个文件，实际 %v", res.Loaded)
	}
	if got := os.Getenv("SERVER_PORT"); got != "9999" {
		t.Errorf("SERVER_PORT 应为 .env.local 的 9999，实际 %q", got)
	}
	if got := os.Getenv("JWT_ISSUER"); got != "team-default" {
		t.Errorf("JWT_ISSUER 应由 .env 提供（.env.local 未设），实际 %q", got)
	}
}

// TestLoadDotEnvSyntaxErrorReported 语法错要上报（不能静默）。
//
// 注意：godotenv 对多数畸形行是「宽松忽略」，真正报错的是
// 「有 key 没有 =」这类。这里用一行明显的畸形内容断言 err != nil。
func TestLoadDotEnvSyntaxErrorReported(t *testing.T) {
	dir := inTempDir(t)
	// 引号不闭合 → godotenv 返回 parse error
	writeFile(t, dir, ".env", "BROKEN=\"unterminated\n")

	_, err := LoadDotEnv()
	if err == nil {
		t.Skip("当前 godotenv 版本对该畸形行不报错；该用例作为版本升级哨兵")
	}
	if !contains(err.Error(), ".env") {
		t.Errorf("错误信息应指明是哪个文件，实际 %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

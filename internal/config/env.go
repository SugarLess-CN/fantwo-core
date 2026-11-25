// Package config .env 显式加载（godotenv）。
//
// 为什么需要这一层：
//
//	viper 的 AutomaticEnv 只能读到**进程环境变量**；它自己不会去读 .env 文件。
//	所以过去想用 .env 必须靠外部工具（direnv / dotenv-cli / docker --env-file），
//	`go run ./cmd/server` 直接跑读不到。这里在 main 最早期把 .env 灌进 os.Environ，
//	viper 的既有逻辑（默认值 < config.yaml < 环境变量）就自动生效了。
//
// 优先级设计（不要改，改之前先看下面的理由）：
//
//		内置默认值  <  config.yaml  <  .env  <  .env.local  <  真实进程环境变量
//
//	 1. `.env` 用 godotenv.Load（**不覆盖**已存在的环境变量）——容器里
//	    `docker run -e POSTGRES_HOST=x` 必须压过镜像里的 .env，这是部署正确性的底线。
//	 2. `.env.local` 用 godotenv.Overload（**覆盖**）——它是开发者的个人覆盖文件，
//	    目的就是压过团队共享的 .env（例如本机换端口、连本地库）。已 gitignore。
//	 3. 两者都不覆盖「真实进程环境变量」吗？——`.env` 不覆盖，`.env.local` 会覆盖。
//	    理由：`.env.local` 是显式的「我要盖掉一切」开关，且只存在于开发机（生产不会有这个文件）。
//	    如果哪天需要「.env.local 也不盖真 env」，把 Overload 换回 Load 即可，一行改动。
//
// 缺失与错误处理：
//
//	文件不存在 = 常态（生产用真实环境变量），静默跳过，**不报错**。
//	文件存在但语法错 = 配置事故，返回 error 让 main 打警告后继续（不阻断启动：
//	语法错的那几行没生效，但其余 env / config.yaml / 默认值仍能撑起一个可用进程）。
package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/joho/godotenv"
)

// EnvFiles 按加载顺序排列的 .env 文件名。
//
// 顺序即优先级：靠后的覆盖靠前的（.env.local 用 Overload）。
const (
	envFileBase  = ".env"
	envFileLocal = ".env.local"
)

// EnvLoadResult 描述本次 .env 加载情况，供启动日志说明「env 是从哪来的」。
type EnvLoadResult struct {
	// Loaded 实际加载成功的文件名（按加载顺序）。
	Loaded []string
	// Missing 不存在而跳过的文件名。
	Missing []string
	// Errs 存在但解析失败的错误（key = 文件名）。
	Errs map[string]error
}

// Any 是否有任何文件被加载。
func (r EnvLoadResult) Any() bool { return len(r.Loaded) > 0 }

// LoadDotEnv 显式加载 .env / .env.local（幂等，可多次调用）。
//
// 必须在 config.Load() **之前**调用：viper 的 AutomaticEnv 读的是 os.Environ，
// 晚于 Load() 再灌 .env 对已经完成的 Unmarshal 无效。
//
// 返回 error 只在「文件存在但语法错」时非 nil；文件不存在不算错误。
func LoadDotEnv() (EnvLoadResult, error) {
	res := EnvLoadResult{Errs: map[string]error{}}

	// ① .env：不覆盖已有环境变量（真实 env 优先）
	loadOne(envFileBase, false, &res)

	// ② .env.local：覆盖（开发者个人覆盖，压过 .env）
	loadOne(envFileLocal, true, &res)

	// 语法错要上报，让 main 打日志（不阻断启动）
	if len(res.Errs) > 0 {
		var joined error
		for name, err := range res.Errs {
			joined = errors.Join(joined, errors.New(name+": "+err.Error()))
		}
		return res, joined
	}
	return res, nil
}

// loadOne 加载单个文件：overload=true 时覆盖已有环境变量。
func loadOne(name string, overload bool, res *EnvLoadResult) {
	if _, statErr := os.Stat(name); statErr != nil {
		// 不存在（含权限不足之外的任何 stat 失败）→ 记为 Missing，不报错。
		// 用 fs.ErrNotExist 判一下只是为了将来区分「权限问题」留口子。
		if errors.Is(statErr, fs.ErrNotExist) || os.IsNotExist(statErr) {
			res.Missing = append(res.Missing, name)
			return
		}
		res.Missing = append(res.Missing, name)
		return
	}

	var err error
	if overload {
		err = godotenv.Overload(name)
	} else {
		err = godotenv.Load(name)
	}
	if err != nil {
		res.Errs[name] = err
		return
	}
	res.Loaded = append(res.Loaded, name)
}

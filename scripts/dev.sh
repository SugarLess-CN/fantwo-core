#!/usr/bin/env bash
# 本地开发启动：先跑 swag init 生成 Swagger 文档，再启动服务
#
# 用法（任意目录均可，脚本会自己切到项目根）：
#   bash scripts/dev.sh
#   SKIP_SWAG=1 bash scripts/dev.sh    # 跳过文档生成，直接起服务
#
# 为什么启动前必须生成 swagger.json：
#   docs/swagger/ 是 swag 的产物（docs.go + swagger.json + swagger.yaml），
#   handler 上的 @Router 注解改了但没重跑 swag，UI 里就是旧接口。
#   注意必须在【项目根】执行 —— 在 tmp/deploy-pkg 之类的暂存目录里跑，
#   包名会变成 tmp_deploy-pkg_internal_handler.*，$ref 全部指向错误定义。
#
# 环境变量：config/config.yml 由程序自动读（环境变量仍可覆盖），
# 本脚本只负责把 go / swag 补进 PATH 并串起「生成 → 启动」两步。

set -euo pipefail

cd "$(dirname "$0")/.."

# Windows 的 Go / swag 默认不在 PATH 里；Linux/macOS 下 command -v 会命中，不追加。
GO_BIN="${GO_BIN:-/c/Program Files/Go/bin}"
SWAG_HOME="${SWAG_HOME:-$HOME/go/bin}"

if [ ! -x "$GO_BIN/go.exe" ] && [ ! -x "$GO_BIN/go" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN="$(dirname "$(command -v go)")"
  else
    echo "✗ 找不到 go，请设 GO_BIN 指向 Go 的 bin 目录" >&2
    exit 1
  fi
fi
export PATH="$PATH:$GO_BIN:$SWAG_HOME"

if [ "${SKIP_SWAG:-0}" != "1" ]; then
  if ! command -v swag >/dev/null 2>&1; then
    echo "==> 未找到 swag，正在安装 v1.16.6（与 go.mod 的 swaggo/swag 对齐）"
    GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" go install github.com/swaggo/swag/cmd/swag@v1.16.6
  fi
  echo "==> 生成 Swagger 文档：docs/swagger/{docs.go,swagger.json,swagger.yaml}"
  # swag 内部会 shell 出 go list，所以 go 必须在 PATH 里（上面已处理）
  # --exclude tmp 必不可少：tmp/ 下有 tmp/deploy-pkg（整项目副本，211 个 .go），
  # 不排除会被 swag 当成包扫进去，$ref 变成 tmp_deploy-pkg_internal_handler.*
  # （本会话实测：definitions 从 6 涨到 11，全部指向错误定义）。
  swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal --exclude tmp,internal/dto
  echo "==> 完成，操作数：$(grep -oE '"(get|post|put|patch|delete)":' docs/swagger/swagger.json | wc -l | tr -d ' ')"
fi

echo "==> 启动服务（配置文件 config/config.yml，SWAGGER_ENABLED=true 时 UI 在 /swagger/index.html）"
exec go run ./cmd/server

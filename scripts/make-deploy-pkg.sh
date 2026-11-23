#!/usr/bin/env bash
# 打包 1panel 部署包：只收「编译 + 运行」必需的文件
#
# 用法（在项目根目录）：
#   bash scripts/make-deploy-pkg.sh
# 产出：
#   dist/fantwo-core-deploy-<日期>.tar.gz（约 1.8MB）
#
# 为什么不直接打包整个项目：
#   tmp/     → 原仓库克隆、临时脚本，几十 MB 且与运行无关
#   .git/    → 历史，服务器上不需要
#   reports/ → 过程性汇报，运维不需要
#   test/    → 集成测试，容器构建用不到（build tag 隔离）
#   dist/    → 交叉编译产物，用 from-source 模式时不需要
#   uploads/ → 本地运行产物
#   AGENTS.md / todo.md → 治理文档，不入仓库

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
DATE="$(date +%Y%m%d)"
OUT_DIR="$ROOT/dist"
STAGE="$ROOT/tmp/deploy-pkg"
TARBALL="$OUT_DIR/fantwo-core-deploy-$DATE.tar.gz"

echo "==> 清理旧暂存目录"
rm -rf "$STAGE"
mkdir -p "$STAGE" "$OUT_DIR"

echo "==> 收集必需文件"

# 编译必需（缺一个都编译不过）
cp -r cmd internal migrations "$STAGE"/
cp go.mod go.sum "$STAGE"/

# docs/swagger 被 internal/router/swagger_routes.go 空导入（_ "…/docs/swagger"）
# → 整个 docs/ 一起拷最省事；只有 983KB，且附带交付文档
cp -r docs "$STAGE"/

# 容器构建与编排
cp Dockerfile.binary docker-compose.build.yml .dockerignore "$STAGE"/

# 配置模板（服务器上 cp 成 .env 再改）
cp .env.build.example "$STAGE"/

# 说明文档
cp README.md "$STAGE"/ 2>/dev/null || true

echo "==> 打 tar.gz"
tar -czf "$TARBALL" -C "$STAGE" .

# 体积与文件数：只用 tar/awk/printf 这些到处都有的工具
# （du / stat -c 在 Git Bash、busybox 精简环境里可能缺失或参数不兼容）
BYTES="$(tar -tzvf "$TARBALL" >/dev/null 2>&1; wc -c < "$TARBALL" | tr -d ' ')"
SIZE="$(awk -v b="$BYTES" 'BEGIN{printf "%.1fM", b/1048576}')"
COUNT="$(tar -tzf "$TARBALL" | grep -vc '/$' || true)"
echo ""
echo "==> 完成"
echo "    文件：$TARBALL"
echo "    大小：$SIZE"
echo "    文件数：$COUNT"
echo ""
echo "上传步骤（1panel）："
echo "  1) scp $TARBALL root@<服务器>:/opt/"
echo "  2) ssh 到服务器：mkdir -p /opt/fantwo-core && tar xzf /opt/$(basename "$TARBALL") -C /opt/fantwo-core"
echo "  3) cd /opt/fantwo-core && cp .env.build.example .env && vim .env"
echo "  4) docker compose -f docker-compose.build.yml up -d --build"

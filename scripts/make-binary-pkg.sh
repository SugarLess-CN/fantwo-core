#!/usr/bin/env bash
# 打 binary 模式部署包：只含「跑起来」必需的 4 个文件
#
# 用法（在项目根目录）：
#   bash scripts/make-binary-pkg.sh
#
# 产出：
#   dist/fantwo-core-binary-pkg.tar.gz（约 21.6MB，含 68.5MB 二进制压缩后）
#
# 这个包的特点：一次上传，之后升级只需要换 dist/ 里的二进制。
#
# 与 make-deploy-pkg.sh（build 模式）的区别：
#   make-deploy-pkg.sh  → 打包源码（1.8MB），服务器上容器内编译
#   make-binary-pkg.sh  → 打包二进制（21.6MB），服务器上只拷不编译

set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
ARCH="${ARCH:-amd64}"
BIN="fantwo-core-linux-$ARCH"
OUT_DIR="$ROOT/dist"
STAGE="$ROOT/tmp/binary-pkg"
TARBALL="$OUT_DIR/fantwo-core-binary-pkg-$ARCH.tar.gz"

echo "==> 交叉编译 linux/$ARCH"
mkdir -p "$OUT_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
  go build -trimpath -ldflags="-s -w" \
  -o "$OUT_DIR/$BIN" ./cmd/server

# 校验产出的确是 Linux ELF（防止误编成 Windows exe 传上去）
#
# 用 cmp 比对前 4 字节（ELF 魔数 0x7f 'E' 'L' 'F'）：
#   - cmp 是 diffutils 的一部分，Git Bash / busybox / 各发行版都有
#   - 不用 od/xxd（Git Bash 实测缺失），不用 go run（tmp/ 在 .dockerignore 与
#     .gitignore 里，某些环境下 go 会因模块解析忽略该目录）
MAGIC_FILE="$(mktemp 2>/dev/null || echo "$ROOT/tmp/_magic.bin")"
printf '\177ELF' > "$MAGIC_FILE"
HEAD_FILE="$(mktemp 2>/dev/null || echo "$ROOT/tmp/_head.bin")"
head -c 4 "$OUT_DIR/$BIN" > "$HEAD_FILE"

if cmp -s "$HEAD_FILE" "$MAGIC_FILE"; then
    echo "    前 4 字节 = 7f 45 4c 46（ELF）✓"
else
    echo "!! 产出不是 ELF 二进制，检查 GOOS/GOARCH（应为 GOOS=linux）" >&2
    rm -f "$MAGIC_FILE" "$HEAD_FILE"
    exit 1
fi
rm -f "$MAGIC_FILE" "$HEAD_FILE"

echo "==> 清理旧暂存目录"
rm -rf "$STAGE"
mkdir -p "$STAGE/dist"

echo "==> 收集文件（只 4 个）"
cp Dockerfile.binary docker-compose.binary.yml .env.binary.example "$STAGE"/
cp "$OUT_DIR/$BIN" "$STAGE/dist/"

echo "==> 打 tar.gz"
tar -czf "$TARBALL" -C "$STAGE" .

BYTES="$(wc -c < "$TARBALL" | tr -d ' ')"
SIZE="$(awk -v b="$BYTES" 'BEGIN{printf "%.1fM", b/1048576}')"
echo ""
echo "==> 完成"
echo "    文件：$TARBALL"
echo "    大小：$SIZE"
echo ""
echo "上传步骤（1panel）："
echo "  1) scp $TARBALL root@<服务器>:/opt/"
echo "  2) ssh：mkdir -p /opt/fantwo-core && tar xzf /opt/$(basename "$TARBALL") -C /opt/fantwo-core"
echo "  3) cd /opt/fantwo-core && cp .env.binary.example .env && vim .env"
echo "  4) docker compose -f docker-compose.binary.yml up -d --build"
echo ""
echo "后续升级（只换二进制）："
echo "  1) 重新执行本脚本（或在开发机单独 go build）"
echo "  2) scp $OUT_DIR/$BIN root@<服务器>:/opt/fantwo-core/dist/"
echo "  3) ssh：cd /opt/fantwo-core && docker compose -f docker-compose.binary.yml up -d --build"

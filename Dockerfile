# 多阶段构建：静态编译 → 精简运行镜像
#
# 构建：docker build -t fantwo-core:latest .
# 运行：docker run --env-file .env -p 3000:3000 fantwo-core:latest
#
# 说明：刻意不使用 `# syntax=docker/dockerfile:1` 与 RUN --mount=type=cache，
# 避免构建时额外拉取 dockerfile 前端镜像（离线/受限网络下会失败）。

# ---------- 构建阶段 ----------
FROM golang:1.26-bookworm AS builder

WORKDIR /build

# 依赖层缓存：先只拷 go.mod/go.sum，再拉依赖
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0：纯静态二进制。
# 本项目图片处理走 disintegration/imaging（纯 Go），未使用 bimg/libvips，故无需 CGO。
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /build/fantwo-core ./cmd/server

# ---------- 运行阶段 ----------
FROM debian:bookworm-slim

# ca-certificates：外部 HTTPS（GitHub OAuth / SMTP / 云厂商审核 / 支付）
# tzdata：日志与业务时间按本地时区
# ffmpeg：video-compression 队列依赖外部命令
# curl：HEALTHCHECK 探针
# Debian 官方源在国内常 502，换阿里云镜像源（deb822 格式，bookworm 起默认）
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
        sed -i 's|deb.debian.org|mirrors.aliyun.com|g; s|security.debian.org|mirrors.aliyun.com|g' \
            /etc/apt/sources.list.d/debian.sources; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates tzdata ffmpeg curl; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /build/fantwo-core /app/fantwo-core

# 本地存储模式下上传产物落这里（生产建议挂卷或用 S3）
RUN mkdir -p /app/uploads

# 非 root 运行
RUN useradd -m -u 10001 appuser && chown -R appuser:appuser /app
USER appuser

ENV SERVER_PORT=3000 \
    SERVER_MODE=release \
    LOG_FORMAT=json \
    STORAGE_DRIVER=local \
    STORAGE_LOCAL_DIR=/app/uploads

EXPOSE 3000
VOLUME ["/app/uploads"]

# 健康检查走 /health：该端点恒返回 200（探针语义），依赖状态在 data.dependencies 里，
# 因此这里只判 HTTP 可达性，不做业务级断言。
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS "http://127.0.0.1:${SERVER_PORT}/health" >/dev/null || exit 1

ENTRYPOINT ["/app/fantwo-core"]

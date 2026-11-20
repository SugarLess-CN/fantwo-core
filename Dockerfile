# 多阶段构建：静态编译 → 极简运行镜像
#
# 构建（默认，含 ffmpeg 支持视频压缩）：
#   docker build -t fantwo-core:latest .
# 构建（最小镜像，去掉 ffmpeg；video-compression 队列将不可用）：
#   docker build --build-arg WITH_FFMPEG=false -t fantwo-core:slim .
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

# CGO_ENABLED=0：纯静态二进制（可跑在 alpine/scratch）。
# 本项目图片处理走 disintegration/imaging（纯 Go），未使用 bimg/libvips，故无需 CGO。
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /build/fantwo-core ./cmd/server

# ---------- 运行阶段 ----------
# alpine 而非 debian-slim：基础层 12MB vs 114MB，ffmpeg 包体积也显著更小。
# CGO_ENABLED=0 的静态二进制无 glibc 依赖，alpine 可直接运行。
FROM alpine:3.20

# 是否安装 ffmpeg（视频压缩队列依赖外部命令；不用视频压缩可关掉省体积）
ARG WITH_FFMPEG=true

# ca-certificates：外部 HTTPS（GitHub OAuth / SMTP / 短信网关 / 支付）
# tzdata：日志与业务时间按本地时区
# curl：HEALTHCHECK 探针
# ffmpeg：video-compression 队列
# 阿里云镜像源（官方源国内常超时）
RUN set -eux; \
    if [ -f /etc/apk/repositories ]; then \
        sed -i 's|https://dl-cdn.alpinelinux.org|https://mirrors.aliyun.com|g' /etc/apk/repositories; \
    fi; \
    apk add --no-cache ca-certificates tzdata curl; \
    if [ "$WITH_FFMPEG" = "true" ]; then apk add --no-cache ffmpeg; fi; \
    update-ca-certificates

WORKDIR /app

COPY --from=builder /build/fantwo-core /app/fantwo-core

# 本地存储模式下上传产物落这里（生产建议挂卷或用对象存储）
RUN mkdir -p /app/uploads

# 非 root 运行
RUN adduser -D -u 10001 appuser && chown -R appuser:appuser /app
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

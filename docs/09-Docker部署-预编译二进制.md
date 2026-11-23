# 预编译二进制 Docker 部署指南

> 目标：**容器里只跑二进制，不做任何编译**。开发机交叉编译一次，服务器只负责跑。
> 适合：服务器没有 Go 环境、或不想在服务器上拉 1.2GB 的 golang 镜像 + `go mod download`。

---

## 一、为什么用这种方式

| 方案 | 服务器要做什么 | 服务器要拉什么 | 耗时 |
|---|---|---|---|
| `Dockerfile`（源码构建） | 拉 golang:1.26.11（**1.23GB**）+ 下载全部 Go 依赖 + 编译 | 1.23GB 基础镜像 + 数百 MB 依赖 | 5~15 分钟，且依赖外网可达 |
| **`Dockerfile.binary`（本方案）** | 只 `COPY` 一个已编译好的二进制 | alpine:3.20（**12MB**） | **几秒** |

---

## 二、第一步：在开发机交叉编译

**在项目根目录执行**（Windows / macOS / Linux 都可以，不需要 Linux 环境）：

```bash
# x86_64 服务器（最常见的云服务器）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o dist/fantwo-core-linux-amd64 ./cmd/server

# ARM64 服务器（AWS Graviton / 阿里云倚天 / 华为鲲鹏）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags="-s -w" \
  -o dist/fantwo-core-linux-arm64 ./cmd/server
```

两个关键参数：

- **`CGO_ENABLED=0`** — 必须。生成静态二进制，才能在 alpine（musl libc）里跑。本项目图片处理用的是纯 Go 的 `disintegration/imaging`，没有 CGO 依赖，所以关掉 CGO 不影响任何功能。
- **`-ldflags="-s -w"`** — 可选，去掉调试符号和 DWARF，体积从 ~95MB 降到 ~72MB。

产物验证（在 Linux 上跑）：

```bash
file dist/fantwo-core-linux-amd64
# 期望：ELF 64-bit LSB executable, x86-64, statically linked
```

---

## 三、第二步：把文件传到服务器

只需要**两个文件**：

```
你的部署目录/
├── fantwo-core-linux-amd64     # 上面编译出来的二进制（71MB）
├── Dockerfile.binary           # 项目里的这个文件
└── docker-compose.binary.yml   # 可选，要一键起 PG/Redis 时用
```

用 `scp` 传：

```bash
scp dist/fantwo-core-linux-amd64 Dockerfile.binary docker-compose.binary.yml \
    user@your-server:/opt/fantwo/
```

> **注意**：`Dockerfile.binary` 里的 `ARG BINARY` 默认值是 `dist/fantwo-core-linux-amd64`（相对构建上下文）。如果你按上面把二进制放在构建目录根下，构建时要覆盖：
> ```bash
> docker build -f Dockerfile.binary --build-arg BINARY=fantwo-core-linux-amd64 -t fantwo-core:1.0.0 .
> ```
> 如果想保持默认值生效，就保持 `dist/` 目录结构。

---

## 四、第三步：构建镜像

```bash
cd /opt/fantwo

# 带 ffmpeg（视频压缩队列可用）—— 388MB
docker build -f Dockerfile.binary \
  --build-arg BINARY=fantwo-core-linux-amd64 \
  -t fantwo-core:1.0.0 .

# 不带 ffmpeg（省 ~176MB，video-compression 队列不可用）—— 212MB
docker build -f Dockerfile.binary \
  --build-arg BINARY=fantwo-core-linux-amd64 \
  --build-arg WITH_FFMPEG=false \
  -t fantwo-core:1.0.0-slim .
```

实测构建耗时：**约 5 秒**（alpine 已缓存的情况下），首次拉 alpine 也只要十几秒。

镜像内做的事只有三件：装 `ca-certificates tzdata curl`（+可选 ffmpeg）、拷二进制、建非 root 用户。

---

## 五、第四步：起服务

### 方式 A — 和 PG / Redis 一起（推荐）

```bash
cd /opt/fantwo

# 1) 准备 .env
cp .env.example .env
vim .env
#   必填两项：
#     JWT_SECRET=<openssl rand -hex 32 生成>
#     POSTGRES_PASSWORD=<自己定>
#   可选：
#     APP_PORT=3000              # 宿主机端口
#     APP_IMAGE=fantwo-core:1.0.0  # 与第三步的 tag 一致

# 2) 起栈
docker compose -f docker-compose.binary.yml up -d

# 3) 验证
curl http://127.0.0.1:3000/health
```

### 方式 B — 只跑 app，连外部已有的 PG / Redis

```bash
docker run -d --name fantwo-core \
  --restart unless-stopped \
  -p 3000:3000 \
  --add-host host.docker.internal:host-gateway \
  -e POSTGRES_HOST=host.docker.internal \
  -e POSTGRES_PORT=5432 \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=<你的密码> \
  -e POSTGRES_DATABASE=fantwo_core \
  -e POSTGRES_SSL_MODE=disable \
  -e REDIS_ADDR=host.docker.internal:6379 \
  -e JWT_SECRET=<openssl rand -hex 32> \
  -e SERVER_MODE=release \
  -e LOG_FORMAT=json \
  -v fantwo-uploads:/app/uploads \
  fantwo-core:1.0.0
```

> `--add-host host.docker.internal:host-gateway` 是 Linux 上访问宿主机服务的标准做法（Docker 20.10+）。PG/Redis 在别的机器上就直接填那台的 IP。

---

## 六、验证部署成功

```bash
# ① 容器健康（Dockerfile 里的 HEALTHCHECK 已配好）
docker ps --filter name=fantwo-core
# 期望 STATUS: Up xxx (healthy)

# ② 依赖连通
curl -s http://127.0.0.1:3000/health | python3 -m json.tool
# 期望 data.dependencies.postgres.status = "up"，redis.status = "up"

# ③ 配置种子已初始化（应为 56 条）
docker exec fantwo-postgres psql -U postgres -d fantwo_core \
  -c "SELECT count(*) FROM config WHERE deleted_at=0;"

# ④ 登录 + 读配置（验证业务真的通）
TOKEN=$(curl -s -X POST http://127.0.0.1:3000/api/v1/user/login \
  -H "Content-Type: application/json" -H "device-id: deploy-check" \
  -d '{"account":"<管理员账号>","password":"<密码>"}' \
  | grep -oE '"token":"[^"]+"' | cut -d'"' -f4)
curl -s http://127.0.0.1:3000/api/v1/config -H "Authorization: Bearer $TOKEN" | head -c 200
```

> 如果 `GET /config` 返回 401 或 403，说明账号还没挂权限——用现有管理员在管理端分配 `setting:read`。

---

## 七、升级流程（二进制方案最大的优势）

```bash
# 1) 开发机：改代码 → 重新交叉编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" \
  -o dist/fantwo-core-linux-amd64 ./cmd/server

# 2) 传到服务器（覆盖旧文件）
scp dist/fantwo-core-linux-amd64 user@server:/opt/fantwo/

# 3) 服务器：重新 build 一个新 tag（几秒）
cd /opt/fantwo
docker build -f Dockerfile.binary \
  --build-arg BINARY=fantwo-core-linux-amd64 \
  -t fantwo-core:1.0.1 .

# 4) 改 .env 里的 APP_IMAGE=fantwo-core:1.0.1，然后重建容器
docker compose -f docker-compose.binary.yml up -d app

# 5) 回滚：把 APP_IMAGE 改回 1.0.0 再 up -d app
```

**保留旧 tag 就是回滚能力**。不要在升级时复用同一个 tag（`docker build -t fantwo-core:latest` 覆盖掉），否则回滚时没镜像可用。

---

## 八、必须注意的几件事

### 1. 数据库迁移是自动的

服务启动时会 `AutoMigrate`（建表/改列）+ `EnsureSeeds`（插缺失的配置种子，幂等）。**不需要手工跑 SQL**。

但要注意：`EnsureSeeds` **只插不删**。若从老版本升级，历史废弃配置行不会被清掉，需手工清理（见 `docs/08-配置迁移运维手册.md` 第六节）。

### 2. 上传文件要挂卷

`docker-compose.binary.yml` 已把 `/app/uploads` 挂到 named volume `uploads`。如果你用 `docker run`，**必须**加 `-v`，否则容器重建后用户上传的图片全丢。

### 3. 时区

镜像设了 `TZ=Asia/Shanghai`。如果需要别的时区，在 compose 的 environment 里覆盖 `TZ`。

### 4. 端口不要只靠映射

`docker-compose.binary.yml` 里 PG 和 Redis 只映射到 `127.0.0.1`（不是 `0.0.0.0`），避免把 5432/6379 直接暴露在公网。**app 是唯一对外端口**，前面建议再套一层 Nginx / Caddy 做 TLS。

### 5. 国内服务器拉不到 Docker Hub

如果服务器拉 `postgres:18-alpine` / `redis:7-alpine` 失败（`failed to resolve reference ... EOF`），配一个镜像加速器：

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json <<'EOF'
{
  "registry-mirrors": ["https://docker.m.daocloud.io", "https://dockerproxy.com"]
}
EOF
sudo systemctl restart docker
```

或者直接用服务器上已有的镜像，把 compose 里的 `image:` 改成实际 tag（`docker images` 查）。

### 6. 二进制架构必须匹配

`GOARCH=amd64` 编译的二进制**不能**跑在 arm64 服务器上（反之亦然）。症状是容器起来就 `exec format error`。

```bash
# 查服务器架构
uname -m     # x86_64 → amd64；aarch64 → arm64
```

---

## 九、与源码构建方案的取舍

| 场景 | 推荐 |
|---|---|
| 服务器能稳定访问 Go 模块代理、不在意构建耗时 | `Dockerfile`（源码构建），CI/CD 里一条命令搞定 |
| **服务器没 Go 环境 / 外网受限 / 想快速迭代** | **`Dockerfile.binary`（本方案）** |
| 想让镜像构建完全可复现（同源码同镜像） | `Dockerfile`（源码构建） |
| 想在 CI 里统一编译 + 多平台发布 | 两者结合：CI 编译多架构二进制 → 分别用 `Dockerfile.binary` 打包 |

本项目镜像里跑的是 `CGO_ENABLED=0` 的静态二进制，**两种方案最终产物完全一致**（同一个二进制），差别只在「在哪编译」。

---

## 十、相关文件

| 文件 | 说明 |
|---|---|
| `Dockerfile` | 多阶段源码构建（原方案） |
| `Dockerfile.binary` | **本文档对应的预编译二进制方案** |
| `docker-compose.yml` | 源码构建版编排（含 Elasticsearch） |
| `docker-compose.binary.yml` | **预编译二进制版编排**（不含 ES） |
| `docs/08-配置迁移运维手册.md` | 配置怎么改、生效时间、老库清理 SQL |
| `.env.example` | 环境变量模板（含 DB 优先说明） |
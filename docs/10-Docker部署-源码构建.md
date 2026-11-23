# 10 · Docker 部署指南（源码构建，推荐）

> 目标：把服务部署到 Linux 服务器，用 Docker 运行。
> 本文档对应 `Dockerfile` + `docker-compose.yml`（容器内源码编译，一条命令搞定）。

---

> ⚠️ **本文档已拆分，请优先看新版本**
>
> 编排文件已按构建方式拆成两份显式文件（不再用单个 compose 靠 BUILD_TARGET 切换）：
>
> | 需求 | 编排 | env 模板 | 教程 |
> |---|---|---|---|
> | 容器内编译（宿主机零依赖） | `docker-compose.build.yml` | `.env.build.example` | `docs/12` |
> | 只跑预编译二进制（秒级重建） | `docker-compose.binary.yml` | `.env.binary.example` | `docs/12` |
>
> 本文档保留下来是因为其中**编排之外的章节**仍然有效且有价值
> （Nginx/Caddy 反代 + TLS 配置、日常运维命令、备份、国内镜像加速、故障排查）。
> 只是文中的文件名请按上表替换：`docker-compose.yml` → 对应模式的编排文件，
> `.env.example` → 对应模式的 env 模板。

## 一、为什么用源码构建

之前想过「开发机交叉编译 → 只把二进制放进容器」。实测两条路都通，但**生产推荐源码构建**：

| | 源码构建（`Dockerfile`） | 二进制挂载 |
|---|---|---|
| 服务器要拉 | golang:1.26.11（**1.23GB**）+ Go 依赖 | alpine（**12MB**） |
| 构建耗时 | 3~15 分钟（依赖已缓存时约 2 分钟） | 0 |
| 产物完整性 | 证书 237 个、时区 CST、curl 探针、ffmpeg、非 root | **证书仅 1 个、时区 UTC、无 curl/ffmpeg、root** |
| HTTPS 外呼（OAuth / SMTP / 云审核） | 正常 | **TLS 握手失败**（实测被降级到 http） |
| 可复现性 | 同源码同镜像 | 依赖开发机环境 |

**裸 alpine 挂二进制的实测缺陷**（我跑过）：

```
$ docker exec <裸挂容器> sh -c "ls /etc/ssl/certs/ | wc -l; ls /usr/share/zoneinfo/ | wc -l; date"
1              ← 正确应有 237 个
0              ← 完全没有时区数据
Mon Nov 23 15:35:22 UTC 2026    ← UTC，不是 Asia/Shanghai
```

时区错 8 小时会让「今日签到」边界、积分每日重置、日志时间戳全部偏移——这是静默故障，排查成本很高。

所以本文档走源码构建：**一条命令，环境完整，代价是首次构建几分钟**。

---

## 二、前置条件

| 项 | 要求 |
|---|---|
| Docker | 20.10+（含 `docker compose` 子命令，v2） |
| 内存 | ≥ 2GB（构建时 golang 编译峰值约 1.5GB） |
| 磁盘 | ≥ 5GB（镜像 + 数据卷） |
| 网络 | 能拉基础镜像 + Go 模块（国内需配加速器，见第六节） |

验证：

```bash
docker --version          # Docker version 20.10+
docker compose version    # Docker Compose version v2.x
```

---

## 三、部署步骤

### 3.1 传代码到服务器

```bash
# 方式一：git clone
git clone https://github.com/SugarLess-CN/fantwo-core.git /opt/fantwo-core
cd /opt/fantwo-core

# 方式二：本地打包上传（排除无需上传的内容）
tar --exclude=tmp --exclude=dist --exclude=.git --exclude=bin \
    -czf fantwo-core.tar.gz .
scp fantwo-core.tar.gz user@server:/opt/
ssh user@server 'cd /opt && tar xzf fantwo-core.tar.gz -C fantwo-core'
```

### 3.2 准备 .env

```bash
cd /opt/fantwo-core
cp .env.example .env
```

**必须改的两项**：

```bash
# 生成随机 JWT 密钥
openssl rand -hex 32
# 输出形如 7f3a9b2c... （64 个十六进制字符），填进 .env

vim .env
```

```ini
JWT_SECRET=7f3a9b2c...（上面生成的那串）
POSTGRES_PASSWORD=你的数据库密码
```

其余可留默认。常用可选项：

```ini
APP_PORT=3000                # 宿主机端口
TZ=Asia/Shanghai             # 时区
APP_IMAGE=fantwo-core:1.0.0  # 镜像 tag，便于回滚
SEARCH_ENABLED=false         # 不开 ES 时必须 false
SWAGGER_ENABLED=false        # 生产关掉
```

> `POSTGRES_PASSWORD` 和 `JWT_SECRET` 用了 `:?` 语法：**不填直接报错退出**，不会静默用默认值起一个不安全的服务。

### 3.3 构建并启动

```bash
docker compose up -d --build
```

首次会：拉 golang 基础镜像（1.23GB）→ `go mod download` → 编译 → 拉 alpine → 打运行镜像。**约 3~15 分钟**。

后续改代码只需：

```bash
docker compose up -d --build app     # 只重建 app，不动 PG/Redis
```

### 3.4 验证

```bash
# ① 容器状态
docker compose ps
# 期望：fantwo-core  Up (healthy)

# ② 健康检查
curl -s http://127.0.0.1:3000/health | python3 -m json.tool
# 期望 data.dependencies.postgres.status = "up"、redis.status = "up"

# ③ 配置种子已初始化（应为 56 条）
docker exec fantwo-postgres psql -U postgres -d fantwo_core \
  -c "SELECT count(*) FROM config WHERE deleted_at=0;"

# ④ 业务链路（登录 + 读配置）
TOKEN=$(curl -s -X POST http://127.0.0.1:3000/api/v1/user/login \
  -H "Content-Type: application/json" -H "device-id: deploy-check" \
  -d '{"account":"<管理员账号>","password":"<密码>"}' \
  | grep -oE '"token":"[^"]+"' | cut -d'"' -f4)
curl -s http://127.0.0.1:3000/api/v1/config -H "Authorization: Bearer $TOKEN" | head -c 200
```

---

## 四、编排说明

`docker-compose.yml` 起三个服务：

| 服务 | 镜像 | 端口（只绑回环） | 说明 |
|---|---|---|---|
| `postgres` | postgres:18-alpine | `127.0.0.1:5432` | 数据落 named volume `pgdata` |
| `redis` | redis:7-alpine | `127.0.0.1:6379` | 开 AOF 持久化，数据落 `redisdata` |
| `app` | 本地构建 | `0.0.0.0:${APP_PORT}` | 唯一对外端口，**建议前面套 Nginx/Caddy 做 TLS** |

**Elasticsearch 默认不启动**（`profiles: ["search"]`）：国内服务器基本拉不动 `docker.elastic.co`，且大部分场景不需要全文检索。要开：

```bash
docker compose --profile search up -d
# 并在 .env 里设 SEARCH_ENABLED=true
```

### 为什么 PG / Redis 只绑 127.0.0.1

```yaml
ports:
  - "127.0.0.1:5432:5432"     # 而不是 "5432:5432"
```

`"5432:5432"` 等价于 `0.0.0.0:5432`，会**把数据库直接暴露在公网**。容器之间走 compose 内部网络（`postgres:5432`），根本不需要映射到宿主机；映射只是为了你从宿主机用 `psql` 连进去调试。

需要远程连库（pgAdmin / Navicat）时，用 SSH 隧道，不要把端口开到公网：

```bash
ssh -L 5432:127.0.0.1:5432 user@your-server
# 然后本地连 127.0.0.1:5432
```

---

## 五、日常运维

### 5.1 查看日志

```bash
docker compose logs -f app          # 应用日志
docker compose logs --tail=100 app  # 最近 100 行
docker compose logs -f              # 全部服务
```

日志是 JSON 格式（`LOG_FORMAT=json`），方便接 Loki / ELK：

```json
{"level":"info","ts":1795448884,"caller":"middleware/logger.go:41","msg":"request","status":200,"path":"/api/v1/config","latency":"2.6ms"}
```

### 5.2 改配置（不需要重启）

**业务配置改了不重启**——走管理端接口：

```bash
curl -X PATCH http://127.0.0.1:3000/api/v1/config \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '[{"key":"upload_max_image_size","value":"7"}]'
```

只有「仅 env」的项（存储驱动 / S3 七项 / DB / Redis / JWT）才需要改 `.env` + 重建容器：

```bash
vim .env && docker compose up -d app
```

详见 `docs/08-配置迁移运维手册.md`。

### 5.3 备份

```bash
# 数据库
docker exec fantwo-postgres pg_dump -U postgres fantwo_core \
  | gzip > backup-$(date +%F).sql.gz

# 上传文件（named volume）
docker run --rm -v fantwo-core_uploads:/data -v $(pwd):/backup \
  alpine tar czf /backup/uploads-$(date +%F).tar.gz -C /data .

# 恢复数据库
gunzip -c backup-2026-11-23.sql.gz | \
  docker exec -i fantwo-postgres psql -U postgres -d fantwo_core
```

> volume 名前缀是 compose 项目名（`name: fantwo-core`），所以是 `fantwo-core_pgdata` / `fantwo-core_uploads`。用 `docker volume ls` 确认。

### 5.4 升级

```bash
cd /opt/fantwo-core

# 1) 拉新代码
git pull

# 2) 用新 tag 重建（保留旧 tag = 回滚能力）
sed -i 's/APP_IMAGE=.*/APP_IMAGE=fantwo-core:1.0.1/' .env
docker compose up -d --build app

# 3) 回滚：把 APP_IMAGE 改回旧 tag，再 up -d
```

**数据库迁移是自动的**：启动时 `AutoMigrate` 建表/改列 + `EnsureSeeds` 插缺失的配置种子（幂等）。

> `EnsureSeeds` **只插不删**。从老版本升级时，历史废弃配置行不会被清掉，需手工清理（见 `docs/08` 第六节）。

### 5.5 停止

```bash
docker compose stop            # 停，保留容器和数据
docker compose down            # 删容器，保留数据卷
docker compose down -v         # 连数据卷一起删（会丢数据库和上传文件，慎用！）
```

---

## 六、国内服务器必读

### 6.1 拉不到 Docker Hub

症状：

```
Error response from daemon: failed to resolve reference "docker.io/library/redis:7-alpine":
failed to do request: Head "https://registry-1.docker.io/v2/library/redis/manifests/7-alpine": EOF
```

三种解法，任选：

**解法 A：配镜像加速器（推荐）**

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json <<'EOF'
{
  "registry-mirrors": [
    "https://docker.m.daocloud.io",
    "https://dockerproxy.com"
  ]
}
EOF
sudo systemctl daemon-reload
sudo systemctl restart docker
```

**解法 B：用国内镜像站前缀覆盖 image**

```bash
# .env 里加
POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:18-alpine
REDIS_IMAGE=docker.m.daocloud.io/library/redis:7-alpine
```

**解法 C：用宿主机上已有的镜像**

```bash
docker images | grep -E "postgres|redis"
# 假设已有 postgres:latest 和 redis:latest

# .env 里加
POSTGRES_IMAGE=postgres:latest
REDIS_IMAGE=redis:latest
```

> 三种解法都通过 compose 的 `${VAR:-默认值}` 语法生效，**不用改 compose 文件本身**。

### 6.2 Go 模块下载慢

`Dockerfile` 里已默认 `GOPROXY=https://goproxy.cn,direct`。海外服务器可覆盖：

```bash
docker compose build --build-arg GOPROXY=https://proxy.golang.org,direct app
```

### 6.3 apk 源

`Dockerfile` 里已把 alpine 源换成阿里云镜像（`mirrors.aliyun.com`），无需额外配置。

---

## 七、进阶：Nginx / Caddy 反向代理 + TLS

app 容器只监听 HTTP 3000。生产请在前面加一层 TLS 终止。

**Caddy（自动申请 Let's Encrypt 证书，最省事）**

`Caddyfile`：

```
api.your-domain.com {
    reverse_proxy 127.0.0.1:3000
}
```

```bash
docker run -d --name caddy \
  --network host \
  -v $PWD/Caddyfile:/etc/caddy/Caddyfile \
  -v caddy-data:/data \
  caddy:2
```

**Nginx**

```nginx
server {
    listen 443 ssl http2;
    server_name api.your-domain.com;

    ssl_certificate     /etc/letsencrypt/live/api.your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.your-domain.com/privkey.pem;

    client_max_body_size 600M;   # 视频上传上限（对应 upload_max_video_size=500MB）

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket（消息域的实时推送需要）
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }
}
```

**两个必须注意的点**：

1. **`client_max_body_size`**：不设的话 nginx 默认 1MB，视频上传直接 413。要 ≥ `upload_max_video_size`
2. **WebSocket 升级头**：`/ws` 端点不加 `Upgrade`/`Connection` 头会握手失败，实时消息全断

---

## 八、故障排查

| 现象 | 排查 |
|---|---|
| 容器 `Up` 但不 `healthy` | `docker compose logs app` 看启动日志；`curl localhost:3000/health` 看依赖状态 |
| `JWT_SECRET 必须设置` 报错退出 | `.env` 里没填，`openssl rand -hex 32` 生成一个 |
| 数据库连不上 | `docker compose ps` 看 postgres 是否 healthy；`docker compose logs postgres` |
| `exec format error` | 用了别的架构的基础镜像（少见）；改用默认的 `postgres:18-alpine` |
| 端口被占用 | 改 `.env` 的 `APP_PORT` / `POSTGRES_PORT` / `REDIS_PORT` |
| 上传大文件 413 | nginx 的 `client_max_body_size` 没设 |
| WebSocket 连不上 | nginx 缺 `Upgrade` / `Connection` 头 |
| 时区不对 | `.env` 里设 `TZ=Asia/Shanghai`，然后 `docker compose up -d` 重建容器 |

---

## 九、文件清单

| 文件 | 说明 |
|---|---|
| `Dockerfile` | 多阶段源码构建（本文档使用） |
| `docker-compose.yml` | 生产编排（PG + Redis + app，ES 可选） |
| `.env.example` | 环境变量模板（含配置项说明） |
| `Dockerfile.binary` | 备选：预编译二进制方案（容器内不编译） |
| `docker-compose.binary.yml` | 备选：预编译二进制编排 |
| `docs/09-Docker部署-预编译二进制.md` | 备选方案详解 |
| `docs/08-配置迁移运维手册.md` | 配置怎么改、生效时间、老库清理 SQL |

---

## 十、实测记录（本机 Docker Desktop）

```
# 构建
$ docker build -t fantwo-core:src-build-test .
→ 成功，388MB（含 ffmpeg）
→ 构建阶段用了本地缓存 golang:1.26.11-bookworm，实际约 2 分钟

# 运行
$ docker run -d --name fantwo-deploy-test -p 3995:3000 \
    -e POSTGRES_HOST=host.docker.internal ... fantwo-core:src-build-test
→ Up 15 seconds (healthy)

# 健康
$ curl http://127.0.0.1:3995/health
→ postgres up / redis up / queue available

# 容器内环境（对比裸挂载方案）
$ docker exec fantwo-deploy-test date
Mon Nov 23 23:48:05 CST 2026          ← 时区正确（裸挂载是 UTC）
$ docker exec fantwo-deploy-test sh -c "ls /etc/ssl/certs/ | wc -l"
237                                     ← 证书齐全（裸挂载只有 1 个）

# 业务链路
$ 登录 → token 2320 字节
$ 配置条数 → 56
$ 打码条数 → 5（aliyun_access_key_id / aliyun_access_key_secret /
                tencent_secret_id / tencent_secret_key / search_password）
```

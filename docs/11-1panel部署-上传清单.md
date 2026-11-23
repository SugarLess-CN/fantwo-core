# 11 · 1panel 部署：上传哪些文件

> 目标：在 1panel 的容器编排里部署本服务。
> 上传**包**而不是整个仓库——整包约 1.8MB，234 个文件。

---

## 一、结论：只上传这些

```
fantwo-core-deploy-<日期>.tar.gz        ← 唯一要上传的文件（1.8MB）
```

解压后是这样的目录结构，**都在里面了**：

```
/opt/fantwo-core/
├── cmd/                      ← 入口 main.go
├── internal/                 ← 204 个 .go 文件（全部业务代码）
├── migrations/               ← 8 个文件（种子数据，main.go 直接 import）
├── docs/                     ← 983KB
│   └── swagger/              ← 关键！docs.go 被 swagger_routes.go 空导入
├── go.mod / go.sum           ← 依赖清单
├── Dockerfile.binary         ← 多阶段构建（from-source / from-binary）
├── docker-compose.yml        ← 编排
├── .dockerignore             ← 构建上下文裁剪
├── .env.example              ← 配置模板（服务器上 cp 成 .env）
└── README.md
```

---

## 二、**不要**上传的（说明原因）

| 不传 | 原因 |
|---|---|
| `tmp/` | 原仓库克隆 + 临时脚本，**几十 MB**，与运行完全无关 |
| `.git/` | 版本历史，服务器不需要（且可能几百 MB） |
| `reports/` | 过程性汇报文档，运维不需要 |
| `test/` | 集成测试集中在这个目录，用 `build tag` 隔离，容器构建不会碰它 |
| `dist/` | 交叉编译产物，用 `from-source` 模式时不需要 |
| `uploads/` | 本地运行产生的上传文件 |
| `AGENTS.md` / `todo.md` | 治理文档，本来就不入仓库 |
| `Makefile` | 可选，服务器上不会用它（直接用 docker compose） |
| `Dockerfile` | 旧的源码构建版，已被 `Dockerfile.binary` 统一取代 |

---

## 三、为什么 `docs/` 必须传（最容易漏的一项）

**代码里有空导入**：

```go
// internal/router/swagger_routes.go:26
_ "github.com/SugarLess-CN/fantwo-core/docs/swagger"
```

作用是 `swag init` 生成的 `docs.go` 会在 `init()` 里把 swagger 文档注册进 swaggo 的全局表。**不传 `docs/swagger/` 就编译失败**：

```
no required module provides package github.com/SugarLess-CN/fantwo-core/docs/swagger
```

> `.dockerignore` 里排除了 `*.md`，但 `docs/swagger/docs.go` 是 `.go` 文件，不会被排除。这条规则踩过坑，注释里已经写明。

---

## 四、`migrations/` 也必须传

`cmd/server/main.go:44` 直接 import：

```go
"github.com/SugarLess-CN/fantwo-core/migrations"
```

它导出成就码清单和种子数据（`AchievementCodes()` 等），服务启动时用。不传同样编译失败。

---

## 五、打包命令

### 方式 A：用脚本（推荐）

```bash
# 在开发机项目根目录
bash scripts/make-deploy-pkg.sh
```

输出：

```
==> 完成
    文件：dist/fantwo-core-deploy-20261123.tar.gz
    大小：1.8M
    文件数：234
```

### 方式 B：手工打包

```bash
tar -czf fantwo-core-deploy.tar.gz \
    cmd internal migrations docs \
    go.mod go.sum \
    Dockerfile.binary docker-compose.yml .dockerignore .env.example README.md
```

> 注意 `tar` 一定要显式列出 `.dockerignore` 和 `.env.example`——以 `.` 开头的文件容易被 `*` 通配漏掉。

---

## 六、1panel 上的操作步骤

### 6.1 上传解压

```bash
# 开发机
scp dist/fantwo-core-deploy-20261123.tar.gz root@<服务器IP>:/opt/

# 服务器（或 1panel 的「文件」功能直接上传）
mkdir -p /opt/fantwo-core
tar xzf /opt/fantwo-core-deploy-20261123.tar.gz -C /opt/fantwo-core
ls /opt/fantwo-core
# 应该看到：cmd internal migrations docs go.mod go.sum Dockerfile.binary docker-compose.yml ...
```

### 6.2 配 .env

```bash
cd /opt/fantwo-core
cp .env.example .env
openssl rand -hex 32          # 复制输出
vim .env
```

必填两项：

```ini
JWT_SECRET=<上面 openssl 生成的那串>
POSTGRES_PASSWORD=<自己定>
```

国内服务器如果拉不到 Docker Hub，再加上：

```ini
POSTGRES_IMAGE=postgres:latest        # 用 1panel 应用商店已装的镜像
REDIS_IMAGE=redis:latest
```

> 1panel 应用商店通常已经装了 PostgreSQL 和 Redis。可以直接用那两个容器的镜像名，`docker images` 查实际 tag。

### 6.3 用 1panel 编排部署

**路径**：1panel 面板 → 容器 → 编排 → 创建编排

| 字段 | 填什么 |
|---|---|
| 名称 | `fantwo-core` |
| 编排模板 | 选「本地目录」 |
| 目录 | `/opt/fantwo-core` |
| 编排文件 | `docker-compose.yml` |

或者直接在服务器上跑：

```bash
cd /opt/fantwo-core
docker compose up -d --build
```

**首次构建约 3~10 分钟**（拉 golang:1.26.11 1.23GB + go mod download + 编译）。

### 6.4 验证

```bash
# ① 容器状态（healthy 才算成功）
docker compose ps
# 期望：fantwo-core  Up (healthy)

# ② 健康检查
curl -s http://127.0.0.1:3000/health | python3 -m json.tool
# 期望 data.dependencies.postgres.status = "up"、redis.status = "up"

# ③ 配置种子（应为 56 条）
docker exec fantwo-postgres psql -U postgres -d fantwo_core \
  -c "SELECT count(*) FROM config WHERE deleted_at=0;"
```

---

## 七、1panel 特有注意事项

### 7.1 端口冲突

1panel 应用商店装的 PostgreSQL / Redis 已经占了 5432 / 6379。本项目的 compose 里也起了这两个服务，**会冲突**。

三个解法：

**解法 A（推荐）：用 1panel 已装的 PG/Redis**

删掉 compose 里的 `postgres` 和 `redis` 两个服务，app 改成连外部：

```yaml
# docker-compose.yml 里只保留 app
services:
  app:
    environment:
      POSTGRES_HOST: 172.17.0.1        # 宿主机在 docker bridge 上的地址
      # 或 1panel 里 PG 容器的 IP/容器名
      POSTGRES_PORT: "5432"
      REDIS_ADDR: 172.17.0.1:6379
```

更稳妥的办法：把 app 加入 1panel 已有容器的网络：

```bash
docker network ls | grep 1panel
# 假设叫 1panel-network
docker network connect 1panel-network fantwo-core
# 然后 POSTGRES_HOST 直接填 1panel 里 PG 的容器名
```

**解法 B：改端口**

`.env` 里改：

```ini
POSTGRES_PORT=15432
REDIS_PORT=16379
```

**解法 C：不用 compose 的 PG/Redis**，只跑 app

```bash
docker compose up -d app
```

### 7.2 数据卷位置

compose 用了 named volume（`pgdata` / `uploads`），实际落在 `/var/lib/docker/volumes/fantwo-core_*`。

1panel 的「容器 → 存储卷」能看到。**备份要备这两个**。

如果想改用 1panel 的目录管理（推荐，方便备份），把 compose 里改成绑定挂载：

```yaml
volumes:
  - ./data/pgdata:/var/lib/postgresql/data      # postgres
  - ./data/uploads:/app/uploads                 # app
```

然后在 1panel「文件」里能直接看到、打包备份。

### 7.3 反向代理 + HTTPS

1panel 自带「网站 → 反向代理」，比手写 Nginx 配置省事：

1. 网站 → 创建网站 → 反向代理
2. 代理地址：`http://127.0.0.1:3000`
3. SSL → 申请 Let's Encrypt 证书

**两个必须手动加的配置**（1panel 的反代默认不带）：

`client_max_body_size 600M;` —— 不设的话视频上传 413（默认 1MB）

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

—— `/ws` 端点不加这三行，WebSocket 握手失败，实时消息全断。

在 1panel 的「网站 → 配置文件」里加到 `location /` 段内。

### 7.4 防火墙

只放行反代端口（80/443）和 app 端口（如果直接暴露）。**不要**放行 5432 / 6379——compose 里已经绑了 `127.0.0.1`，但保险起见确认一下 1panel 的防火墙规则。

---

## 八、升级与回滚

```bash
cd /opt/fantwo-core

# 1) 上传新包（或 git pull，如果你用 git 方式）
scp fantwo-core-deploy-20261201.tar.gz root@server:/opt/
tar xzf /opt/fantwo-core-deploy-20261201.tar.gz -C /opt/fantwo-core
# 注意：tar 解压会覆盖代码，但不会删 .env（.env 不在包里）

# 2) 用新 tag 重建
sed -i 's/APP_IMAGE=.*/APP_IMAGE=fantwo-core:1.0.1/' .env
docker compose up -d --build app

# 3) 回滚
sed -i 's/APP_IMAGE=.*/APP_IMAGE=fantwo-core:1.0.0/' .env
docker compose up -d app
```

**数据库迁移自动做**（启动时 `AutoMigrate` + `EnsureSeeds`，都是幂等的）。但 `EnsureSeeds` **只插不删**，老版本废弃的配置行需要手工清理（见 `docs/08` 第六节）。

---

## 九、包内容清单（234 个文件，1.8MB）

| 路径 | 文件数 | 大小 | 必需性 |
|---|---|---|---|
| `internal/` | 204 | 3.1MB | **必需**（全部业务代码） |
| `docs/` | 14 | 983KB | **必需**（`docs/swagger/docs.go` 被空导入） |
| `migrations/` | 8 | 16KB | **必需**（`main.go` 直接 import） |
| `cmd/` | 1 | 5KB | **必需**（入口） |
| `go.mod` / `go.sum` | 2 | 53KB | **必需**（依赖） |
| `Dockerfile.binary` | 1 | 5KB | **必需**（构建） |
| `docker-compose.yml` | 1 | 6.7KB | **必需**（编排） |
| `.dockerignore` | 1 | 469B | 必需（裁剪构建上下文，不加会慢） |
| `.env.example` | 1 | 7.5KB | 必需（配置模板） |
| `README.md` | 1 | — | 可选（说明） |

压缩后 1.8MB（tar.gz）。

---

## 十、实测验证

用打好的包做了一次完整构建，证明**包是自足的**：

```
$ tar -xzf fantwo-core-deploy.tar.gz -C /tmp/verify-pkg
$ ls
Dockerfile.binary  README.md  cmd  docker-compose.yml  docs  go.mod  go.sum  internal  migrations

$ docker build -f Dockerfile.binary --target from-source -t fantwo-core:pkgtest .
→ 成功（代码完整、依赖可解、编译通过）
```

> `config/` 目录没打进去——里面只有一个 `config.yaml`（本地开发用的 ES 地址），生产通过环境变量配，不需要。如果想让包更完整，可以加上 `config/`（1 个文件 133 字节）。

# fantwo-core

PicArt 社区后端的 Go + Gin 重构（原 NestJS 项目 `coruni/picart`）。权威输入：`tmp/picart/swagger.json` + `tmp/picart/src`，功能清单见 `docs/`。

## 技术栈

gin / gorm(PostgreSQL) / golang-jwt/v5 / go-redis/v9 / asynq / viper / zap / gorilla/websocket，详见 `docs/03-技术选型.md`。

## 快速开始

```bash
# 1. 配置：复制模板为 config.yaml（已入 .gitignore），或直接用环境变量覆盖
cp internal/config/config.example.yaml config.yaml

# 2. 运行（无 PostgreSQL/Redis 也能起：DB/Redis 懒连接，缺失只打 warning，health 照常）
go run ./cmd/server

# 3. 验证
curl http://127.0.0.1:3000/health   # {"code":0,"message":"ok","data":{...}}
curl http://127.0.0.1:3000/ping
```

端口默认 3000；环境变量覆盖规则：层级键点号转下划线（`postgres.host` → `POSTGRES_HOST`）。

## API 文档（Swagger）

Swagger UI 由 swaggo 生成，**默认关闭**；开发环境显式开启后再访问：

```bash
# 1) 装 CLI（一次性；不进 go.mod）
go install github.com/swaggo/swag/cmd/swag@latest

# 2) 生成文档到 docs/swagger/（改了 handler 注解后必须重跑，产物入库）
swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal

# 3) 开启 UI 并启动
SWAGGER_ENABLED=true go run ./cmd/server
# 或 config.yaml 里设 swagger.enabled: true

# 4) 访问（两条路径等价，均可用）
open http://127.0.0.1:3000/swagger/index.html            # 根路径（推荐）
open http://127.0.0.1:3000/api/v1/swagger/index.html     # 业务前缀下的别名
curl http://127.0.0.1:3000/swagger/doc.json              # 原始 OpenAPI
```

- 开关：`swagger.enabled` / `SWAGGER_ENABLED`（默认 `false`，**生产不要开**）。
- 注册：`internal/router/swagger_routes.go` 的 `RegisterSwaggerRoutes(r gin.IRouter, cfg)`；
  参数用 `gin.IRouter` 接口，故 `*gin.Engine`（根路径）与 `*gin.RouterGroup`（业务前缀）都能传。
  当前两处各调用一次：`router.go` 挂根路径、`registry.go` 挂 `/api/v1` 别名。
  如只想保留根路径，删掉 `registry.go` 末尾那一行即可。
- `docs/swagger/` 是生成物但**入库**：它是交付物，且让未装 swag CLI 的人也能直接看文档。
  改动注解后务必重跑第 2 步，否则文档与代码不一致。
- 注解现状：已覆盖 user/auth、article、comment、order、payment、message 的核心端点
  （23 路径 / 30 操作 / 7 个 tag）；其余域按业务批次逐步补齐。
  新增端点写法：在 handler 函数上方注释块内加 `@Summary/@Tags/@Param/@Success/@Router`，
  响应类型统一引用 `handler.ResponseBody`（分页用 `handler.ResponsePageBody`）。

## 健康检查与可观测

`GET /health` 恒返回 HTTP 200 + `{code:0}`（探针语义：依赖抖动不触发重启），
依赖状态与运行时摘要放在 `data` 内：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "status": "up", "timestamp": 1795056624405, "uptime": 128,
    "dependencies": { "postgres": {"status":"up"}, "redis": {"status":"down","error":"..."} },
    "websocket": { "connections": 12, "rooms": 9 },
    "queue": { "available": true, "queues": [{"name":"image-audit","pending":3,"active":1,"retry":0,"archived":0}], "totals": {"pending":3} }
  }
}
```

- WS 连接数/房间数来自 `internal/ws` 的进程级原子计数（无锁，见 `hub.go`）。
- 队列统计来自 asynq `Inspector`（`internal/queue/stats.go`），Redis 不可用时 `queue.available=false`。
- `GET /ping` 是更轻的探活（不触达任何依赖），适合 liveness probe。

## 目录导览

```
cmd/server/          入口：配置→DB/Redis(懒连接)→迁移→路由→优雅退出
internal/config/     viper 配置（config.yaml + env 覆盖；模板 config.example.yaml）
internal/database/   GORM(PostgreSQL) 与 go-redis 初始化（懒连接、失败降级）
internal/model/      GORM 实体（BaseModel：UUID 主键 + 软删）
internal/handler/    HTTP 处理层（解析/校验/响应，不写业务；swagger_types.go 为文档展示类型）
internal/service/    业务逻辑层（规则/事务/事件/队列投递）
internal/repository/ 数据访问层（GORM 查询封装）
internal/middleware/ JWT 鉴权(强制/可选/RBAC)、CORS、zap 日志、recovery
internal/pkg/jwtx/     JWT 签发与解析（claims 含 roles/permissions）
internal/pkg/response/ 统一响应 {code,message,data}（对齐原仓库）
internal/pkg/errcode/  错误码体系（code=HTTP 状态码 + 1000+ 业务段）
internal/router/     路由注册（/api/v1 前缀 + /health + /ping + Swagger UI）
internal/ws/         WebSocket hub（gorilla，对应原 /ws-message，含连接/房间原子计数）
internal/queue/      asynq 队列（image-audit / text-audit / video-compression）+ stats.go 可观测
migrations/          GORM AutoMigrate 入口（新实体在此注册）
docs/                功能清单、API 清单、技术选型、骨架说明
docs/swagger/        swaggo 生成的 OpenAPI 文档（生成物，入库）
```

## 约定

- 代码注释中文、标识符英文；临时文件进 `tmp/`（不入 git）。
- 响应包装：成功 `{code:200,message:"操作成功",data}`；失败 `{code,message,data:null,timestamp,path}`；health/ping 用 `{code:0,message:"ok"}`。
- 数据库 PostgreSQL，全新建库走 AutoMigrate（`migrations.Run`）。
- 权限点命名 `模块:动作`（如 `article:create`），多权限 OR 关系，JWT 内携带不查库。

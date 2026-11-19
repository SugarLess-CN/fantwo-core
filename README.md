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

## 目录导览

```
cmd/server/          入口：配置→DB/Redis(懒连接)→迁移→路由→优雅退出
internal/config/     viper 配置（config.yaml + env 覆盖；模板 config.example.yaml）
internal/database/   GORM(PostgreSQL) 与 go-redis 初始化（懒连接、失败降级）
internal/model/      GORM 实体（BaseModel：UUID 主键 + 软删；sample 为骨架示例）
internal/handler/    HTTP 处理层（解析/校验/响应，不写业务）
internal/service/    业务逻辑层（规则/事务/事件/队列投递）
internal/repository/ 数据访问层（GORM 查询封装）
internal/middleware/ JWT 鉴权(强制/可选/RBAC)、CORS、zap 日志、recovery
internal/pkg/jwtx/     JWT 签发与解析（claims 含 roles/permissions）
internal/pkg/response/ 统一响应 {code,message,data}（对齐原仓库）
internal/pkg/errcode/  错误码体系（code=HTTP 状态码 + 1000+ 业务段）
internal/router/     路由注册（/api/v1 前缀 + /health + /ping）
internal/ws/         WebSocket hub（gorilla，对应原 /ws-message，事件名占位）
internal/queue/      asynq 队列（image-audit / text-audit / video-compression）
migrations/          GORM AutoMigrate 入口（新实体在此注册）
docs/                功能清单、API 清单、技术选型、骨架说明
```

## 约定

- 代码注释中文、标识符英文；临时文件进 `tmp/`（不入 git）。
- 响应包装：成功 `{code:200,message:"操作成功",data}`；失败 `{code,message,data:null,timestamp,path}`；health/ping 用 `{code:0,message:"ok"}`。
- 数据库 PostgreSQL，全新建库走 AutoMigrate（`migrations.Run`）。
- 权限点命名 `模块:动作`（如 `article:create`），多权限 OR 关系，JWT 内携带不查库。

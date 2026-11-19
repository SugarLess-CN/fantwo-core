# 社区后端（fantwo-core）

Go + Gin 重构 coruni/picart 社区后端，复刻原 NestJS 服务全部功能；新仓库 https://github.com/SugarLess-CN/fantwo-core.git

## 职责与边界

- 负责：从原 NestJS 后端（https://github.com/coruni/picart.git）迁移全部业务功能到 Go + Gin；功能清单与 API 语义文档化。
- 不负责：前端、原 NestJS 仓库的功能演进、线上部署运维。
- API 文档权威来源：**原仓库自带 `swagger.json`**（OpenAPI 3.0.0，title `PicArt API`，description `PicArt 图片社区 API 文档`，**211 路径 / 275 操作 / 24 个 tag / 23 个业务域**），位于 `tmp/picart/swagger.json`；已整理进 `docs/02-API文档清单.md`。它与源码 `tmp/picart/src` 共同构成唯一事实来源。
- Apifox MCP（serverName `apifox-picart`，`.dsh/mcp.yml`）：**当前 `--project=6542782` 指向的是另一个电商系统任务的项目，不属于 picart**；picart 专属 MCP 尚未配置。在 picart 自己的 project id 就位前，该 MCP 返回的任何内容**不得**用于 picart 的范围界定、接口实现或字段设计。MCP 只读，且不得通过 MCP 写文档。

## 项目概览

- 技术栈：Go（gin 为主框架）+ 第三方库优先；原项目为 NestJS + TypeScript + TypeORM + PostgreSQL + Redis(Bull/Keyv) + Elasticsearch + Socket.io。**重构目标库：PostgreSQL（用户 2026-11-19 指定，替换此前 MySQL 选型）；不需兼容存量数据，全新建库用 AutoMigrate。**
- 目标仓库：`https://github.com/SugarLess-CN/fantwo-core.git`（远程 origin）。
- 原仓库：`https://github.com/coruni/picart.git`（只读参考，克隆到 `tmp/` 作为参考，不上传 git）。
- 目录地图：
  - `tmp/`：所有临时文件、原仓库克隆、调研草稿、构建中间件；**不上传 git**。
  - `docs/`：正式功能总结、架构设计、API 映射等可交付文档。
  - 根目录 `AGENTS.md` / `todo.md`：治理中枢。
- 全局约束：
  1. **临时文件一律写入 `tmp/`，不上传 git**（`.gitignore` 排除 `tmp/`）。
  2. 每次完成任务要总结，并 git commit + push。
  3. AGENTS.md 与 todo.md 要及时更新（同一任务内落盘）。
  4. 模块化实现，**能用第三方功能的优先用第三方，不全部自己写**；动手前先搜索调研并落盘结论。
  5. 只使用**已核验身份**的文档源；当前 picart 权威源为 `tmp/picart/swagger.json` + 源码。MCP 仅只读，且不得用于 picart 范围界定（见上）。

## 模块清单

- `AGENTS.md`：本文件，整棵树的入口与全局规则。
- `todo.md`：总账本，挂跨目录任务与阶段进度。
- `.dsh/mcp.yml`：项目级 MCP 配置（`apifox-picart` → 实际 project=6542782，**属另一电商系统，非 picart**；picart 专属 MCP 待配置）。
- `tmp/`（豁免：临时目录，忽略且无独立职责；内容见本级与 `docs/`）。
- `docs/`（后续按需建文档；若形成独立模块再建 AGENTS.md）。

## 子目录索引

- `tmp/`（豁免：临时文件与原仓库克隆，均不上传 git）
- `docs/`：正式交付文档目录
  - `01-原项目功能清单.md`：基于源码的 picart 功能考古，含 23 模块 / 25 controller / 48 entity / 鉴权 / 队列 / WS 事件
  - `02-API文档清单.md`：基于 `swagger.json` 的 211 路径 / 275 操作整理

## 约定与陷阱

- **第三方优先（硬规则）**：实现某能力前，先搜索 GitHub / 官方库 / Go 生态，确认是否有成熟稳定方案；优先顺序：项目已有实现 → 依赖已有能力 → 生态成熟库 → 自己写。选型结论写入 `docs/` 与本文件 `## 约定与陷阱`，注明候选、许可证、为何采用/否掉。
- **大功能必查，小功能免查**：独立功能面（认证、限流、ORM、缓存、队列、文件存储、短信、任务调度等）动第一行代码前必须先调研；拼写、小工具函数、明确 bug 修复可直接写。
- **MCP 只读**：禁止通过 MCP 写接口/改文档；MCP 仅用于读取。
- **陷阱：数据源身份必须先验证再采信**。2026-11-17 曾把 `apifox-picart` 这个 serverName 当作“就是 picart”的依据，误采信了另一电商项目的 150 个端点文档。教训：serverName / 配置名是人写的，不能当项目身份证据；OpenAPI 返回的 `info.title` 也可能只是「默认模块」。验证手段：比对 `info.title`/`description` 与目标项目名、比对仓库自带 `swagger.json` 的路径集合、抽样看关键字（如 picart 应有 `/article` `/comment`，不应有 `guild` `profit` `cart` `douyin`）。身份未验证的第三方文档一律标注「未核验」并禁止进入实现依据。
- **picart 身份基线（用于快速识别错源）**：`info.title` = `PicArt API`，`description` = `PicArt 图片社区 API 文档`，全局前缀 `api/v1`，响应包装 `{code,message,data}`，含用户/文章/评论/合集/积分/成就/装饰品/表情包/标签/分类/轮播/举报/邀请/搜索/统计/上传/订单/支付/内容审核/权限/角色/配置/信息 共 23 域。
- **写入范围**：默认只在工作区目录树内部读写；工作区外需用户当轮明确同意。
- 中文思考与回复；代码、命令、路径、专有名词保持原样。
- 每次 git 提交前确认 `tmp/` 未被追踪。

## 变更记录

- 2026-11-17 建立根治理文档：第三方优先调研、tmp 不上传、每轮 git 提交、MCP 只读规则落盘。
- 2026-11-17 事故修正：将 `apifox-picart`／project 6542782 误认为 picart 数据源，实际属另一电商任务；改以仓库自带 `swagger.json`（PicArt API，211 路径）为权威，新增「数据源身份先验证」陷阱条款。同时修正技术栈：原仓库为 NestJS/TypeScript，非 Rust。
- 2026-11-17 T-04 完成：`docs/03-技术选型.md` 落盘（gin/gorm/asynq/gorilla/官方云 SDK 等，全部 go list 实测）；关键结论含 Node bcrypt 哈希零迁移兼容、Bull→asynq 队列平移、Socket.io→gorilla 自研 hub。
- 2026-11-17 T-03a 完成：`docs/01-原项目功能清单.md`（源码考古，23 模块/25 controller/48 entity）。T-03b 完成：`docs/02-API文档清单.md`（swagger 权威版，211 路径/275 操作/74 schema/25 tag）；污染版已归档 `tmp/02-电商6542782-API快照-作废-勿用于picart.md`。
- 2026-11-19 用户决策落盘：①Apifox MCP 搁置不管，swagger.json + 源码为唯一权威；②数据库改 **PostgreSQL**（gorm driver/postgres），替换 MySQL 选型；③**不需兼容存量数据**，全新建库 AutoMigrate。docs/03 已同步修订。

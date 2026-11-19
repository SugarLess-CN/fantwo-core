# 社区后端（fantwo-core）

Go + Gin 重构 coruni/picart 社区后端，复刻原 NestJS 服务全部功能；新仓库 https://github.com/SugarLess-CN/fantwo-core.git

## 职责与边界

- 负责：从原 NestJS 后端（https://github.com/coruni/picart.git）迁移全部业务功能到 Go + Gin；功能清单与 API 语义文档化。
- 不负责：前端、原 NestJS 仓库的功能演进、线上部署运维。
- API 文档权威来源：**原仓库自带 `swagger.json`**（OpenAPI 3.0.0，title `PicArt API`，description `PicArt 图片社区 API 文档`，**211 路径 / 275 操作 / 24 个 tag / 23 个业务域**），位于 `tmp/picart/swagger.json`；已整理进 `docs/02-API文档清单.md`。它与源码 `tmp/picart/src` 共同构成唯一事实来源。
- Apifox MCP 双服务器，身份已核清（用户 2026-11-19 三次纠正）：
  - `apifox-office`（工具后缀 p122zz，project 6542782）：**另一电商系统任务的项目，与 picart 无关，永久禁用**——曾两次误当 picart 数据源，据此派发的电商批次已全部撤回。
  - `apifox-picart`（工具后缀 buvy0r/abp1z0）：**这才是 picart 的 MCP**，内容即 picart 社区系统 API 文档；首次 ref 读取报「broken link」，需先调 `refresh_project_oas` 重新拉取，可用后作为 swagger.json 之外的交叉校验源。picart **没有电商面**，范围 = 社区系统（swagger.json 211 路径 + 源码）。
  - MCP 一律只读，不得用于写文档。

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
- **子智能体纪律（硬规则，用户 2026-11-19 指定）**：①所有派发用 `spawn_teammate`（持久成员、可复用），禁止一次性 subagent/subagent_fork；②**派发后不等待、不轮询、不阻塞**——lead 立即返回做其它任务（规划、下一步派发、文档、验收），成员完成后其回报会自动送达；③lead 的工作循环：派发→（不等）→处理回报→验收→下一任务，任何时刻手上都有可推进的事；④成员回报只用 send_message 异步送达，lead 不为单个任务空转。
- **成员按业务域命名并复用**如 `skeleton`、`auth-user`、`content`、`payment`，同一域的后续任务派给同一成员。
- **并行派发（用户 2026-11-19 指定）**：低耦合、可并行的模块派**不同成员**同时开工，不必串行排队；有依赖或共享文件的域才串行/复用同一成员。lead 同时管理多个运行中成员，按回报到达顺序处理。
- **大功能必查，小功能免查**：独立功能面（认证、限流、ORM、缓存、队列、文件存储、短信、任务调度等）动第一行代码前必须先调研；拼写、小工具函数、明确 bug 修复可直接写。
- **MCP 只读**：禁止通过 MCP 写接口/改文档；MCP 仅用于读取。
- **陷阱：数据源身份必须先验证再采信**。2026-11-19 最终定论（用户三次纠正后确立）：**picart 没有任何电商面**。`apifox-office` MCP（project 6542782，含 guild/profit/cart/douyin 内容）**属于另一个电商系统任务，与 picart 无关，永久禁止参与 picart 的任何决策**。曾两次误把它当 picart 数据源并据此派发「电商批次」，均已撤回。picart 唯一形态 = 社区系统：`tmp/picart/swagger.json`（211 路径）+ `tmp/picart/src` 源码。真正的 picart MCP 是 `apifox-picart`（工具后缀 abp1z0），首次 ref 读取报「broken link」，需先调 refresh/read_project_oas 重新拉取后再用。教训：serverName 是人写的，以「用户确认 + 内容比对（picart 应有 /article /comment，不应有 guild profit cart douyin）」双验，验证不过宁可不用。
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

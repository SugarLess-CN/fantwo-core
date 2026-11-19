# TODO — 社区后端（fantwo-core）

当前阶段：阶段一 — 调研原仓库（已完成），准备阶段二 Go 骨架

## 进行中

- [x] T-02 初始化 git：克隆原仓库到 tmp/，关联 origin，.gitignore 排除 tmp/ — 2026-11-17 首次 commit 已完成
- [x] T-03a 基于源码的功能考古 — 2026-11-17，docs/01-原项目功能清单.md（23 模块/25 controller/48 entity）
- [ ] T-03b 基于 tmp/picart/swagger.json（权威源，211 路径/275 操作）整理 API 文档清单 → docs/02-API文档清单.md — 子代理执行中
- [x] T-04 Go+Gin 架构设计与第三方库调研 — 2026-11-17，docs/03-技术选型.md 落盘（候选库全部 go list 实测）
- [ ] T-05 分模块用 Go 复刻全部功能（按 24 个 tag 业务域逐批实现）
- [ ] T-06 每轮总结并 git commit + push，同步更新 AGENTS.md 与 todo.md

## 暂停

（空）

## 待办

- [ ] T-07 picart 专属 Apifox MCP 配置（用户侧操作：提供 picart 的 project id；当前 apifox-picart 实际指向 6542782 属另一电商任务，已禁用其参与 picart 决策）
- [ ] T-08 更新 docs/02-API文档清单.md 旧版（若此前子代理基于错误电商文档产出，需以 swagger.json 重写）

## 阻塞

（空）

## 已完成（最近若干项）

- [x] T-01 建立根 AGENTS.md 与 todo.md 治理骨架 — 2026-11-17
- [x] 事故修正：apifox-picart/project 6542782 误读为 picart 数据源 → 以仓库 swagger.json 为权威源，规则已写入 AGENTS.md — 2026-11-17

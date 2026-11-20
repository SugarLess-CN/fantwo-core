# 06 · API 覆盖报告（swagger → Go 实现）

> 生成方式：以 `tmp/picart/swagger.json`（OpenAPI 3.0.0，**275 个操作**）为基准，
> 用 `router.New(Option{DB: <未连接但非 nil>})` 实例化 gin 引擎、导出 `engine.Routes()`，
> 把 swagger 的 `{id}` 与 gin 的 `:id` 归一化为通配后逐条比对。

## 总览

| 指标 | 值 |
|---|---|
| swagger 操作数 | 275 |
| Go 实际注册（/api/v1 下，归一化去重） | 335 |
| **覆盖率** | **100.0%** |
| 缺失 | 0 |

> 注：`go run` 实测注册路由总数为 **337** 条（含 /health、/ping、/ws-message 等非 swagger 端点）；
> 代码注册数多于 swagger 是因为按原 NestJS 任务书挂了别名路径（如 /auth/login 与 /user/login 并存）。

## 分业务域覆盖

| 业务域(tag) | swagger 操作 | 已实现 | 覆盖率 |
|---|---|---|---|
| 用户管理 | 46 | 46 | 100% |
| 文章管理 | 33 | 33 | 100% |
| 装饰品管理 | 21 | 21 | 100% |
| 信息管理 | 19 | 19 | 100% |
| 积分管理 | 16 | 16 | 100% |
| 系统配置 | 13 | 13 | 100% |
| 表情包管理 | 13 | 13 | 100% |
| 角色管理 | 10 | 10 | 100% |
| 订单管理 | 10 | 10 | 100% |
| 合集管理 | 10 | 10 | 100% |
| 上传管理 | 10 | 10 | 100% |
| 评论管理 | 10 | 10 | 100% |
| 标签管理 | 9 | 9 | 100% |
| 成就管理 | 8 | 8 | 100% |
| 支付管理 | 7 | 7 | 100% |
| 内容审核 | 6 | 6 | 100% |
| 邀请管理 | 6 | 6 | 100% |
| 轮播管理 | 6 | 6 | 100% |
| 举报管理 | 6 | 6 | 100% |
| 权限管理 | 5 | 5 | 100% |
| 分类管理 | 5 | 5 | 100% |
| 搜索管理 | 3 | 3 | 100% |
| 统计模块 | 2 | 2 | 100% |
| App | 1 | 1 | 100% |

## 结论

- swagger 定义的 275 个操作**全部实现**（缺失 0）。
- 实现中还额外提供了 swagger 未收录的端点（emoji packs 8 条、/tag/hot、/permission/tree、
  /article/{id}/share、/article/browse/progress 等），来自原 NestJS 源码与 apifox-picart MCP 交叉核对。
- 认证/权限：JWT + RBAC（roles/perms + perm_ver 版本号比对，热路径零 DB 查询）。
- 支付三回调（alipay/wechat/epay）均为 NoAuth 并自带 SDK 验签，幂等由状态机 + 唯一索引保证。

## 复现命令

```bash
go run ./tmp/routeaudit   # 打印全部注册路由 + 总数（tmp 不入 git）
```

## 覆盖口径修正（2026-11-21）

**重要**：上文 100% 是对**仓库自带 swagger.json（275 操作）**的覆盖。
但 `swagger.json` **不是完整清单** —— Apifox MCP（project「PicArt 图片社区 API 文档」）
里的端点更多，例如：

| MCP 独有端点 | 仓库 swagger.json | 说明 |
|---|---|---|
| `/user/certification/{me,status,identity,apply,admin/*}` | ❌ 无 | 实名/创作者认证整套 |
| `/user/wallet/{balance,transactions,statistics,withdraw}` | ❌ 无 | 钱包与提现 |
| `/user/commission/{config,calculate}` | ❌ 无 | 抽成配置 |
| `/user/{id}/{followers,followings}` + count | ❌ 无 | 关注关系查询 |
| `/user/sign-in/{,records,stats}` | ❌ 无 | 签到 |
| `/article/browse/{history,stats,recent,batch-delete}` | ❌ 无 | 浏览历史 |
| `/article/hot-search`、`/article/{id}/profile-pin` | ❌ 无 | 热搜、主页置顶 |
| `/user/config`、`/user/config/{notifications,commission}` | ❌ 无 | 用户级配置 |
| `/comment/{id}/pin` | ❌ 无 | 评论置顶 |

**逐条核对结果**：MCP 独有的端点中，除 `GET /user/certification/me` 外全部已实现；
该端点已于本轮补齐（`UserService.MyCertifications` + `UserCertRepo.ListCertsByUser`），
实测「申请认证 → /me 返回记录」链路通过。

**结论**：以 **仓库 swagger.json ∪ MCP 文档** 为准，实际覆盖已完整（路由总数 343 条）。

**教训**：验收"覆盖率 100%"时必须先确认基准清单本身完整。
只比对单一数据源会漏掉数据源之间不重叠的部分。

# 18 — 用户对外展示号码 nick_id

> 用户需求（2026-11-28）：给用户表加 `nick_id`，数字、bigint、自增、从 10001 起；
> 对外输出 `nick_id` + `nick_id_str` 两个字段避免 JS 精度丢失；可搜索；
> 默认不允许重复和修改，用配置开关控制。

---

## 一、设计要点

### 1.1 为什么是 bigint 且必须拆成两个字段

`nick_id` 是对外展示号码（类似 QQ 号），量级会超过 **JS `Number.MAX_SAFE_INTEGER`（2^53-1）**。
只给 `number` 的话，前端 `JSON.parse` 那一刻精度就丢了，之后再怎么处理都救不回来。

所以对外约定**成对使用**：

| 字段 | 类型 | 用途 |
|---|---|---|
| `nick_id` | number | 方便前端直接比较/排序（**可能丢精度，仅用于非精确场景**） |
| `nick_id_str` | string | **精确值**，展示与回传一律用它 |

**入参也收字符串**：`{"nick_id": "9007199254740993"}`，后端 `parseNickID` 转 int64。

### 1.2 为什么用 PostgreSQL SEQUENCE 而不是 `max()+1`

`max(nick_id)+1` 在并发注册下会读到同一个 max → 两个用户拿到同一个号。
SEQUENCE 的 `nextval` 是原子且不依赖事务隔离级别，是唯一正确做法。

### 1.3 为什么只有普通索引，没有唯一索引

业务上 **nick_id 不是唯一标识**（唯一主键仍是 `id`），用户可以改成重名号。
加 `uniqueIndex` 会让「B 也想用 10001」在 DB 层直接 500。
唯一性由**配置开关 + 应用层查重**控制，不由 DB 约束。

---

## 二、改动清单

| 文件 | 改动 |
|---|---|
| `internal/model/user.go` | `NickID *int64`，column `nick_id`，`type:bigint`，`index:idx_user_nick_id` |
| `internal/model/config.go` | 新增 `ConfigGroupNickID` + 2 个键常量 + 2 条种子（78→**80**） |
| `migrations/migrate_user.go` | `ensureNickIDSequence`：建序列（START WITH 10001）+ 按 created_at 回填 + 前推序列 |
| `internal/repository/user.go` | `NextNickID`（nextval）、`ExistsNickID`（查重）；`List`/`ListByIDs` 的 keyword 加 `CAST(nick_id AS TEXT) LIKE` |
| `internal/service/common.go` | `nickIDPair`（拆双字段）、`parseNickID`（字符串→bigint） |
| `internal/service/user_service.go` | `buildUserView`/`ListUsersForSearch` 填双字段；`applyNickID`（开关逻辑）；`UpdateProfile`/`UpdateUser` 接线；`anyFieldProvided` |
| `internal/service/article_dto.go` | `AuthorBrief` + 双字段 + 转换 |
| `internal/service/author_resolver.go` | `ResolveAuthors` 填双字段（Select 补 `nick_id`） |
| `internal/service/message_service.go` | `briefUser` 填双字段（私信 counterpart） |
| `internal/service/auth_service.go` | 注册取号；短信登录自动注册取号 |
| `internal/service/oauth_service.go` | OAuth 建号取号 |
| `internal/dto/{user,article,message}.go` | 4 个 DTO 各 + 2 字段 |
| `scripts/backfill-nick-id.sql` | **兼容脚本**（处理两个冲突点，见 §五） |

### 返回位置（5 处，全覆盖）

| DTO | 覆盖的接口 |
|---|---|
| `dto.UserDto` | `/user/profile`、`/user/{id}`、`/user` 列表、登录/注册响应 |
| `dto.UserSearchItemDto` | `/search/users`、关注/粉丝列表 |
| `dto.AuthorBriefDto` | 文章/评论里的作者 |
| `dto.MessageUserBriefDto` | 私信会话 counterpart、消息 sender/receiver |

---

## 三、配置开关

通过**通用配置接口**改（不要直接改 DB —— 见 §六.1）：

```bash
PATCH /api/v1/config/group/nick_id
Authorization: Bearer <admin token>

[{"key":"nick_id_allow_modify","value":"true"}]
```

| 键 | 默认 | 语义 |
|---|---|---|
| `nick_id_allow_modify` | `false` | `false` → 传 `nick_id` 被**静默忽略**，只能系统发号 |
| `nick_id_allow_duplicate` | `false` | `false` → 改号时查重，撞号返回 `10005 common.already_exists` |

四种组合的行为（**全部真机验证，见 §四**）：

| modify | duplicate | 改号 | 撞号 |
|---|---|---|---|
| false | any | 静默忽略（200，值不变） | — |
| true | false | 允许 | **10005** |
| true | true | 允许 | 允许重名 |

---

## 四、验证证据（真机，端口 4292–4294）

### 4.1 迁移与起点

```
迁移前：nick_id 列 0，序列 0
迁移后：列 1，序列 1，NULL 残留 0

按 created_at 排序发号（起点 10001）：
  freshuser       10001  2026-11-19
  sgnuser         10002  2026-11-19
  ...
  test_admin      10005  2026-11-25
  test_user       10006  2026-11-25
```

### 4.2 注册自增

```
第 1 个注册 → nick_id: 10009 | nick_id_str: '10009'
第 2 个注册 → nick_id: 10010 | nick_id_str: '10010'
全库查重 → 0 行
号段 → 10001 ~ 10010（10 人）
```

### 4.3 所有返回位置

```
GET /user/profile        → nick_id: 10006 | nick_id_str: '10006'
GET /user/{id}（公开）   → nick_id: 10006 | nick_id_str: '10006'
GET /user（列表）        → 首条 nick_id=10007 nick_id_str='10007'
GET /search/users        → 首条 nick_id=10007 nick_id_str='10007'
```

### 4.4 配置开关四组合

```
① modify=true,  duplicate=true  → 改成别人的 10001 → status 0，nick_id=10001  ✅ 允许重名
② modify=true,  duplicate=false → 改成别人的 10002 → 10005 common.already_exists ✅ 查重
③ modify=true,  duplicate=false → 改成未占 66666   → status 0，nick_id=66666   ✅ 允许
④ modify=false                  → 传 nick_id=88888 → status 0，值仍 66666      ✅ 静默忽略不报错
```

### 4.5 兼容脚本的冲突场景（构造后实测）

**场景**：把 2 个用户置 NULL，其余已占 10001–10006。

```
跑脚本前：max(nick_id) = 10006
跑脚本后：
  被回填的 2 人 → 10007 / 10008   ← 从 max+1 起，**没撞**已占的 10001-10006
  still_null   → 0
  查重组       → 0 行
  号段         → 10001 ~ 10008（8 人）
  last_value   → 10008            ← 下一个新注册从 10009 起
```

**再注册两个**：`10009` / `10010`，全库仍 0 重号。

---

## 五、兼容脚本的两个冲突点（用户特别问的）

脚本 `scripts/backfill-nick-id.sql` 已处理，注释里写明了原因：

### 冲突点 1：存量回填的号 vs 已被占用的号

**天真写法** `ROW_NUMBER() + 10000` 会无脑从 10001 发号。但库里可能**已经有人占了 10001 以上**
（上线后新注册的用户、或手工改过号）。一撞就是两个用户同号。

**本脚本的处理**：起点 = `GREATEST(10000, 现有 max(nick_id))`，
即「从 10001 起，但如果有人占了 10001 以上，就接着往后排」。

### 冲突点 2：回填之后的**新注册** vs 存量号

新注册走 `nextval('user_nick_id_seq')`。如果脚本发完号**不推序列**，序列还停在旧位置
→ 新用户拿到 10001/10002 直接撞上刚回填的号。

**本脚本的处理**：回填后立刻 `setval(seq, max(nick_id))`，下一个 nextval 就是 max+1。

**还有一个隐蔽窗口**：脚本执行期间正在注册的新用户，可能与脚本交错取到同一个号。
脚本用 `pg_advisory_xact_lock` 让注册侧短暂排队。**最稳仍是维护窗口执行或先停写。**

---

## 六、遗留与风险

### 6.1 改配置必须走 HTTP 接口，别直接 UPDATE DB

`ConfigService` 有 **进程内缓存 + Redis 缓存（`config:cache:<key>`，TTL 10 分钟）**。
直接 `UPDATE config` 不会刷新缓存 —— 实测踩到：DB 改成 `true`，服务读到的仍是 `false`。

`PATCH /config/group/:group` 会 invalidate 缓存，是唯一正确用法。

### 6.2 `len(fields)==0` 的语义已调整（顺带修的）

`UpdateProfile` 原本「没有任何可更新字段 → 10008」。加了 `nick_id` 后出现新情况：
`nick_id_allow_modify=false` 时前端整包回传（只有 `nick_id`），字段被静默忽略 → `fields` 空 → 误报 10008。

现在区分两种：

```
确实一个字段都没传 → 10008（客户端调用错误，原语义保留）
传了但全被忽略     → 幂等 no-op，返回当前资料
```

### 6.3 存量回填的号段可能与「预期 10001 起」不完全一致

如果生产库在 nick_id 上线后已经注册过用户（占了 10001+），脚本会从 `max+1` 起发，
**不是严格从 10001**。这是**故意的**——避免撞号比「号段漂亮」重要。
若要严格 10001 起，必须先清空已有 nick_id（不推荐）。

### 6.4 配置开关暴露方式

目前用**通用配置接口** `PATCH /config/group/nick_id`（body 是 `[{key,value}]` 数组）。
没有为这两个开关单独加端点 —— 通用能力已够，且新增专用端点会多一份要维护的契约。
若前端希望「一个开关一键切换」的极简接口，可以再加，成本很小。

### 6.5 搜索是 `CAST(... AS TEXT) LIKE`

`nick_id` 是 bigint，`LIKE` 必须先转 text。这意味着查询**用不上索引**（`LIKE '%x%'` 天然不走 B-tree）。
当前用户量下无影响；若将来用户上万且搜索频繁，可按需加 `pg_trgm` 索引或改成「精确匹配优先」。

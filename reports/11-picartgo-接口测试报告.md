# 11 — picartgo 接口测试技术报告

> 任务：T-39　｜　执行人：api-tester　｜　执行日期：2026-11-25
> 增量流水见 [`api-test-log.md`](./api-test-log.md)（127 条，L-001 ~ L-126），本报告是结论。
> 规范依据：[`api-test-spec.md`](./api-test-spec.md)（日志格式与执行规则）

---

## 一、测试范围与方法

### 1.1 环境

| 项 | 值 |
|---|---|
| 代码 | 工作区 `E:\workspace\Repo\社区后端`，HEAD 含 P0 修复 `27f06e8` |
| 运行方式 | `go run ./cmd/server`，`.env` 显式加载 |
| 端口 | 链-01~08 用 `3990`；链-09/10 用 `3992`（避开 lead 占用的 3990/3991） |
| 数据库 | PostgreSQL 容器 `postgre`，库 `fantwo_core`，用户 `postgres` |
| 缓存 | Redis 容器 `redis`，`127.0.0.1:6379` |
| 配置 | `POSTGRES_PASSWORD=123456`、`JWT_SECRET` 44 字符（> 32 硬要求）、`SERVER_PORT` 动态改 |
| 测试工具 | `curl`（全部响应来自真实 curl 输出）+ `docker exec psql`（数据核对与清理）+ `python`（JSON 解析） |

### 1.2 时间窗口

2026-11-25 **10:51:52**（服务首次 `/health` 通）— **11:33:14**（服务停止），约 41 分钟连续实测。

### 1.3 覆盖口径

| 指标 | 数值 | 说明 |
|---|---|---|
| swagger 声明 paths | **246** | `docs/swagger/swagger.json` |
| swagger 声明 operations | **320** | 同一 path 多方法分别计数 |
| 源码注册路由点 | **350** | `grep -c '(GET\|POST\|PATCH\|PUT\|DELETE)(' internal/router/*_routes.go` |
| **实测唯一接口** | **65** | 日志去重后的 `(METHOD, path)` 组合数 |
| **能对上 swagger 的** | **59** | 用 `scripts/coverage-report.py` 归一化路径后比对 |
| 覆盖率 | **18.4%**（59/320 operations） | 按逻辑链覆盖，非全量穷举 |
| 日志条目 | **129** | L-001 ~ L-128 |

> ⚠️ **覆盖率口径修正（2026-11-25 复核）**：初版报告写「86 个唯一接口 / 26.9%」，是**手工去重且没有
> 做路径归一化**得出的，偏高。用 `scripts/coverage-report.py` 重算后为 **65 个唯一 `(METHOD,path)`
> / 59 个能对上 swagger / 18.4%**。
>
> 差异原因：日志里的路径混用了 gin 冒号风格（`/article/:id`）与 swagger 花括号风格（`/article/{id}`），
> 手工去重会把同一接口算成两个；另有 6 条是路径笔误（如 `/msg/conversations` 实际前缀是 `/message`），
> 不该计入覆盖。
>
> **复现方式**：`python scripts/coverage-report.py`（会在末尾列出全部未覆盖接口）。

**覆盖策略说明（为什么不追 100%）**：任务要求「按逻辑链测」（spec 第三节），10 条链覆盖的是**主链路 + 每条链的安全面**，不是 320 个 operation 的全集。所以：
- **业务主链路**：链-01~06 把注册→内容→上传→积分→浏览→交易的端到端跑通，每步都验正反两向
- **安全面**：链-07/08/09/10 用**批量脚本**打 20 + 18 + 13 + 22 个端点，安全面覆盖率远高于业务面
- **未覆盖的 261 个 operation** 集中在：认证子域（邮箱/短信验证码）、运营子域（装饰品/表情包/邀请/举报的写操作）、RBAC 子域、支付回调、消息域写操作、审核域复审 —— 见第七节

### 1.4 逻辑链执行顺序（严格按 spec 第三节，链内不跳步）

链-01 → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09 → 10，全部按序执行完毕。

---

## 二、总体结果

| 指标 | 数值 |
|---|---|
| 日志条目 | **129** |
| 有明确结论的条目 | **124** |
| ✅ 通过 | **111**（89.5%） |
| ⚠️ 通过但有问题 | **9**（7.3%） |
| ❌ 失败 | **4**（3.2%） |
| 发现缺陷 | **13**（P0×2 / P1×4 / P2×5 / P3×2） |
| 已修复并复测通过 | **5**（弱密码、评论孤儿、P0-1 黑名单、P0-2 越权写、P1 OAuth 文案） |
| 未修复（已记录） | **8**（P2×5 中 4 项设计如此/已知风险、P3×2、P2 账号枚举 1 项裁决保持） |

**4 条 ❌ 的归属**：

| 条目 | 接口 | 严重度 | 现状 |
|---|---|---|---|
| L-003 / L-016 | `POST /user/register` 弱密码放行 | P1 安全 | **已修 + 复测通过** |
| L-036 / L-037 | `POST /comment` 对不存在文章建评论 | P1 数据 | **已修 + 复测通过** |
| L-091 | 登出 token 黑名单绕过 | **P0** | **已修 + lead 验收（`27f06e8`）** |
| L-117 / L-127 | `PATCH /user/:id` 普通用户改他人资料 | **P0** | **已修（lead）+ 我复测 403/20003** |

---

## 三、逻辑链实测结果

| 链号 | 链路 | 步数 | 结果 | 链路级问题 |
|---|---|---|---|---|
| **链-01** | 注册 → 登录 → 取信息 → 更新 → 改密 → 重登 | 15 | ✅13 / ⚠️1 / ❌1 | **弱密码放行（P1，已修）**；账号枚举面（P2，裁决保持） |
| **链-02** | 发文章 → 列表 → 详情 → 更新 → 点赞 → 评论 → 收藏 → 删除 | 22 | ✅21 / ❌1 | **评论可挂到不存在的文章（P1，已修）**；IDOR 预演 2 条全拦 |
| **链-03** | 上传 → 查记录 → 关联文章 | 7 | ✅7 | 无缺陷。魔数校验、体积限制、权限降级全对 |
| **链-04** | 签到 → 积分 → 流水 → 成就 | 8 | ✅8（1 条语义澄清） | 无缺陷。**顺带验证了改密后 token 全端失效** |
| **链-05** | 分类/标签 → 按分类查 → 搜索 | 6 | ✅6 | 无缺陷。空结果统一 `[]` |
| **链-06** | 下单 → 查订单 → 取消 | 13 | ✅13 | 无缺陷。金额服务端取值、状态机、越权 404 全对 |
| **链-07** | 未登录访问需鉴权接口（20 条） | 13 | ✅11 / ⚠️1 / ❌1 | **P0 黑名单绕过（补充用例 L-091 发现，已修）**；错误码不统一（P1，随 P0 修完） |
| **链-08** | 普通用户访管理员接口（18 条） | 1（汇总） | ⚠️1 | 15 条 403 全拦、3 条路径笔误；仅错误码不统一（P1，已统一） |
| **链-09** | OAuth 5 类 | 14 | ✅12 / ⚠️2 | **文案 `%s` 未替换（P1，已修 + 复测 L-128）**；QQ 上游错误映射 500（P2） |
| **链-10** | IDOR 交叉访问（22 条） | 23 | ✅21 / ⚠️1 / ❌1 | **`PATCH /user/:id` 越权写（P0，已修 + 复测 L-127）**；`/upload/info/:id` 公开暴露元数据（P2） |

**10 条链全部执行完毕，链内无跳步。** 两条 P0 均出自安全链（链-07 的补充用例、链-10），印证了任务书「链-07/08/10 是安全链，最容易出真问题」的判断。

---

## 四、发现的问题清单（按严重度）

| # | 严重度 | 接口 | 问题（期望 vs 实际） | 是否已修 | 修改内容 |
|---|---|---|---|---|---|
| 1 | **P0** | `POST /article`（登出态 token） | 期望登出后任何需鉴权端点 401；实际 200 成功建文。**7 个域不查黑名单** | **是** | `internal/router/*_routes.go` 10 处 `JWTAuth` → `JWTAuthV2`；`middleware/authz.go:128` PermVer 加 `cur != 0` 跳过（commit `27f06e8`） |
| 2 | **P0** | `PATCH /user/:id` | 期望普通用户 403；实际 200 且**真改掉他人 nickname**。`canUserWrite` 用了「改自己」的 `user:update` | **是** | `user_routes.go:197` `canUserWrite` → `canUserManage`（lead 修，注释「P0 教训（2026-11-25 接口测试发现）」）；复测 L-127 403/20003 且数据未变 |
| 3 | P1 | `POST /user/register` | 期望弱密码被拒；实际 `"123"` 注册成功建号 | **是** | `internal/service/auth_service.go` Register/ChangePassword/ResetPassword 三处加 `utf8.RuneCountInString` 校验（<6 / <8 / <8） |
| 4 | P1 | `POST /comment` | 期望不存在的 articleId → 404；实际 200 建孤儿评论 | **是** | `internal/service/comment.go:212-216` 加文章存在性校验 |
| 5 | P1 | OAuth callback | 期望英文完整文案；实际返回 `"Third-party login failed: %s"`（占位符未替换） | **是** | `internal/pkg/i18n/en.go:52` 去 `: %s`（lead 修）；复测 L-128 文案完整 |
| 6 | P1 | 全站鉴权失败 | 期望同一语义同一码；实际未登录 10002 vs 20001、无权限 10003 vs 20003 | **是**（随 #1） | 10 域迁 V2 后自然统一为 20001 / 20003 |
| 7 | P2 | `POST /user/login` | 账号不存在 → 404/20006「User not found」；密码错 → 400/20007。两态可区分（枚举面） | **否（裁决保持）** | 与原仓库 `user.service.ts:216-222` 逐条一致，属**产品决策非缺陷**；改动会破坏前端既有分支 |
| 8 | P2 | `POST /user/oauth/qq/web/callback` | 期望客户端伪造 code → 4xx；实际 500/10007（`class=upstream`） | **否** | 安全面正确（不泄漏上游响应体，遵守 docs/14 禁令 2）；瑕疵在监控污染 |
| 9 | P2 | 任意未注册路径 | 期望统一响应体；实际 gin 原生纯文本 `404 page not found` | **否** | 建议 `engine.NoRoute(...)` 返回 `response.Fail(errcode.ErrNotFound)` |
| 10 | P2 | `GET /upload/info/:id` | NoAuth 公开端点返回含 `path`/`userId`/`hash`/`originalName` 的元数据 | **否（设计如此）** | 原仓库同为 `@NoAuth`，重构保持对齐；UUID 不可猜，判可接受 |
| 11 | P2 | `POST /order/membership` | 合法 plan 是 `1m`/`3m`/`6m`/`12m`/`lifetime`，swagger 声明为 `object` 无枚举 | **否** | 前端会猜 `MONTHLY` 而失败；属文档缺口 |
| 12 | P3 | `POST /order/membership` | 响应 `authorId` 恒为字面量 `"1"`（平台单哨兵值） | **否（设计如此）** | `order_service.go:157` 注释「对齐原仓库 authorId=1」；前端若反查用户会 404，建议文档标注 |
| 13 | P3 | 多域 | 包装风格不统一：`sign-in/records` 直出数组 vs `wallet/transactions` 用 `{list,meta}`；消息域用 `data.data`+`data.meta` 双层；`completionRate` 是字符串 `"9.52"` | **否** | 均不违反契约，属一致性债务 |

**严重度定义**（照 spec 第五节）：P0 安全/数据错误；P1 契约不符；P2 体验瑕疵；P3 建议。

---

## 五、已修复的缺陷明细

### 5.1 P0-1 登出 token 黑名单绕过（本轮最重要发现）

**症状**：用户登出后，token 在 7 个业务域仍然可用。

```
① POST /user/logout  + 有效token → 200 {"message":"Logged out","status_code":0}
② GET  /user/profile + 同一token → 401 20001  ← V2 中间件拦住（正确）
③ POST /article      + 同一token → 200 0     ← V1 中间件放行，真建出文章 e5de2557-...
```

**根因**：两套鉴权中间件并存，校验深度不同。

| 中间件 | 位置 | 校验内容 | 未登录返回 |
|---|---|---|---|
| `JWTAuth`（旧） | `middleware/jwt.go:16` | **只验签** | 10002 `Unauthorized` |
| `JWTAuthV2`（新） | `middleware/authz.go:21` | 验签 + **黑名单** + **全端登出时间** + **perm_ver** | 20001 `Please log in first` |

迁移状态：**仅 `user_routes.go:42` 用了 V2，其余 7 个域（10 处）仍是 V1**。`authz.go:3-4` 的注释自认「并存（不改其签名）」，是 knowingly 搁置的技术债。

**危害**：
1. 用户登出/改密后，旧 token 在 article/comment/order/payment/upload/audit/message 域继续可用 —— 公共设备登出形同虚设
2. `report.handled` 封号后，被封用户仍能发内容（token 有效期 `JWT_EXPIRES_IN=1h` 内）
3. `perm_ver` 不校验 → 管理员改权限后，旧 token 里的 perms 继续生效直到过期

**改动（2 个文件族，11 个文件）**：

```go
// ① internal/router/{article,audit,message,ops,order,payment,upload}_routes.go
//    10 处，逐一替换：
-middleware.JWTAuth(opt.Signer)
+middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
//    每个文件补 import "…/internal/pkg/authtoken"

// ② internal/middleware/authz.go:128 —— 修 PermVer 的降级方向矛盾
-// 3) 权限版本比对：不一致要求刷新（权限已变更）
-if cur := authtoken.PermVer(ctx, st, claims.UserID); cur != claims.PermVer {
+// 3) 权限版本比对：perm_ver 单调递增，**0 = 未设置或查询失败 → 跳过比对**。
+// 与上面黑名单 / 全端登出同向 fail-open，避免 Redis 故障把已登录用户全站打死：
+// 从未 bump 过的用户库里就没有 perm_ver key（读到 0），那种场景 token 里携带的也是 0，
+// 所以「cur == 0 跳过」不会漏判任何真实发生过的权限变更 —— 变更必然伴随 bump，
+// key 一定存在且 > 0，此时才走严格比对。
+if cur := authtoken.PermVer(ctx, st, claims.UserID); cur != 0 && cur != claims.PermVer {
```

**为什么必须同时改 ②**：`Blacklisted` / `LogoutAllAt` 在 Redis 出错时 fail-open（返回 false / 零值），只有 `PermVer` 返回 0。若不跳过，Redis 一挂 → 所有 `claims.PermVer != 0` 的用户被判 20002 → **全站 403**。修一个 P0 引入另一个 P0。而 `perm_ver` 语义是单调递增的「变更计数器」，真实的 0 只出现在「从未 bump 过」，此时 token 里也是 0，跳过不损失任何判定能力。

**前置安全验证（改之前做的）**：`NewRedisStore(nil)` 返回 `nil`（`authtoken.go:39-44`），而 `JWTAuthV2`/`parseBearerV2` 的三处调用都对 `st == nil` 做了处理（`Blacklisted`→false、`LogoutAllAt`→zero、`PermVer`→0），**不 panic**。

**复测证据**（lead 独立验收 6 项全过，见 `27f06e8`）：

| 验收项 | 结果 |
|---|---|
| ① 登出后需登录接口 | `POST /article` / `POST /comment` / `PATCH /user/profile` / `POST /order/membership` 全 401/20001 ✅ |
| ② 未登出正常访问 | 5 个域全 200（没一刀切）✅ |
| ③ 公开接口仍公开 | `GET /article`、`GET /comment/article/x` 仍 200 ✅ |
| ④ 错误码统一 | 无 token 访问 5 域全 20001，无 10002 ✅ |
| ⑤ **Redis 故障降级** | `docker stop redis` → 登录成功 + 5 域全 200，**无 20002** ✅（验证 ② 生效）|
| ⑥ 垃圾数据 | `articles WHERE title LIKE 'p0-%'` = 0 条（401 拦住）✅ |

我自己在 3992 上的旁证：`GET /user/oauth/bindings` 无 token → 401/**20001**（不再是旧 V1 的 10002，见 L-101）。

**测试过程中的一个自我纠正**：lead 首次测 `GET /article` 得 200 曾误判「没修好」—— 那是**公开接口**（`article_routes.go:89` 用 `opt1` 可选登录）。真正泄漏的是 `POST /article`（`auth` + `article:create`），已 401。

### 5.2 P1-1 弱密码放行

**症状**：3 位纯数字密码 `"123"` 注册成功并建号（L-003）。

**根因**：`auth_service.go` 三处密码入口只有空判，无长度校验。原仓库有：

| 原仓库 | 规则 |
|---|---|
| `create-user.dto.ts:32` | `@MinLength(6)` ← 注册 |
| `change-password.dto.ts:25` | `@MinLength(8)` ← 改密码 |

**改动**（`internal/service/auth_service.go`，3 处 + import）：

```go
// Register（L130）
if utf8.RuneCountInString(in.Password) < 6 {
    return nil, errcode.ErrInvalidParam.WithKey(i18n.AuthPasswordTooShort)
}
// ChangePassword（L352）
if utf8.RuneCountInString(newPwd) < 8 {
    return nil, errcode.ErrInvalidParam.WithKey(i18n.AuthNewPasswordTooShort)
}
// ResetPassword（L379）—— 与改密同口径
```

**为什么用 `utf8.RuneCountInString` 不是 `len`**：中文密码 1 字 = 3 byte，用 `len` 判「2 个中文字 = 6 byte」会误放行。

**复测证据**（7 条全过，L-016~L-022）：

| 用例 | 期望 | 实测 |
|---|---|---|
| 3 位英文 | 400/10008 | 400/10008 "Password must be at least 6 characters" ✅ |
| **4 字中文**（12 byte） | 拒 | **拒** ✅ ← 若用 `len` 会放行 |
| **5 字中文**（15 byte） | 拒 | **拒** ✅ |
| 6 字中文 | 过 | 200 ✅ |
| 6 位英文 | 过 | 200 ✅ |
| 改密 7 位 | 400 | 400 "New password must be at least 8 characters" ✅ |
| 改密 8 位 | 过 | 200 ✅ |

**质量门**：`gofmt -l internal/` 空 / `go build ./...` 无输出 / `go vet ./...` 零输出 / `go test ./... -count=1` **16 包全绿**（10:59:46）。

### 5.3 P1-2 评论可挂到不存在的文章

**症状**：给 `articleId=00000000-...` 评论返回 200 并建出孤儿评论（L-036）。

**根因**：原仓库 `comment.service.ts:570-572` 在事务内先查文章，不存在即抛错；Go 版漏了这一步。

**改动**（`internal/service/comment.go:212-216`）：

```go
// 文章存在性校验：对齐原仓库 comment.service.ts:570-572（article 不存在直接报错，不落脏数据）。
_, artErr := repository.NewArticleRepository(s.repo.DB()).FindByID(ctx, req.ArticleID)
if artErr != nil {
    return nil, errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)
}
```

**复测**：400/10004 → 正确的 **404/10004 "Article not found"**（L-037），且未落库。

**遗留（lead 已记 todo）**：该校验在**事务外**，理论上存在「查到存在 → 毫秒级被删 → 评论仍落库」的窗口。概率极低（文章多为软删），修楼层并发时一并包进事务即可。

### 5.4 P0-2 普通用户可改任意用户资料（`PATCH /user/:id`）

**症状**：普通用户 B 成功修改了用户 A 的昵称（L-117）。

```
B 的 JWT perms = ['user:read','user:update']
PATCH /api/v1/user/a3baeae0-...(A的id)  {"nickname":"hacked"}
→ 200 {"message":"Success","status_code":0,"data":{"nickname":"hacked",...}}
A 的 nickname 真被改成 "hacked"
```

**根因（逐层确证）**：

```go
// internal/router/user_routes.go:45（修复前）
canUserWrite := middleware.RequireAnyPermission("user:update", "user:manage")
// internal/router/user_routes.go:189（修复前）
admin.PATCH("/:id", canUserWrite, userH.Update)   // ← 「管理员改他人」用了「改自己」的权限点
```

`user:update` 是**每个普通用户都有的**——`PATCH /user/profile`（改自己）正是靠它。挂在「改他人」的路由上，等于人人可改任何人。

**同组对照证明是单点遗漏**：

| 路由 | 权限点 | B 能过吗 | 判定 |
|---|---|---|---|
| `GET /user` | `user:manage` | ❌ 403 | ✅ 已收紧（todo 有过记录） |
| `DELETE /user/:id` | `user:delete, user:manage` | ❌ 403（L-113/L-120） | ✅ 安全 |
| `GET /user/:id` | `user:read, user:manage` | ✅ 但那是公开主页 | ✅ 合理 |
| **`PATCH /user/:id`** | `user:update, user:manage` | ✅ **200 改成功** | ❌ **就这行** |

**改动**（lead 执行）：

```go
// internal/router/user_routes.go:197
-admin.PATCH("/:id", canUserWrite, userH.Update)
+// P0 教训（2026-11-25 接口测试发现）：这里原用 canUserWrite（`user:update`），
+// 而 user:update 是普通用户「改自己资料」就有的权限 —— 等于任何登录用户都能改任何人。
+admin.PATCH("/:id", canUserManage, userH.Update)
```

**复测证据（L-127，真实输出）**：

| 断言 | 结果 |
|---|---|
| B 改 A 的资料 | **403 / 20003 "Permission denied"**（原 200/0）✅ |
| A 的资料是否被改 | 用 A 自己的 token 读 `GET /user/profile` → `nickname = None`（A 从未设昵称）→ **未写入** ✅ |

**我做的临时处置**：发现当时（修复前）立刻用 A 的 token `PATCH /user/profile {"nickname":"test_nickname_01_v2"}` 把被篡改的值回滚，避免污染后续测试。

### 5.5 P1-3 OAuth 失败文案占位符未替换

**症状**：伪造 code 回调返回 `"Third-party login failed: %s"`（L-095/L-097）。

**根因**：`en.go:52` 的 `AuthOAuthFailed` 仍带 `: %s`，而 `oauthFailed()` 用 `WithKey(i18n.AuthOAuthFailed)` 未传 `Args`。T-37c 的收口记录写着「AuthOAuthFailed **去占位符**（细节场景改 AuthOAuthFailedDetail）」——**这一步没落地**。

**改动**（lead 执行）：

```go
// internal/pkg/i18n/en.go:52
-AuthOAuthFailed:           "Third-party login failed: %s",
+AuthOAuthFailed:           "Third-party login failed",
```

**复测证据（L-128）**：新 state + 伪造 code → `400 / 10008 / "Third-party login failed"`，占位符已消失。

---

## 六、与 Apifox schemas + 原仓库 swagger 的差异

### 6.1 对照基线说明（口径已修正）

| 基线 | 位置 | 能提供什么 |
|---|---|---|
| **Apifox（默认模块）** | `tmp/apifox-picart-full.json` / `tmp/apifox-paths.txt` | **243 paths + 90+ schemas** |
| **本项目 swagger** | `docs/swagger/swagger.json` | 246 paths / 320 operations / 43 definitions |
| **原仓库 swagger** | `tmp/picart/swagger.json` | 211 paths / 275 operations（PicArt API 原始契约） |

**过程修正记录**：早期结论「Apifox 项目内无 paths」**是误判** —— `read_project_oas` 只能读**默认模块**（返回 `info.title = "默认模块"`），新接口在 `picartgo` 模块里。用户把接口移入默认模块后即可读到 243 paths。根因：`apifox-mcp-server` 调 `/v1/projects/{id}/export-openapi` **不带 moduleId**，而该参数实测 0/1~15/20/50/100 全返回空。

### 6.2 路径级差异（实测）

```
apifox 243 paths  vs  本项目 246 paths
```

| 差异方向 | 数量 | 明细 | 判定 |
|---|---|---|---|
| **Apifox 有 / 本项目缺** | **0** | — | ✅ 无缺口（此前的 3 条 `certification/admin/*` 前缀问题已由 DTO 批次修好，本次实测确认 swagger 已正确） |
| 本项目有 / Apifox 缺 | **3** | `/user/oauth/bindings`、`/user/oauth/bindings/link`、`/user/oauth/bindings/{provider}/{channel}` | **文档旧** —— T-37e 新增端点尚未导入 Apifox，属正常时滞 |

### 6.3 Apifox 侧污染残留（非本轮引入，需人工清理）

**5 个污染 schema 与干净版并存且内容不同**：

| 项 | 值 |
|---|---|
| 污染 schema | `tmp_deploy-pkg_internal_handler.{ResponseBody, ResponsePageBody, ResponsePageInner, ResponsePageMeta, loginRequest}` |
| 干净版 | `internal_handler.{同 5 个}` |
| 来源 | `tmp/deploy-pkg`（整项目源码副本）被 swag 扫描污染后导入 Apifox 留下 |
| **内容差异** | 污染版 `{"code":200,"message":"操作成功",...}`（旧契约、中文、无 trace_id/timestamp）<br>干净版 `{"data":{},"message":"Success","status_code":200,...}`（新契约、英文、有 trace_id/timestamp） |
| 影响 | 按污染版解析的前端/测试读 `code` 字段永远 undefined，且看不到 `trace_id`（排障断链） |
| 处置 | **需在 Apifox 界面手工删除**（`apifox-picart-import` 只用于导入，不得用于删除） |
| 本地已修 | 三处 swag 命令加 `--exclude tmp`；`tmp/deploy-pkg` 与 `tmp/binary-pkg` 已删 |

### 6.4 DTO 字段名对照（camelCase 一致性）

抽查本轮实测的请求/响应字段，全部 camelCase：

| 接口 | 字段 | 是否符合 |
|---|---|---|
| `POST /user/login` | `account` / `password` / `refreshToken` | ✅ |
| `POST /user/password/change` | `oldPassword` / `newPassword` | ✅ |
| `PATCH /user/profile` | `nickname` / `avatar` / `background` / `birthDate` / `gender` | ✅ |
| `POST /article` | `title` / `categoryId` / `tagIds` / `tagNames` / `videoUrl` / `viewPrice` / `isFeatured` / `isPinnedOnProfile` | ✅ |
| `POST /order/article` | `articleId` / `remark` | ✅ |
| `POST /user/oauth/qq/app/login` | `accessToken` / `openId` / `payload` | ✅ |
| 响应（点赞） | `isLiked` / `reactionType` / `likes` | ✅ |
| 响应（收藏） | `isFavorited` / `favoriteCount` | ✅ |

**无 snake_case 泄漏** —— payment 域 41 处历史问题已修，本轮未见回归。

### 6.5 与 swagger 声明的偏差（实现与文档不一致）

| # | 接口 | swagger 声明 | 实测 | 判定 |
|---|---|---|---|---|
| 1 | `POST /order/membership` | `body object`（无枚举、无字段说明） | 合法 `plan` 仅 `1m`/`3m`/`6m`/`12m`/`lifetime` | **文档旧**（应补枚举） |
| 2 | 全部 320 operations | `@Success 200 {object} ResponseBody`（`data` 为空 `{}`） | 实际 data 结构丰富（见日志） | **文档旧**（缺 `{data=dto.Xxx}` 嵌套覆盖，lead 已在 `docs/18` 立规） |
| 3 | `GET /user/oauth/bindings` | 未在 Apifox | 实际存在且工作 | **Apifox 旧** |
| 4 | 307 个存量 operationId | 中文（历史遗留） | 无影响（仅文档侧） | **双方都不对**（口径已定为 `<Controller>_<method>`，暂不批量改） |

---

## 七、未覆盖与遗留

### 7.1 明确未测的（及原因）

| 项 | 原因 | 建议后续怎么测 |
|---|---|---|
| **真实 OAuth 授权**（微信/QQ 完整跳转换 token） | 需真人在微信/QQ 授权页操作，无法自动化 | 人工冒烟：取 URL → 手机扫码 → 观察 302 → 检查建号与绑定 |
| **微信 App 渠道登录** | 需微信客户端 SDK 产出真 code | 真机 App 联调 |
| **ES 搜索降级**（ES 不可用时 DB 兜底） | 未停 ES 验证 | `docker stop` ES → 打 `/article/search` → 应走 DB 且不 500 |
| **S3 存储分支** | 需 MinIO/OSS（Docker Hub 受限拉不到镜像） | 有 S3 环境后跑 T-32 |
| **支付回调**（微信/支付宝 notify） | 需真实支付网关签名 | 用官方沙箱或本地验签桩 |
| **并发压测**（评论楼层并发、点赞竞争、积分透支） | 属性能/竞态测试，非接口契约测试；lead 已单列 todo | `k6`/`vegeta` 并发脚本 + 结果断言 |
| **RBAC 写操作**（建角色/分配权限/菜单） | 需管理员账号，本轮只有普通用户 | 种子 admin 账号后补测 |
| **消息域写操作**（发私信/撤回/拉黑） | 需两个账号真实互发，本轮只测了读的隔离性 | 双账号会话流 |
| **审核域复审**（通过/驳回回写） | 需人工触发审核队列 | 已由 T-24 验证过，本轮未复测 |
| **运营子域写操作**（装饰品购买/表情包上传/邀请） | 与本轮 10 条链无交集 | 单列一批运营域测试 |

### 7.2 未修复但已记录（详见第四节 #7~#13）

**已全部修完的两项（原为本节主要遗留）**：
- ~~P0-2 `PATCH /user/:id` 越权写~~ → **lead 已修**（`canUserManage`），我复测 L-127 通过
- ~~P1 OAuth 文案 `%s`~~ → **lead 已修**（`en.go:52` 去占位符），我复测 L-128 通过

**仍未修（8 项，均已进 lead 的 todo）**：

| # | 严重度 | 项 | 性质 |
|---|---|---|---|
| 7 | P2 | 登录账号枚举面（404 User not found vs 400 密码错） | **裁决保持** —— 与原仓库逐条一致，产品决策非缺陷 |
| 8 | P2 | QQ 上游错误映射 500/10007 | 安全面正确，监控面瑕疵 |
| 9 | P2 | gin 未注册路径返回纯文本 `404 page not found` | 契约一致性，`NoRoute` 未配 |
| 10 | P2 | `/upload/info/:id` NoAuth 公开返回元数据 | **设计如此**，与原仓库对齐 |
| 11 | P2 | 会员 plan 取值未进 swagger 枚举 | 文档缺口 |
| 12 | P3 | 会员订单 `authorId` 恒为 `"1"` | **设计如此**（平台单哨兵值），建议文档标注 |
| 13 | P3 | 响应包装风格不统一（数组直出 vs `{list,meta}`；消息域 `data.data` 双层；`completionRate` 字符串） | 一致性债务，不违反契约 |

### 7.3 待查（不是接口缺陷）

| 项 | 现象 | 定性 |
|---|---|---|
| **DAILY_LOGIN 积分活动未启用** | 自动签到已记录（`isAuto=true, consecutiveDays=1`），但 `wallet/balance=0` 且流水空 | 疑 `points_activity` 表无 `DAILY_LOGIN` 记录 → **数据/配置问题**，非接口缺陷 |

### 7.4 测试方法坑（给后续测试者，避坑用）

**`soft_delete` 插件用 `deleted_at = 0` 表示未删除，与 gorm 原生相反。**

`internal/model/base.go:28` 用 `soft_delete.DeletedAt`（`gorm.io/plugin/soft_delete`）：
- **未删除 = `deleted_at = 0`**（int64 零值）
- 已删除 = 非 0（unix 秒时间戳）
- 而 `gorm.DeletedAt` 的语义是 **NULL = 未删除**

我第一次用 SQL 造 `user_oauth_binding` 测试数据时写了 `deleted_at = NULL`，接口稳定返回空数组（L-102），SQL 直查却有 1 行 —— 差点误判成「绑定列表查询有 bug」。改成 `deleted_at = 0` 后立刻正常（L-102b）。

**结论**：手工造测试数据必须写 `deleted_at=0`；判断「是否已删」不能用 `IS NULL`，要用 `= 0`。

**另一个坑**：`psql -c "多条语句"` 是**单事务**，中间任一条报错会导致前面成功的 DELETE 全部回滚。造/清测试数据时用 `psql -f 文件`（逐语句自动提交）或拆成多次 `-c`。

### 7.5 测试数据清理情况

**全部清理，0 残留**（11:32:24 复查）：

| 表 | 清理前 | 清理后 |
|---|---|---|
| `user`（`test_%`） | 5 | **0** |
| `articles`（`T-39%` / `p0-%`） | 4 | **0** |
| `comments` | 1 | **0** |
| `orders` | 1 | **0** |
| `collections`（`test_%`） | 2 | **0** |
| `uploads`（`test-%`） | 1 | **0** |
| `user_oauth_binding` | 1 | **0** |
| 上传的物理文件 | 1（125 B） | **已删**（`uploads/2026/11/25/13/0c/130c7aa5….png`） |

连带清理：`user_roles` 6 行、`user_device` 30 行、`user_config` 5 行、`user_followings` 1 行、`article_likes` 1 行、`article_favorites` 1 行、`collection_items` 0 行。

**服务进程已停止**：`taskkill //IM server.exe //F`，`tasklist` 确认无残留（11:33:14）。

**未清理项**：无。（lead 建的两篇 `p0-lead-check*` 由 lead 自行清理，我复查时已为 0 条。）

---

## 八、结论与建议

### 8.1 是否可交付

**可交付** —— 主链路（注册/登录/内容/上传/积分/浏览/交易）**功能完整、契约规范、异常路径齐备**；两条 P0 与 P1 文案均已修复并复测通过，安全面达到可对外标准。

| 维度 | 评价 |
|---|---|
| 业务主链路 | ✅ 端到端跑通，无阻塞缺陷 |
| 响应契约（英文 / status_code / 空数组 / camelCase） | ✅ 全站一致（抽查无违规） |
| 权限矩阵 | ✅ 18 个管理员端点无一泄漏；文章/评论/合集/订单/用户越权全拦（P0-2 修复后） |
| 写操作鉴权 | ✅ P0-2 修复后普通用户无法改他人资料 |
| 会话安全 | ✅ P0-1 修复后，登出/改密/封号在全站生效 |
| 第三方登录 | ✅ 伪造 token 被拦且不建号；文案已修 |
| 文档一致性 | ⚠️ Apifox 243 / 本项目 246，差 3 条是未导入的新端点；swagger 的 `data` 定义仍大面积空 `{}` |

### 8.2 上线前必改项（按优先级）

| # | 项 | 工作量 | 影响 |
|---|---|---|---|
| **—** | ~~P0-1 黑名单绕过~~ | — | **已修** `27f06e8`，6 项验收全过 |
| **—** | ~~P0-2 `PATCH /user/:id` 越权写~~ | — | **已修**（`canUserManage`），复测 L-127 通过 |
| **—** | ~~P1 OAuth 文案 `%s`~~ | — | **已修**（`en.go:52`），复测 L-128 通过 |
| 1 | P2 `engine.NoRoute` 返回统一响应体 | ~5 行 | 未注册路径返回纯文本，破坏契约一致性 |
| 2 | P2 会员 plan 补进 swagger 枚举 | 注解 | 前端猜 `MONTHLY` 会失败 |
| 3 | P2 评论楼层并发（lead 已记 todo） | 中 | 小流量概率低，但会造成楼层重复 |
| 4 | P2 QQ 上游错误语义（500 → 4xx） | 小 | 污染错误率告警 |

> **交付判断**：三项必修（P0×2 + P1×1）已全部落地，上述 4 项 P2 属**发布前优化项**，不阻塞前端联调。

### 8.3 Breaking change 清单（前端必读）

P0-1 修复带来的**明确的**错误码变更（lead 裁定「统一到 2xxxx」，2xxxx 才是本项目认证域的设计，10002/10003 是 V1 历史遗留）：

| 语义 | 旧（V1 域） | 新（全站统一） | 影响范围 |
|---|---|---|---|
| 未登录 / token 失效 | `10002` | **`20001`** | article / comment / order / payment / upload / audit / message 共 7 域 |
| 无权限 | `10003` | **`20003`** | 同上 |
| 权限版本落后 | 无 | `20002`（新增） | 管理员改权限后旧 token |

**前端适配建议**（过渡期双码兼容，一个版本后删旧码）：

```js
// 未登录 → 去登录
if (code === 20001 || code === 10002) { redirectToLogin(); }
// 无权限 → 提示
if (code === 20003 || code === 10003) { toast('Permission denied'); }
// 新增：权限已变更，需刷新 token
if (code === 20002) { await refreshToken(); retry(); }
```

**好消息**：`user` 域本来就是 20001/20003（V2），前端若已适配 user 域，则这批变更**零成本**。

### 8.4 后续测试建议

1. **补齐第五节 P0-2 修复后的复测**（B 改 A → 403）
2. **补测运营子域 + RBAC 写操作**（本轮完全未覆盖，约 80 个 operation）
3. **并发测试**（评论楼层、点赞、积分透支）—— 用 `k6` 而非手工
4. **ES 降级与 S3 分支**（需额外环境）
5. **真实 OAuth 人工冒烟**（微信扫码 + QQ 登录各一次）
6. **把 `soft_delete` 的 `deleted_at=0` 约定写进项目测试规范**（本轮最大的非缺陷踩坑）

### 8.5 本轮方法论沉淀（可复用）

| 沉淀 | 价值 |
|---|---|
| **批量脚本探安全面** | `tmp/auth-batch.sh` / `tmp/admin-batch.sh` 一次打 20 个端点，比逐个 curl 快 10 倍；错误码不统一、路径笔误一屏内全暴露 |
| **对照 trio** | 「登出前后同一 token 打不同域」这个三步对照，一次就定位了 P0-1 的根因（哪个域用哪套中间件） |
| **耗时差当证据** | state 重放 13ms vs 首次 336ms，证明校验在上游调用之前（L-097）—— 不用抓包也能证 |
| **PR 前先测「有数据态」** | 绑定列表空数组时看不出 openid 是否泄漏，必须造数据才能验（L-102b） |
| **日志增量落盘的纪律** | 127 条全部真实 curl 输出，事后无需回忆 —— spec 的「不许攒批」是对的 |

---

**报告完成时间**：2026-11-25 11:33
**配套产物**：[`api-test-log.md`](./api-test-log.md)（127 条流水）、[`api-test-spec.md`](./api-test-spec.md)（测试规范）

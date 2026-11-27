# 14 — T-39 JS→Go 字段补齐收口

> 依据 `docs/28-前端对接差异对照.md`（前端交接单 A~E 五段逐项核对）。
> 3 成员并行、范围互斥；lead 做对比工具、验收脚本与独立复验。

---

## 一、本轮改了什么

### 1.1 先做的对比工具（没有它只能靠猜）

| 文件 | 内容 |
|---|---|
| `scripts/cmp-js-go-fields.py`（新建） | JS `tmp/js/openapi.json` ↔ Go `exports/openapi.json` 字段级比对。三处关键设计：① **命名归一化**（JS camelCase → snake_case 再比，否则 76 个 schema 全对不上）② **`allOf` / `$ref` 递归展开**（只看顶层 `properties` 会漏字段）③ **路径参数名归一**（`/{userId}` 与 `/{user_id}` 视为同一路径） |
| `scripts/verify-t39.py`（新建） | 22 项验收脚本。**读产物不读意图**：不看 git diff、不看成员报告，只读 swagger / dto 源码 / handler 源码 |
| `docs/28-前端对接差异对照.md`（新建，~430 行） | A~E 五段逐项判定 + 12 项待办按影响面排序 |

### 1.2 用户域（`dto-modeler`）

| 文件 | 改了什么 |
|---|---|
| `internal/dto/user.go` | `UpdateUserConfigRequest` 5 → **24 字段**；`UserDto` +`Config`（31 字段）；`RegisterRequest` 5 → 7（+`invite_code`/`verification_code`）；新建 `UpdateContactRequest`（3 字段） |
| `internal/service/user_service.go` | `commissionFields` → **`configFields`**（覆盖 24 字段）；`userConfigDto` 补 `ID`/`CreatedAt`/`UpdatedAt`（lead 补，见 §1.5） |
| `internal/service/auth_service.go` | `LoginResultWithConfig`（含 config）+ `Register` 消费邀请码/验证码 |
| `internal/handler/auth.go` | 3 处登录改走 `LoginResultWithConfig` |
| `internal/handler/oauth.go` | `oauthLoginResult` 加 `c`/`svc` 参数，4 个 OAuth 登录全改 |
| `internal/handler/user.go` | `UpdateContact` 改专用 DTO + 补验证码校验 |

### 1.3 内容域（`dto-content2`）

| 文件 | 改了什么 |
|---|---|
| `internal/handler/article.go` | `GET /article` 加 `sort`（enums latest/popular/sort）；`GET /article/search` 补 `keyword`/`category_id`/`sort_by` 注解 |
| `internal/handler/article_interaction.go` | `GET /article/browse/history` 加 `order`（newest/oldest） |
| `internal/handler/comment.go` | `GET /comment/{id}` 加 replies 分页；`onlyAuthor` → `only_author` |
| `internal/handler/tag.go` | `GET /tag` 支持 `keyword`（`name` 兜底向后兼容） |
| `internal/service/comment.go` | `FindCommentDetail` 加 `page, limit` |
| `internal/service/article.go` / `repository/article.go` | `UserBrowseHistory` 加 `order`；`ArticleListQuery` 支持 `sort` |

### 1.4 消息域（`dto-msg`）

| 文件 | 改了什么 |
|---|---|
| `internal/handler/message_handler.go` | `GET /message` 补 5 条注解 **+ 新增 `ListByUserFiltered` 接通实现**；`Search` 补 7 条注解 + `is_read`/`is_broadcast` 改名；`PrivateHistory` 补 `cursor`/`limit` |
| `internal/service/message_service.go` | 新增 `ListByUserFiltered`（复用内部 `list()`，避免两套查询口径漂移） |
| `internal/dto/message.go` | `ConversationDto` +`counterpart_id` |
| `internal/service/message_private_service.go` | 2 处组装点填 `CounterpartID` |
| `internal/service/message_vo_test.go` | 新增 2 条不变量单测 |
| `scripts/json-naming-check.py` | 新增 `scan_handler_query_keys()` + **词法扫描 `_comment_mask()`** |

### 1.5 lead 亲手做的（不属任何成员范围）

| 文件 | 改动 |
|---|---|
| `internal/handler/search.go` | `GET /search` 补 `category_id`/`sort_by` 注解 |
| `internal/handler/role.go` | `queryBool(c, "isActive")` → `"is_active"` + 补 `name`/`is_active` 注解 |
| `internal/service/user_service.go` | `userConfigDto` 补 `ID`/`CreatedAt`/`UpdatedAt`（**审计时发现的既有假契约**：声明了三个字段但从不赋值，前端拿到 `""` 会误判为无效记录） |

---

## 二、功能变化（对外可见）

### 2.1 响应体

| 接口 | 改前 | 改后 |
|---|---|---|
| `POST /user/login`、`POST /user/sms/login`、GitHub 回调、4 个 OAuth 登录 | 无 `config` | **有 `config`（28 字段）** |
| `GET /user/profile`、`GET /user/{id}` | 无 `config` | **有 `config`** |
| `GET /user`、关注/粉丝列表 | — | **仍无 `config`**（防响应体爆炸，`omitempty` 保证不出现） |
| `GET /message/private/conversations` | 有 `counterpart`（对象） | **+`counterpart_id`（字符串）**，两者并存 |
| `GET /user/config` | `id`/`created_at`/`updated_at` 恒为 `""` | **有真实值** |

### 2.2 请求体

| DTO | 改前 | 改后 |
|---|---|---|
| `UpdateUserConfigRequest` | 5 字段（只有佣金） | **24 字段**（5 佣金 + 11 通知 + 6 隐私 + 2 其他） |
| `RegisterRequest` | 5 字段 | **7 字段**（+`invite_code` +`verification_code`） |
| `UpdateContactRequest` | 不存在（复用通用 `UpdateProfileRequest`） | **新建**（`email`/`phone`/`verification_code`） |

### 2.3 查询参数（新增/改名）

| 接口 | 参数 |
|---|---|
| `GET /article` | **+`sort`**（latest/popular/sort） |
| `GET /article/search` | **+`keyword` +`category_id` +`sort_by`** |
| `GET /article/browse/history` | **+`order`**（newest/oldest） |
| `GET /comment/{id}` | **+`page` +`limit`**（replies 分页） |
| `GET /tag` | **+`keyword`**（`name` 保留兼容） |
| `GET /message` | **+`type` +`is_read` +`is_broadcast`**（且真过滤） |
| `GET /message/private/conversations/{user_id}/messages` | **+`cursor` +`limit`** |
| `GET /role/list` | **+`name` +`is_active`** |
| `GET /search` | **+`category_id` +`sort_by`** |

### 2.4 ⚠️ breaking changes

| 变化 | 影响 | 前端适配 |
|---|---|---|
| `GET /message` 的 `isRead` → `is_read`、`isBroadcast` → `is_broadcast` | 旧名**静默忽略**（实测 `?isRead=true` 返回全部） | 改参数名 |
| `GET /comment/article/{id}` 的 `onlyAuthor` → `only_author` | 同上 | 改参数名 |
| `GET /role/list` 的 `isActive` → `is_active` | 同上 | 改参数名 |
| `PATCH /user/profile/contact` 现在**必填** `verification_code` | 旧调用会 400 | 改调 `POST /user/email/verification` 或 `/user/profile/phone/code` 拿码 |
| `PATCH /user/profile/contact` 的 `email` 与 `phone` **一次只能传一个** | 同时传会 10008 | 拆两次调用 |

### 2.5 安全修复（最重要的一条）

**改前**：`PATCH /user/profile/contact` 用通用 `UpdateProfileRequest`，**没有任何验证码校验** ——
任何人拿到有效 token 就能把账号邮箱改成自己的，然后走「忘记密码」接管账号。

**改后**（真机实测）：

| 场景 | 结果 |
|---|---|
| 无验证码改邮箱 | `10008 common.bad_request` ✅ 拒 |
| 假验证码 `000000` | `20011 auth.captcha_invalid` ✅ 拒（**证明真的在验，不是只判空**） |
| 同时传 email+phone | `10008 common.invalid_parameter` ✅ 拒 |
| 邮箱未被改动 | SQL 直查确认 ✅ |

---

## 三、配置项 / 接口清单

**无新增配置项**。改动的接口见 §2.3 全表。

`swagger`：`paths: 246`（未变）/ `definitions: 291`（+1 `dto.UpdateContactRequest`）。

---

## 四、验证证据（真实命令与输出）

### 4.1 质量门

```
$ gofmt -l internal/ migrations/ cmd/
（空）

$ go build ./...          → exit=0
$ go vet ./...            → exit=0
$ go test ./... -count=1  → 16 包 ok

$ swag init ... --exclude tmp,internal/dto
exit=0   危险词(unsupported|cannot find)=0   paths:246 defs:291   污染:无

$ python scripts/json-naming-check.py
✓ 命名全部合规（全站 snake_case）

$ python scripts/verify-t39.py
======================================================================
T-39 验收：22 通过 / 0 未通过
======================================================================

$ python scripts/dto-coverage.py
总操作数 317   data 未具名 3 (0%)   已具名 314 (99%)
```

### 4.2 swagger 落地核对

```
✓ GET    /article                            缺=无   (sort)
✓ GET    /article/search                     缺=无   (keyword/category_id/sort_by)
✓ GET    /article/browse/history             缺=无   (order)
✓ GET    /comment/{id}                       缺=无   (page/limit)
✓ GET    /tag                                缺=无   (keyword)
✓ GET    /message                            缺=无   (type/is_read/is_broadcast)
✓ GET    /role/list                          缺=无   (name/is_active)
✓ GET    /search                             缺=无   (keyword/category_id/sort_by)

dto.UpdateUserConfigRequest: 24 字段
dto.RegisterRequest: 7 字段
dto.UpdateContactRequest: 3 字段
dto.UserDto: 31 字段
dto.ConversationDto: 6 字段
```

### 4.3 真机验证（端口 4031，独立起服务自己验，不复用成员结论）

**① 登录 config + 审计字段（lead 修的假契约）**
```
config 字段数: 28
id: 'f5c3fc8d-3270-4d25-bec6-62dd79d07408'      ← 真实值
created_at: '2026-11-25T11:43:02+08:00'
```

**② PATCH /user/config 写隐私 + 水印 + 通知**
```
status: 0  msg: common.ok
回读: hide_comments=True  hide_followers=True  watermark_enabled=True
      enable_sms_notification=False  remark='T39C-VERIFY'
```

**③ PATCH /user/config 空体（保留 len(fields)==0 语义）**
```
status: 10008  msg: common.invalid_parameter
```

**④ D1 `GET /article?sort` 真的改变顺序**
```
sort=latest : 4bf422db,7dd4f514,55496578,6628e74c
sort=popular: 4bf422db,4ed95f8e,7dd4f514,55496578   ← 顺序不同，4ed95f8e 挤进第 2 位
sort=sort   : 4bf422db,7dd4f514,55496578,6628e74c
```

**⑤ D3 `GET /article/browse/history?order` 真的反向**
```
order=newest: 55496578 | 7dd4f514 | 4bf422db
order=oldest: 4bf422db | 7dd4f514 | 55496578   ← 完全相反
```

**⑥ D7 `GET /comment/{id}` replies 分页（造 5 条回复）**
```
page=1&limit=2  → replies=2
page=2&limit=2  → replies=2        ← 第 2 页有数据
page=1&limit=10 → replies=5        ← 全部
null 计数: 0
```
`replies` 是纯列表，但同层有 `reply_count` 供前端算页数。

**⑦ D8 `GET /tag?keyword`（造 T39风景 / T39人物 / T39其他）**
```
keyword=（无）: 3 条
keyword=T39  : 3 条
keyword=风景 : 1 条     ← 精确过滤
keyword=人物 : 1 条
keyword=zzz  : 0 条
name=风景    : 1 条     ← 旧参数向后兼容
```

**⑧ D4 `GET /message?type`（造 system/notification/private 各 1 条）**
```
type=（无）    : 3 条
type=system       : 1 条
type=notification : 1 条
type=private      : 1 条
```

**⑨ snake_case 生效 / camelCase 失效**
```
is_read=false      → 3 条    ← 全部未读，生效
is_read=true       → 0 条    ← 无已读，生效
is_broadcast=false → 3 条    ← 生效
isRead=true        → 3 条    ← 旧名被忽略（=全部），breaking change 实证
```

**⑩ UpdateContact 安全（见 §2.5）**
```
无验证码      → 10008
假验证码      → 20011 auth.captcha_invalid
email+phone   → 10008
```

**⑪ `/role/list?is_active`**
```
status: 0  条数: 2
```

### 4.4 测试数据与环境复原

```
msg=0  tag=0  comment=0  browse=0        ← 全部清零
remark=''  hide_c=false                   ← config 已复原
服务已 taskkill，端口 4031 释放
```

---

## 五、遗留与风险

### 5.1 本轮未做（需产品/前端决策）

| 事项 | 说明 |
|---|---|
| `/search/tags`、`/search/users` | **两个版本都没有这两个路径**。前端可能一直调 404 靠本地过滤兜住。补齐 `sort_by`/`category_id` 后改调 `/article/search` 即可；`/tag?keyword=` 与 `/user?keyword=` 已等价，**不需要新建** |
| 注册验证码是否改必填 | 当前是「非空才校验」（兼容旧客户端）。改必填只需删一层 `if` |
| `PATCH /user/profile/contact` 是否支持一次改两项 | 当前一次一项（验证码按 target+scene 签发，一个 code 不可能同时命中两个目标）。要支持得改成两个码字段或加 `target_type` |
| `dto.ArticleDto.Content` 的 omitempty | 见 todo.md R-2，仍未拍板 |

### 5.2 本轮发现但未修的既有缺陷

| 发现 | 说明 |
|---|---|
| `userConfigDto` 三个审计字段恒为空 | **本轮已修**（§1.5）。属审计发现的既有假契约，非本次引入 |
| `GET /message` 的 `List` 从不读过滤参数 | **本轮已修**（dto-msg 新增 `ListByUserFiltered`）。原实现传空 `MessageFilter{}`，只补注解会产出「文档说能过滤、实际不过滤」 |
| 4 处 camelCase query 参数 | **本轮已全修**（原以为 2 处，工具增强后又抓出 `onlyAuthor`/`isActive` 2 处） |

### 5.3 工具增强（防同类问题复发）

`scripts/json-naming-check.py` 原来只扫 `json:` / `form:` tag，**扫不到 handler 里的硬编码参数名**。
本轮加了 `scan_handler_query_keys()`：
- 覆盖 `c.Query("x")` / `c.GetQuery("x")` / `c.DefaultQuery("x", ...)`
- 覆盖辅助函数形式 `helper(c, "x")` —— **这是漏掉 `onlyAuthor`/`isActive` 一整轮的根因**
- 覆盖 `c.Param("x")`

`_comment_mask()` 重写了两次才对：
- 第一版「数 `/*` 个数」→ 被 `// 路径对齐 docs/02：/role**、/permission**` 这条中文注释骗成块注释开头，之后整个文件被当注释，漏掉 `role.go` 的违规
- 最终版改成**单词法扫描**（识别 `//`、`/* */`、`"..."`、`'...'`、反引号）

### 5.4 三条可复用的教训（已写入 AGENTS）

1. **「DTO 加了 + service 支持了」≠「接上了」**：handler 若用匿名 struct 或旧字段构造入参，新字段到不了业务层。而**正向用例永远发现不了** —— 必须验一个「该字段生效」的负向用例（假码必须报错、不同过滤值必须返回不同条数）。
   > 实证：dto-modeler 第一次只改 DTO+service，注册带假码仍返回 `status_code: 0`，看起来一切正常。
2. **「注解缺失」有两种**：一种是实现已有只是没声明（D6 cursor/limit）；另一种是**实现也缺**（`GET /message` 的 `type`，`List` 传空 filter）。只补注解会产出「文档说能过滤、实际不过滤」的更糟状态。**动手前先确认属于哪种**。
3. **工具撒谎比代码有债更危险**：命名检查器漏扫辅助函数形式，导致 4 处违规只报出 2 处，剩 2 处藏了一整轮。

### 5.5 未提交的并发改动说明

本轮 3 成员并行改的是**互斥文件集**，lead 已用 `verify-t39.py`（读产物不读意图）与**独立真机复验**（不复用成员结论）双重确认，全部通过后统一提交。
---

# 第二轮（收尾）

## 六、第二轮改了什么

| 文件 | 改动 |
|---|---|
| `internal/dto/user.go` | `UpdateUserRequest` 9 → **20 字段**；`CreateUserRequest` 8 → 9（`verification_code`）；`CertificationDto` +`certified_at` |
| `internal/service/user_service.go` | `UpdateUserInput` 20 字段；`UpdateUser` 覆盖全部（含 `hashPassword` / `ExistsUsername` / `SetLogoutAll` / `AssignRoleToUser`） |
| `internal/service/user_actions_service.go` | `certified_at` 映射 `reviewed_at`（model 无 `certified_at` 列） |
| `internal/dto/article.go` | `reaction_stats` / `user_reaction` 的注释补充 |
| `internal/repository/article.go` | 新增 `ReactionStatsMap` / `ReactionSet`（批量，避免 N+1） |
| `internal/service/article_view.go` | 新增 `emptyReactionStats()` / `orEmptyReactionStats()`；`BuildViews` 填充两字段 |
| `scripts/fix-param-example.py`（新建） | 把 `example(...)` 从描述引号内移到引号外，**259 处** |

## 七、第二轮修掉的两个既有缺陷

### 7.1 `reaction_stats` 恒为 `null`（docs/21 违规）

`dto.ArticleDto.ReactionStats map[string]int` 注释写着「无表态时是全 0 对象，不是 null」，
但**全仓没有任何组装点** → nil map 被 `json.Marshal` 输出 `null`。
同源的 `user_reaction` 也从未填充（空 string 不违反 docs/21，但同样是假契约）。

修法：
- `ReactionStatsMap(ctx, ids)` —— `GROUP BY article_id, reaction_type`，一次查全部
- `ReactionSet(ctx, userID, ids)` —— 当前用户对这批文章的表态
- `emptyReactionStats()` —— 7 个固定键全 0（对齐原 `buildEmptyReactionStats()`）
- `orEmptyReactionStats(actual)` —— 全 0 底 + 真实计数合并，**永远 7 键齐全**

真机：
```
无表态 → {"angry":0,"dislike":0,"haha":0,"like":0,"love":0,"sad":0,"wow":0}  user_reaction=''
点赞 love → 同上但 love:1                                              user_reaction='love'
null 计数: 0
```

### 7.2 `example(...)` 写在描述引号内是错的（259 处）

swag v1.16 的 `findAttr`（`operation.go:495`）用 `\s+example\(.*\)` 提取属性，**要求 example 前有空白**：

| 写法 | 结果 |
|---|---|
| `"标签名关键字（推荐）example(风景)"` | `）` 后无空格 → **完全匹配不到** → example 丢失（实测 6 个参数） |
| `"搜索关键字 example(风景)"` | 能提取，但 `example(风景)` **文字残留在 description**（259 处） |
| `"标签名关键字（推荐）" example(风景)` | ✅ 两者都对 |

`scripts/fix-param-example.py` 全局修正 259 处。修正后：

```
非 body 参数 269 | 有 example 267 | 无 2  （2 个是 file 类型，本就无法给示例）
description 残留 example( 的: 0   (改前 259)
```

## 八、第二轮验证

```
$ gofmt -l internal/ migrations/ cmd/   → 空
$ go build ./...                        → exit=0
$ go vet ./...                          → 零输出
$ go test ./... -count=1                → 16 包全绿
$ swag init ... --exclude tmp,internal/dto → exit=0, 危险词=0, paths=246 defs=291
$ python scripts/verify-t39.py          → 22 通过 / 0 未通过
$ python scripts/json-naming-check.py   → ✓ 全站 snake_case
$ python scripts/cmp-js-go-fields.py    → 字段缺失 35 → 3
```

**最终 3 个「残留」全是误报**：`Decoration` 的 `ACHIEVEMENT_BADGE`/`AVATAR_FRAME`/`COMMENT_BUBBLE`
—— JS schema 生成器把 `type` 字段的**枚举值当成了属性名**。我们用 `type` 字段表达，语义正确。

## 九、第二轮遗留

**dto-modeler 报告失败（无 closing message）**，但代码完整。lead 逐项独立验收：

```
UpdateUser 的 fields 覆盖 17 个键 + membership_start/end（map 字面量）+ role_ids（AssignRoleToUser）= 20 ✓
password → hashPassword（无明文落库）✓
username → ExistsUsername + ErrUsernameExists ✓
banned  → SetLogoutAll（封禁撤全部 token）✓
空请求 → len(fields)==0 && len(RoleIDs)==0 ✓
```

**未做的真机验证**（成员失败前没跑）：封禁后旧 token 是否立刻失效、改密码后新旧密码行为。
lead 已跑 `config` / `sort` / `order` / `replies` / `tag keyword` / `message type` /
`is_read` / `UpdateContact` 八组真机，**未单独验 `PATCH /user/{id}` 的封禁链路** —— 这是本轮唯一
没走到真机的改动，风险点是 `SetLogoutAll` 的 redis key 与中间件读取是否同源（代码路径看起来一致，
但没实测）。记入 todo 作为下轮首验项。

## 十、第二轮补验：封禁链路（发现并修掉一个真漏洞）

dto-modeler 失败前没跑封禁真机，lead 补验，**查出一个设计缺陷**。

### 10.1 先验代码路径（都正确）

```
封禁写入 → status=BANNED / banned=时间 / ban_reason 全部落库 ✓
SetLogoutAll → Redis key `user:<id>:logout_all` 确实写入 ✓
中间件 JWTAuthV2 → 判 `claims.IssuedAt.Before(logoutAllAt.Add(1s))` 才拒 ✓
```

### 10.2 第一次测试「失败」，实为测试参数错

用 `banned="2026-11-27T10:00:00Z"` 封禁 → token 仍可用。

查 Redis + 解 JWT 才发现：

```
logout_all = 1795773600 = 10:00:00   ← 我传的时间
token iat  = 1795773641 = 10:00:41   ← 登录比封禁「晚」41 秒
判 iat < logout_all → false → 不拒
```

**代码没错，是我传的时间比 token 签发还早。** 用当前时间重测 → `20001 auth.need_login`，通过。

### 10.3 但这暴露了真漏洞：传「过去时间」可绕过封禁

管理员若传 `banned="2020-01-01"`（补录语义：「这人早该封了」），
`status` 写成 `BANNED`，但**所有现存 token 都还有效** —— 封禁形同虚设。

实测复现：
```
PATCH /user/{id} {"banned":"2020-01-01T00:00:00Z"}
库内: BANNED banned=2020-01-01 00:00:00+00   ← 状态对
该用户 token 调 /user/profile → status_code: 0  ← ✗ 仍可用
```

### 10.4 修法

撤 token 的时刻取 `max(封禁时间, now)`：

```go
revokeAllAt = t
if now := time.Now(); revokeAllAt.Before(now) {
    revokeAllAt = now
}
```

**`banned` 字段仍写管理员传入的原值**（那是「封禁生效日期」，语义上就是过去时间），
**只有撤 token 的时刻取 max** —— 两个语义分开，不混用。

修复后复测（同样传 2020-01-01）：
```
库内: BANNED banned=2020-01-01 00:00:00+00   ← 保持原值
该用户 token 调 /user/profile → status_code: 20001 → ✓ 已失效
```

### 10.5 这条的教训

**「正向用例通过」不等于安全**：第一次测封禁看起来「代码是对的、我参数错了」，
但同一份代码在「传过去时间」这个**完全合法的输入**下就是漏洞。
`max(传入, now)` 这种兜底必须在写代码时就想清楚，不能靠测试发现。

测试数据已清（用户 + Redis key 全删，0 残留）。

---

# 第三轮：两项用户裁定事项

用户 2026-11-27 裁定：`/search/tags`、`/search/users` **需要新建**；`certified_at` **需要加列**。

## 十一、新建 `/search/tags` 与 `/search/users`

| 文件 | 改动 |
|---|---|
| `internal/dto/user.go` | 新建 `UserSearchItemDto`（12 字段，**不含 email/phone/wallet**） |
| `internal/service/user_service.go` | 新建 `ListUsersForSearch`（公开搜索，填 `is_followed`）+ `followingIDs` |
| `internal/handler/user.go` | 新建 `SearchUsers`（`@Router /search/users`） |
| `internal/handler/tag.go` | 新建 `SearchTags`（`@Router /search/tags`，复用 `parseTagQuery` + `TagService.List`） |
| `internal/router/search_routes.go` | 注册两条（可选登录：游客能搜，登录后拿到 `is_followed`） |

### 关键设计：`/search/users` 为什么不复用 `UserDto`

`UserDto` 有 31 字段，含 `email` / `phone` / `wallet` / `banned` / `ban_reason` / `invite_code` / `role_ids`。
搜索是**公开端点**（任何登录用户都能搜人），直接复用等于把全站用户的联系方式和余额
暴露给任何人。新建 `UserSearchItemDto` 只留卡片展示 + 关注态需要的 12 个字段。

### 为什么 `/search/tags` 要独立方法而不直接挂 `Tag.List`

`Tag.List` 的 swag 注解锚点固定在 `@Router /tag`。直接复用它，`/search/tags` 这条
**不会出现在 swagger 里**（实测确认：paths 只多了 `/search/users` 一条）。
所以写了个 `SearchTags` 薄方法 —— 内部 `parseTagQuery` + `TagService.List` 完全复用，
零重复实现，额外成本只有 6 行注解。

### 真机验证

```
/search/tags
  keyword=（无）→ 2 条
  keyword=T43  → 2 条
  keyword=风景 → 1 条     ← 过滤生效
  keyword=zzz  → 0 条
  status_code 全 0

/search/users?keyword=test
  条数: 2
  字段: article_count avatar created_at description follower_count
        following_count id is_followed is_member level nickname username
  email=None  phone=None  wallet=None     ← 敏感字段未泄漏
  is_followed: False
  null 计数: 0
```

## 十二、`certified_at` 加独立列

### 为什么不能只映射 `reviewed_at`

| 字段 | 语义 | APPROVED | REJECTED | REVOKED |
|---|---|---|---|---|
| `reviewed_at` | 审核**动作**时间 | 有值 | **有值** | 有值 |
| `certified_at` | 认证**生效**时间 | 有值 | **NULL** | **NULL** |

映射到 `reviewed_at` 会让前端在「被拒」场景显示「认证于 xxxx」—— 语义完全错。
用户裁定加列，已加。

### 改动

| 文件 | 改动 |
|---|---|
| `internal/model/user.go` | `UserCertification` +`CertifiedAt *time.Time`（`column:certified_at`） |
| `internal/service/user_actions_service.go` | `AuditCertification` 通过时写 `certified_at`；`RevokeCertification` 置 NULL；`certDto` 读独立列 |
| `migrations/migrate_user.go` | 已含 `&model.UserCertification{}`，AutoMigrate 自动加列 |

真机确认列已建：`user_certification.certified_at | timestamp with time zone`。

### 真机三态验证

```
① APPROVED  → status: APPROVED  reviewed_at: 2026-11-27T19:09:13+08:00
                                certified_at: 2026-11-27T19:09:13+08:00   ← 两者都有
② REVOKED   → status: REVOKED   reviewed_at: 2026-11-27T19:09:32+08:00
                                certified_at 字段存在: False               ← 消失
                库内: REVOKED certified=NULL
③ REJECTED  → status: REJECTED  reviewed_at: 2026-11-27T19:09:34+08:00
                                certified_at 存在: False                   ← 未误写
                库内: REJECTED certified=NULL
```

## 十三、第三轮质量门

```
gofmt -l internal/ migrations/ cmd/  → 空
go build ./...                       → exit=0
go vet ./...                         → 零输出
go test ./... -count=1               → 16 包全绿
swag init ... --exclude tmp,internal/dto → exit=0, 危险词=0, paths 246 → 248, defs 291 → 292
json-naming-check                    → ✓ 全站 snake_case
verify-t39                           → 22 通过 / 0 未通过
cmp-js-go-fields                     → 字段缺失 3（全是 Decoration 枚举误报）
```

新增 swagger 条目：`/search/tags`、`/search/users`（各 3 参数：keyword/page/limit）
新增 schema：`dto.UserSearchItemDto`（14 属性，含 2 个 omitempty）

测试数据已清：`tags` T43 系列 0 条、`user_certification` t43 系列 0 条。

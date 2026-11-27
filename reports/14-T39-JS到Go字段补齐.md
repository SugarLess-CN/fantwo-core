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
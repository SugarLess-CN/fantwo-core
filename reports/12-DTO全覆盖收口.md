# 12 — DTO 全覆盖收口报告

> 任务：T-40（DTO 全覆盖工程）　｜　收口日期：2026-11-25
> 执行：8 个批次并行 + lead 收尾 + 本报告由 `dto-modeler`（第 1 批机制验证批执行者）撰写
> 规范依据：[`docs/18-DTO复用规范.md`](../docs/18-DTO复用规范.md)、[`docs/21-JSON序列化规范.md`](../docs/21-JSON序列化规范.md)
> 提交链：`d18c513` → `1f096a5` → `5d9f45f` → `198b6cf` → `723a32e` → `db69f47` → `b1648d1` → `2909641` → `871d7c7` → `a3b0ab7` → `43d574f` → `6e8f9ee`

---

## 〇、一句话结论 + 与任务书的 4 处偏差（先说结论，再看细节）

**结论**：`data` 未具名从 **223（70%）降到 3（0%）**，definitions 从 **6 涨到 260**，service 层契约漂移 **42 → 0**，真机全站 null **归零**。机制（swag 的 `{data=dto.Xxx}` 嵌套覆盖）经 8 个批次 317 个操作验证成立。

**但本报告独立复核时发现 3 处与任务书/既有说法不一致，先列在此，细节见 §五**：

| # | 任务书/既有说法 | 实测 | 影响 |
|---|---|---|---|
| 1 | `dto-coverage.py --drift` 报「✓ 无契约漂移」= 全部清零 | 还剩 **3 个函数**返回 `*map[string]interface{}`：`category.go:260 Remove`、`tag.go:283 Follow`、`tag.go:316 Unfollow`。脚本正则只匹配 `(map[string]interface{}, error)`，**漏了带 `*` 的形态** | 这 3 个 handler 的注解已声明 `dto.DeleteResultDto` / `dto.TagFollowDto`，运行时却抛 map —— **残余契约漂移，正是本轮要消灭的那类问题** |
| 2 | 覆盖率「未具名 3（0%）」，剩余 3 条全是 302 重定向 | 深查 `data.list` 后，另有 **2 条分页接口的 list 无 items**：`GET /search`、`GET /tag/followed/list`（注解仍写旧壳 `{object} ResponsePageBody`，swagger 里是 `internal_handler.ResponsePageBody`） | 脚本只判「data 不是空 `{}`」，没继续深挖 `data.list` 是否具名 → **真实未具名 5 条（1.6%）**，不是 3 条 |
| 3 | 任务书说「新增 13 个 dto 文件」 | 实际 **17 个**（`article` `audit` `collection` `comment` `common` `config` `content_meta` `decoration` `emoji` `message` `order` `payment` `points` `profile` `rbac` `upload` `user`），合计 **287 类型 / 1468 字段 / 3845 行** | 无功能影响，纯数字口径 |
| 4 | 「Apifox 同步进行中」（schema 260 已导） | 导入**已完成且 580/580 全绿**，但批次文件里 **835 处 `$ref` 用的是 `#/definitions/`**（OAS3 无效路径）→ Apifox 侧所有嵌套引用**静默降级为空对象**。脚本已修，**Apifox 仍是脏数据，需重导** | **P0：DTO 工程对 Apifox 的价值当前等于 0**。详见 §五 R-8 |

> **顺带澄清一个容易看错的数字**：契约漂移出现过 **42 / 37 / 30** 三个数，都出现在不同文档里，容易误以为互相矛盾。实际是三个时点：
> **42** = lead 派发时的人工清单（`role_service.go` 手工数了 7）；
> **37** = `--drift` 工具首次实测（该文件实为 6，且已排除 4 个 oauth 误报）；
> **30** = `6e8f9ee` 清零时真正待改的数量（37 里 7 个期间已被成员改完）。
> 数据来源：`git log -1 871d7c7` 与 `git log -1 6e8f9ee` 的提交正文。

**为什么这三条要说在最前面**：DTO 这件事的价值全在「文档 == 运行时」。如果收口报告只贴绿色数字，下一个人会以为 100% 完成，去查 `/search` 的前端会拿到空的 list 定义。**漏报的比没做的更危险**。

---

## 一、本轮改了什么

### 1.0 起点：为什么非做不可

改造前的 `docs/swagger/swagger.json`（246 paths / 6 definitions）：

```json
"200": {"schema": {"$ref": "#/definitions/internal_handler.ResponseBody"}}
```
```json
"internal_handler.ResponseBody": {
  "properties": {"data": {}, "message": {...}, "status_code": {...},
                 "trace_id": {...}, "timestamp": {...}}
}
```

**五个具体病灶**（docs/18 §一 有原文）：

| 问题 | 改造前 | 后果 |
|---|---|---|
| `data` 无类型 | `{}` | 前端/Apifox 看不到任何业务字段，等于没有文档 |
| 分页 `items` 无类型 | 只有 `Inner/PageMeta` 骨架 | 列表接口每条记录字段全缺 |
| 请求体内联 | 245 个接口只有 1 个具名（`loginRequest`） | 改一个字段要翻 245 处 |
| 命名带包名前缀 | `internal_handler.ResponseBody` | 进 Apifox 显示一长串 |
| 命名不规范 | `loginRequest` 小写开头 | 违反 Go 导出规则 |

**根因不是"大家偷懒"**：swag 的 schema 名 = `包名末段.类型名`，而所有 handler 都在 `internal/handler` 包 —— 在那里定义类型，名字必然是 `internal_handler.Xxx`。**要改善命名，只能把类型搬到另一个包**，这就是 `internal/dto/` 存在的唯一理由。

### 1.1 批次 1（`dto-modeler`，本人）：机制验证批

**这是整轮工程的关键一步** —— 不是「顺手抽几个 DTO」，而是要**先证明 swag 的嵌套覆盖语法真的生效**，否则后面 7 个批次全是在空转。

| 文件 | 行数 | 类型 | 字段 | 内容 |
|---|---|---|---|---|
| `internal/dto/common.go` | 78 | 5 | 17 | `ResponseBody` `ResponsePageBody` `PageBody` `PageMeta` `SuccessDto` |
| `internal/dto/user.go` | 586 | 59 | 264 | user/auth 域全部请求 + 响应 |

改的 handler：`auth.go`（12 个 handler / 15 行）、`user.go`（24 / 26）、`user_actions.go`（21 / 21）。

**验证结果**（这一条决定了后面所有批次能不能派）：
```
paths: 246  defs: 6 → 43（38 个 dto.*）
/user/profile 200 schema:
{"allOf":[{"$ref":"#/definitions/dto.ResponseBody"},
          {"type":"object","properties":{"data":{"$ref":"#/definitions/dto.UserProfileDto"}}}]}
```
`data` 从一个空 `{}` 变成一个具名 `$ref`。**机制成立。**

**这一批顺带踩出两条 swag 硬约束**（docs/18 已收录，是本轮最大的方法论产出）：

**① `--exclude` 必须带 `internal/dto`**
不加时，dto 包被 swag 以两种 PkgPath 各登记一次（`go list -deps` 给完整模块路径 / 目录扫描给相对路径）→ swag 判定 `NotUnique` → 改用全路径前缀，schema 名变成
`github_com_SugarLess-CN_fantwo-core_internal_dto.UserDto`。
**实测对比**（同一份代码，只差一个参数）：

```bash
# 不加 internal/dto
swag init ... --exclude tmp
→ definitions: github_com_SugarLess-CN_fantwo-core_internal_dto.UserDto

# 加了
swag init ... --exclude tmp,internal/dto
→ definitions: dto.UserDto
```
为什么 `internal/dto` 能被 exclude 掉还不影响解析：**该包里没有 handler 注解**（annotations 全在 handler），exclude 只跳过「注解扫描」；类型定义由 `go list -deps` 的依赖解析照常登记，字段完整。

**② 用了 `dto.Xxx` 注解的 handler 文件，必须自己 import dto**
swag 解析 `{object} dto.LoginRequest` 时，**只在注解所在文件的 import 列表**里找包路径。只在别的文件（如 `swagger_types.go`）import 是无效的：

```
ParseComment error in file internal/handler/user.go for comment:
'@Success 200 {object} dto.ResponseBody{data=dto.UserProfileDto}':
cannot find type definition: dto.ResponseBody
```

落地方式（每文件末尾一行，不参与运行逻辑）：
```go
var _ = dto.ResponseBody{}
```

**③ 分页契约与 docs/18 初稿不符（我按真实契约落，没照抄文档）**
§3.1 当年写的是 `PageBody{list,total,page,size}`，而运行时是 `response.PageData{list, meta:{total,page,limit,totalPages}}`。照文档写会造出一份**与实现不符**的文档，故改为 `PageBody{List, Meta PageMeta}`。docs/18 后续已同步修正。

**④ 顺手修掉 3 处 swagger 路径错误**（原 `@Router` 少了 `/user` 前缀）：
`user_actions.go` 的 `/certification/admin/applications` 三条实际注册在 `v1.Group("/user")` 下，文档路径比真实路径少一层 → 前端按文档生成 SDK 会 404。真机验证：带 `/user` → 200，不带 → 404。改后 swag 的 `declared multiple times` 警告随之消失。

### 1.2 批次 2–7：按域铺开（17 个文件 / 287 类型 / 1468 字段）

| 文件 | 行数 | 类型 | 字段 | 覆盖域（对应 swagger tag） | 批次 |
|---|---|---|---|---|---|
| `common.go` | 78 | 5 | 17 | 全部接口的外壳 | 1（本人） |
| `user.go` | 586 | 59 | 264 | 用户管理 37 + 用户认证 32 的一部分 | 1（本人） |
| `article.go` | 334 | 31 | 148 | 文章 34 | 2 |
| `comment.go` | 77 | 6 | 28 | 评论 10 | 2 |
| `upload.go` | 313 | 27 | 139 | 上传 11 | 3 |
| `emoji.go` | 155 | 11 | 56 | 表情包 21 | 3 |
| `decoration.go` | 222 | 10 | 103 | 装饰品 21 | 3 |
| `collection.go` | 156 | 13 | 48 | 合集 11 | 4 |
| `profile.go` | 39 | 2 | 5 | `/profile/menus` | 4 |
| `points.go` | 275 | 16 | 94 | 积分 16 | 5 |
| `order.go` | 114 | 5 | 24 | 订单 10 | 5 |
| `payment.go` | 126 | 5 | 25 | 支付 8 | 5 |
| `message.go` | 299 | 23 | 106 | 消息 19 | 6 |
| `config.go` | 163 | 10 | 43 | 系统配置 17 | 7 |
| `rbac.go` | 173 | 15 | 49 | 角色 13 + 权限 6 | 7 |
| `audit.go` | 141 | 8 | 36 | 内容审核 6 | 7 |
| `content_meta.go` | 594 | 41 | 283 | 标签 10 / 分类 5 / 轮播 6 / 成就 8 / 举报 6 / 邀请 6 / 搜索 5 / 统计 2 | 7–8 |
| **合计** | **3845** | **287** | **1468** | **246 paths / 317 操作 / 24 tags** | — |

> **批次归属与任务书口径的差别**：任务书按「成员」分 8 批，本表按「文件」列。类型数也有出入（任务书称 message 18 类型，实际 `message.go` 23 个；称 config/role/permission/audit 23 类型，实际 10+15+8=33）。成员分工以 lead 的派发记录为准，本节只保证**文件与数字可复算**：
> `python -c "import re,io,glob; ..."` 见 §四 D-6。

**逐域的「为什么拆这几个 DTO」原则**（docs/18 §五 的落地）：

- **同一个结构被 2 个以上接口用 → 必须抽**（如 `UserDto` 被 `/user`、`/user/{id}`、`/user/{id}/followers`、登录 user 字段共用）
- **只有 1 个接口用，但仍具名** —— 为了 swagger 能生成可读 schema 名（如 `BatchMembershipCheckDto` 只有 1 字段、1 接口）
- **不抽** —— 错误响应的 `data`（空字段省略，无需定义）、文件流（`@Produce octet-stream`）

### 1.3 第二条线：JSON 空值规范（`198b6cf`，与 DTO 交织）

DTO 抽出来之后立刻暴露第二个问题：**dto 里的 `*string` 会输出 `null`**。这一轮同步做了（docs/21）：

| 改动 | 位置 | 改前 → 改后 |
|---|---|---|
| 响应结构收归 dto | `service.UserView` / `RoleBrief` / `CategoryView` | 各写一份结构 → 改为 `dto.Xxx` 别名，单一来源 |
| 删掉手写 map | `user_service.go` 的 `toMap` | `map[string]interface{}` 逐字段抄 → 直接用 dto |
| 认证视图 | `certView` → `certDto` | 返回 map → 返回 `dto.CertificationDto` |
| `GetConfig` 返回类型 | `*model.UserProfile` | 直接把 model 抛给前端（加一列就自动泄漏）→ `dto.UserConfigDto` |
| 辅助函数 | `internal/service/common.go` | 新增 `strOrEmpty(*T) string` / `sliceOrEmpty([]T) []T` |

**为什么 model 层指针不改**：数据库要区分 SQL NULL 与空串（`*string` 是正确建模）；转换只发生在 **dto/View 组装层**。这条边界如果搞反，会把 DB 语义污染成 API 语义。

### 1.4 第三条线：工具与制度

| 产出 | 提交 | 作用 |
|---|---|---|
| `scripts/dto-coverage.py` | `723a32e` | 量化「data 未具名」——**发现「外层 `$ref` 掩盖 data 空对象」的判断陷阱** |
| 同上 `--drift` | `871d7c7` | 静态扫 service 层是否还有 `map[string]interface{}` 返回（补 coverage 的盲区） |
| `--list` | `723a32e` | 按域列出未具名接口，供各批次自查验收 |
| `docs/18` §三硬约束 + §五 Apifox 流程 | 多轮 | 把踩过的坑写成规范，避免下一批重复 |
| `docs/21` + `docs/fixes/` 制度 | `198b6cf` | 空值契约 + 修复文档七节机制（F-001~F-007） |
| `scripts/apifox-import.mjs` 补 `schemaOverwriteBehavior` | `db69f47` | **不带它 Apifox 不更新已有数据模型**，改完 DTO 看到旧结构而日志显示成功 |

**`schemaOverwriteBehavior` 这个坑值得单独说**：随手一个遗漏，后果是「本地 DTO 改了、Apifox 里还是老的、日志全是 ✓」。没有真机反查就永远发现不了。这印证了一条方法论：**导入成功 ≠ 数据正确**。

---

## 二、功能变化（对前端/运维可见）

**本章是给前端看的适配清单。DTO 抽取本身不改变运行时行为，但同期做的空值规范化 + 2 个契约纠错是 breaking change。**

### 2.1 空值表现（最大一类影响）

| 场景 | 改前 | 改后 | 前端要做什么 |
|---|---|---|---|
| 字符串字段为空 | `"email": null` | `"email": ""` | `if (x === null)` → `if (!x)`；`null` 判断可删 |
| 数组为空 | `"list": null` / `"images": null` | `"list": []` / `"images": []` | `.map()` 不再需要 `?.` 兜底 |
| 时间字段为空 | `"birthDate": "0001-01-01T00:00:00Z"` 或 `null` | **字段整个不出现** | `if ('birthDate' in obj)`；不要 `new Date(x)` 直接吃 |
| 嵌套对象为空 | `"certification": null` | **字段整个不出现** | 同上，用属性存在性判断 |

**为什么时间不传 `""`**：`new Date("")` 得到 `Invalid Date`；`time.Time` 零值输出 `0001-01-01T00:00:00Z` 看着像有效数据。**不传是唯一诚实的表示**（docs/21 §二）。

**真机证据**（§四 V-3）：登录响应的 `user` 对象 28 个字段，`null` 计数 **0**；空字符串字段 6 个（`avatar` `description` `background` `address` `email` `phone`）全部为 `""`。

### 2.2 两个真实契约纠错（不是风格问题，是错）

**① `DeletionStatusDto.Status`：`string` → `int`**

```go
// 改前（文档说状态是字符串）
Status string `json:"status" example:"pending"`
// 改后（与 model.DeletionStatus* 同源）
Status int `json:"status" example:"0"`   // 0=无 1=待注销 2=已撤销
```
**依据**：`user_actions_service.go` 的 `RequestDeletion` / `DeletionStatus` / `CancelDeletion` 实际返回的 `model.DeletionStatusNone/Pending/Canceled` 是 **int 常量**。文档声明 string，前端按 `=== 'pending'` 判断会**永远为 false** —— 这是"照文档写代码必错"的典型。

**② `FavoriteItemDto` 的 `cover` / `summary` / `authorId`：`*string` → `string`**

```go
// 改前
Cover *string `json:"cover"`
// 改后
Cover string `json:"cover"`   // 空时 ""
```
**依据**：docs/21 规则①（字符串空时 `""`，不用指针）。**这是 breaking change**：前端原来判 `null` 的地方要改成判空串。

### 2.3 文档层面的变化（不影响运行时，但影响前端生成 SDK）

| 项 | 改前 | 改后 |
|---|---|---|
| `data` 字段类型 | 空 `{}`（零定义） | `$ref: dto.XxxDto`（真实字段） |
| 分页 `data.list` | 空 `{}` | `items: $ref: dto.XxxDto`（两级 `allOf` 嵌套） |
| 请求体 | 245 个内联匿名 / 只有 1 个具名 | 全部 `dto.XxxRequest` |
| schema 名 | `internal_handler.ResponseBody` | `dto.ResponseBody` |
| 命名规范 | `loginRequest`（小写开头） | `dto.LoginRequest` |
| 分页元信息 | `{list,total,page,size}`（文档）与 `{list,meta}`（运行时）不一致 | 统一为 `{list,meta:{total,page,limit,totalPages}}` |

### 2.4 运维/测试侧

- **`docs/swagger/swagger.json` 体积与内容**：definitions 6 → 260（`dto.*` 256 + 4 个遗留 `internal_handler.*`），Apifox 导入从「只有壳」变成 260 个数据模型
- **质量门新增一条检查**：`swag` 必须 `grep -icE "unsupported|any is|cannot find|declared multiple" tmp/swag.out` = 0。原因：`interface{}` 字段带 `example` 时 swag 报 `unsupported` 但 **exit=0**，会静默产出残缺 definitions（AGENTS Step 2 已固化）

---

## 三、配置项 / 接口清单

### 3.1 新增文件

| 路径 | 行数 | 类型 | 字段 |
|---|---|---|---|
| `internal/dto/*.go`（17 个） | 3845 | 287 | 1468 |

### 3.2 schema（swagger definitions）

| 指标 | 数值 |
|---|---|
| `definitions` 总数 | **260** |
| `dto.*` | **256** |
| 遗留 `internal_handler.*` | **4**（`ResponseBody` / `ResponsePageBody` / `ResponsePageInner` / `ResponsePageMeta`） |
| `tmp_*` 污染 | 0 |

### 3.3 接口覆盖（246 paths / 317 操作，按 tag）

| tag | 操作 | tag | 操作 | tag | 操作 |
|---|---|---|---|---|---|
| 用户管理 | 37 | 积分 | 16 | 轮播 | 6 |
| 文章 | 34 | 角色 | 13 | 内容审核 | 6 |
| 用户认证 | 32 | 合集 | 11 | 邀请 | 6 |
| 装饰品 | 21 | 上传 | 11 | 权限 | 6 |
| 表情包 | 21 | 评论 | 10 | 举报 | 6 |
| 消息 | 19 | 订单 | 10 | 分类 | 5 |
| 系统配置 | 17 | 标签 | 10 | 搜索 | 5 |
| 成就 | 8 | 支付 | 8 | 统计 | 2 |

### 3.4 工具

| 命令 | 作用 | 输出示例 |
|---|---|---|
| `python scripts/dto-coverage.py` | 统计 `data` 未具名的操作数 + 按域明细 | `总操作数 317 / data 未具名 3 (0%)` |
| `python scripts/dto-coverage.py --list` | 逐条列出未具名接口 | `GET /user/oauth/qq/web/callback` |
| `python scripts/dto-coverage.py --drift` | 静态扫 service 层 `map[string]interface{}` 返回 | `✓ 无契约漂移`（**注意 §五 R-1 的盲点**） |
| `python scripts/dto-coverage.py --all` | 以上全部 | — |

---

## 四、验证证据

> 全部为 2026-11-25 本机实跑输出，命令与结果原样粘贴。

### D-1　质量门

```bash
$ gofmt -l internal/ migrations/ cmd/
（空输出）

$ go build ./...
$ echo $?
0

$ go vet ./...
$ echo $?
0

$ go test ./... -count=1
ok  	github.com/SugarLess-CN/fantwo-core/internal/config	0.527s
ok  	github.com/SugarLess-CN/fantwo-core/internal/event	0.049s
ok  	github.com/SugarLess-CN/fantwo-core/internal/middleware	0.105s
ok  	github.com/SugarLess-CN/fantwo-core/internal/model	0.080s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/authtoken	0.125s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode	0.065s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/filecheck	0.058s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n	0.058s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx	0.078s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/oauthkit	0.150s
ok  	github.com/SugarLess-CN/fantwo-core/internal/pkg/response	0.102s
ok  	github.com/SugarLess-CN/fantwo-core/internal/queue	2.068s
ok  	github.com/SugarLess-CN/fantwo-core/internal/router	0.073s
ok  	github.com/SugarLess-CN/fantwo-core/internal/service	0.432s
ok  	github.com/SugarLess-CN/fantwo-core/internal/service/infra	0.070s
ok  	github.com/SugarLess-CN/fantwo-core/internal/ws	0.162s

$ go test ./... -count=1 | grep -cE "^ok"
16
```

### D-2　swag 生成

```bash
$ "$(go env GOPATH)/bin/swag.exe" init -g cmd/server/main.go -o docs/swagger \
    --parseDependency --parseInternal --exclude tmp,internal/dto > tmp/swag.out 2>&1
$ echo "exit=$?"
exit=0

$ grep -icE "unsupported|any is|cannot find|declared multiple" tmp/swag.out
0

$ tail -4 tmp/swag.out
2026/11/25 20:45:02 Generating dto.IdentityDto
2026/11/25 20:45:02 create docs.go at docs/swagger/docs.go
2026/11/25 20:45:02 create swagger.json at docs/swagger/swagger.json
2026/11/25 20:45:02 create swagger.yaml at docs/swagger/swagger.yaml
```

**产物可复现性**（本报告撰写时复核）：重跑上述命令后 `git status --porcelain` 里**没有出现 `docs/swagger/*`** —— 说明生成结果与已提交版本字节一致，注解与产物没有漂移。若下次重跑后 `docs/swagger` 变红，就意味着有人改了注解但没重新生成。

### D-3　definitions 与污染检查

```bash
$ python -c "import json;d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'));\
defs=d['definitions'];print('paths:',len(d['paths']),'defs:',len(defs),\
'dto.*:',len([k for k in defs if k.startswith('dto.')]));\
print('非 dto:',sorted(k for k in defs if not k.startswith('dto.')));\
print('污染:',[x for x in defs if 'tmp' in x] or '无')"
paths: 246 defs: 260 dto.*: 256
非 dto: ['internal_handler.ResponseBody', 'internal_handler.ResponsePageBody',
         'internal_handler.ResponsePageInner', 'internal_handler.ResponsePageMeta']
污染: 无
```

### D-4　覆盖率（工具口径）

```bash
$ python scripts/dto-coverage.py
总操作数      317
data 未具名   3  (0%)
已具名        314  (99%)

未具名按域:
  /user                     3
```

```bash
$ python scripts/dto-coverage.py --list | tail -5
未具名按域:
  /user                     3
  GET     /user/oauth/github/callback
  GET     /user/oauth/qq/web/callback
  GET     /user/oauth/wechat/web/callback
```

这 3 条**是正确例外**：实际是 `c.Redirect(http.StatusFound, loc)`，响应体为空、token 走 `Location` 头，注解已标 `@Success 302` + `@Header`，不该硬编 200 的 `$ref`。

### D-5　契约漂移（工具口径 vs 深查）

```bash
$ python scripts/dto-coverage.py --drift
==============================================================
契约漂移检查：service 层返回 map[string]interface{} 的函数
==============================================================
✓ 无契约漂移
```

**但深查一次就露出 3 个**（这是本报告 §〇 第 1 条）：

```bash
$ grep -rn "map\[string\]interface{}, error)\|map\[string\]interface{}, \*errcode.Err)" internal/service/ | wc -l
4

$ grep -rn "map\[string\]interface{}, error)\|map\[string\]interface{}, \*errcode.Err)" internal/service/ | head -5
internal/service/category.go:260:func (s *CategoryService) Remove(...) (*map[string]interface{}, *errcode.Err) {
internal/service/infra/search_service.go:522:func (s *Searcher) fetchArticles(...) ([]map[string]interface{}, error) {
internal/service/tag.go:283:func (s *TagService) Follow(...) (*map[string]interface{}, *errcode.Err) {
internal/service/tag.go:316:func (s *TagService) Unfollow(...) (*map[string]interface{}, *errcode.Err) {
```

- `search_service.go:522` 是**内部函数**（`fetchArticles` 取 ES 结果），不算对外契约，正确
- 其余 **3 个是指针形态 `*map[string]interface{}`**，脚本正则 `\(\s*map\[...\]...\)` 不匹配 `(*map[...]` → **漏报**

而这 3 个 handler 的注解确实已经声明了 dto：
```bash
$ grep -n -B8 "func (h \*CategoryHandler) Remove" internal/handler/category.go | grep "@Success"
123://	@Success	200	{object}	dto.ResponseBody{data=dto.DeleteResultDto}

$ grep -n -B8 "func (h \*TagHandler) Follow" internal/handler/tag.go | grep "@Success"
177://	@Success	200	{object}	dto.ResponseBody{data=dto.TagFollowDto}
```
→ **文档说 `dto.TagFollowDto`，运行时抛 `*map[string]interface{}`**。就是本轮要消灭的那类漂移，只是换了个指针写法逃过检查。

### D-6　深查 `data.list`（工具漏报的第 2 条）

```bash
$ python -c "
import json
d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'))
defs=d['definitions']
print('internal_handler.ResponsePageInner:',json.dumps(defs['internal_handler.ResponsePageInner'],ensure_ascii=False))
"
internal_handler.ResponsePageInner: {"type":"object","properties":{"list":{},"meta":{"$ref":"#/definitions/internal_handler.ResponsePageMeta"}}}
```

`list` 是空 `{}`。定位到具体接口：

```bash
$ python -c "...（沿 data → list → items 深挖）..."
data.list 未具名的接口: 2
   ('GET', '/search', 'list 无 items')
   ('GET', '/tag/followed/list', 'list 无 items')
```

两条的注解源头：

```bash
$ grep -n "@Success.*ResponsePageBody" internal/handler/*.go
internal/handler/search.go:129://	@Success	200	{object}	ResponsePageBody
internal/handler/tag.go:98://	@Success	200	{object}	ResponsePageBody
```

**为什么脚本没报**：`data_typed()` 只判断「`data` 不是空 `{}`」。旧壳 `internal_handler.ResponsePageBody.data` 指向 `ResponsePageInner`（非空）→ 判为「已具名」，但它内部的 `list` 是 `{}` → 前端拿不到列表项字段。

### D-7　DTO 文件实测统计

```bash
$ wc -l internal/dto/*.go | tail -1
  3845 total

$ ls internal/dto/*.go | wc -l
17

$ python -c "...（正则统计每个文件的 type 数与字段数）..."
article.go          31 类型  148 字段
audit.go             8 类型   36 字段
collection.go       13 类型   48 字段
comment.go           6 类型   28 字段
common.go            5 类型   17 字段
config.go           10 类型   43 字段
content_meta.go     41 类型  283 字段
decoration.go       10 类型  103 字段
emoji.go            11 类型   56 字段
message.go          23 类型  106 字段
order.go             5 类型   24 字段
payment.go           5 类型   25 字段
points.go           16 类型   94 字段
profile.go           2 类型    5 字段
rbac.go             15 类型   49 字段
upload.go           27 类型  139 字段
user.go             59 类型  264 字段
TOTAL              287 类型 1468 字段
```

### D-8　真机空值扫描（`go run ./cmd/server` + curl）

环境：`SERVER_PORT=3993`，PG 容器 `postgre` / 库 `fantwo_core`，Redis 容器 `redis` 在线；账号 `test_admin`（`scripts/test-seed.sh` 产出）。

**登录响应 user 对象**：

```bash
$ curl -s -X POST http://127.0.0.1:3993/api/v1/user/login \
    -H 'Content-Type: application/json' -H 'Device-Id: report-check' \
    -d '{"account":"test_admin","password":"Test123456"}' | python -c "..."
user 字段数: 28
null 值字段: []
:null 出现次数: 0
原始响应片段: {"id":"5491ed36-...","username":"test_admin","nickname":"test_admin",
"avatar":"","description":"","background":"","address":"","gender":"other",
"email":"","phone":"","myInviteCode":"","status":"ACTIVE","articleCount":0,...}
```

**25 个 GET 接口逐个扫 `:null`**：

```
/user/profile                    null=0  status_code=0
/user/config                     null=0  status_code=0
/user/sign-in/stats              null=0  status_code=0
/user/sign-in/records            null=0  status_code=0
/user/wallet/balance             null=0  status_code=0
/user/wallet/transactions        null=0  status_code=0
/user/wallet/statistics          null=0  status_code=0
/user/profile/favorites          null=0  status_code=0
/user                            null=0  status_code=0
/profile/menus                   null=0  status_code=0
/user/certification/identity     null=0  status_code=0
/user/commission/config          null=0  status_code=0
/user/membership/batch-check     null=0  status_code=0
/article?page=1&limit=2          null=0  status_code=0
/comment?articleId=4bf422db-...  null=0  status_code=0
/tag                             null=0  status_code=0
/category                        null=0  status_code=0
/banners                         null=0  status_code=0
/emoji/packs                     null=0  status_code=0
/points/balance                  null=0  status_code=0
/invite/my                       null=0  status_code=0
/invite/stats                    null=0  status_code=0
/tag/followed/list               null=0  status_code=0
/search?keyword=x                null=0  status_code=10007   ← ES 未配置，预期失败
/tag/following/list              null=0  status_code=NON-JSON ← 无此路由，404 纯文本
=== 汇总: 请求 25 个，status_code=0 的 23 个，:null 合计 0 ===
```

两条非 0 都不是缺陷：`/search` 未配 ES（返回内部错误码），`/tag/following/list` 路由不存在（实际是 `/tag/following/list` 未注册，swagger 里也没有该 path —— 是我探测时猜错的路径，**404 纯文本是 gin 默认行为，属已登记的 P2 遗留**）。

**`ArticleDto.Content` 的 omitempty 真机表现**：

```
列表 /article?page=1&limit=3（withContent=false）：
  4bf422db-...  content字段存在: False  | summary: ''
  7dd4f514-...  content字段存在: False  | summary: ''
  55496578-...  content字段存在: False  | summary: ''

详情 /article/4bf422db-...：
  content 字段存在: True | 值长度: 11
  summary 字段存在: True | 值: ''
```

→ 列表里 `content` **整个字段不出现**（未加载正文），`summary` 是 `""`。这正是 §五 R-2 那条待决策问题的直接证据。

收尾：服务已 `taskkill`（`after_kill=000`），临时 token 文件已删。

### D-9　Apifox 同步状态（**含一个 P0：导入 100% 成功，但引用全断**）

```bash
$ head -5 tmp/apifox-import.log
[2026-11-25 12:42:24] [INFO] ========== Apifox 批量导入开始 ==========
[2026-11-25 12:42:24] [INFO] 项目: 6542782 | 源目录: tmp/apifox-enhanced/ | dry_run: false | start: 1
[2026-11-25 12:42:24] [INFO] schema: 260 | operation: 320

$ grep -c '✓ \[schema\]' tmp/apifox-import.log
260
$ grep -c '✓ \[op\]' tmp/apifox-import.log
321
$ grep -cE '✗|ERROR|422' tmp/apifox-import.log
0

$ sed -n '602,603p' tmp/apifox-import.log
[2026-11-25 12:50:01] [INFO] ========== 导入完成 ==========
[2026-11-25 12:50:01] [INFO] 总计: 580 | 成功: 580 | 失败: 0
```

**日志全绿、580/580 成功、0 失败** —— 但**只读 MCP 反查发现所有嵌套 `$ref` 在 Apifox 侧退化成空对象**。这是本报告独立复核的第 4 个问题，也是最严重的一个（详见 §五 R-8，级别 **P0**）：

```bash
# 我自己复核导入批次里的 $ref 前缀
$ grep -ohE '"\$ref" *: *"[^"]+"' tmp/apifox-enhanced/*.json | grep -oE '#/(definitions|components)' | sort | uniq -c
    835 #/definitions        ← 全是 swagger 2.0 的路径，而文档头写 openapi: 3.0.3
```

OAS3 里没有 `#/definitions/` 这个路径 → Apifox 解析不到 → **静默降级为空 `{}`，不报错**。所以「导入日志 100% 成功」这件事在本轮**完全不能作为同步成功的证据**。

**脚本已修，重生成已验证**（我复核修复产物）：

```bash
$ python scripts/gen-apifox-enhanced.py docs/swagger/swagger.json tmp/apifox-verify/
生成完成: 260 schema + 320 operation → tmp/apifox-verify/

$ grep -ohE '"\$ref" *: *"[^"]+"' tmp/apifox-verify/*.json | grep -oE '#/(definitions|components)' | sort | uniq -c
    835 #/components          ← 修复生效
```

**结论**：`scripts/gen-apifox-enhanced.py` 的 `_rewrite_refs()` 已生效（`tmp/apifox-verify/` 835/835 正确），但 **Apifox 里现在躺着的是修复前那批脏数据** → 需要重新生成 + 重导，然后按 TODO 里那三条做只读反查。**同步未完成。**

---

## 五、遗留与风险

> 按「会不会咬人」排序。R-1 / R-2 / R-3 是**本轮没做完的真问题**，不是背景说明。

### R-1　`--drift` 脚本有盲点：3 处指针形态的 map 返回漏报（**建议下轮修**）

- **现象**：`--drift` 报「✓ 无契约漂移」，实际还有 3 个函数返回 `*map[string]interface{}`（D-5）
- **根因**：脚本正则 `\)\s*\(\s*map\[string\]interface\{\}\s*,` 只匹配不带 `*` 的形态
- **后果**：`GET /category/{id}`（DELETE）、`POST /tag/{id}/follow`、`DELETE /tag/{id}/follow` 三个接口文档声明 `dto.DeleteResultDto` / `dto.TagFollowDto`，运行时抛 map
- **修法（二选一）**：
  - A：脚本正则改为 `\(\s*\*?\s*map\[...\]`（1 行，先让工具说真话）
  - B：3 个 service 函数改成返回具名 dto（约 30 行）
  **建议先 A 后 B** —— 工具撒谎比代码有债更危险，因为下一轮还会有人说「已清零」

### R-2　`ArticleDto.Content` 的 `omitempty`：设计意图正确但实现有洞（**需决策**）

- **位置**：`internal/dto/article.go:63` `Content string json:"content,omitempty"`
- **设计意图**（作者注释已写）：列表 `withContent=false` 时不加载正文，用 `omitempty` 让「未加载」与「正文为空」可区分
- **实际洞**：`omitempty` 对空字符串同样生效 → **一篇 `content:""` 的文章，在详情接口里 `content` 也会消失**，而同结构的 `summary` 返回 `""`（D-8 已实测列表/详情的对照）
- **真机复现限制（诚实说明）**：库里 `select count(*) from articles where content='' or content is null` = **0**，没有空正文样本，因此**"详情的 content 消失"是同一 omitempty 机制的推论，未用真机样本复现**
- **两个解法**：
  - **A 去掉 omitempty**：列表里 `""` 与「正文为空」不再可区分 —— 简单，但语义损失
  - **B 拆 `ArticleDto` / `ArticleDetailDto`**：列表用前者（无 content 字段）、详情用后者（content 必在）—— 彻底，成本是新增一个 DTO + 改 2 个 handler 注解 + 前端知道有两个模型
- **倾向**：B。理由是「字段按场景存在」正是 docs/21 §四之一 认可的合法例外形态（`CreatePaymentResultDto.Status` 就是这个模式），而 omitempty 不是；A 会让「正文为空」这个真实状态在 API 上不可表达
- **状态：未定，需 lead 拍板**

### R-3　`/search` 与 `/tag/followed/list` 的 `data.list` 无类型（§〇 第 2 条）

- **位置**：`internal/handler/search.go:129`、`internal/handler/tag.go:98`
- **现状**：注解仍是旧壳 `{object} ResponsePageBody`，swagger 里落到 `internal_handler.ResponsePageInner`，`list` 为 `{}`
- **修法**：两行注解改成
  ```go
  //@Success 200 {object} dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}}   // search
  //@Success 200 {object} dto.ResponsePageBody{data=dto.PageBody{list=[]dto.TagDto}}       // tag/followed
  ```
  并确认 service 返回的确实是这两个类型（`search` 返回 ES 命中项，需核对字段是否与 `ArticleDto` 一致 —— 不一致就该新建 `SearchHitDto`）
- **顺带**：`grep "{object}\s*ResponseBody"` 还有 **7 处 `@Failure`** 用旧壳。错误响应的 `data` 省略，**不影响前端拿字段**，属一致性清理，优先级低

### R-4　请求 DTO 在 service 层与 dto 层各一份（架构债，已登记）

- **现象**：`service.CreateArticleInput`（实际绑定、含 `binding` 校验）与 `dto.CreateArticleRequest`（文档）字段并行维护
- **为什么要两份**：handler 绑定的是 service 的 Input（业务分支需要它），dto 只服务 swagger。**合并的做法**是让 handler 直接绑 dto 再转 service Input —— 但那会让 `binding` 校验前移到 handler 层，是**行为变化**（错误码从 service 的业务码变成 handler 的 10008）
- **代价**：改字段要改两处、手动同步。已在 dto 文件头写警示注释
- **判据（什么时候必须动）**：如果出现「文档有、实际不收」或「实际收、文档没有」的偏差，就是这条债在咬人。目前未见报告

### R-5　5 处已登记的「合法例外」（不要照规范改回去）

见 [`docs/21 §四之二`](../docs/21-JSON序列化规范.md)。摘录：

| 字段 | 为什么偏离 | 改回去的后果 |
|---|---|---|
| `MenuNodeDto.Children` | `/profile/menus` 实测是平铺列表，`role_service.go:543` 从不填 | 声明 `children: []` 是假契约，前端会写死判断 |
| `CreatePaymentResultDto.Status` | 语义是「只有余额支付才有」，不是「状态为空」 | 去掉 omitempty 会让支付宝/微信也返回 `status:""` |
| `CreatePaymentResultDto.PaymentURL` / `CodeURL` | 按支付方式有条件返回 | 两个都在时前端不知用哪个 |
| `UserConfigDto.Username` / `CertificationDto.Type` / `Slogan` | 「字段本身按场景存在」 | — |
| `EpaySignatureRequest.Params` | **请求体**不受空值规范约束，加 omitempty 只为 Apifox 示例不渲染 null | — |

**判据**：偏离只有两种合法理由 —— ①「字段按场景存在否」（有条件返回）②「底层实现根本不产生该字段」。**「图省事」不是理由。**

### R-6　遗留的 4 个 `internal_handler.*` definition

`ResponseBody` / `ResponsePageBody` / `ResponsePageInner` / `ResponsePageMeta` 仍在 definitions 里，被 R-3 的 2 条分页 + 7 条 `@Failure` 引用。R-3 修完可以直接从 swagger 消失（`docs/18` 的验收口径是「旧匿名类型已消失」，目前尚未达到）。

### R-7　覆盖率工具的口径本身要写进文档

`dto-coverage.py` 的 `data_typed()` 只看 `data` 是否非空 `{}`，**不递归看 `data.list`**。R-3 就是这么漏掉的。
**建议**：`--list` 增加 `--deep` 选项（沿 `data → list → items` 递归），或者把 R-3 修完后在脚本里加一条断言「不允许存在 4 个 `internal_handler.*` definition」——后者更简单也更直接。

### R-8　🔴 **P0** Apifox 侧 835 处 `$ref` 前缀无效，导入「全绿」但模型全断（同步未完成）

**这是本轮最贵的一个坑：DTO 工程 99% 覆盖率在 Apifox 侧的价值被一个 10 行的缺失函数抹平了。**

- **现象**：schema 数量对（256 个 `dto.*` 都在）、`dto.UserDto.email`/`phone` 是 `string` 无 nullable，**看起来全对**。但所有**嵌套引用**在 Apifox 侧变成空对象：

  ```json
  // Apifox 侧 GET /user/profile 的 200
  {"allOf":[
    {"type":"object","properties":{}},                                        ← 应为 $ref dto.ResponseBody
    {"type":"object","properties":{"data":{"type":"object","properties":{}}}}  ← 应为 $ref dto.UserProfileDto
  ]}
  ```
  前端按 Apifox 生成 SDK，`data` 仍是空类型 —— **正是 docs/18 开头要消灭的那个「`data: {}`」在 Apifox 里原样复现**。

- **根因**：`scripts/gen-apifox-enhanced.py` 的 `swagger2_to_oas3()` 把 `definitions` 原样搬进 `components.schemas`，但**没有递归改写 `$ref` 路径**。实测（D-9）：批次文件里 **835 处引用全是 `#/definitions/...`**，而文档头声明 `openapi: 3.0.3` —— OAS3 里没有这个路径，Apifox 解析不到就**静默降级，不报错**。

- **为什么比 `schemaOverwriteBehavior` 那个坑更隐蔽**：那个是「不覆盖」，这个是「覆盖了但引用全断」；导入日志同样 100% 成功（580/580 ✓，0 ✗，D-9）。**两个坑都证明同一件事：导入成功 ≠ 数据正确。**

- **状态：脚本已修，Apifox 尚未重导**
  - `_rewrite_refs()` 已加入（`scripts/gen-apifox-enhanced.py:282-286` 附近，递归改写 `$ref`）
  - **我已复核修复生效**：`python scripts/gen-apifox-enhanced.py ... tmp/apifox-verify/` → 835 处 `#/components`（D-9）
  - **待做**：用修复后的产物重导 → 再按三条做只读反查（definitions 数 / `dto.UserDto.email` 类型 / 某接口 `data` 是否指向 `#/components/schemas/dto.Xxx`）
  - **重导前的本地闸门**（比导入 8 分钟再发现划算）：
    ```bash
    grep -ohE '"\$ref" *: *"[^"]+"' tmp/apifox-enhanced/*.json \
      | grep -oE '#/(definitions|components)' | sort | uniq -c
    # 期望：835 #/components；若出现 #/definitions 就别导入
    ```

- **顺带一条工程教训**：`gen-apifox-enhanced.py` 曾被编辑坏过（`main` 里 73 行批次逻辑缩进错误，整块吸进 `map_tag` 的 `return` 之后成死代码）→ 脚本 `exit=0` 但**跑不出任何批次**。已搬回（`tmp/gen-apifox-enhanced.py.bak` 留有备份）。**结论：脚本"跑完没报错"必须配一条产物断言（文件数 / $ref 前缀），否则等于没验。**

---

## 附：本轮方法论收获（给下一轮做同类工程的人）

1. **先验证机制，再铺开** —— 第 1 批只做 user 域 60 个类型，代价是几分钟；如果机制不成立却先派 5 个人并行，代价是 5 个人的返工
2. **工具报绿要交叉验证一次** —— `--drift` 报 0 而实际有 3 个，`coverage` 报 3 而实际 5 个。**工具的口径就是它的盲点**，先读工具源码再信它的数字
3. **文档与实现不一致时，以实现为准，并改文档** —— `PageBody{list,meta}` 是这样改过来的（不是把实现改成文档）
4. **命名问题往往没有"优雅"解** —— `dto.Xxx` 的实现方式是「exclude 掉自己的包，让 go list 单独登记」，本质是绕过 swag 的 NotUnique 判定。**能说清原理的 hack 比"按规范应该"的假设可靠**
5. **一个随手遗漏会让整个机制静默失效** —— `schemaOverwriteBehavior` 少了 4 个单词，Apifox 里 260 个 schema 就全是旧的，而日志全绿

# 16 · Swagger 注解与 operationId 规范（AI 执行手册）

> 读者：接手的 AI / 新成员。目标：新增或修改接口后，`docs/swagger/swagger.json` 里的
> `operationId` 与前端契约 `picart_native_app/openapi.json` 保持一致，且 100% 唯一。
> 本文只讲**怎么写字**，不讲业务。写前读一遍，写完按 §6 自检。

---

## 1. 硬规则（MUST，无例外）

| # | 规则 |
|---|---|
| R1 | 每个 HTTP handler 的注解块**必须有且只有一个** `@ID`，紧贴 `@Router` 上一行 |
| R2 | `@Router` 的路径必须与**实际注册路由完全一致**，包含 group 前缀（如 `v1.Group("/user")` 下的路由要写 `/user/...`） |
| R3 | 路径参数用 `{name}`，**不是** gin 的 `:name`、也不是 `*` 通配 |
| R4 | `operationId` **全局唯一**，不得重复（重复会让 OpenAPI 文档非法，Apifox/代码生成器直接报错） |
| R5 | 字符集只允许 `[A-Za-z0-9_]`，**首字符是字母**；禁止中文、空格、连字符、点号 |
| R6 | 已有正确 `@ID` 的接口**不许改**。改 ID 等于改前端生成的方法名，属于破坏性变更 |
| R7 | 别名路由（同一 handler 挂在第二条路径上，如 `/auth/login` 与 `/user/login`）**不单独写注解块**，注解只挂在主路径 |

## 2. operationId 取值顺序（唯一权威顺序）

必须按此顺序取值，**前面的命中就用前面的，不许跳级、不许自创**：

1. **前端契约** `E:\project\picart_native_app\openapi.json` —— 按 `method + path` 精确匹配（path 用 `{}` 形式）。
2. **`docs/02-API文档清单.md`** —— 原文没有 OpenAPI 文件，表格里 `operationId` 列即权威。
3. **Go 函数名** —— `接收者类型_方法名`，如 `OpsHandler_EmojiPackList`、`AuthHandler_SendSmsCode`。

> 为什么顺序不能反：前端 `/server` 契约是客户端真实调用的凭据，改了 operationId 会影响
> 客户端代码生成与 Apifox 用例绑定。历史上有 10 条 `docs/02` 与契约不一致（见 §7），一律以契约为准。

## 3. 两种风格并存，别统一

| 场景 | 风格 | 实例 |
|---|---|---|
| 原 NestJS 项目已有接口 | `XxxController_动词` | `UserController_login`、`AchievementController_claimAllRewards` |
| 重构后新增/契约未收录 | `XxxHandler_方法名` | `OpsHandler_EmojiPackCreate`、`ConfigHandler_ByKey` |

两者混排是**既定现状**（271 条继承 + 46 条新增），不要为了「整齐」去批量改名。
契约里若有小写开头的 `tagController_followedList` 这类笔误，**原样保留**，改了就和前端对不上；
要纠正必须先在契约侧确认。

## 4. 标准写法

```go
// Login POST /user/login（另挂 POST /auth/login 别名）。
//
//	@Summary		用户登录
//	@Description	账号（用户名或邮箱）+ 密码登录
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param			Device-Id	header	string	false	"设备标识"
//	@Param			body		body	loginRequest	true	"登录参数"
//	@Success		200	{object}	ResponseBody
//	@Failure		401	{object}	ResponseBody
//	@ID				UserController_login
//	@Router			/user/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
```

要点：

- 注解块用 `//` + **制表符**分隔，与相邻行对齐；`@ID` 的值单独一列。
- `@ID` 放在 `@Router` **上一行**（swag 两种位置都认，但本项目统一放这里，便于 diff）。
- handler 上有中文注释说明方法名 + 路径（`// Login POST /user/login`），方便对照路由注册处。

## 5. 新增接口时怎么起名（决策树）

```
新接口写注解
├─ 契约 openapi.json 里有同 method+path？
│   └─ 有 → 直接抄它的 operationId，一字不改
├─ docs/02 表格里有同 method+path？
│   └─ 有 → 抄表格第三列
└─ 都没有（新增/内部/别名）
    └─ 用 <接收者>_<方法名>，如 func (h *OpsHandler) EmojiPackList → OpsHandler_EmojiPackList
        └─ 若撞名（同一函数挂两条路径且都要注解）→ 主路径保留原名，其余加 _alias / _2 后缀
```

## 6. 生成与自检（改完注解必须跑）

```bash
# 1) 生成（Windows 下 go/swag 常不在 PATH，先补齐）
export PATH="$PATH:/c/Program Files/Go/bin:$HOME/go/bin"
swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal

# 1.5) 定义名归一化（把 github_com_..._internal_service.X 压成 service.X）
python scripts/normalize-swagger.py

# 2) 自检：操作数、operationId 覆盖、唯一性
python -c "
import json
from collections import Counter
d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'))
ops=[(m,p,o.get('operationId')) for p,i in d['paths'].items() for m,o in i.items() if m in ('get','post','put','patch','delete')]
ids=[x[2] for x in ops]
print('operations',len(ops),'| withId',sum(1 for i in ids if i),'| unique',len(set(ids)))
print('dup',{k:v for k,v in Counter(ids).items() if v>1} or 'none')
"
```

合格线：`operations == withId == unique`，`dup == none`。当前基线 **317 / 317 / 317**。
新增接口后这个数应当增加，若只增 operations 不增 withId，说明漏了 `@ID`。

## 7. 踩过的坑（照这个表排查，别再试一遍）

| 症状 | 原因 | 修法 |
|---|---|---|
| `$ref` 指向 `#/definitions/tmp_deploy-pkg_internal_handler.*` | 在 `tmp/deploy-pkg/` 暂存目录里跑了 `swag init`，包名被目录名污染 | 必须在**项目根**生成；打包脚本里不要调 swag |
| Swagger 路径少了 `/user` 前缀，前端 404 但后端能通 | 注解按 handler 内的相对路径写，忘了 `v1.Group("/user")` 前缀 | 抄路由注册处完整路径，别抄函数名 |
| `swag init` 报 `exec: "go": executable file not found` | swag 内部要 shell 出 `go list` | 把 Go 的 bin 目录加进 PATH（见 §6） |
| operationId 重复，文档校验失败 | 同一函数挂两条路径，两条都写了同样 `@ID` | 主路径保留，其余加后缀或干脆不写注解（见 R7） |
| 生成的接口名变成 `UserController_login_2` | 撞名自动加序号 | 说明起名逻辑有问题，回 §5 重挑 |
| 定义名长成 `github_com_SugarLess-CN_fantwo-core_internal_service.CreateArticleRequest` | `--parseDependency` 会把包路径编进 definitions | 跑 `python scripts/normalize-swagger.py`（`dev.sh` 已内置）。**不要用 `// swagger:model`**——swag v1.16.6 源码里没有这个关键字，写了也不生效 |

## 8. 禁止事项

- ❌ 不要为了让「文档更全」给别名路由补第二份注解块（会产生重复 operationId）。
- ❌ 不要按路径字符串拼 operationId（`user_login_post` 这类），与既有 317 条风格不兼容。
- ❌ 不要手改 `docs/swagger/*` 产物——它随时会被 `swag init` 覆盖，改注解才是源头。
  （唯一例外是 `scripts/normalize-swagger.py` 的自动归一化，它跟着生成流程跑，可重复执行。）
- ❌ 不要在 Dockerfile / 打包脚本里生成文档（容器内只需编译好的 `docs.go`）。
- ❌ 不要把 `SWAGGER_ENABLED` 在生产打开（UI 暴露全部接口清单）。

## 9. 相关文件

| 文件 | 作用 |
|---|---|
| `internal/handler/*.go` | 注解源头（唯一要改的地方） |
| `docs/swagger/{docs.go,swagger.json,swagger.yaml}` | swag 产物，入库但**不手改** |
| `internal/router/swagger_routes.go` | UI 挂载 + `SWAGGER_ENABLED` 开关 |
| `scripts/dev.sh` | 本地启动（先生成文档再起服务，`SKIP_SWAG=1` 可跳过） |
| `scripts/normalize-swagger.py` | 定义名归一化（`github_com_..._service.X` → `service.X`），`dev.sh` 自动调用 |
| `docs/02-API文档清单.md` | 原项目 275 操作的 path/operationId 对照 |
| `picart_native_app/openapi.json` | 前端契约，operationId 的第一权威源 |

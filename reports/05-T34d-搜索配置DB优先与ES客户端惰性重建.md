# 05 — T-34d 搜索配置 DB 优先 + ES client 配置签名惰性重建

- 任务：T-34d（cfg-search）
- 完成时间：2026-11-23
- 成员：`cfg-search`
- 写作用域：`internal/service/infra/search_service.go`、`internal/service/infra/search_service_test.go`、`internal/service/deps.go`、装配处 `internal/router/search_routes.go`
- **未做 git commit**：按 lead 指令由 lead 统一提交。

## 一、本轮改了什么

### 1. `internal/service/infra/search_service.go`（主战场）

| 位置（当前行号） | 改前 | 改后 |
|---|---|---|
| 1-19 文件头注释 | 只说明对齐原仓库与原实现差异 | 增补「运行期可调（T-34d）」段落：5 个 `search_*` key DB 优先、env 兜底；四项 client 身份参数按「配置签名 + 惰性重建」处理 |
| 24 import | 无 `log` | 新增 `log`（重建/失败日志，infra 包此前无日志设施，用标准库不引依赖） |
| 38-50 `searchParamsTTL` | 无 | 新增常量 `1s`：ES 参数的本地快照窗口，注释说明为何需要（bulk 每行一次 `Index()`，无节流会放大成上万次配置读取） |
| 56-68 `esParams` + `parseAddresses` | 无 | 新增类型与方法：地址解析，JSON 数组优先、失败/为空回退逗号分隔（对齐 `internal/config/infra.go:216 parseListEnv` 语义），去空白/去空项/去重/保序；返回 `nil` 表示「无可核地址」 |
| 107-110 `signParams` | 无 | 新增纯函数：`%q|%q|%q|%q`（地址切片、用户名、密码、索引）——用 `%q` 避免分隔符碰撞；**`enabled` 不进签名**（开关一抖不该重建连接） |
| 112-132 `Searcher` 结构 | `cfg` + `mu sync.Mutex`（client/initErr）+ `logs` | 加 `cfgSvc *ConfigService`；拆两把锁：`paramsMu sync.RWMutex`（参数快照）+ `mu sync.RWMutex`（client/initErr/**新增 `signature`**）；`mu` 由 `Mutex` 升为 `RWMutex` 以支持签名快路径 |
| 149 `WithConfigService` | 无 | 新增，风格同 `storage.go:114`；nil 时全部走启动期 env |
| 155-168 `Enabled()` / `Index()` | `s.cfg.Enabled && len(s.cfg.Addresses)>0` / `s.cfg.ArticlesIndex` | 改为读 `currentParams()`（DB 优先、env 兜底）；`Index()` 空值回退 `articlesIndexDefault` |
| 170-196 `readParams()` | 无 | 新增：以 `s.cfg` 为底，注入 `cfgSvc` 后用 `GetBool/GetString` 覆盖。**地址只有解析出非空列表才采用**（否则新建库种子值 `[]` 会把 env 地址清空）；索引为空串同样回退 |
| 198-214 `currentParams()` | 无 | 新增：`searchParamsTTL` 内直接返回快照（`paramsMu.RLock`），过期则重读一次并写回 |
| 216 `configSignature()` | 无 | 新增：`signParams(currentParams())` |
| 218-279 `ensureClient()` | 无（旧逻辑是 `clientOrInit` 里「只初始化一次，失败就永久记住 initErr」） | 新增惰性重建：① `search_enabled` 关闭或地址为空 → 直接 `return nil` 不建 client；② `mu.RLock` 比对签名，未变零分配返回；③ 变化则释放读锁、`mu.Lock` 双检后重建（`elasticsearch.NewClient` 传地址/用户名/密码）；④ 失败**不 panic**：记日志 + 保留旧 client + 记录签名（同一套坏参数不重复重连刷日志，参数一改自然重试）；⑤ 成功且替换了旧 client 时打一行「已按新配置重建」日志 |
| 281-297 `clientOrInit()` | 内部只建一次 | 改为薄封装：先判 `Enabled()`（关闭时旧 client 虽在内存也不放行）→ `ensureClient()` → 取 `client`。**所有使用 client 的方法都只经由此处**，`ensureClient` 因此只有一个调用入口，不会漏点 |
| 476-486 `SyncArticles()` | 直接 `clientOrInit`，未配置即返回错误 | 入口加 `if !s.Enabled() { return 0, nil }`：关闭时**跳过索引**（批量同步不该因开关失败） |
| 539-552 `bulkIndex()` | 循环内每行调 `s.Index()` | 循环前取一次 `idx := s.Index()`，循环内复用 |
| 659-670 `SearchArticles()` | 未配置即返回错误 | 入口加 `if !s.Enabled() { return 空结果, nil }`：运行期关掉搜索时**短路为空列表**，不报 500 |

`Ping` / `Status` / `EnsureIndex` / `ClearIndex` 未改动：它们全部经 `clientOrInit`，自动继承 `ensureClient`；`Status` 原有的 `if !s.Enabled()` 已随 `Enabled()` 变成 DB 优先。

### 2. `internal/router/search_routes.go`（装配处，8-24 行）

改前：

```go
infraSearchSingleton = infra.NewSearcher(cfg.Search).WithLogs(infraSearchLogsSingleton)
```

改后：

```go
infraSearchSingleton = infra.NewSearcher(cfg.Search).
	WithLogs(infraSearchLogsSingleton).
	WithConfigService(infraConfigSvc(opt))
```

`infraConfigSvc(opt)` = `infra.Default(opt.DB, opt.Redis)`（`internal/router/config_routes.go:20`，进程级单例，见 `infra/singleton.go`）——**不 New 新实例**，否则又会多一份配置缓存与敏感词树。

> 说明：任务书允许的写域里写了 `deps.go`，但 `grep -rn "NewSearcher" internal/` 全仓只有这一处装配，`internal/service/deps.go` 里根本没有 `Searcher` 引用（它的 `Cfg` 字段是给用户域 service 用的）。按「最小改动」原则 **deps.go 未改动**，注入落在任务书同时授权的「装配处（deps.go 或 router）」。

### 3. `internal/service/infra/search_service_test.go`（新增，约 380 行）

纯内存测试，不连 ES、不连 DB/Redis：

- `newStubConfigService`：构造 `ConfigService{cache: map}`，因 `rdb == nil` 时 `cached()` 读进程内 map，预填即等价「DB 里有值」；`db == nil` 时 miss 返回 def —— 从而无需真库即可验证 DB 优先。
- `resetParamsCache`：清 `paramsAt`，绕过 1s 快照让测试立即读到新值。
- 用例：`TestParseAddresses`（8 个子用例）、`TestSignParams`、`TestConfigSignature_ReflectsChanges`、`TestReadParams_DBOverridesEnv`、`TestReadParams_SeedEmptyAddressesFallsBackToEnv`、`TestReadParams_CommaSeparatedAddresses`、`TestEnsureClient_DisabledSkipsClient`、`TestEnsureClient_BuildsAndRebuildsOnSignatureChange`、`TestClientOrInit_ClosedEvenWithStaleClient`、`TestWithConfigService_NilIsSafe`。

## 二、功能变化（对运维/前端可见）

| 场景 | 改前行为 | 改后行为 |
|---|---|---|
| 后台把 `search_enabled` 改 false | 无效果，仍走启动期 env 值 | 下一个请求即生效；`/search/*` 返回 5030，`GET /search` 走 handler 的同一分支返回 5030；即使直接调 service，`SearchArticles` 返回空列表、`SyncArticles` 跳过并返回 0 条（不报 500） |
| 后台把 `search_enabled` 改回 true | — | 复用内存里的旧 client（签名未变，不重建），零延迟恢复 |
| 后台改 `search_addresses`（JSON 数组或逗号分隔） | 必须改 env 并重启 | 下一次请求按签名变化**惰性重建** client，打一行 `[search] ES client 已按新配置重建（index=… addresses=…）` |
| 后台改 `search_username` / `search_password` | 必须重启 | 同上，凭据变化进签名即重建 |
| 后台改 `search_articles_index` | 必须重启 | `Index()` 立即返回新索引；下一次 client 使用触发重建（重建日志含新 index）。索引切换后**旧索引数据不会自动迁移**，需重新 `POST /search/sync/articles` |
| 新建库（种子 `search_addresses="[]"`） | 不适用 | 空数组**不会**覆盖 env 里的 `SEARCH_ADDRESSES`，地址仍取 env —— 避免升级即失去搜索 |
| ES 地址填错（不可达） | 启动期建 client，请求时报连接错误 | 行为不变（go-elasticsearch 惰性连接）；但重建失败（地址格式非法）不会 panic，记日志并保留旧 client |

## 三、配置项 / 接口清单

DB `config` 表（分组 `search`，常量在 `internal/model/config.go`，由 cfg-seed 落地）：

| key | 类型 | 默认值（种子） | 公开 | 敏感 | 读取点 |
|---|---|---|---|---|---|
| `search_enabled` | boolean | `true` | 是 | 否 | `readParams` → `Enabled()` |
| `search_addresses` | json | `[]` | 否 | 否 | `parseAddresses` → `readParams` |
| `search_username` | string | `""` | 否 | 否 | `readParams` |
| `search_password` | string | `""` | 否 | **是**（cfg-mask 负责打码） | `readParams` |
| `search_articles_index` | string | `picart_articles` | 否 | 否 | `readParams` → `Index()` |

受影响的对外接口（签名不变）：`GET /search`、`GET /search/status`、`POST /search/sync/articles`、`POST /search/clear/articles`。

env 兜底键（保持有效）：`SEARCH_ENABLED` / `SEARCH_ADDRESSES` / `SEARCH_ADDRESS` / `SEARCH_USERNAME` / `SEARCH_PASSWORD` / `SEARCH_ARTICLES_INDEX`。

## 四、验证证据（真实命令 + 真实输出）

### 1) `gofmt -l internal/service/ internal/service/infra/`

```
$ gofmt -l internal/service/ internal/service/infra/
internal\service\auth_service.go
internal\service\role_service.go
internal\service\user_actions_service.go
internal\service\verifycode.go
```

**这 4 个文件不是本批产物**，证据：

```
$ git status --porcelain | grep auth_service
(无输出)          # 未出现在改动列表里 = 工作区内容 == HEAD 内容
```

即它们在 HEAD 里就不符合 gofmt，属历史遗留；不在本批写作用域，**未触碰**。

> **lead 后续处置（2026-11-22）**：这 4 个文件连同另外 9 个历史遗留文件，已由 lead 用 `gofmt -w` 一次性收口（纯格式化、零逻辑变化），全仓 `gofmt -l internal/ cmd/ test/` 现为**空输出**。提交时单独拆成一个纯格式化 commit，不与功能改动混在一起。

本批改动的三个文件逐个检查为干净：

```
$ gofmt -l internal/service/infra/search_service.go internal/service/infra/search_service_test.go internal/router/search_routes.go
--- 我改的文件 gofmt 检查完毕（无输出=通过）---
```

### 2) `go build ./...`

```
$ go build ./...
(无输出=通过)
```

### 3) `go vet ./internal/service/...`

```
$ go vet ./internal/service/...
(无输出=通过)
```

### 4) `go test ./internal/service/... | tail -20`

```
$ go test ./internal/service/...
ok  	github.com/SugarLess-CN/fantwo-core/internal/service	(cached)
ok  	github.com/SugarLess-CN/fantwo-core/internal/service/infra	(cached)
```

（并行期间曾出现 `config_service_test.go` 的 `TestConfigIsSensitiveKey` 红，属 cfg-mask 正在写的中间态，与本批无关，现已随其改动转绿。）

### 5) 新增测试

```
$ go test ./internal/service/infra/ -run 'TestParseAddresses|TestSignParams|TestConfigSignature|TestReadParams|TestEnsureClient|TestWithConfigService|TestClientOrInit' -v
=== RUN   TestParseAddresses
=== RUN   TestParseAddresses/JSON_数组（DB_推荐写法）
...
--- PASS: TestParseAddresses (0.00s)
--- PASS: TestSignParams (0.00s)
--- PASS: TestConfigSignature_ReflectsChanges (0.00s)
--- PASS: TestReadParams_DBOverridesEnv (0.00s)
--- PASS: TestReadParams_SeedEmptyAddressesFallsBackToEnv (0.00s)
--- PASS: TestReadParams_CommaSeparatedAddresses (0.00s)
--- PASS: TestEnsureClient_DisabledSkipsClient (0.00s)
--- PASS: TestEnsureClient_BuildsAndRebuildsOnSignatureChange (0.01s)
--- PASS: TestClientOrInit_ClosedEvenWithStaleClient (0.00s)
--- PASS: TestWithConfigService_NilIsSafe (0.00s)
PASS
ok  	github.com/SugarLess-CN/fantwo-core/internal/service/infra	0.089s
```

### 6) 自查：`s.cfg.*` 全部只作 DB 读取的 fallback

```
$ grep -n "s\.cfg\.Search" internal/service/infra/search_service.go
(无输出)

$ grep -n "s\.cfg\." internal/service/infra/search_service.go
172:		Enabled:   s.cfg.Enabled,
173:		Addresses: s.cfg.Addresses,
174:		Username:  s.cfg.Username,
175:		Password:  s.cfg.Password,
176:		Index:     s.cfg.ArticlesIndex,
```

全部落在 `readParams()`（170-196 行）里作为 `esParams` 的**初始值**，紧接着被 `cfgSvc.GetBool/GetString` 覆盖 —— 即「作为 DB 读取的 def 参数」位置，无一处是直接生效的读取。**没有启动期不可迁的值**：client 已改成签名驱动的惰性重建，索引名走 `Index()`。

## 五、遗留与风险

1. **生效延迟 1 秒**：`searchParamsTTL = 1s` 的参数本地快照 + `ConfigService` 自身的 Redis 缓存（`configCacheTTL = 10min`，但 `Update/UpdateAll/UpdateGroup` 会立即 `invalidate`）。正常路径（后台 PATCH → 缓存失效 → 1s 内被读到）总延迟 ≤ 1s；若通过直接改数据库（绕过 API，不失效缓存）则要等 Redis 缓存 10 分钟过期。
2. **地址变更后首次请求才重建**：没有后台探活，重建发生在「配置签名变化后的第一个用到 client 的请求」上，该请求本身要多花一次 `elasticsearch.NewClient` 的毫秒级开销。
3. **地址填成不可达值不会被拦**：`go-elasticsearch` 的 `NewClient` 只做参数校验、不连接，所以「地址写错」在重建时不失败（旧 client 会被替换成不可达 client），错误以「ES 检索失败」在后续请求暴露。重建失败（格式非法）才保留旧 client。若要「不可达即回滚」，需要引入重建前探活（会让每次改配置多一次 RTT），本批未做。
4. **索引切换不迁数据**：改 `search_articles_index` 后新索引是空的，需手工跑 `POST /search/sync/articles`。
5. **多实例**：每个实例各自按签名重建，靠 `ConfigService` 的 Redis 一级缓存收敛（同一份 DB 值）；没有跨实例的「重建广播」，各实例最多各自晚 1s。
6. **保留旧 client 的语义**：关闭 `search_enabled` 时**不销毁** client（重新打开可零成本复用），但 `clientOrInit` 会先判 `Enabled()`，旧 client 不会流到调用方 —— 已有 `TestClientOrInit_ClosedEvenWithStaleClient` 守护。
7. **`gofmt` 历史遗留已收口**：见四.1 的 lead 处置注记 —— 13 个文件（含本批报告提到的 4 个）已一次性 `gofmt -w`，全仓现为空输出。此项不再是遗留。
8. **`internal/service/deps.go` 未改**：见一.2 说明。

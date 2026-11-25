# oauthkit — 微信 / QQ / GitHub 第三方登录 Go SDK

一个零业务耦合的第三方登录协议层。只做四件事：**拼授权 URL、code 换 token、
token 换用户、刷新 token**。不持 Redis / DB / 日志，不做账号绑定判断。

- 依赖：仅 [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2)（BSD-3，且只有 GitHub 实现使用）
- 模块路径：`github.com/SugarLess-CN/fantwo-core/pkg/oauthkit`
- Go 版本：1.26+

```bash
go get github.com/SugarLess-CN/fantwo-core/pkg/oauthkit
```

---

## 1. 快速开始

```go
import (
    "context"
    "errors"

    "github.com/SugarLess-CN/fantwo-core/pkg/oauthkit"
)

// ① 按 (provider, channel) 构造 —— 微信 web 与 app 是两套 appid，必须分开
p, err := oauthkit.New(oauthkit.ProviderWechat, oauthkit.ChannelWeb, oauthkit.Config{
    ClientID:     os.Getenv("WECHAT_WEB_APPID"),
    ClientSecret: os.Getenv("WECHAT_WEB_SECRET"),
    RedirectURI:  "https://api.example.com/oauth/wechat/web/callback",
})
if err != nil {
    return err // 组合不支持，或 github scope 为空
}
if !p.Configured() {
    return errNotConfigured // 未配置齐全时**不要**发授权链接
}

// ② 生成授权链接（state 必须由调用方生成并自行存取，SDK 不碰 Redis）
state := randomState()
url, err := p.AuthURL(state)

// ③ 回调：code 换 token
tok, err := p.Exchange(ctx, code)

// ④ token 换用户
u, err := p.UserInfo(ctx, tok)
// u.Provider / u.ID(openid) / u.UnionID / u.Nickname / u.AvatarURL / u.Email
```

完整可运行示例见 [`example_test.go`](./example_test.go)：`go test ./pkg/oauthkit/ -run Example -v`

---

## 2. 渠道矩阵

按 `(provider, channel)` 实例化。**同一平台的 web 与 app 是不同 appid**，同一用户
在两渠道 `openid` 不同、`unionid` 相同 —— 所以 `Name()` 给平台、`Channel()` 给渠道，
两者不能混用。

| provider | channel | 授权发起方 | 后端换 token | 后端拿 openid | 后端必须校验 |
|---|---|---|---|---|---|
| `wechat` | `web` | 后端给 URL | 是 | 换 token 时返回 | 否（自己换的） |
| `wechat` | `app` | 客户端 SDK | 是（客户端只交 code） | 换 token 时返回 | 否（自己换的） |
| `qq` | `web` | 后端给 URL | 是 | 额外调 `oauth2.0/me` | 否 |
| `qq` | `app` | 客户端 SDK | **否**（SDK 直接给 token） | SDK 直接返回 | **是（`ValidateToken`）** |
| `github` | `web` | 后端给 URL | 是 | `/user` 返回 `id` | 否 |

不支持的方法返回哨兵错误，不 panic：

| 方法 | 不支持的组合 | 返回 |
|---|---|---|
| `AuthURL` | 微信 app、QQ app | `ErrAuthURLUnsupported` |
| `Exchange` | QQ app | `ErrExchangeUnsupported` |
| `Refresh` | GitHub web、QQ app | `ErrRefreshUnsupported` |

> `ChannelMP`（微信公众号 H5）与 `ChannelDevice`（GitHub Device Flow）是预留常量，
> `New()` 会返回错误，尚未实现。

### 两个必须保留的不对称

- **微信 App 交 code**：客户端拿不到 token，由后端 `Exchange` 换取 → 可信。
- **QQ App 交 access_token**：token 与 openid 都来自客户端，可信度为零 → **必须**
  `ValidateToken`（内部打 `oauth2.0/me` 并比对 openid 一致性）后才能 `UserInfo`。

不要为「接口统一」而抹平：把 QQ 的安全模型套到微信上会多一次无意义的 `sns/auth`
调用；把微信的模型套到 QQ 上则是**直接信任客户端 token**，等于账号可被接管。

---

## 3. 配置项（16 个键）

键名命名口径：`<provider>_<channel>_<field>`。键名的唯一来源是
`oauthkit.ConfigKeys(provider, channel)` —— **不要在业务代码里手写这些字符串**。

| key | 类型 | 敏感 | 说明 |
|---|---|---|---|
| `wechat_web_appid` | string | 否 | 微信网站应用 AppID（PC 扫码） |
| `wechat_web_secret` | string | **是** | 微信网站应用 AppSecret |
| `wechat_web_redirect_uri` | string | 否 | 扫码回调（域名须与开放平台授权域名一致） |
| `wechat_web_success_redirect_url` | string | 否 | 登录成功后前端跳转地址（默认 `/`） |
| `wechat_app_appid` | string | 否 | 微信移动应用 AppID（**与 web 不同**） |
| `wechat_app_secret` | string | **是** | 微信移动应用 AppSecret（**只在服务端使用，绝不下发客户端**） |
| `qq_web_appid` | string | 否 | QQ 网站应用 appid |
| `qq_web_appkey` | string | **是** | QQ 网站应用 appkey（即 client_secret） |
| `qq_web_redirect_uri` | string | 否 | 网页回调（主域名下，不能 IP+端口） |
| `qq_web_success_redirect_url` | string | 否 | 登录成功后前端跳转地址 |
| `qq_app_appid` | string | 否 | QQ 移动应用 appid（**与 web 不同**） |
| `github_client_id` | string | 否 | GitHub OAuth App client ID |
| `github_client_secret` | string | **是** | GitHub OAuth App client secret |
| `github_redirect_uri` | string | 否 | 回调地址 |
| `github_scope` | string | 否 | 授权作用域，默认 `read:user user:email`（空格分隔） |
| `github_success_redirect_url` | string | 否 | 登录成功后前端跳转地址 |

**故意不设的键（设计决策，不要补）**：

- `qq_app_appkey` —— QQ 移动端由客户端 SDK 自己换 token，后端只做
  `ValidateToken` + `UserInfo`，**用不到 AppKey**。少一个凭据就少一个泄漏面。
- `wechat_web_scope` / `wechat_app_scope` / `qq_web_scope` —— scope 用代码默认值
  （`snsapi_login` / `snsapi_userinfo` / `get_user_info`），由 `Config.normalize` 补；
  只有 GitHub 需要显式配置（要控制是否请求 `user:email`）。
- `wechat_app_success_redirect_url` —— app 渠道由客户端处理跳转，后端不出 URL。
- `qq_use_unionid` —— QQ 的 unionid 需单独申请权限（未申请时请求会返回 `100048`），
  当前实现不主动请求。

`Config` 结构体的字段与上表的映射：

```go
type Config struct {
    ClientID           string // appid / client_id
    ClientSecret       string // secret / appkey / client_secret（qq app 渠道留空）
    RedirectURI        string // 回调地址（app 渠道留空）
    Scope              string // 空则按 provider/channel 补默认值
    SuccessRedirectURL string // 仅业务用，不参与协议
}
```

---

## 4. 接线禁令（7 条，违反即事故）

这 7 条全部来自真机踩坑，是使用本 SDK 时必须遵守的**接线层**约束
（SDK 内部已尽量做成运行时硬约束，但日志/查询/参数校验属于调用方责任）。

### 禁令 1｜禁止把 `Token` / `User` 整体打进日志

`Token.Raw []byte` 保存**原始响应体**，而微信与 QQ 的换 token 响应**都含
`access_token` + `refresh_token` 明文**（QQ 是裸 query string，直接可读）。

| 禁止写法 | 后果 |
|---|---|
| `fmt.Printf("%+v", tok)` | `Raw:[123 34 97 ...]` 是字节数组**不是加密**，逐字节转 char 立即还原明文 |
| `zap.Any("token", tok)` | 结构化日志会被采集系统长期留存 |
| `json.Marshal(tok)` | `Raw` 是 `[]byte`，JSON 里变成 base64，比字节数组更隐蔽 |
| `fmt.Errorf("failed: %+v", tok)` | 错误信息进日志 = 同一条泄漏路径 |

**允许打日志的字段**：`Provider` / `Channel` / `OpenID` / `UnionID`（debug 级）/
`ExpiresIn` / `Scope` / `Verified`。

`Raw` 的唯一合法用途：解析失败时在 debug 级输出**长度**（`len(tok.Raw)`），不输出内容。

### 禁令 2｜错误信息里禁止夹带响应体片段

只放**长度**、**HTTP 状态码**、**平台错误码**（`errcode=40029` / `ret=100019`）；
不放响应体、不放 token 片段、不放 openid 全值。

### 禁令 3｜参数先本地校验

空 `state` / 空 `code` / 空 `refresh_token` / 空 `access_token` 一律**在打上游之前**
判掉，返回 `ErrInvalidArgument`。

**判定口诀**：`ErrInvalidArgument` = 我的错、重试无用；
`ErrCodeInvalid` / `ErrTokenExpired` / `ErrUpstream` = 平台侧失败，可能要重试或引导用户。
两者混用会让调用方朝相反方向处理（例如对一个必失败的空参数反复请用户重新授权）。

### 禁令 4｜`Token.UnionID` 空串必须先判空再查询

`(provider, unionid)` 若不是唯一索引，**空串在 SQL 里会互相匹配**（空串 ≠ NULL）。
所有拿不到 unionid 的用户（微信 web 常见、GitHub 恒无）会被并成同一个账号 ——
比登录失败严重得多的数据事故。

```go
// ✅ 正确
if tok.UnionID != "" {
    if b, err := findByUnionID(db, tok.Provider, tok.UnionID); err == nil { ... }
}
// ❌ 错误
b, err := findByUnionID(db, tok.Provider, tok.UnionID)
```

### 禁令 5｜`Channel == "app" && !Verified` 的 token 一律不信

SDK 的 `UserInfo` 已硬拒绝（返回 `ErrTokenNotVerified`），但接线者要理解**为什么**：

- 微信 App：客户端交 **code**，后端 `Exchange` 换来 token → `Verified` 由 Exchange 置 true。
- QQ App：客户端交 **access_token + openid** → 必须 `ValidateToken` 后才 `Verified=true`。

**禁止**为了让流程跑通而手工 `tok.Verified = true` —— 那等于把伪造防护整条拆掉。

### 禁令 6｜校验类端点的响应必须双向验证

`sns/auth` / `oauth2.0/me` 的解析必须 ①能解析 **且** ②解析出的内容确实含该平台的
标志字段。只有 ① 等于没验：

- 微信 `sns/auth` 的成功体是 `{"errcode":0,"errmsg":"ok"}`。若用**值类型**接 `errcode`，
  字段缺失 → 零值 0 → 判成成功 → **上游返回任何非微信格式的 200 响应都能让伪造 token
  通过校验**（SDK 已改指针类型：字段缺失即 `ErrUpstream`）。
- QQ 的 `url.ParseQuery` 会把 HTML 错误页解成一个怪 key 且 `err=nil`；JSON 分支同理
  （SDK 已加「必须命中已知字段」判定）。

### 禁令 7｜`expires_in` 的边界

- `ExpiresIn == 0` 的语义是「**不过期**」（GitHub 默认如此）。若上游因解析失败静默给了 0，
  **过期 token 会被当成永久有效** → 解析失败必须报错，不能兜底成 0。
- 溢出：`time.Duration(n) * time.Second` 是 int64 纳秒乘法，超过 ~292 年回卷到过去。
  SDK 已截断到 `math.MaxInt64 / int64(time.Second)`。
- QQ 移动端 SDK 回调的 `expires_in` 是**字符串** `"7776000"` 且 `msg` 拼成 `"sucess"`，
  用 `ParseQQAppLoginPayload` 解析，不要自己解。

---

## 5. 错误哨兵（14 个）

全部哨兵用 `errors.Is` 判断，**不要比对字符串**。

| 哨兵 | 含义 | 调用方该怎么做 |
|---|---|---|
| `ErrNotConfigured` | 配置不齐（appid / secret / redirect_uri 缺失） | 降级该端点（未配置而非 500） |
| `ErrInvalidArgument` | 调用方参数错误（空 state / code / token） | **改代码**，重试无用 |
| `ErrAuthURLUnsupported` | 该渠道不出授权 URL（微信 app / QQ app） | 走客户端 SDK 路径 |
| `ErrExchangeUnsupported` | 该渠道不做 code 换 token（QQ app） | 走 `ValidateToken` |
| `ErrRefreshUnsupported` | 平台/渠道无刷新（GitHub、QQ app） | 用户重新登录 |
| `ErrTokenNotVerified` | app token 未经 `ValidateToken` 就 `UserInfo` | 先校验（安全事件） |
| `ErrTokenMismatch` | token 与声称的 openid 不匹配 | 拒绝并告警（疑似伪造） |
| `ErrCodeInvalid` | 授权码无效/过期/已使用（微信 40029、QQ 100019、GitHub `bad_verification_code`） | 引导用户重新授权 |
| `ErrCredentialBad` | 应用凭据被拒（appid / secret / appkey 错） | **运维事故**，查配置 |
| `ErrTokenExpired` | access_token 已过期（微信 40014/42001、QQ 100013） | 刷新或重新授权 |
| `ErrRefreshDead` | refresh_token 过期/已撤销 | 用户重新授权 |
| `ErrScopeMissing` | 用户未授予必需作用域（典型：GitHub 未授 `user:email`） | 提示重新授权并允许相应权限 |
| `ErrEmailMissing` | 拿不到已验证主邮箱（**不是错误**，GitHub 用户可能没邮箱） | 建号时 email 留空 |
| `ErrUpstream` | 上游异常（网络 / 5xx / 响应无法解析） | 可重试；细节只进日志 |

---

## 6. 已知限制

### QQ 的部分端点会被 WAF 拦（服务器侧直连）

2026-11-25 实测（从本机服务器侧直连 `graph.qq.com`）：

| 端点 | 结果 |
|---|---|
| `/oauth2.0/authorize` | **HTTP 501**，返回一段 `waf.tencent.com/501page.html` 跳转脚本（HTML） |
| `/oauth2.0/token` | **HTTP 501**，同上 |
| `/oauth2.0/me` | 正常：`callback( {"error":100013,"error_description":"access token is illegal"} );` |
| `/user/get_user_info` | 正常：`{"ret":-22,"msg":"openid is invalid"}` |

**⚠️ 这是间歇性的，不是固定行为。** 同一天两次独立实测得到相反结果：

| 实测 | 时间 | `/token` | `/authorize` |
|---|---|---|---|
| 成员 A | 上午 | **501**（10 秒重试、加 Referer/Accept/换 UA 均无效） | **501** |
| 成员 B（lead） | 下午 | **200** `callback({"error":100005,...})` | **302** |

**结论：按出口 IP / 时段触发的间歇性风控。** 不要把它当稳定行为写进任何断言。

**对测试的硬要求**：凡涉及 QQ `/token`、`/authorize` 的测试，
**必须同时接受两种结果**（正常的业务错误 或 WAF 501），只有 `ErrCredentialBad` 判失败。
把 501 当固定行为会让测试在另一个 IP 上假红。

**不受影响**：`/oauth2.0/me` 与 `/user/get_user_info` 两次实测都正常；
真实浏览器跳转始终不受影响（风控针对服务器 IP 直连）。

对测试的影响：

- 凭据有效性**仍可验证**：微信打两个 `/sns/oauth2/access_token`（假 code → `40029` 即凭据有效）、
  QQ 打 `/oauth2.0/me`（假 token → `100013` 即上游可达）。两者都不受 WAF 影响。
- QQ web 的 `/token` 凭据测试要**接受两种结果**：`ErrCodeInvalid`（未被拦）或
  `ErrUpstream`（被 WAF 拦）。判失败只有一种情况：`ErrCredentialBad`。
- 实现见 `credentials_test.go`（`//go:build credentials`）。

历史观测里出现过「间隔 3 秒即恢复」的情况，可能与时段的出口 IP 有关；
出于谨慎，**自动化测试仍建议对 QQ 调用做 ≥3 秒节流**：

```go
var lastQQCall time.Time

func qqThrottle() {
    if d := time.Since(lastQQCall); d < 3*time.Second {
        time.Sleep(3*time.Second - d)
    }
    lastQQCall = time.Now()
}
```

### 其它

- **GitHub Device Flow**（`ChannelDevice`）与**微信公众号 H5**（`ChannelMP`）未实现。
- 微信 `sns/userinfo` 自 2021-10-20 起不再返回 `sex` / `province` / `city` / `country`，
  本 SDK 不解析这些字段（解析出来也不可靠）。
- QQ `unionid` 需单独申请权限，未申请时请求会返回 `100048`；本 SDK 不主动请求。
- 微信 `Refresh` 在 access_token 未过期时**不改变 access_token，只续期** ——
  实现以响应原值为准，不要靠「token 是否变化」判断刷新成功。

---

## 7. 目录结构

```
pkg/oauthkit/
├── doc.go            # 包文档（快速开始 / 渠道矩阵 / 错误处理 / 安全约束）
├── README.md         # 本文件（对外说明）
├── types.go          # Provider 接口 + Token / User + 14 个哨兵错误
├── config.go         # Config 结构 + New() 工厂 + ConfigKeys()
├── config_keys.go    # 16 个配置键常量（唯一定义源）
├── wechat.go         # 微信 web / app
├── qq.go             # QQ web / app（含 JSONP 剥壳、裸 query string 解析）
├── github.go         # GitHub web（基于 x/oauth2）
├── wechat_test.go    # 微信：21 个用例
├── qq_test.go        # QQ：24 个用例（含 stripJSONP / qqFlexInt 边界）
├── github_test.go    # GitHub：14 个用例
└── example_test.go   # 4 个可执行示例（go test -run Example）
```

测试全部走 `httptest` fixture，**零外部网络依赖**：

```bash
go test ./pkg/oauthkit/ -count=1        # 113 条断言
go test ./pkg/oauthkit/ -run Example -v # 4 个示例
```

---

## 8. 许可证与依赖

- 本包代码随主仓库 `github.com/SugarLess-CN/fantwo-core` 分发。
- 唯一第三方依赖 `golang.org/x/oauth2`（BSD-3-Clause），仅 `github.go` 使用。
- 微信 / QQ 的协议解析为自实现：生态里的 Go SDK 要么漏了微信 `#wechat_redirect`
  强制锚点，要么把 QQ 的 JSONP / 裸 query string 处理写成玩具级，故不引用。
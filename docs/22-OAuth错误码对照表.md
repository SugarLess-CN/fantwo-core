# OAuth 错误码全链路对照表

> **改第三方登录时的唯一错误码查找表。** 本文回答三个问题：
> ① 上游（微信/QQ/GitHub）返回什么码；② 我方 SDK 把它归成哪个哨兵；③ 用户最终看到什么 `status_code` 和文案。
>
> **与 `docs/14` 的分工**：`docs/14 §1.3 / §2.3 / §3.3` 是**官方原文全表**（调研归档，只增不改）；
> 本文是**实现侧的映射真相**（代码实际怎么归类），两者必须能对上，对不上就是 bug。
>
> 最后更新：2026-11-25（依据 `internal/pkg/oauthkit/*.go` 的实际实现，非文档推测）

---

## 一、三层链路（先理解结构，再看表）

```
上游返回 errcode
      ↓  各 provider 自己的 xxxErrorSentinel / xxxCodeError
oauthkit 15 个哨兵错误
      ↓  service.mapOAuthErr（唯一映射点）
errcode.Err（含 HTTP 码 + 业务码）
      ↓  response.Fail
用户看到 {status_code, message}
```

**只有 `mapOAuthErr` 一个映射点** —— 改映射只改这里，不要在 handler 里做二次判断。

---

## 二、15 个哨兵错误（`internal/pkg/oauthkit/types.go`）

| 哨兵 | 含义 | 调用方该做什么 |
|---|---|---|
| `ErrNotConfigured` | provider 未配置（appid/secret 缺失） | 运维补配置，**不是用户问题** |
| `ErrInvalidArgument` | 我方编程错误（空 code、空 state 传给 SDK） | 修代码 |
| `ErrAuthURLUnsupported` | 该渠道不支持生成授权 URL | 接线错误（如 QQ app 不该要 URL） |
| `ErrExchangeUnsupported` | 该渠道不支持 code 换 token | 接线错误（QQ app 走 `ValidateToken`） |
| `ErrRefreshUnsupported` | 该渠道不支持刷新 | 接线错误 |
| `ErrTokenNotVerified` | 客户端上报的 token 未先经 `ValidateToken` | 修代码（先验后用） |
| `ErrTokenMismatch` | token 与声称的 openid 不匹配 | **疑似伪造，告警** |
| `ErrCodeInvalid` | 授权 code 无效/过期/已用过 | 引导重新登录，**别重试 token 接口** |
| `ErrCredentialBad` | 应用凭据被上游拒绝 | 运维查 appid/secret |
| `ErrTokenExpired` | access_token 过期/失效 | 引导重新授权 |
| `ErrRefreshDead` | refresh_token 已废 | 引导重新授权 |
| `ErrScopeMissing` | 用户未授所需 scope | 引导补授权 |
| `ErrEmailMissing` | 拿不到 verified primary 邮箱 | **可容忍**：建号时 email 留空 |
| `ErrUpstream` | 上游异常/未知码 | 重试或报障 |

---

## 三、哨兵 → 用户可见（`service.mapOAuthErr`，唯一映射点）

| 哨兵 | HTTP | status_code | 用户看到的文案 | 前端动作 |
|---|---|---|---|---|
| `ErrNotConfigured` | 400 | **20026** | Third-party login is not configured | 提示「暂不支持该登录方式」 |
| `ErrInvalidArgument` | 400 | **10008** | Invalid request parameter | 修代码/传参 |
| `ErrCodeInvalid` | 400 | **10008** | Third-party login failed | **重新登录**（别重试 token） |
| `ErrCredentialBad` | 400 | **10008** | Third-party login failed | 报障（凭据问题） |
| `ErrTokenExpired` | 401 | **10002** | Your session has expired, please sign in again | **重新授权** |
| `ErrRefreshDead` | 401 | **10002** | Your session has expired, please sign in again | **重新授权** |
| `ErrTokenMismatch` | 403 | **10003** | Third-party login verification failed | **告警**（疑似伪造） |
| `ErrTokenNotVerified` | 403 | **10003** | Third-party login verification failed | 告警 |
| `ErrScopeMissing` | 400 | **10008** | Required permission not granted | 引导补授权 |
| `ErrEmailMissing` | 400 | **10008** | Third-party login failed | **可降级**：仍可建号 |
| `ErrAuthURLUnsupported` 等 3 个 | 400 | **10008** | Third-party login failed | 接线错误，报障 |
| `ErrUpstream` / 未知 | 500 | **10007** | Internal server error | 重试/报障，细节只进日志 |

**⚠️ 已知缺陷**：`ErrCodeInvalid` / `ErrCredentialBad` / `ErrEmailMissing` / 三个 Unsupported **都映射到同一个 `10008`** ——
用户无法区分「code 过期」「凭据坏了」「没邮箱」。已登记待办（P2），见 §六。

---

## 四、微信 errcode → 哨兵（`wechatErrorSentinel`，`wechat.go:575`）

**微信的特殊性：HTTP 状态码恒 200**，成败只能看 body 里的 `errcode`。

| 微信 errcode | 官方含义 | 哨兵 |
|---|---|---|
| `40029` | code 无效 | `ErrCodeInvalid` |
| `40163` | code 已被使用 | `ErrCodeInvalid` |
| `41008` | 缺少 code | `ErrCodeInvalid` |
| `40030` | refresh_token 无效 | `ErrRefreshDead` |
| `42002` | refresh_token 过期 | `ErrRefreshDead` |
| `41003` | 缺少 refresh_token | `ErrRefreshDead` |
| `40001` | AppSecret 错误 | `ErrCredentialBad` |
| `40013` | AppID 无效 | `ErrCredentialBad` |
| `40066` | 不合法的 openid | `ErrCredentialBad` |
| `40164` | IP 白名单限制 | `ErrCredentialBad` |
| `40014` | access_token 无效 | `ErrTokenExpired` |
| `42001` | access_token 超时 | `ErrTokenExpired` |
| `40003` | openid 无效 | `ErrTokenMismatch` |
| `41002` | 缺少 appid | `ErrNotConfigured` |
| `41004` | 缺少 AppSecret | `ErrNotConfigured` |
| `48001` | api 未授权（scope 不足） | `ErrScopeMissing` |
| 其余（`-1` 系统繁忙 / `40002` grant_type 非法 / `41001` / `41009` 缺参数 / `45009` 日限额 / `45011` 限频 / `10060` 未上架日限额 / 未知） | — | `ErrUpstream` |

**两个反直觉的点（源码注释里写清了，别"修正"）**：

1. **换 token / userinfo 的成功响应体没有 `errcode` 字段**
   → 所以 `ErrCode` 用**值类型**，缺失 = 0 = 成功。
2. **`sns/auth` 的成功体明确是 `{"errcode":0,"errmsg":"ok"}`**
   → 所以那边 `ErrCode` 用**指针**，缺失 = `ErrUpstream`（拒绝）。

**同一个平台两个接口相反的判定** —— 这是真实性差异，不许为"统一"而抹平。

---

## 五、QQ 返回码 → 哨兵（`qqCodeError`，`qq.go:837`）

**QQ 同样无 HTTP 语义，全在 body 里。** 且有 JSONP 包装：`callback( {...} );`

| QQ code | 官方含义 | 哨兵 |
|---|---|---|
| `100018` | 获取 code 失败 | `ErrCodeInvalid` |
| `100019` | 用 code 换 token 失败 | `ErrCodeInvalid` |
| `100020` | **code 被重复使用** | `ErrCodeInvalid` |
| `100067` / `100068` | code 相关 | `ErrCodeInvalid` |
| `100013` | access_token 失效 | `ErrTokenExpired` |
| `100014` | access_token 过期 | `ErrTokenExpired` |
| `100015` | access_token 被废除 | `ErrTokenExpired` |
| `100016` | access_token 验证失败 | `ErrTokenExpired` |
| `100045` | 用户改密 | `ErrTokenExpired` |
| `100021` / `100022` | refresh_token 获取失败 | `ErrRefreshDead` |
| `100036` ~ `100039` | refresh_token 失效/过期/撤销/达上限 | `ErrRefreshDead` |
| `100042` | refresh_token 非法 | `ErrRefreshDead` |
| `100001` | 缺 client_id | `ErrCredentialBad` |
| `100002` | 缺 client_secret | `ErrCredentialBad` |
| `100008` | appid 不存在 | `ErrCredentialBad` |
| `100009` | appkey 非法 | `ErrCredentialBad` |
| `100040` / `100041` | 凭据相关 | `ErrCredentialBad` |
| `110401` / `110404` | 权限相关（凭据层面） | `ErrCredentialBad` |
| `100030` / `100031` | 未授权该 api | `ErrScopeMissing` |
| `100048` / `100049` | **unionid 权限未申请** | `ErrScopeMissing` |
| `100052` / `100057` / `100066` | 权限不足 | `ErrScopeMissing` |
| 其余 | — | `ErrUpstream` |

**实测验证（2026-11-25）**：用假 code 打真实 `/token` 返回
`callback( {"error":100005,"error_description":"param code is wrong or code has been used"} );`
—— 注意官方文档写 `100005` 是「缺少 code」，实测是「code 错误」，**文档与实现有偏差**，以实测为准。

---

## 六、GitHub → 哨兵（`github.go`）

GitHub 用**标准 OAuth2 + HTTP 状态码**，映射简单得多：

| GitHub 响应 | 哨兵 | 说明 |
|---|---|---|
| HTTP 200 + 正常 token | — | 成功 |
| HTTP 400 `invalid_grant` | 由 x/oauth2 转成 error | 通常落到 `ErrUpstream` |
| HTTP **403** | `ErrScopeMissing` | **明确翻译**：403 是 scope 不足，不是"没邮箱" |
| scope 明确不含 `user:email`（且不含 `user`） | `ErrScopeMissing` | **省一次必然 403 的往返** |
| `/user/emails` 返回空或全非 primary+verified | `ErrEmailMissing` | 这是唯一的 `ErrEmailMissing` 触发点 |

**设计要点**（`github.go:29-34` 注释）：
- **403 与「没有主邮箱」是两件事**，旧实现混为一谈
- `ErrEmailMissing` **只**用于「确实没有 primary && verified 邮箱」
- `user` scope ⊃ `user:email`（官方口径），判断时要同时看

---

## 七、state 相关（web 渠道专属）

| 情况 | 当前 status_code | 应该 |
|---|---|---|
| 空 state | `10008` | 正确 |
| **state 不存在 / 已过期 / 已用** | **`10008`（与上游失败不可区分）** | ❌ 应给专用码 `20031` |
| Redis 不可用 | `10003`（503） | 正确（fail-closed，不发授权链接） |

**已知缺陷（P1）**：`verifyOAuthState` 的 `n == 0` 分支返回 `oauthFailed()`，
与 `mapOAuthErr` 对上游错误的返回**完全相同** —— 前端无法区分「链接过期」与「上游挂了」。

**实测证据（2026-11-25，三种情况返回完全一致）**：
```
有效 state + 假 code  → {"status_code":10008,"message":"Third-party login failed"}
state 重放            → {"status_code":10008,"message":"Third-party login failed"}
伪造 state            → {"status_code":10008,"message":"Third-party login failed"}
```

**修法**：`errcode_user.go` 加 `BizOAuthStateInvalid = 20031`（20027~20030 已用，20031 空闲），
文案建议 `"The login link has expired, please sign in again"`（比 "failed" 有行动指引）。

---

## 八、自查：如何验证映射与代码一致

```bash
# ① 微信映射表
sed -n '/func wechatErrorSentinel/,/^}/p' internal/pkg/oauthkit/wechat.go

# ② QQ 映射表
sed -n '/func qqCodeError/,/^}/p' internal/pkg/oauthkit/qq.go

# ③ 哨兵 → 用户可见（唯一映射点）
sed -n '/func mapOAuthErr/,/^}/p' internal/service/oauth_service.go

# ④ 交叉验证：三方对照（本文 vs 代码 vs 官方）
grep -n "错误码" docs/14-第三方登录调研.md
```

**任何一处对不上就是 bug** —— 本文的价值就在于让"对不上"变得可发现。

---

## 九、真凭据验证记录（2026-11-25 实测）

用**假 code** 验证凭据有效性，不需要真实用户授权：

| 渠道 | 上游返回 | 结论 |
|---|---|---|
| 微信 web `wx546665b783674034` | `{"errcode":40029,"errmsg":"invalid code"}` | ✅ **凭据有效**（微信先验 appid+secret 才查 code） |
| 微信 App `wxf33c27468076dcd6` | `40029` 同上 | ✅ 凭据有效 |
| QQ web `102065886` | `/token` → `{"error":100005,...}`；`/authorize` → HTTP 302 | ✅ 凭据有效、端点可达 |
| QQ App `1028...0304` | `/me` → `{"error":100013,"access_token is illegal"}` | ✅ 上游可达 |
| GitHub | 未配置 | — |

**判定口诀**：
- 微信 `40029` = 凭据有效 / `40013` = appid 错 / `40125` = secret 错
- QQ `100005` = 凭据有效 / `100007`、`100010` = 凭据错

---

## 十、QQ 上游的频率限制（测试必读）

**曾误判为「WAF 永久封锁」，实测是频率限制。**

| 端点 | 实测 |
|---|---|
| `/oauth2.0/authorize` | HTTP **302** 正常，145ms |
| `/oauth2.0/token` | 正常返回业务错误 |
| `/oauth2.0/me` | 正常 |
| `/user/get_user_info` | 正常 |

**触发条件**：短时间内连续调用（实测连打 2 次即 501）。
**规避**：调用间隔 ≥ 3 秒。**不是绕过 WAF**，是尊重对方的限流。

自动化测试加节流：
```go
var lastQQCall time.Time
func qqThrottle() {
    if d := time.Since(lastQQCall); d < 3*time.Second {
        time.Sleep(3*time.Second - d)
    }
    lastQQCall = time.Now()
}
```

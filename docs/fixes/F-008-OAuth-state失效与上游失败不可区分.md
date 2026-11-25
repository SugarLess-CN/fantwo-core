# F-008 OAuth state 失效与上游失败不可区分

| 项 | 内容 |
|---|---|
| 发现时间 | 2026-11-25 |
| 发现方式 | lead 在写 `docs/22-OAuth错误码对照表` 时真机复测发现 |
| 严重度 | P1（可用性：前端无法判断该做什么动作） |
| 影响面 | 全部 web 渠道回调（微信/QQ/GitHub 共 3 个平台 × 2 种模式） |
| 状态 | ✅ 已修复并复测 |
| 相关提交 | 见本次提交 |
| 相关文档 | `docs/22-OAuth错误码对照表.md` §七 |

## 一、现象

web 回调三种失败场景返回**完全相同**的响应：

```
① 有效 state + 假 code（上游拒）→ {"status_code":10008,"message":"Third-party login failed"}
② 同一 state 重放（防重放生效）  → {"status_code":10008,"message":"Third-party login failed"}
③ 伪造 state                    → {"status_code":10008,"message":"Third-party login failed"}
```

前端无法区分三种情况，而这三种**该做的动作完全不同**：
- ① 上游挂了 → 提示「稍后重试」
- ② 链接已用过 → 提示「重新登录」
- ③ 疑似伪造 → 应告警

## 二、复现步骤

```bash
# 起服务
SERVER_PORT=4031 go run ./cmd/server &

# 拿一个有效 state
ST=$(curl -s http://127.0.0.1:4031/api/v1/user/oauth/wechat/web/url \
  | python -c "import sys,json;print(json.load(sys.stdin)['data']['state'])")

# ① 有效 state + 假 code
curl -s -X POST .../wechat/web/callback -H "Content-Type: application/json" \
  -H "Device-Id: x" -d "{\"code\":\"FAKE\",\"state\":\"$ST\"}"
# ② 同一 state 再打一次（重放）
# ③ 伪造 state
# 改前：三者 status_code 都是 10008
```

## 三、根因

`internal/service/auth_service.go:681`（改前）：

```go
if n == 0 {
    return oauthFailed()          // ← state 无效
}
```

而 `internal/service/oauth_service.go:72`：

```go
func oauthFailed() *errcode.Err {
    return errcode.ErrInvalidParam.WithKey(i18n.AuthOAuthFailed)
}
```

`mapOAuthErr` 对上游错误（`ErrCodeInvalid` / `ErrCredentialBad` / `ErrEmailMissing`）
**也返回同一个 `oauthFailed()`** —— 两者输出完全相同，物理上不可区分。

## 四、修复方案

新增专用错误码：

| 文件 | 改动 |
|---|---|
| `errcode_user.go` | `BizOAuthStateInvalid = 20031` + `ErrOAuthStateInvalid`（HTTP 400） |
| `i18n/keys.go` + `en.go` | `auth.oauth_state_invalid` → `"The login link has expired, please sign in again"` |
| `auth_service.go` | **只改 `n == 0` 一个分支** |

**HTTP 用 400 而非 401 的理由**：401 会被前端拦截器当成「本站登录态失效」→ 清 token 跳登录页；
而这里的正确动作是「重点一次第三方登录」，不是「重新登录本站」。

**文案选「可执行」而非「失败」**：`"The login link has expired, please sign in again"`
直接告诉用户该做什么，比 `"Third-party login failed"` 有用。

### 刻意**不改**的两处（取舍）

| 场景 | 保持 | 理由 |
|---|---|---|
| 空 state | `ErrInvalidParam`（10008） | 这是我方编程错误，不是「链接过期」 |
| `Del` 失败（消费不掉） | `ErrDependencyUnavailable`（503） | 删不掉说明 Redis 有问题，用户重来还是会失败 —— **报 503 比报「链接过期」诚实** |

## 五、验证

### 三场景现在可区分

| # | 场景 | 实测 |
|---|---|---|
| ① | 有效 state + 假 code | `10008` "Third-party login failed" |
| ② | state 重放 | **`20031`** "The login link has expired, please sign in again" |
| ③ | 伪造 state | **`20031`** 同上 |

### 反向验证（未被误伤）

| # | 场景 | 实测 |
|---|---|---|
| ④ | 空 state | `10008` "Invalid request parameter"（仍 ErrInvalidParam） |
| ⑤ | Redis 不可用 | `10009` "Dependency service unavailable..."（仍 503） |

### 质量门

```
gofmt -l internal/ pkg/        → 空
go build ./...                 → OK
go vet ./...                   → 零输出
go test ./... -count=1         → 16 包全绿
i18n 守卫                       → 333 个 Key 全部有文案，无孤儿
```

## 六、影响与风险

**Breaking change**：多了一个 `20031`。前端拦截器若把「所有非 0」当同类处理，行为不变；
若要精细处理，需加这个分支（建议：`20031` → 提示重新发起第三方登录，**不要清本地 token**）。

**同类未修**（已登记）：
- `ErrCodeInvalid` / `ErrCredentialBad` / `ErrEmailMissing` / 3 个 `Unsupported`
  **仍共用 `10008`** —— 这 6 个语义不同的错误对外不可区分。
  修法同本案例（各给专用码），但影响面更大，需单独排期。

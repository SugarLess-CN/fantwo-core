# 13-T37f OAuth 收尾：state 错误码 + SDK 独立化 + 三层测试

> 快照：2026-11-25　｜　三轮任务 A/B/C 全部验收通过
> 相关：`docs/14 §9`（SDK 独立化）/ `docs/22`（错误码全链路）/ `docs/fixes/F-008`（state 缺陷）

---

## 一、本轮改了什么

### 任务 A：state 失效与上游失败可区分（真实缺陷修复）

| 文件 | 改动 |
|---|---|
| `internal/pkg/errcode/errcode_user.go` | 加 `BizOAuthStateInvalid = 20031` + `ErrOAuthStateInvalid`（HTTP 400） |
| `internal/pkg/i18n/keys.go` | 加 `AuthOAuthStateInvalid Key = "auth.oauth_state_invalid"` |
| `internal/pkg/i18n/en.go` | `"The login link has expired, please sign in again"` |
| `internal/service/auth_service.go` | `verifyOAuthState` 的 `n == 0` 分支：`oauthFailed()` → `errcode.ErrOAuthStateInvalid` |

**只改一个分支**，`Del` 失败仍走 503、空 state 仍走 10008。

### 任务 B：oauthkit 独立化

```
internal/pkg/oauthkit/  →  pkg/oauthkit/     （git mv，全部识别为 R，历史不丢）
```

**方案选 B（直接改 import + 删老包）**，不保留别名转发层：

| 方案 | 取舍 |
|---|---|
| A 别名转发 | 永久留两套同名包，`go doc` 重复，**类型别名层会吞掉编译期错误**（改字段名不报错） |
| **B 采纳** | 改 5 处 import（`model/config.go` + `service/` 下 4 个），成本远低于永久维护一层别名 |

**新增文件**：
- `doc.go`（85 行）包文档：快速开始 / 渠道矩阵 / 错误处理 / 安全约束
- `README.md`（307 行）对外说明：矩阵 / 16 键表 / 7 条禁令 / 14 哨兵 / 已知限制
- `example_test.go`（106 行）4 个可执行示例

### 任务 C：三层测试

| 层 | 文件 | 内容 | 是否进 CI |
|---|---|---|---|
| 1 | `fixtures_test.go` + `fixtures_assert_test.go` | 6 条**真实响应** fixture + 6 条断言 | ✅ 默认跑 |
| 2 | `credentials_test.go` | 5 条，`//go:build credentials`，凭据从环境变量读 | ❌ 需 tag |
| 3 | `scripts/oauth-e2e.sh`（269 行） | 5 渠道人工 E2E | ❌ 人工 |

**第 1 层最有价值的 3 条**（新增）：把真实 WAF 501 HTML 页喂给 `qqParseTokenBody` 与 `/me` 解析路径，
断言必须返回 `ErrUpstream` 且不泄漏 payload；再反证该 HTML 页里确实含 `=` 号 ——
**把「`url.ParseQuery` 对 HTML 页不报错」这个坑钉进测试**，将来有人想删「必须命中已知字段」判定时会立刻红。

---

## 二、功能变化

### 对外可见（breaking change）

| 场景 | 改前 | 改后 |
|---|---|---|
| state 重放 / 伪造 | `10008` "Third-party login failed" | **`20031`** "The login link has expired, please sign in again" |
| state 过期 | `10008` 同上 | **`20031`** 同上 |

**前端适配**：`20031` → 提示重新发起第三方登录，**不要清本地 token**（这不是本站登录态失效）。
旧代码若按 `10008` 判 state 失效，需同时匹配 `20031`。

### 内部结构

| 项 | 改前 | 改后 |
|---|---|---|
| SDK 位置 | `internal/pkg/oauthkit`（外部不可 import） | **`pkg/oauthkit`（可 `go get`）** |
| 测试断言 | 113 | **123**（+6 fixture +4 Example） |

---

## 三、配置项 / 接口清单

**新增 1 个 status_code**：`20031 BizOAuthStateInvalid`（段位 2xxxx = 认证用户域，正确）。

**新增 1 个 i18n key**：`auth.oauth_state_invalid`（总 Key 数 332 → **333**）。

**SDK 配置键 16 个**（不是 19 —— 以 `config_keys.go` 代码为准）：

```
wechat_web_appid / wechat_web_secret / wechat_web_redirect_uri / wechat_web_success_redirect_url
wechat_app_appid / wechat_app_secret
qq_web_appid / qq_web_appkey / qq_web_redirect_uri / qq_web_success_redirect_url
qq_app_appid
github_client_id / github_client_secret / github_redirect_uri / github_scope / github_success_redirect_url
```

**4 个「SDK 不读、勿补」**：3 个 `*_scope`（用代码默认值）+ `wechat_app_success_redirect_url`
（app 渠道不出 URL）+ `qq_app_appkey`（QQ 移动端由客户端 SDK 换 token，不需要）。

**哨兵错误 14 个**（不是 15 —— 以 `types.go` 实际 `errors.New` 计数为准）。

---

## 四、验证证据

### 任务 A：五场景真机复测（lead 独立复测，不只信成员报告）

```
① 有效 state + 假 code  → {"status_code":10008,"message":"Third-party login failed",...}
② state 重放            → {"status_code":20031,"message":"The login link has expired, please sign in again",...}
③ 伪造 state            → {"status_code":20031,"message":"The login link has expired, please sign in again",...}
④ 空 state              → {"status_code":10008,"message":"Invalid request parameter",...}
⑤ Redis 不可用          → {"status_code":10009,"message":"Dependency service unavailable, please try again later",...}
```

④⑤ 是**反向验证**：确认修复没有把不该混进来的情况也改成 20031。

### 任务 B：外部 import probe（决定性验证）

```bash
cd tmp/sdk-probe    # 项目外新建模块，replace 指向本项目
go run .
```
```
wechat web <nil>
qq app AuthURL -> oauthkit: provider does not support auth url for this channel
key: wechat_web_appid
```

搬包前这行会报 `use of internal package ... not allowed`。**三项都验了**：
provider 构造、渠道能力差异返回正确哨兵、配置键常量可访问。

### 任务 C：三层测试

```
go test ./pkg/oauthkit/ -count=1 -v | grep -cE "^(    ---|--- PASS)"  → 123
go test ./pkg/oauthkit/ -run Example -v                              → 4/4 PASS
go test -tags credentials ./pkg/oauthkit/ -v                         → 5/5 PASS
bash -n scripts/oauth-e2e.sh                                         → 语法通过
```

第 2 层真实运行（凭据从 DB 读出后 export）：
```
--- PASS: TestWechatWebCredentialsValid     凭据有效：上游报 code 无效（code_invalid）
--- PASS: TestWechatAppCredentialsValid
--- PASS: TestQQWebCredentialsOrWAF         接受 ErrCodeInvalid 或 ErrUpstream 两种
--- PASS: TestQQAppValidateTokenReachesUpstream   100013 → ErrTokenExpired
--- PASS: TestQQAppUserInfoWithoutValidateNeverHitsUpstream   0ms 本地拒
```

### 质量门

```
gofmt -l internal/ pkg/                → 空
go build ./...                         → OK
go vet ./...                           → 零输出
go vet -tags credentials ./pkg/oauthkit/ → 零输出
go test ./... -count=1                 → 16 包全绿
i18n 守卫                               → 333 个 Key 全部有文案，无孤儿
```

---

## 五、遗留与风险

### R1｜QQ 上游风控的真实性质（**重要，别再反复**）

同一天两次独立实测结论**相反**：

| 实测方 | `/token` | `/authorize` | 重试手段 |
|---|---|---|---|
| 成员 | **501**（WAF 页） | **501** | 10 秒重试、加 Referer/Accept/换 UA 均无效 |
| lead | **200** `100005` | **302** | 三次共 15 连测全正常 |

**结论：按出口 IP / 时段触发的间歇性风控，不是固定行为。**

**对测试的硬要求**：凡涉及 QQ `/token`、`/authorize` 的测试，
必须**同时接受两种结果**，只有 `ErrCredentialBad` 判失败。
把 501 当固定行为会在另一个 IP 上假红。

**文档已按此改写**：`pkg/oauthkit/README.md §6` 用「双实测对照表」呈现，
`docs/14 §9.7` 保留三次修订轨迹（① 永久封锁 → ② 频率限制 → ③ 间歇性路径风控），
把每一版为什么不成立都留在文档里。

**不变的部分**：`/oauth2.0/me` 与 `/user/get_user_info` 两次实测都正常；真实浏览器跳转始终不受影响。

### R2｜6 个哨兵仍共用 10008（同类问题，未修）

`ErrCodeInvalid` / `ErrCredentialBad` / `ErrEmailMissing` / 3 个 `Unsupported`
语义完全不同却都映射到 `10008` —— 与 F-008 是同一类问题，但影响面更大（要动 6 个映射）。
修法同 F-008（各给专用码），需单独排期。

### R3｜`golang.org/x/oauth2` 在 go.mod 仍标 `// indirect`

搬包不改变依赖，`go mod tidy` 会转正。**go.mod 归 lead**，下次动 go.mod 时顺手 tidy。

### R4｜`scripts/oauth-e2e.sh` 可能没有可执行位

本机 MSYS 无 `chmod`。已用 `git update-index --chmod=+x` 处理。
若 clone 后仍不可执行，用 `bash scripts/oauth-e2e.sh` 不受影响。

### R5｜本轮没做的事

- **没做真的端到端 OAuth 登录**（需要真人浏览器授权拿真 code）——
  第 3 层脚本已备好，但没有真人操作就无法验证「真 code → 真 userinfo → 建号」这一段
- **GitHub 渠道未验证**（密钥为空）
- **微信 App 渠道未端到端验证**（需要移动端 SDK 配合）

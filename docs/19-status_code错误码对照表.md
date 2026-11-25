# status_code 错误码对照表

> **前端 / 测试 / 联调的唯一错误码查找表。** 数据由代码自动提取后人工核对，非手写。
> 提取脚本逻辑：扫 `internal/pkg/errcode/*.go` 的 `Err` 定义 → 关联 `BizCode` 常量值 →
> 关联 `i18n` 的英文文案（`internal/pkg/i18n/en.go`）。
> 重新生成：见文末「如何保持本表与代码同步」。
> 最后更新：2026-11-25（第 2 次 P0 修复后）

---

## 一、先搞清一件事：status_code 与 HTTP 状态码**互相独立**

```
HTTP 状态码：传输/协议层语义（200 / 400 / 401 / 403 / 404 / 500 …），给网关、监控、CDN 看
status_code：业务语义（0 / 10001 / 20001 / 20003 …），给客户端决定「做什么动作」看
```
**两者同时返回，不要用一个推另一个。** 例：
```json
{"message":"Please log in first","status_code":20001,"trace_id":"...","timestamp":1795576632000}
//                        ↑ 业务码：去登录        ↑ HTTP 也是 401，但这是巧合，不是绑定关系
```
**为什么这么设计**：同一个 HTTP 401 可能是「没登录」「token 过期」「被封禁」三种完全不同的处理，
客户端只看 HTTP 码无法区分；而 10004（不存在）可能配 404 也可能配 200（防枚举场景）。

---

## 一之二、契约落地的九条硬规则

> 从 AGENTS.md 下分（2026-11-25，AGENTS 尺寸治理）。这是实现的约束，不是建议。

1. **所有接口响应一律英文**，格式 `{"message":"<英文>","status_code":<业务码>,"data":<object>}`；
   `data`/`message` 为空时**整个字段省略**（`MarshalJSON` 自定义序列化），`status_code` 恒在。
2. **status_code 与 HTTP 状态码无关**（分段见第三节）。
3. **message 一律走 i18n key**（`internal/pkg/i18n/`，332 个 key），**禁止在 handler/service 里硬编码**响应文案。
   加新文案流程：`keys*.go` 加 Key 常量 → `en.go` 加英文 → 业务代码引用，严禁绕过。
   （守卫测试 `TestEveryKeyConstantHasText` / `TestNoOrphanCatalogEntry` 会抓漏）
4. **原塞在 data 里的 message 一律提顶层**：用 `response.SuccessMsg(c, i18n.XXX, data)`，data 只放业务数据。
5. **错误码**：`errcode.Err` 持 `BizCode`+`Key`+`Args`；`WithKey/WithKeyf` 是推荐入口，
   `With/Withf` 只接受英文；`WithBizCode` 覆盖域细分码。
6. **测试断言按 `Err.BizCode` / `Err.Key` 契约**（`assertRejectKey` helper），
   **不锁文案字面量** —— 文案润色不该造成假红。
7. **例外（不用统一响应体）**：微信支付回调 `{"code":"SUCCESS"/"FAIL"}`（微信服务器契约）；
   `/health` 只返回 `{"status_code":0}`。
8. **存库业务数据保留中文**：`AuditResult.Reason`、订单标题、积分流水 `Description`、
   emoji「未分类」分组值 —— 这些是 **data 内容不是 message**，别翻译。
9. **内部错误细节进日志不进响应**（`svcInternal` 对外统一 `"Internal server error"`）。

## 二、响应体形状

```json
{ "data": {...}, "message": "英文文案", "status_code": 0, "trace_id": "uuid", "timestamp": 毫秒 }
```
| 字段 | 恒在？ | 说明 |
|---|---|---|
| `status_code` | ✅ 恒在 | 0 = 成功；非 0 见下表 |
| `trace_id` | ✅ 恒在 | 排障用，报问题时带上它 |
| `timestamp` | ✅ 恒在 | 毫秒时间戳 |
| `data` | ❌ 空时**整个字段省略** | 不是 `null`，是**不存在** |
| `message` | ❌ 空时省略 | 英文，来自 i18n，**前端可直接展示** |
**例外**（不走此契约）：微信支付回调返回 `{"code":"SUCCESS"}`（微信服务器要求）；`/health` 只返回 `{"status_code":0}`。

---

## 三、分段规则

| 段位 | 含义 | 当前已用 |
|---|---|---|
| `0` | 成功 | 1 个 |
| `1xxxx` | 通用（跨域复用） | 15 个 |
| `2xxxx` | 认证与用户域 | 26 个 |
| `3xxxx` | 内容域（预留，暂用通用码） | 0 个 |
| `4xxxx` | 交易域（预留，暂用通用码） | 0 个 |
| `5xxxx` | 运营域（预留，暂用通用码） | 0 个 |
| `6xxxx` | 基础设施域（预留） | 0 个（常量已定义未启用） |
| `7xxxx` | RBAC | 7 个 |

**注意**：`3xxxx`~`6xxxx` 目前只定义了段位起点、没有细分码 —— 这些域的错误一律复用 `1xxxx` 通用码。
将来要给某域细分时，在该段内新增，不要挪用别的段。

---

## 四、全量对照表

### 成功

| status_code | HTTP | 常量 | 英文文案 | 触发场景 |
|---|---|---|---|---|
| `0` | 200 | `BizOK` | Success | 一切正常 |

### 1xxxx — 通用（跨域）

| status_code | HTTP | 常量 | 英文文案 | 触发场景 |
|---|---|---|---|---|
| `10001` | BadRequest | `ErrBadRequest` | Invalid request | 参数格式错误（缺字段 / 类型不对 / JSON 解析失败） |
| `10002` | Unauthorized | `ErrUnauthorized` | Unauthorized | 未登录（**历史码**，2026-11-25 后认证域改用 20001） |
| `10003` | Forbidden | `ErrForbidden` | Permission denied | 无权限（**历史码**，认证域改用 20003） |
| `10004` | NotFound | `ErrNotFound` | Resource not found | 资源不存在（ID 查不到 / 已被删除） |
| `10005` | Conflict | `ErrConflict` | Resource conflict | 唯一性冲突（重复创建同名资源） |
| `10006` | TooManyRequests | `ErrTooManyRequests` | Too many requests, please try again later | 触发限流 |
| `10007` | InternalServerError | `ErrInternal` | Internal server error | 服务端内部错误（细节只进日志，不进响应） |
| `10008` | BadRequest | `ErrInvalidParam` | Invalid request parameter | 业务参数不合法（**最常用**：缺必填、格式对但值非法） |
| `10009` | ServiceUnavailable | `ErrDependencyUnavailable` | Dependency service unavailable, please try again later | 依赖不可用（DB / Redis / 上游挂了）→ 503，客户端可重试 |
| `10010` | Conflict | `ErrResourceInUse` | Resource is still in use | 资源被占用（有引用，不能删） |
| `10011` | Gone | `ErrExpired` | %s has expired | 资源已过期（验证码 / 邀请码 / 链接超时） |
| `10012` | Forbidden | `ErrNotAllow` | This operation is not allowed | 操作不被允许（状态机不允许此转换） |
| `10013` | Conflict | `ErrRewardClaimed` | Reward has already been claimed | 奖励已领取过（幂等拦截） |
| `10014` | InternalServerError | `ErrOperationFailed` | Operation failed: %s | 操作失败（非致命，可重试） |
| `10015` | Conflict | `ErrAlreadyExists` | %s already exists | 资源已存在（同 10005，语义更轻） |

### 2xxxx — 认证与用户域

| status_code | HTTP | 常量 | 英文文案 | 触发场景 |
|---|---|---|---|---|
| `20001` | Unauthorized | `ErrNeedAuth` | Please log in first | **未登录 / 登录态失效**（缺 token、验签失败、已登出、全端登出）→ 跳登录 |
| `20002` | Unauthorized | `ErrPermVersionStale` | Permissions changed, please log in again | 权限版本落后（管理员改过你的权限）→ 刷新 token 或重新登录 |
| `20003` | Forbidden | `ErrNoPermission` | Permission denied | **无权限**（已登录但没有该权限点）→ 提示无权访问 |
| `20005` | Conflict | `ErrMembershipDuplicated` | Membership is still active, no need to renew | 重复开通会员（已是会员又下单） |
| `20006` | NotFound | `ErrUserNotExist` | User not found | 用户不存在 |
| `20007` | BadRequest | `ErrPasswordError` | Incorrect username or password | 账号或密码错误 |
| `20008` | Conflict | `ErrUsernameExists` | This username is already taken | 用户名已被占用 |
| `20009` | Conflict | `ErrEmailExists` | This email is already registered | 邮箱已被占用 |
| `20010` | Conflict | `ErrPhoneExists` | This phone number is already registered | 手机号已被占用 |
| `20011` | BadRequest | `ErrVerifyCodeInvalid` | Invalid or expired verification code | 验证码不正确 |
| `20012` | Gone | `ErrVerifyCodeExpired` | Verification code expired | 验证码已过期 |
| `20013` | TooManyRequests | `ErrVerifyCodeTooOften` | Please wait before requesting another code | 验证码发送过于频繁 |
| `20014` | Unauthorized | `ErrRefreshTokenInvalid` | Invalid refresh token | refresh token 无效 |
| `20015` | Unauthorized | `ErrRefreshTokenExpired` | Refresh token expired | refresh token 已过期 → 重新登录 |
| `20016` | BadRequest | `ErrDeviceIDRequired` | The device-id header is required | 缺少 device-id 请求头 |
| `20017` | Forbidden | `ErrFollowSelf` | You cannot follow yourself | 不能关注自己 |
| `20018` | BadRequest | `ErrSignInAlready` | Already signed in today | 今日已签到 |
| `20019` | BadRequest | `ErrInsufficientBalance` | Insufficient points | 余额不足 |
| `20020` | Conflict | `ErrDeletionPending` | Account deletion already requested and is in the cooling-off period | 已有注销申请在处理中 |
| `20021` | BadRequest | `ErrDeletionNotPending` | No pending account deletion request | 没有待处理的注销申请 |
| `20022` | Forbidden | `ErrAccountDeleted` | This account has been deleted | 账号已注销 |
| `20023` | Conflict | `ErrCertificationExists` | A certification is already pending or approved | 已有认证申请（不能重复提交） |
| `20024` | NotFound | `ErrCertificationNotFound` | Certification record not found | 认证记录不存在 |
| `20025` | Conflict | `ErrCertificationNotPending` | This certification is not pending review | 认证申请不在待审状态 |
| `20026` | BadRequest | `ErrOAuthNotConfigured` | Third-party login is not configured | 该第三方登录渠道未配置（appid/secret 缺失） |
| `20027` | Forbidden | `ErrAccountBanned` | This account has been banned | 账号已被封禁 |
| `20028` | Forbidden | `ErrOAuthUnbindLastMethod` | This is your last way to sign in, please set a phone number or email before unbinding | 解绑会移除最后一种登录方式 → 先绑手机/邮箱 |
| `20029` | NotFound | `ErrOAuthBindingNotFound` | This third-party account is not bound to your profile | 该第三方账号未绑定到你的账号 |
| `20030` | Conflict | `ErrOAuthBindingConflict` | This third-party account is already bound to another user | 该第三方账号已被其他用户绑定 |

### 7xxxx — RBAC

| status_code | HTTP | 常量 | 英文文案 | 触发场景 |
|---|---|---|---|---|
| `70001` | BadRequest | `ErrRoleNameExists` | Role name %s already exists | 角色名已存在 |
| `70002` | NotFound | `ErrRoleNotExist` | Role not found | 角色不存在 |
| `70003` | Forbidden | `ErrRoleIsSystem` | Built-in system roles cannot be deleted | 系统内置角色不可删除 |
| `70004` | Conflict | `ErrRoleInUse` | Role is assigned to users and cannot be deleted | 角色正在使用中，不能删 |
| `70005` | NotFound | `ErrPermissionNotExist` | Permission not found | 权限点不存在 |
| `70006` | BadRequest | `ErrPermissionNameExists` | Permission name already exists | 权限点名已存在 |
| `70007` | Conflict | `ErrPermissionInUse` | Permission is still referenced by roles and cannot be deleted | 权限点正在使用中，不能删 |
---

## 五、前端处理建议

### 5.1 统一拦截（推荐）

```js
axios.interceptors.response.use(
  (res) => res.data,
  (err) => {
    const body = err.response?.data ?? {}
    const code = body.status_code

    // 登录态失效 → 清本地态 + 跳登录（带 returnUrl）
    // 注意：10002 是 2026-11-25 之前的历史码，双码兼容期请一并判断
    if (code === 20001 || code === 10002) {
      clearAuth(); location.href = `/login?redirect=${encodeURIComponent(location.pathname)}`
      return Promise.reject(body)
    }

    // 权限版本落后 → 静默刷新 token 后重放原请求（只重试一次，防死循环）
    if (code === 20002) return refreshTokenThenRetry(err.config)

    // 无权限 → 提示，不要跳登录（用户是登录的）
    if (code === 20003 || code === 10003) { toast(body.message); return Promise.reject(body) }

    // 依赖不可用 → 可重试
    if (code === 10009) { toast('服务暂时不可用，请稍后重试'); return Promise.reject(body) }

    // 其余：直接把 message 展示给用户（文案已英文，且来自 i18n，可安全展示）
    toast(body.message ?? 'Request failed')
    return Promise.reject(body)
  }
)
```

### 5.2 必须区分的三组

| 场景 | 码 | 动作 |
|---|---|---|
| 没登录 / 登录失效 | `20001`（旧 `10002`） | 跳登录 |
| 登录了但没权限 | `20003`（旧 `10003`） | 提示无权，**不要跳登录** |
| 权限被管理员改过 | `20002` | 刷新 token 重放，**不要跳登录** |

这三组混起来是联调期最常见的 bug（尤其把 20003 当 20001 处理 → 用户明明登录着却被踢出去）。

### 5.3 排障

遇到 `500` / `10007` 时，把响应里的 `trace_id` 给后端，能在日志里精确定位那一次请求。
**不要**把 `data` 里的内容当错误原因 —— 内部错误细节按设计不进响应。

---

## 六、变更记录（影响客户端的）

| 日期 | 变更 | 影响 |
|---|---|---|
| 2026-11-24 | status_code 从「HTTP 码别名」改为**独立业务码**（5~6 位分段） | 初始化 |
| 2026-11-24 | 响应体新增 `trace_id` + `timestamp` | 客户端可开始记录 trace_id 排障 |
| **2026-11-25** | **登出/黑名单校验补全后，全站「未登录」统一为 `20001`**（原 user 域 20001、其余域 10002） | **breaking change**：`10002` 判断需扩展到 `20001` |
| **2026-11-25** | 同上，「无权限」统一为 `20003`（原 `10003`） | 同上：`10003` → 加 `20003` |
| 2026-11-25 | 新增 `20028`/`20029`/`20030`（第三方绑定管理） | 无（新接口） |
| 2026-11-25 | 新增 `20027`（账号被封禁） | 无（原为通用码） |

---

## 七、如何保持本表与代码同步

本表是**从代码提取**的，不是手写维护 —— 手写必然过期。三个来源：

```bash
# 1) 所有 Err 定义与业务码（唯一定义源）
grep -nE "Biz[A-Za-z]+\s+=|Err[A-Za-z]+\s+= &Err\{" internal/pkg/errcode/*.go

# 2) 英文文案（key → 文案）
grep -nE "^\s*[A-Z][A-Za-z0-9]*:" internal/pkg/i18n/en.go

# 3) 反向守卫：key 常量 ↔ 文案 必须一一对应（缺一个测试就红）
go test ./internal/pkg/i18n/ -run "TestEveryKeyConstantHasText|TestNoOrphanCatalogEntry" -v
```

**加新错误码的完整流程**：

1. `internal/pkg/errcode/errcode_<域>.go`：加 `Biz<名字> = <段位内递增>` 常量 + `Err<名字> = &Err{BizCode:..., HTTP:..., Key: i18n.<Key>}`
2. `internal/pkg/i18n/keys_<域>.go`：加 `Key` 常量
3. `internal/pkg/i18n/en.go`：加英文文案（**忘了这步 `TestEveryKeyConstantHasText` 会红**）
4. 业务代码用 `errcode.Err<名字>` 或 `errcode.Err<名字>.WithKey(...)`
5. **回本文件对应段位表格加一行**（这是唯一需要手工的一步）

**不要**复用别的段位的码。段位是给客户端做粗分类判断用的（比如「2xxxx 都是账号相关」）。

---

## 八、常见误用（review 时该拦下的）

| 误用 | 后果 | 正确做法 |
|---|---|---|
| 用 `HTTP 码` 判断业务动作 | 10004 有时配 404 有时配 200（防枚举），判断失效 | 永远看 `status_code` |
| 把 `user:update`（改自己）用在「改他人」路由上 | **P0 越权**（2026-11-25 真实发生过） | 改他人一律 `user:manage` |
| 错误码文案里塞 `%s` 但不传参数 | 用户看到 `"Third-party login failed: %s"`（2026-11-25 真实发生过） | 细节场景用 `*Detail` 系列 key 并传参 |
| 同一语义在不同域用不同码 | 客户端要写 N 套判断（2026-11-25 修的就是这个） | 通用语义用 `1xxxx` 或认证用 `2xxxx`，全站统一 |
| 内部错误细节进响应 | 泄漏表名 / SQL / 上游响应体 | 细节只进日志，响应用 `ErrInternal` |

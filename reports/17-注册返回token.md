# 17 — 注册即登录：`POST /user/register` 返回 token

> 用户需求：**注册之后要返回 token（含 token 与 refresh_token），字段与登录接口大致一致。**
> 日期：2026-11-28

---

## 一、本轮改了什么

| 文件 | 改动 |
|---|---|
| `internal/service/auth_service.go` | ① `RegisterInput` +`DeviceID`；② 新增 `RegisterResult`（注册 → `issueTokens` → `LoginResultWithConfig`） |
| `internal/handler/auth.go` | 注册 handler 改调 `RegisterResult` 并传 `deviceIDOf(c)`；swagger 响应类型改 `dto.LoginResultDto`、补 `device-id` header 注解 |

`Register` 本身**未改**（仍返回 `*model.User`）—— 管理端建号 `CreateUser` 复用它，不需要 device 也不需要 token，保持原样。

## 二、功能变化

### 2.1 响应结构（breaking change）

| | 改前 | 改后 |
|---|---|---|
| `data` 类型 | `dto.UserDto`（31 字段） | `dto.LoginResultDto`（**32 字段**） |
| `token` | ❌ 无 | ✅ 1061 字符 |
| `refresh_token` | ❌ 无 | ✅ 1061 字符 |
| `config` | ❌ 无 | ✅ 28 字段（用户设置） |
| `certification` | ❌ 无 | ✅ brief 版（未认证时不传） |
| `equipped_decorations` | ❌ 无 | ✅（未佩戴时不传） |

**注册与登录的字段集实测完全一致**：

```
共同字段数: 32 | 注册 32 / 登录 32
注册独有: 无
登录独有: 无
```

前端收益：注册后**不用再调一次 `POST /user/login`**，少一次往返，也不会出现
「注册成功但登录失败」的中间态。

### 2.2 新增行为：`device-id` 必填

注册现在与登录**同口径**要求 `device-id` 头：

```
POST /user/register  （不带 device-id）
→ status_code: 20016  auth.device_id_required
```

**为什么必须要求**：`refresh_token` 与设备绑定（`user_device` 表按
`(user_id, device_id)` 唯一）。空 device_id 会让所有「没传头」的客户端被合并成同一条设备，
多端登录与全端登出都会失效（todo 里记录的已知问题）。

**前端要做的**：注册请求补 `Device-Id` 头（如果之前没传）。登录已经在传了，通常同一个 http client 会带上。

## 三、验证（真机，端口 4221）

**① 注册返回 token / refresh_token / config**

```
status_code: 0 | message: common.ok
token 长度: 1061
refresh_token 长度: 1061
config 存在: True
equipped_decorations 存在: False     ← 新用户无装扮，omitempty 正确不传
字段数: 32
null 计数: 0
```

**② 注册 vs 登录字段集对比** → 见 §二.1（32/32，无差异）

**③ 不传 device-id**

```
status_code: 20016 | message: auth.device_id_required
```

**④ 注册返回的 token 直接可用**

```
用该 token 调 GET /user/profile → status_code: 0 | username: usetok1795836648
```

**⑤ refresh_token 可换新 token**

```
POST /user/refresh-token → status_code: 0 | 新 token 长度: 1061
```

**⑥ swagger**

```
swag exit=0，危险词 0，paths 260 / defs 310
/user/register 的 200 schema 含 LoginResultDto: True
```

质量门：`gofmt` 空 / `build` OK / **17 包全绿 FAIL 0**。

测试账号已删（`user_roles` / `user_device` / `user_config` / `user` 全清，残留 0）。

## 四、遗留与风险

### 4.1 breaking change 说明

响应 `data` 从 `UserDto` 变成 `LoginResultDto`。虽然 `LoginResultDto` **内嵌** `UserDto`
（所有原字段仍在同一层），但：

- 如果前端做了 **exact shape 校验**（如 zod `.strict()`），会因多出 `token`/`refresh_token`/`config` 等字段而报错 → 需放宽为 `.passthrough()` 或补字段。
- 如果前端把注册响应**直接当 user 对象**存进 store，现在会多存 token 等字段 —— 通常无害。

### 4.2 顺带暴露的既有问题（本轮未修）

**其余 4 个登录入口仍不校验 device-id**（todo 已记）：
`SmsLogin` / `WechatAppLogin` / `QQAppLogin` / `QQAppLoginPayload` / `GithubCallback`。
注册这次已对齐登录口径，但这几个还是缺 —— 同样会写空 `device_id`。
建议在 handler 层统一加 `requireDeviceID` 帮助函数，一次覆盖全部入口。

### 4.3 注册不创建 `user_config` 的影响

`RegisterResult` 里 `LoginResultWithConfig` 会调 `configOf`。若该用户还没有
`user_config` 行，`config` 会是 `nil` → 字段不传。

**实测新注册用户 `config 存在: True`** —— 说明注册流程里（或 `GetConfig` 内部）
会自动建默认配置，无需额外处理。

若将来这条链路变化导致 `config` 缺失，前端会在注册后看不到默认设置，
届时需要「注册后建默认 config」显式化。

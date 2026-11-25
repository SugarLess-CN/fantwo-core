# F-003 Login 缺 device-id 返回错错误码

| 项 | 内容 |
|---|---|
| 发现时间 | 2026-11-25 14:40 |
| 发现方式 | 用户报障（Apifox 登录拿到 10008） |
| 严重度 | P1（契约级；客户端无法判断缺什么） |
| 影响面 | `POST /user/login` |
| 状态 | ✅ 已修复并复测 |
| 相关提交 | `5d9f45f` |
| 相关文档 | `docs/19` §四（20016）、`docs/20` §3.4 |

## 一、现象

用户在 Apifox 里：

```js
myHeaders.append("Device-Id", "");   // ← 空字符串
// → {"message":"Invalid request parameter","status_code":10008}
```

真机三态：

| 请求 | 改前 | 改后 |
|---|---|---|
| 不传 `Device-Id` | 10008 | **20016** |
| `Device-Id: ""` | 10008 | **20016** |
| `Device-Id: IQOO` | 200 | 200 |
| `account` 空（对照） | 10008 | 10008（未误伤） |

## 二、复现步骤

```bash
curl -s -X POST .../user/login -H "Content-Type: application/json" \
  -d '{"account":"test_admin","password":"Test123456"}'
# 改前：{"status_code":10008,"message":"Invalid request parameter"}
# 改后：{"status_code":20016,"message":"The device-id header is required"}
```

## 三、根因

```go
// 改前 auth_service.go:90
if in.Account == "" || in.Password == "" || in.DeviceID == "" {
    return nil, "", "", errcode.ErrInvalidParam   // 10008，看不出缺什么
}
```

三项参数**合并判断** → 统一返回通用参数错误。

而代码里本来就有专门的 `ErrDeviceIDRequired`（20016），
`RefreshToken` 接口用的就是它（`handler/auth.go:118`）—— **同一语义两个码**。

## 四、修复方案

拆开判：

```go
if in.Account == "" || in.Password == "" {
    return nil, "", "", errcode.ErrInvalidParam
}
// device-id 是**请求头**不是 body 字段，缺失时客户端需要知道「去补哪个头」
if in.DeviceID == "" {
    return nil, "", "", errcode.ErrDeviceIDRequired
}
```

与 `RefreshToken` 口径统一，也用上了本来就为它定义的 `AuthDeviceIDRequired` 文案。

## 五、验证

```
① 不传 Device-Id         → 20016 The device-id header is required
② Device-Id 空字符串     → 20016
③ Device-Id 有值         → 200 / token 2301 字符
④ account 空（对照组）   → 10008 Invalid request parameter

go build OK / go vet 零输出 / go test 16 包全绿
```

## 六、影响与风险

**Breaking change**：仅错误码从 10008 变 20016，客户端若硬编码 10008 需同步。

**同类未修（已记 todo）**：另外 5 个登录入口**完全不校验** device-id：

`SmsLogin`(:505) / `WechatAppLogin`(:901) / `QQAppLogin`(:927) /
`QQAppLoginPayload`(:936) / `GithubCallback`(:847)

后果：空 deviceID 一路走到 `issueTokens` → `UpsertDevice` 写入 `user_device.device_id = ''`。
而 devices 表按 `(user_id, device_id)` 唯一，**所有没传 device-id 的客户端会被合并成同一条设备记录**，
多端登录 / 全端登出语义受影响。

建议在 handler 层加统一的 `requireDeviceID(c)`，而不是 5 个 service 方法各写一遍。

**文档修正**：`docs/20` 原写「缺失返回 20016」，写的时候没实测（当时实际是 10008），已改。

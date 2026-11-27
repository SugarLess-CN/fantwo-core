# qqbot — QQ 官方机器人（BOT API v2）Go SDK

只做两件事：**发消息** 与 **收事件（webhook）**。零依赖（仅标准库），不依赖本项目任何内部包，
可被任意 Go 项目直接引用 —— 与 [`pkg/oauthkit`](../oauthkit) 同一定位。

```bash
go get github.com/SugarLess-CN/fantwo-core/pkg/qqbot   # 或直接拷 pkg/qqbot 目录
```

---

## 1. 快速开始

```go
c, err := qqbot.New(qqbot.Config{
    AppID:     "1020xxxx",        // qqbot_app_id
    AppSecret: "your-app-secret", // qqbot_app_secret —— 取 access_token 用
    BotSecret: "your-bot-secret", // qqbot_bot_secret —— **webhook 验签用**
})
if err != nil {
    if errors.Is(err, qqbot.ErrNotConfigured) {
        // 未配置：把 QQ 机器人功能整体降级，而不是每条消息都失败
        return
    }
    return err
}
```

⚠️ **AppSecret 与 BotSecret 是两个不同凭据**。拿 AppSecret 去算 webhook 签名会**静默验不过**
（平台侧叫「机器人密钥」）。这是接线时最常见的错法，单测里专门有一条反例覆盖它。

---

## 2. 发消息

```go
_, err := c.SendC2C("USER_OPENID", qqbot.SendRequest{      // 或 c.SendGroup("GROUP_OPENID", ...)
    MsgType: qqbot.MsgTypeText,
    Content: "你的邀请码是 ABC123",
    MsgID:   msgID, // 被动回复：来自事件的 **d.id**
    MsgSeq:  1,
})
```

### 2.1 三种发送模式（官方互斥语义）

| 模式 | 请求特征 | 时效 | 次数/额度 | 用户可否阻断 |
|---|---|---|---|---|
| ① 被动回复 | `MsgID` 或 `EventID` 非空 | 收到消息后 **60 分钟** | 同一 msg_id 最多 **4 次**（`msg_seq` 1→4） | 否 |
| ② 互动召回 | `IsWakeup = true` | 用户对话后 **30 天** | 4 个周期各 1 条 | 是 |
| ③ 普通主动 | 两者都为空 | 随时 | 单关系 **20/qpm**、每用户 **1000 条/天** | 是 |

**SDK 在本地拦截非法组合**（`KindInvalidArgument`，不打上游、不消耗频控额度）：

| 非法组合 | 为什么拦 |
|---|---|
| `MsgID` + `IsWakeup` | 召回与被动回复互斥 |
| `MsgID` + `EventID` | 官方是二选一 |
| `MsgSeq` 有值但两个 ID 都没有 | 说明调用方以为自己在被动回复却没带 ID，静默忽略更难查 |
| `MsgType=0` 但 `Content` 为空 | 发一条空消息没有意义 |
| `MsgType=2` 但 `Markdown` 为空 / `Content` 非空 | 官方：填了 markdown 则 content 必须为空 |
| 传了 `Markdown` 却声明 `MsgType=0` | 平台会忽略 markdown，静默丢内容 |
| 空 openid / 不支持的 `MsgType` / 负数 `MsgSeq` | 参数错误 |

`qqbot.SendMode(req)` 返回 `"passive_reply"` / `"wakeup"` / `"proactive"`，
发件池按模式分流时不用自己再写一遍判断。

---

## 3. 收事件（webhook）

```go
payload, validation, err := c.ParseWebhook(body, r.Header)   // body 必须是**原始字节**
switch {
case err != nil:
    w.WriteHeader(http.StatusForbidden)        // fail-closed
case validation != nil:
    json.NewEncoder(w).Encode(validation)      // op=13 地址验证：原样回写
case payload.Type == qqbot.EventC2CMessageCreate:
    var msg qqbot.C2CMessage
    if err := payload.DecodeData(&msg); err != nil { ... }
    _ = msg.ID          // ★ 被动回复要用的 msg_id 在 d.id
    _ = msg.Author.OpenID
}
```

### 3.1 三态语义

| op | 返回 | 说明 |
|---|---|---|
| `13` 地址验证 | `(payload, validation, nil)` | **不带签名头**，不能要求签名；算出签名回写 `{plain_token, signature}` |
| `0` 业务事件 | `(payload, nil, nil)` | **必须验签** |
| 其它 / 结构非法 / 验签失败 | `(nil, nil, err)` | 未知 op 明确报错，不当业务事件放行 |

### 3.2 签名算法（两个必踩的坑）

```go
seed := botSecret
for len(seed) < ed25519.SeedSize {   // 32
    seed = strings.Repeat(seed, 2)   // ★ 循环填充，不是 hash
}
seed = seed[:ed25519.SeedSize]
_, privateKey, _ := ed25519.GenerateKey(strings.NewReader(seed))

msg := eventTs + plainToken          // ★ event_ts 在前、plain_token 在后
signature := hex(ed25519.Sign(privateKey, msg))
```

官方标准答案（`SignValidation` 单测锚点，逐字比对）：

```
appid=11111111  secret=DG5g3B4j9X2KOErG
plain_token=Arq0D5A61EgUu4OxUvOp  event_ts=1725442341
→ 87befc99c42c651b3aac0278e71ada338433ae26fcb24307bdc5ad38c1adc2d01bcfcadc0842edac85e85205028a1132afe09280305f13aa6909ffc2d652c706
```

对不上就说明派生或字段顺序错了。**把 secret 做 md5/sha256 再当 seed 也会错** —— 必须原样填充。

---

## 4. 错误处理：只看 Kind，不认错误码

```go
var qe *qqbot.Error
if errors.As(err, &qe) {
    switch qe.Kind {
    case qqbot.KindRetryable:       // 频控/去重/上游抖动 → 退避重试
    case qqbot.KindWindowExpired:   // 被动窗口过期 → 进池等待
    case qqbot.KindPermanent:       // 用户拒收/违规/无好友 → 死信
    case qqbot.KindConfig:          // 凭据/机器人状态 → 改配置
    case qqbot.KindInvalidArgument: // 我传错了 → 改代码
    }
}
```

**为什么必须由 SDK 分类**：官方错误码 40+ 个且会变；散落在业务代码里必然走样。
`Error.Code` / `Error.Message` 只用于人工排查，**禁止用于分支判断**（官方明说 message 内容可能随时调整）。

| Kind | 代表错误码 | 池子动作 |
|---|---|---|
| `KindWindowExpired` | `40034005` `304103` `40034128` `40034026` | `DEFERRED`，等用户下次发言 |
| `KindRetryable` | `40034100` `40054005` `50055002` `100001` `40034004` `40034122` | `PENDING`，退避重试 |
| `KindPermanent` | `40054013` `40054004` `40034006` `40034105` `40034024` `40054007` 等 | `FAILED`，出池进死信 |
| `KindConfig` | `100007` `100016` `10004` | 改配置，重试无用 |
| `KindInvalidArgument` | —（本地校验产生，Code 恒 0） | 改代码 |
| `KindUnknown` | 未收录的新码 | 保守按**可重试**，避免误判成永久失败丢消息 |

⚠️ `KindWindowExpired.Retryable() == false`：它不是「等一会儿再试」，而是「等用户开口」。
盲目重试只会反复撞 `40034128` 并浪费频控额度。

---

## 5. access_token 管理

| 要点 | 实现 |
|---|---|
| 有效期 | 官方 7200s；缓存 `expires_in - 60s`（临近 60 秒内平台会返回新值，旧值仍有效） |
| 并发 | 互斥 + 进锁双检 → 10 个 goroutine 同时发消息只刷 **1 次**上游 |
| 401 | 丢缓存，下一次自动取新 token（给可重试错误，不递归重试） |
| `expires_in` | 官方是**字符串** `"7200"`；`flexInt64` 同时兼容字符串/数字/浮点 |
| 缺 `expires_in` | **报错**，不猜默认值（否则过期 token 会长期留在内存） |
| `Ping()` | 强制拉一次 token，**不吃缓存** —— 缓存命中的 200 会把失效凭据一直报成健康 |

---

## 6. 配置项

| 键 | 必填 | 说明 |
|---|---|---|
| `qqbot_app_id` | ✅ | 机器人 AppID |
| `qqbot_app_secret` | ✅ | ClientSecret（取 token）。**敏感** |
| `qqbot_bot_secret` | ✅（收事件） | webhook 验签 seed。**敏感**，仅发消息可不配 |
| `qqbot_enabled` | — | 业务侧总开关（SDK 不感知，调用方据此决定是否 New） |
| `qqbot_api_base` | — | 默认 `https://api.bot.qq.com` |
| `qqbot_token_url` | — | 默认 `https://api.bot.qq.com/app/getAppAccessToken` |
| `qqbot_invite_command` / `qqbot_invite_auto_reply` | — | 业务文案，SDK 不使用 |

`qqbot.SensitiveConfigKeys` 列出需要打码的两项（对齐 `model.ConfigSensitiveKeys` 语义）。

---

## 7. 已知限制 / 待真机确认

1. **业务事件的验签请求头未进官方文档**。`docs/29 §5.3` 只记录了 op=13 的签名算法，
   没有记录业务事件的验签头。本 SDK 采用官方 webhook 的 Discord 风格约定：
   `X-Signature-Ed25519` + `X-Signature-Timestamp`，签名对象 = `timestamp + 原始 body`。
   **首次真机联调时请核对这两个头名**（若平台用的是别的头名，改 `webhook.go` 的
   `HeaderSignature` / `HeaderTimestamp` 两个常量即可，逻辑不变）。
2. **只实现 HTTP，不含 WebSocket 长连接**。webhook 模式是官方推荐且本项目唯一需要的。
3. **富媒体 / 图文卡片只透传**（`MsgTypeMedia` / `MsgTypeCard`）：`media` / `ark` 字段需要
   先调上传接口拿 `file_info`，本轮不纳入。
4. **回调地址限制在平台侧**：必须 HTTPS，端口只允许 80 / 443 / 8080 / 8443。

---

## 8. 目录结构

```
pkg/qqbot/
├── doc.go            包文档 + 最小用法示例
├── types.go          Payload / SendRequest / Client 接口（契约，勿改签名）
├── errors.go         ErrorKind 三分类 + KindOf 映射表（核心产出）
├── config_keys.go    配置键常量 + Config（唯一定义源）
├── token.go          access_token 缓存与并发刷新
├── client.go         构造 + HTTP 往返 + 响应归一化 + Ping
├── message.go        SendC2C / SendGroup + 参数互斥校验
├── webhook.go        验签 + 三态解析 + 地址验证
├── README.md         本文档
└── *_test.go         25 个用例（httptest 假上游，不发真网络）
```

## 9. 依赖与许可证

**零第三方依赖**：`net/http` / `encoding/json` / `crypto/ed25519` / `encoding/hex` / `sync`。
不涉及 go.mod 变更。许可证随主仓库。

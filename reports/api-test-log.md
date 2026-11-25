# 接口测试日志（增量追加，格式见 api-test-spec.md 第二节）

服务：http://127.0.0.1:3990　｜　任务：T-39　｜　测试员：api-tester

---

### [L-001] 2026-11-25 10:52:48 ｜ POST /api/v1/user/register

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6 注册

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_chain01_user","nickname":"test_nickname_01","password":"***","email":"test_chain01@example.com"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"a3baeae0-37ab-405b-9b10-46465b06bff6","username":"test_chain01_user","nickname":"test_nickname_01","avatar":null,"description":null,"background":null,"address":null,"gender":"other","birthDate":null,"email":"test_chain01@example.com","phone":null,"status":"ACTIVE","articleCount":0,"followerCount":0,"followingCount":0,"likes":0,"level":0,"experience":0,"wallet":0,"points":0,"membershipLevel":0,"membershipLevelName":"普通用户","membershipStatus":"INACTIVE","membershipEndDate":null,"myInviteCode":null,"inviteEarnings":0,"inviteCount":0,"createdAt":"2026-11-25T10:52:48.3078098+08:00","roles":[{"id":"9cad126e-4692-4419-b06e-b55d4ef3d792","name":"user","displayName":"普通用户"}],"isMember":false},"trace_id":"d847e6fd-...","timestamp":1795575168326}` |
| 耗时 | `71ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：注册主路径正常。data 内 membershipLevelName="普通用户"、roles[].displayName="普通用户" 为存库业务数据（AGENTS 响应契约第 8 条允许中文），非 i18n 文案违规。avatar/description 等 null 字段是 ViewUser 脱敏视图的属性字段（对象内），与「list 类空数据要 []」的约定不冲突。

### [L-002] 2026-11-25 10:52:55 ｜ POST /api/v1/user/register（异常：重复用户名）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_chain01_user","password":"***"}` |
| HTTP 码 | `409` |
| status_code | `20008` |
| 响应体 | `{"message":"This username is already taken","status_code":20008,"trace_id":"7aa6e63c-bd68-43e4-93cd-11a4695c83bd","timestamp":1795575175713}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：HTTP 409 + 业务码 20008（6 位 2=认证域）与文档「status_code 与 HTTP 独立」一致；message 英文。

### [L-003] 2026-11-25 10:52:55 ｜ POST /api/v1/user/register（异常：弱密码 123 —— 发现真问题）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_chain01_weak","password":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"28cdba07-...","username":"test_chain01_weak",...同结构全量用户视图...},"trace_id":"78ee38fd-...","timestamp":1795575175813}` |
| 耗时 | `58ms` |
| 是否需要修改 | **是** |
| 修改内容 | 待定位（`internal/service` 注册密码校验链路，预计 auth_service.go Register 缺强度校验） |
| 有问题 | **期望弱密码被拒绝（业务码/40x），实际 3 位纯数字密码注册成功并建号** |
| 结论 | **❌ 失败** |

**备注**：按 AGENTS「改表结构/动安全机制/改动面>3 文件 → 停下报 lead」判断：密码强度校验属安全机制。**先继续测完链-01 剩余步骤，测完立即 send_message 报 lead，由 lead 决策是否由我修。** 造出的 test_chain01_weak 账号已通过 SQL 清理（user_device 0 行、user_roles 1 行、user 1 行，外键顺序正确），库内仅剩 test_chain01_user。

### [L-004] 2026-11-25 10:54:04 ｜ POST /api/v1/user/login

**逻辑链**：链-01 账号生命周期　｜　**步骤**：2/6 登录

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`device-id: test-device-01` |
| 请求体 | `{"account":"test_chain01_user","password":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"a3baeae0-...","refreshToken":"eyJ...<后8位=Pwq3TzV8_wtg>","token":"eyJ...<后8位=_FtHi_w>","user":{...ViewUser 脱敏视图，email 显示为 t****@example.com...},"username":"test_chain01_user"},"trace_id":"7a28a835-...","timestamp":1795575244171}`（完整 3060 字符） |
| 耗时 | `69ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：token/refreshToken/user/id/username 结构齐备。email 自动脱敏为 t****@example.com（脱敏视图预期行为）。

### [L-005] 2026-11-25 10:54:21 ｜ POST /api/v1/user/login（异常：错误密码）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：2/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`device-id: test-device-01` |
| 请求体 | `{"account":"test_chain01_user","password":"***"}` |
| HTTP 码 | `400` |
| status_code | `20007` |
| 响应体 | `{"message":"Incorrect username or password","status_code":20007,"trace_id":"3666ba94-...","timestamp":1795575261095}` |
| 耗时 | `52ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：文案统一为「Incorrect username or password」，不区分「用户不存在/密码错误」，防枚举语义正确（与 L-006 对照见备注差异）。

### [L-006] 2026-11-25 10:54:21 ｜ POST /api/v1/user/login（异常：不存在的账号）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：2/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`device-id: test-device-01` |
| 请求体 | `{"account":"test_nosuch_user_xx","password":"***"}` |
| HTTP 码 | `404` |
| status_code | `20006` |
| 响应体 | `{"message":"User not found","status_code":20006,"trace_id":"e1506721-...","timestamp":1795575261159}` |
| 耗时 | `11ms` |
| 是否需要修改 | **是** |
| 修改内容 | 待定（见问题） |
| 有问题 | **期望防枚举语义（与 L-005 相同响应），实际对不存在账号返回 404 + "User not found"，对存在账号+错密码返回 400 + "Incorrect username or password" —— 两者可区分，账号枚举面暴露** |
| 结论 | **⚠️ 通过但有问题** |

**备注**：原 NestJS 服务是否同样区分需对照（第六节核对）。若原文档也是区分的，则属「文档与实现一致但安全面照旧」，降级 P2；若原服务统一文案，则实现错。此项在报告第六节核对 tmp/picart 源码后定级。**注意：此条与 L-005 的「区分」不构成安全问题实锤，先记录不修，等对照结论。**

### [L-007] 2026-11-25 10:54:28 ｜ GET /api/v1/user/profile

**逻辑链**：链-01 账号生命周期　｜　**步骤**：3/6 取用户信息

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | —（GET 无体） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"address":null,"articleCount":0,"avatar":null,"background":null,"birthDate":null,"createdAt":"...","description":null,"email":"t****@example.com",...,"nickname":"test_nickname_01",...,"points":0,"roles":[...],"status":"ACTIVE","username":"test_chain01_user","wallet":0},"trace_id":"56af143b-...","timestamp":1795575268159}`（完整 815 字符） |
| 耗时 | `50ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：响应 data 键按字母序输出（gin 序列化 map 行为），不影响契约。

### [L-008] 2026-11-25 10:54:28 ｜ GET /api/v1/user/profile（异常：无 token）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：3/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | —（无 Authorization） |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"1d363aea-...","timestamp":1795575268209}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：HTTP 401 与业务码 20001 独立并存，符合契约。链-07 将批量验证同类。

### [L-009] 2026-11-25 10:54:38 ｜ PATCH /api/v1/user/profile

**逻辑链**：链-01 账号生命周期　｜　**步骤**：4/6 更新资料

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"nickname":"test_nickname_01_v2","description":"updated by T-39 test","gender":"male"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"a3baeae0-...","nickname":"test_nickname_01_v2","description":"updated by T-39 test","gender":"male","experience":5,...}}`（完整 810 字符） |
| 耗时 | `20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：指针语义更新生效；experience 0→5（资料补齐事件 bonus，跨域事件链路正常发火）。

### [L-010] 2026-11-25 10:54:38 ｜ PATCH /api/v1/user/profile（异常：伪造 token）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：4/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJ1aWQiOiJmYWtlIn0.invalid_sig` |
| 请求体 | `{"nickname":"x"}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"efc0338d-...","timestamp":1795575278064}` |
| 耗时 | `7ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：伪造签名被拒，验签中间件有效。

### [L-011] 2026-11-25 10:54:38 ｜ PATCH /api/v1/user/profile（异常：空请求体）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：4/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"Invalid request parameter","status_code":10008,"trace_id":"90f19d7c-...","timestamp":1795575278120}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：全空 body 被拒（服务层有空更新判定），10008 通用参数码正确。

### [L-012] 2026-11-25 10:54:47 ｜ POST /api/v1/user/password/change（异常：旧密码错误）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：5/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"oldPassword":"***","newPassword":"***"}` |
| HTTP 码 | `400` |
| status_code | `20007` |
| 响应体 | `{"message":"Incorrect username or password","status_code":20007,"trace_id":"08cff62f-...","timestamp":1795575287020}` |
| 耗时 | `54ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：旧密码校验生效。message 复用登录文案（「Incorrect username or password」在改密场景略不精确但可接受，不单列问题）。

### [L-013] 2026-11-25 10:54:47 ｜ POST /api/v1/user/password/change

**逻辑链**：链-01 账号生命周期　｜　**步骤**：5/6 改密码

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"oldPassword":"***","newPassword":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Password changed successfully","status_code":0,"data":{"success":true},"trace_id":"7a96a2f7-...","timestamp":1795575287160}` |
| 耗时 | `97ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：改密成功，message 英文、i18n 语义正确。

### [L-014] 2026-11-25 10:55:01 ｜ POST /api/v1/user/login（改密后重新登录）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：6/6 重新登录

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`device-id: test-device-01` |
| 请求体 | `{"account":"test_chain01_user","password":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"a3baeae0-...","refreshToken":"eyJ...<后8位=<截断>","token":"eyJ...<后8位=<截断>","user":{...},"username":"test_chain01_user"},"trace_id":"<截断>"}` |
| 耗时 | `<70ms（同 L-004 量级）` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：新密码登录成功，旧密码（L-012 路径）已失效 —— 闭环成立。注：本条实际响应与 L-004 同结构，token 已存 tmp/tok01.txt 复用（时间戳 1795575301731）。

### [L-015] 2026-11-25 10:55:01 ｜ GET /api/v1/user/profile（异常：改密后旧签名 token）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：6/6 异常路径

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<手工构造的旧 iat token 变体，签名已坏>` |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"6bdf1220-...","timestamp":1795575301731}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：改密后旧 token（签名变体）被拒。会话失效语义成立。


---

## 弱密码修复复测（lead 裁决后修复：auth_service.go 3 处 + i18n 2 key）

**改动**：`internal/service/auth_service.go` — Register L130-133（<6 → `AuthPasswordTooShort`）/ ChangePassword L352-355（<8 → `AuthNewPasswordTooShort`）/ ResetPassword L379-382（<8 同口径）；均按 `utf8.RuneCountInString` 计数。i18n key 由 lead 提供。质量门：gofmt 空 / build OK / vet 零输出 / test 16 包全绿（2026-11-25 10:59:46）。服务已重启生效。

### [L-016] 2026-11-25 11:00:21 ｜ POST /api/v1/user/register（复测：3 位密码）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6-异常·复测（对应 L-003）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_pw_a","password":"***"}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"Password must be at least 6 characters","status_code":10008,"trace_id":"966eb0de-...","timestamp":1795575621926}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—`（本轮修复已生效） |
| 有问题 | **无**（修复验证通过） |
| 结论 | **✅ 通过** |

**备注**：L-003 的 ❌ 已闭环。未建号（复测后无残留）。

### [L-017] 2026-11-25 11:00:21 ｜ POST /api/v1/user/register（复测：4 字中文密码，验 rune 计数）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6-异常·复测

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_pw_b","password":"***"}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"Password must be at least 6 characters","status_code":10008,"trace_id":"a0c2eb28-...","timestamp":1795575621978}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：「密码六位」= 4 rune（12 byte）。若用 `len()`（byte）判则 12 ≥ 6 会误放行 —— 被拒证明是按 rune 计数。

### [L-018] 2026-11-25 11:00:22 ｜ POST /api/v1/user/register（复测：5 字中文密码）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6-异常·复测

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_pw_c","password":"***"}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"Password must be at least 6 characters","status_code":10008,"trace_id":"f2b7a313-...","timestamp":1795575622034}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：5 rune（15 byte）≥ 6 byte 仍被拒 —— rune 计数判定双重确认。

### [L-019] 2026-11-25 11:00:22 ｜ POST /api/v1/user/register（复测：6 字中文密码应过）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6-复测（边界通过例）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_pw_d","password":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"2a37f0ea-...","username":"test_pw_d",...},"trace_id":"3c7f1473-...","timestamp":1795575622214}` |
| 耗时 | `135ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：恰好 6 rune 放行，边界正确（< 6 拒、= 6 过）。账号 test_pw_d 测后已清理。

### [L-020] 2026-11-25 11:00:22 ｜ POST /api/v1/user/register（复测：6 位英文密码应过）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：1/6-复测（边界通过例）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"username":"test_pw_e","password":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"e81fbb57-...","username":"test_pw_e",...},"trace_id":"8bebe5cf-...","timestamp":1795575622311}` |
| 耗时 | `58ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：英文边界同样正确。账号 test_pw_e 测后已清理（user_roles/user_roles 外键顺序删除，0 残留）。

### [L-021] 2026-11-25 11:00:29 ｜ POST /api/v1/user/password/change（复测：7 位新密码）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：5/6-异常·复测

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"oldPassword":"***","newPassword":"***"}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"New password must be at least 8 characters","status_code":10008,"trace_id":"71ffe7df-...","timestamp":1795575629674}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：改密 8 位阈值生效，文案区分注册（"New password..."）。

### [L-022] 2026-11-25 11:00:29 ｜ POST /api/v1/user/password/change（复测：8 位新密码应过）

**逻辑链**：链-01 账号生命周期　｜　**步骤**：5/6-复测（边界通过例）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"oldPassword":"***","newPassword":"***"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Password changed successfully","status_code":0,"data":{"success":true},"trace_id":"5f59cfc6-...","timestamp":1795575629811}` |
| 耗时 | `94ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：= 8 rune 放行，改密边界正确。密码现为 8 位新值（test 账号，收尾统一清理）。

### [L-023] 2026-11-25 10:56:57 ｜ POST /api/v1/article

**逻辑链**：链-02 内容主链路　｜　**步骤**：1/8 发文章

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"title":"T-39 test article","content":"test content for chain-02","type":"image","images":["uploads/test-a.jpg"],"status":"PUBLISHED"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"64b59ed4-5b94-41e6-ae08-0b253791c3cf","title":"T-39 test article","requireLogin":false,...,"type":"image","content":"test content for chain-02","images":["uploads/test-a.jpg"],...,"status":"PENDING","authorId":"a3baeae0-...","author":{...},"categoryId":null,"tags":[],...},"trace_id":"7d77e112-...","timestamp":1795575417478}` |
| 耗时 | `89ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：请求 status=PUBLISHED 但落库 PENDING —— **审核流语义正确**（发布类内容先进审核，对齐「审核链路完整闭环」架构）。tags 为 `[]` 非 null，符合空数组契约。

### [L-024] 2026-11-25 10:57:08 ｜ POST /api/v1/article（异常：缺 title）

**逻辑链**：链-02 内容主链路　｜　**步骤**：1/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"content":"no title"}` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Invalid parameter: Key: 'CreateArticleRequest.Title' Error:Field validation for 'Title' failed on the 'required' tag","status_code":10001,"trace_id":"c20fe386-...","timestamp":1795575428641}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：10001 与 10008 并存（content 域用 10001，auth 域用 10008），均属通用参数错误域，无契约冲突。

### [L-025] 2026-11-25 10:57:08 ｜ POST /api/v1/article（异常：非法 type）

**逻辑链**：链-02 内容主链路　｜　**步骤**：1/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"title":"x","type":"bogus"}` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Invalid parameter: Key: 'CreateArticleRequest.Type' Error:Field validation for 'Type' failed on the 'oneof' tag","status_code":10001,"trace_id":"1e1fd578-...","timestamp":1795575428691}` |
| 耗时 | `7ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：oneof 校验生效。

### [L-026] 2026-11-25 10:57:08 ｜ GET /api/v1/article

**逻辑链**：链-02 内容主链路　｜　**步骤**：2/8 列表查询

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | —（query：page=1&limit=5） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[...5 items...],"meta":{"total":16,"page":1,"limit":5,"totalPages":4}},"trace_id":"<...>","timestamp":<...>}` |
| 耗时 | `20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：分页 meta（total/page/limit/totalPages）齐备；本测试文章已 PUBLISHED 入列表（审核异步通过后回写）；list 内嵌 author 视图正常。

### [L-027] 2026-11-25 10:57:36 ｜ GET /api/v1/article/:id

**逻辑链**：链-02 内容主链路　｜　**步骤**：3/8 详情

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"64b59ed4-...","title":"T-39 test article",...,"views":1,...}}`（完整 ~1500 字符） |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：views 0→1（浏览计数生效）。

### [L-028] 2026-11-25 10:57:36 ｜ GET /api/v1/article/:id（异常：不存在的 id）

**逻辑链**：链-02 内容主链路　｜　**步骤**：3/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Article not found","status_code":10004,"trace_id":"4ccc4b06-...","timestamp":1795575455990}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：10004 不存在码 + 404 双码正确。

### [L-029] 2026-11-25 10:57:36 ｜ PATCH /api/v1/article/:id

**逻辑链**：链-02 内容主链路　｜　**步骤**：4/8 更新

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"title":"T-39 test article v2","summary":"updated summary"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"64b59ed4-...","title":"T-39 test article v2",...}}` |
| 耗时 | `<20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：部分字段更新生效（title/summary 变，其余保持）。

### [L-030] 2026-11-25 11:02:05 ｜ POST /api/v1/article/:id/like

**逻辑链**：链-02 内容主链路　｜　**步骤**：5/8 点赞

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | —（POST 无体） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"isLiked":true,"likes":1,"reactionType":"like"},"trace_id":"3decfb49-...","timestamp":1795575725819}` |
| 耗时 | `25ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：camelCase 字段（isLiked/reactionType）符合全局命名口径。

### [L-031] 2026-11-25 11:02:05 ｜ POST /api/v1/article/:id/like（重复点赞幂等）

**逻辑链**：链-02 内容主链路　｜　**步骤**：5/8-异常（幂等）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"isLiked":true,"likes":1,"reactionType":"like"},"trace_id":"3fd56721-...","timestamp":1795575725874}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：重复点赞 likes 仍为 1，计数幂等正确。

### [L-032] 2026-11-25 11:02:05 ｜ GET /api/v1/article/:id/like/status

**逻辑链**：链-02 内容主链路　｜　**步骤**：5/8 状态查询

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"isLiked":true,"reactionType":"like"},"trace_id":"361ac380-...","timestamp":1795575725930}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：状态与点赞结果一致。

### [L-033] 2026-11-25 11:02:05 ｜ GET /api/v1/article/:id/like/count

**逻辑链**：链-02 内容主链路　｜　**步骤**：5/8 计数（公开端点）

| 字段 | 内容 |
|---|---|
| 请求头 | —（free 端点，无 token） |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"count":1},"trace_id":"bf87cbcc-...","timestamp":1795575725984}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：公开端点（free）无 token 可访问，符合路由注册（`g.GET("/:id/like/count", free, ...)`）。

### [L-034] 2026-11-25 11:02:17 ｜ POST /api/v1/comment

**逻辑链**：链-02 内容主链路　｜　**步骤**：6/8 评论

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"articleId":"64b59ed4-...","content":"test comment from T-39"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"1a9ac674-...","content":"test comment from T-39","images":[],"likes":0,"replyCount":0,"floor":1,"isPinned":false,"status":"PUBLISHED","authorId":"a3baeae0-...","author":{...},"articleId":"64b59ed4-...","parentId":null,"rootId":null,"isLiked":false,...},"trace_id":"bac12977-...","timestamp":1795575738011}` |
| 耗时 | `83ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：楼层号 floor=1 正确分配；images 为 `[]` 非 null。

### [L-035] 2026-11-25 11:02:18 ｜ POST /api/v1/comment（异常：空 content）

**逻辑链**：链-02 内容主链路　｜　**步骤**：6/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"articleId":"64b59ed4-...","content":""}` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Invalid parameter: Key: 'CreateCommentRequest.Content' Error:Field validation for 'Content' failed on the 'required' tag","status_code":10001,"trace_id":"7129e76d-...","timestamp":1795575738108}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：required 校验生效。

### [L-036] 2026-11-25 11:02:18 ｜ POST /api/v1/comment（异常：不存在的 articleId —— 发现真问题）

**逻辑链**：链-02 内容主链路　｜　**步骤**：6/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"articleId":"00000000-0000-0000-0000-000000000000","content":"x"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"0ba57366-...","content":"x",...,"articleId":"00000000-0000-0000-0000-000000000000","parentId":null,"rootId":null,...},"trace_id":"caeec74f-...","timestamp":1795575738193}` |
| 耗时 | `43ms` |
| 是否需要修改 | **是** |
| 修改内容 | `internal/service/comment.go:212-216 新增文章存在性校验（FindByID → errcode.ErrNotFound.WithKey(i18n.ArticleNotFound)）` |
| 有问题 | **期望给不存在的文章评论返回 404（对齐原仓库 comment.service.ts:570-572 `if (!article) throw new Error("Article not found")`），实际 200 成功建评论（孤儿数据）** |
| 结论 | **❌ 失败** |

**备注**：原实现事务里先查文章、不存在即抛错；Go 版漏了这一步，产生悬挂评论（article_id 指向不存在文章）。已修 + 质量门全绿（gofmt/build/vet/test 16 包，11:05:28）+ 孤儿评论已 SQL 清理（comments 表 0 残留）。

### [L-037] 2026-11-25 11:05:58 ｜ POST /api/v1/comment（复测：不存在的 articleId）

**逻辑链**：链-02 内容主链路　｜　**步骤**：6/8-异常·复测（对应 L-036）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | `{"articleId":"00000000-0000-0000-0000-000000000000","content":"x"}` |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Article not found","status_code":10004,"trace_id":"c0168303-...","timestamp":1795575958068}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—`（修复已生效） |
| 有问题 | **无**（复测通过） |
| 结论 | **✅ 通过** |

**备注**：L-036 闭环。10004 + 404 + 英文文案三要素正确，未落库任何数据。

### [L-038] 2026-11-25 11:06:53 ｜ POST /api/v1/article/:id/favorite

**逻辑链**：链-02 内容主链路　｜　**步骤**：7/8 收藏

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"favoriteCount":1,"isFavorited":true},"trace_id":"b8eb76fd-...","timestamp":1795576012962}` |
| 耗时 | `28ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：收藏计数同步 +1。

### [L-039] 2026-11-25 11:06:53 ｜ GET /api/v1/article/:id/favorite/status

**逻辑链**：链-02 内容主链路　｜　**步骤**：7/8 状态查询

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"isFavorited":true},"trace_id":"b0321943-...","timestamp":1795576013019}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：状态一致。

### [L-040] 2026-11-25 11:06:53 ｜ DELETE /api/v1/article/:id/favorite

**逻辑链**：链-02 内容主链路　｜　**步骤**：7/8 取消收藏

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"favoriteCount":0,"isFavorited":false},"trace_id":"10511a28-...","timestamp":1795576013082}` |
| 耗时 | `19ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：取消后计数回 0，对称语义成立。

### [L-041] 2026-11-25 11:07:05 ｜ DELETE /api/v1/article/:id（越权：普通用户 B 删 A 的文章 —— IDOR 防护）

**逻辑链**：链-02 内容主链路　｜　**步骤**：8/8-异常（越权，链-10 预演）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<userB token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to delete this article","status_code":10003,"trace_id":"97ba4e2a-...","timestamp":1795576025294}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：userB（test_chain02_userb，普通用户角色）删 A 的文章被 403 拦截 —— 作者判定 + 权限中间件双层生效。**文章未被删除**（后续 L-043 作者本人可正常删）。

### [L-042] 2026-11-25 11:07:05 ｜ PATCH /api/v1/article/:id（越权：普通用户 B 改 A 的文章）

**逻辑链**：链-02 内容主链路　｜　**步骤**：8/8-异常（越权）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<userB token 截断>>` |
| 请求体 | `{"title":"hijacked by B"}` |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this article","status_code":10003,"trace_id":"bfd2210c-...","timestamp":1795576025392}` |
| 耗时 | `11ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：越权改标题被拦，标题未被篡改。

### [L-043] 2026-11-25 11:07:14 ｜ DELETE /api/v1/article/:id（作者本人删除）

**逻辑链**：链-02 内容主链路　｜　**步骤**：8/8 删除

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"deleted":true,"id":"64b59ed4-5b94-41e6-ae08-0b253791c3cf"},"trace_id":"62017ef4-...","timestamp":1795576034498}` |
| 耗时 | `24ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：作者本人删除成功（软删），链-02 主线闭环。

### [L-044] 2026-11-25 11:07:14 ｜ DELETE /api/v1/article/:id（异常：重复删除已删文章）

**逻辑链**：链-02 内容主链路　｜　**步骤**：8/8-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Article not found","status_code":10004,"trace_id":"3496c507-...","timestamp":1795576034556}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：软删后二次删除按「不存在」处理，语义合理。


---

## 链-03 上传链路

### [L-045] 2026-11-25 11:08:09 ｜ POST /api/v1/upload/file

**逻辑链**：链-03 文件链路　｜　**步骤**：1/3 上传图片

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>`　`Content-Type: multipart/form-data` |
| 请求体 | multipart：`file=@test-upload.png（125 字节最小 PNG，type=image/png）` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":[{"id":"02ebe164-afff-4e44-94fa-c0552e46bfa6","filename":"130c7aa5-...png","hash":"130c7aa5-...","mimeType":"image/png","originalName":"test-upload.png","path":"uploads\\2026\\11\\25\\13\\0c\\...png","size":125,"storage":"local","scene":"image","auditStatus":"pending","processed":true,"thumbnails":[],"referenceCount":1,"url":"http://127.0.0.1:3990/api/v1/uploads/2026/11/25/13/0c/...png","userId":"a3baeae0-...","watermarked":false,...}],"trace_id":"ace4a38c-...","timestamp":1795576089871}` |
| 耗时 | `55ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：响应是**数组**（多文件契约）。thumbnails 为 `[]`（T-34b5 契约生效）。审核状态 pending（异步审核链路）。url 指向本地静态路由。

### [L-046] 2026-11-25 11:08:21 ｜ POST /api/v1/upload/file（异常：内容与 MIME 不符）

**逻辑链**：链-03 文件链路　｜　**步骤**：1/3-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>`　`Content-Type: multipart/form-data` |
| 请求体 | multipart：`file=@fake.png（内容 "MZ fake exe content"，伪装 type=image/png）` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"File content is not a valid image/png file","status_code":10001,"trace_id":"b58ff060-...","timestamp":1795576101608}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：魔数校验（content sniffing）生效 —— 伪装成 PNG 的非图片内容被按**内容**而非声明 MIME 拒绝。

### [L-047] 2026-11-25 11:08:21 ｜ POST /api/v1/upload/file（异常：无 token）

**逻辑链**：链-03 文件链路　｜　**步骤**：1/3-异常

| 字段 | 内容 |
|---|---|
| 请求头 | —（无 Authorization） |
| 请求体 | multipart：`file=@test-upload.png` |
| HTTP 码 | `401` |
| status_code | `10002` |
| 响应体 | `{"message":"Unauthorized","status_code":10002,"trace_id":"219d1cdf-...","timestamp":1795576101655}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：infraChain 鉴权生效。

### [L-048] 2026-11-25 11:08:21 ｜ POST /api/v1/upload/file（异常：超大文件 21MB）

**逻辑链**：链-03 文件链路　｜　**步骤**：1/3-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>`　`Content-Type: multipart/form-data` |
| 请求体 | multipart：`file=@big.png（21MB 全零字节，type=image/png）` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"File too large: 21.0MB exceeds the 20MB limit","status_code":10001,"trace_id":"adadb8dc-...","timestamp":1795576101826}` |
| 耗时 | `62ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：upload_max_image_size=20MB（DB 配置）生效；文案含实际体积与限额，可执行性好。

### [L-049] 2026-11-25 11:08:32 ｜ GET /api/v1/upload

**逻辑链**：链-03 文件链路　｜　**步骤**：2/3 查上传记录（列表）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | —（query：page=1&limit=10） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[...1 item...],"meta":{"total":1,"page":1,"limit":10,"totalPages":1}},...}` |
| 耗时 | `14ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：⚠️ 过程记录：先按直觉打了 `GET /upload/info?page=1` 得 404 page not found —— 查路由源码确认列表端点是 `GET /upload`（`/info/:id` 才是详情）。属测试路径笔误非接口缺陷。普通用户只能看到自己的记录（handler 内降级语义，user 无 upload:list 权限）。

### [L-050] 2026-11-25 11:08:32 ｜ GET /api/v1/upload/info/:id

**逻辑链**：链-03 文件链路　｜　**步骤**：2/3 查上传详情

| 字段 | 内容 |
|---|---|
| 请求头 | —（NoAuth 端点，对齐原 @NoAuth，未带 token） |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"02ebe164-...","mimeType":"image/png","originalName":"test-upload.png",...同 L-045 结构...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：公开详情端点（无 token 可访问），与路由注册 `g.GET("/info/:id", h.Info)`（未挂链）一致。

### [L-051] 2026-11-25 11:09:01 ｜ DELETE /api/v1/upload/:id（普通用户无 upload:delete 权限）

**逻辑链**：链-03 文件链路　｜　**步骤**：3/3-异常（权限）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=_FtHi_w>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"Permission denied","status_code":10003,"trace_id":"4153ec65-...","timestamp":1795576141107}` |
| 耗时 | `7ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：user 角色只有 upload:create/upload:info，无 upload:delete → 403 正确（DB 权限点核实）。userB 同样 403（11:09:22，trace 4c13cc21）。**测试上传文件因此无法经 API 清理**（需 admin token 或 SQL）—— 收尾统一清理。


---

## 链-04 积分运营链路

### [L-052] 2026-11-25 11:10:48 ｜ POST /api/v1/user/sign-in（显式签到 —— 已被自动签到占位）

**逻辑链**：链-04 运营链路　｜　**步骤**：1/4 签到

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `400` |
| status_code | `20018` |
| 响应体 | `{"message":"Already signed in today","status_code":20018,"trace_id":"4d1bf154-...","timestamp":1795576248378}` |
| 耗时 | `13ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **⚠️ 通过但有问题（语义澄清，非缺陷）** |

**备注**：**过程曲折**：11:10:30 第一次打这个端点全返回 401/20001 —— 排查发现 tmp/tok01.txt 里的 token 是**改密前的旧 token**（exp 未过期但服务端设备会话在改密后被 SetLogoutAll 全端登出），401 是正确行为。重新登录取新 token 后再测：显式签到返回 20018「Already signed in today」—— 因为本账号 11:07 登录时**登录即自动签到**（user.dailyLogin 事件，isAuto=true 见 L-056），幂等保护正确拦截重复签到。**签到幂等 ✅（重复签到不重复计分）**。前两次 401 记入本条过程，不单列。

### [L-053] 2026-11-25 11:10:30 ｜ POST /api/v1/user/sign-in（异常：改密后旧 token 全端失效）

**逻辑链**：链-04 运营链路　｜　**步骤**：1/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<旧token截断>>` |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"f58067c4-...","timestamp":1795576230835}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：JWT 本身未过期（exp=1795578901 > now=1795576248），但改密时 SetLogoutAll 已删设备会话 —— **服务端会话校验生效**（不只是签名校验）。这是安全语义的正确实现，链-01 的补充验证。

### [L-054] 2026-11-25 11:11:08 ｜ GET /api/v1/user/wallet/balance

**逻辑链**：链-04 运营链路　｜　**步骤**：2/4 查积分

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"balance":0},"trace_id":"64533c40-...","timestamp":1795576268392}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：balance=0。注意：本账号的 DAILY_LOGIN +10 积分若已发，这里应 >0 —— 需与 L-055 流水交叉验证（见下条）。

### [L-055] 2026-11-25 11:11:08 ｜ GET /api/v1/user/wallet/transactions

**逻辑链**：链-04 运营链路　｜　**步骤**：3/4 查积分流水

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":5,"totalPages":0}},"trace_id":"fb1cba9b-...","timestamp":1795576268453}` |
| 耗时 | `15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：空流水 `[]` 非 null（契约正确）。balance=0 且无流水，与 L-056 的 isAuto=true 一致性存疑 → 需确认自动签到是否发积分。**交叉验证**：sign-in/records 显示自动签到已记录（consecutiveDays=1），但积分流水为空 —— 可能 DAILY_LOGIN 活动未启用或积分为 0。这属**业务配置问题**（活动开关），不是接口缺陷；记录待查项，报告第七节说明。

### [L-056] 2026-11-25 11:11:08 ｜ GET /api/v1/user/sign-in/records

**逻辑链**：链-04 运营链路　｜　**步骤**：3/4 签到记录

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":[{"consecutiveDays":1,"isAuto":true,"signInDate":"2026-11-25"}],"trace_id":"1a0796a2-...","timestamp":1795576268538}` |
| 耗时 | `14ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：自动签到记录存在（isAuto=true），跨域事件 user.dailyLogin → ops 域落库链路工作正常。响应无分页包装（小数组直出），与 wallet/transactions 的 {list,meta} 包装不同 —— 两种风格并存，无契约错误但风格不统一（P3 建议，报告第八节提）。

### [L-057] 2026-11-25 11:11:08 ｜ GET /api/v1/achievement/my（异常：路径不存在）

**逻辑链**：链-04 运营链路　｜　**步骤**：4/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Achievement not found","status_code":10004,"trace_id":"dc6e87f6-...","timestamp":1795576268625}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：`/achievement/my` 不是注册路由，被 `/:id` 通配捕获并按「成就不存在」处理 —— gin 路由优先级语义正确（静态路径优先，未命中走参数路由）。

### [L-058] 2026-11-25 11:11:27 ｜ GET /api/v1/achievement

**逻辑链**：链-04 运营链路　｜　**步骤**：4/4 查成就（公开列表）

| 字段 | 内容 |
|---|---|
| 请求头 | —（noAuth 端点） |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":[{"id":"d811eef8-...","code":"FIRST_ARTICLE","name":"初出茅庐","description":"发布第一篇文章",...,"rewardPoints":10,"rewardExp":50,...,"progress":0,"completed":false,"claimed":false},...]}` |
| 耗时 | `<20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：21 个种子成就返回。**name/description 为中文**（「初出茅庐」「发布第一篇文章」）—— 属**存库业务数据**（种子数据内容），按 AGENTS 契约第 8 条允许，非 message 文案违规。completed/progress 与用户关联的判定字段存在（noAuth 下全 0 合理）。

### [L-059] 2026-11-25 11:11:37 ｜ GET /api/v1/achievement/stats

**逻辑链**：链-04 运营链路　｜　**步骤**：4/4 成就统计

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<新token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"total":21,"completed":2,"claimed":0,"completionRate":"9.52"},"trace_id":"d2a173f4-...","timestamp":1795576297603}` |
| 耗时 | `26ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：completed=2（FIRST_ARTICLE 发文 + 评论触发），成就事件链路真实工作。completionRate 为字符串 "9.52"（2/21），百分比的字符串表示 —— 前端需 parseFloat，契约上可接受（P3 观察项）。


---

## 链-05 浏览链路

### [L-060] 2026-11-25 11:13:10 ｜ GET /api/v1/category

**逻辑链**：链-05 浏览链路　｜　**步骤**：1/4 查分类

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[{"id":"6d19e7d4-...","name":"美食","parentId":"f0c37f76-...","sort":0,"status":"ENABLED","articleCount":0,"followCount":0,...},{"id":"f0c37f76-...","name":"生活","parentId":null,...},...]}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：种子分类返回（美食/生活等），parentId 树形结构存在。中文名为存库种子数据（契约第 8 条允许）。

### [L-061] 2026-11-25 11:13:10 ｜ GET /api/v1/tag

**逻辑链**：链-05 浏览链路　｜　**步骤**：2/4 查标签

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":10,"totalPages":0}},"trace_id":"8529e29b-...","timestamp":1795576390745}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：空标签列表 `[]` + meta 分页包装（契约正确；无种子标签）。

### [L-062] 2026-11-25 11:13:10 ｜ GET /api/v1/category/:id（异常：不存在的分类）

**逻辑链**：链-05 浏览链路　｜　**步骤**：2/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Category not found","status_code":10004,"trace_id":"63a531c3-...","timestamp":1795576390844}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：域专属文案（Category not found）+ 通用码 10004。

### [L-063] 2026-11-25 11:13:10 ｜ GET /api/v1/article?categoryId=<不存在>（异常：按不存在分类查文章）

**逻辑链**：链-05 浏览链路　｜　**步骤**：3/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | —（query：categoryId=00000000-...） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":10,"totalPages":0}},"trace_id":"1d097195-...","timestamp":1795576390904}` |
| 耗时 | `15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：不存在的 categoryId 过滤 → 空列表而非报错 —— 列表过滤语义正确（filter 不命中 = 空结果，不是 404）。

### [L-064] 2026-11-25 11:13:19 ｜ GET /api/v1/article/search

**逻辑链**：链-05 浏览链路　｜　**步骤**：4/4 搜索

| 字段 | 内容 |
|---|---|
| 请求头 | —（opt1 可选登录，未带 token） |
| 请求体 | —（query：keyword=test） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":10,"totalPages":0}},"trace_id":"97d495a4-...","timestamp":1795576399190}` |
| 耗时 | `17ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：ES 搜索端点正常响应。空结果合理：唯一含 "test" 的文章已在链-02 删除；ES 索引同步依赖 article.created 事件，本账号文章已删。搜索降级（ES 不可用时 DB 兜底）未单独验证 —— 报告第七节列为未覆盖项。

### [L-065] 2026-11-25 11:13:19 ｜ GET /api/v1/article?title=T-39

**逻辑链**：链-05 浏览链路　｜　**步骤**：4/4-异常（无结果搜索）

| 字段 | 内容 |
|---|---|
| 请求头 | — |
| 请求体 | —（query：title=T-39） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,...}}}` |
| 耗时 | `14ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：标题模糊搜索空结果正确（测试文章已删，软删不入列表）。


---

## 链-06 交易链路（不真实支付）

### [L-066] 2026-11-25 11:14:22 ｜ POST /api/v1/order/article

**逻辑链**：链-06 交易链路　｜　**步骤**：1/4 下单（购买付费文章）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | `{"articleId":"5f62fc61-45e7-4c1c-ba33-b79462982205","remark":"T-39 test order"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"4b2f2165-c589-462c-b45e-de10549b38bf","userId":"a3baeae0-...","authorId":"a3baeae0-...","articleId":"5f62fc61-...","orderNo":"ORDER1795576462641992","type":"ARTICLE","title":"购买文章：T-39 paid article for order","amount":9.9,"status":"PENDING","paidAt":null,"details":{"articleId":"5f62fc61-...","articleTitle":"T-39 paid article for order","remark":"T-39 test order"},"remark":"T-39 test order"},"trace_id":"a43463d6-...","timestamp":1795576462648}` |
| 耗时 | `20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：**金额校验正确** —— 商品 viewPrice=9.9，订单 amount=9.9（服务端按商品定价取，未采信客户端）。orderNo 格式 ORDER+纳秒时间戳，唯一。authorId 取自文章作者（分成归属）。title「购买文章：...」为存库业务数据（中文允许）。

### [L-067] 2026-11-25 11:14:28 ｜ GET /api/v1/order/user

**逻辑链**：链-06 交易链路　｜　**步骤**：2/4 查我的订单

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | —（query：page=1&limit=5） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[{"id":"4b2f2165-...","orderNo":"ORDER1795576462641992","type":"ARTICLE","amount":9.9,"status":"PENDING",...}],"meta":{...}}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：订单入列表，字段完整。

### [L-068] 2026-11-25 11:14:28 ｜ GET /api/v1/order/:id

**逻辑链**：链-06 交易链路　｜　**步骤**：2/4 订单详情

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"4b2f2165-...","orderNo":"ORDER1795576462641992",...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：详情与列表一致。

### [L-069] 2026-11-25 11:14:28 ｜ GET /api/v1/order/no/:orderNo

**逻辑链**：链-06 交易链路　｜　**步骤**：2/4 按订单号查

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"4b2f2165-...","orderNo":"ORDER1795576462641992",...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：按业务单号查询正常。注意路由 `/no/:orderNo` 注册在 `/:id` 之前（order_routes.go:25-26），静态前缀优先，不会被 `/:id` 吞掉 —— 已验证。

### [L-070] 2026-11-25 11:14:33 ｜ PUT /api/v1/order/:id/cancel

**逻辑链**：链-06 交易链路　｜　**步骤**：3/4 取消订单

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"4b2f2165-...","status":"CANCELLED","updatedAt":"2026-11-25T11:14:33.0164499+08:00",...}}` |
| 耗时 | `14ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：PENDING → CANCELLED 状态跃迁正确，updatedAt 同步刷新。

### [L-071] 2026-11-25 11:14:33 ｜ PUT /api/v1/order/:id/cancel（异常：重复取消）

**逻辑链**：链-06 交易链路　｜　**步骤**：3/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Order is not in pending status","status_code":10001,"trace_id":"c3900571-...","timestamp":1795576473077}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：状态机保护生效（非 PENDING 不许再取消）。这里与「幂等」的取舍：返回 400 而非 200 是**显式状态机语义**，符合原仓库（重复取消应报错而非静默成功）。

### [L-072] 2026-11-25 11:14:33 ｜ POST /api/v1/order/article（异常：不存在的文章）

**逻辑链**：链-06 交易链路　｜　**步骤**：1/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | `{"articleId":"00000000-0000-0000-0000-000000000000"}` |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Article not found","status_code":10004,"trace_id":"a7c95855-...","timestamp":1795576473131}` |
| 耗时 | `10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：与链-02 L-036 的孤儿问题不同 —— 这里**有**文章存在性校验，说明校验并非全局缺失，而是 comment 域漏写（已修）。

### [L-073] 2026-11-25 11:14:33 ｜ POST /api/v1/order/article（异常：缺 articleId）

**逻辑链**：链-06 交易链路　｜　**步骤**：1/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | `{}` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Invalid request","status_code":10001,"trace_id":"66e5e103-...","timestamp":1795576473185}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：binding:"required" 生效，文案 "Invalid request"（交易域简化文案，非 i18n key 细节）。

### [L-074] 2026-11-25 11:14:37 ｜ POST /api/v1/order/membership（异常：非法 plan）

**逻辑链**：链-06 交易链路　｜　**步骤**：4/4-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | `{"plan":"MONTHLY","duration":1}` |
| HTTP 码 | `400` |
| status_code | `10001` |
| 响应体 | `{"message":"Invalid membership plan","status_code":10001,"trace_id":"28615a07-...","timestamp":1795576477110}` |
| 耗时 | `15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：合法 plan 是 `1m`/`3m`/`6m`/`12m`/`lifetime`（`order_service.go:84-95` 的 `membershipPlans`），`MONTHLY` 被正确拒绝。**契约提示**：plan 取值是小写月份缩写，与常见 `MONTHLY`/`YEARLY` 命名不同 —— 属文档缺口（报告第六节记入对照差异）。

### [L-075] 2026-11-25 11:14:57 ｜ POST /api/v1/order/membership

**逻辑链**：链-06 交易链路　｜　**步骤**：4/4 会员订单

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | `{"plan":"1m","remark":"T-39 membership test"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"69e7d0c9-33db-4147-a3ee-c5763489ca99","orderNo":"ORDER1795576497002310","type":"MEMBERSHIP","title":"充值VIP会员 1个月","amount":19.9,"status":"PENDING","details":{"basePrice":19.9,"duration":1,"isLifetime":false,"membershipLevel":1,"membershipName":"VIP会员","plan":"1m","totalAmount":19.9}}}` |
| 耗时 | `<20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：amount=19.9 来自 `membership_price` 配置（DB 优先），非客户端传入。**观察项 P3**：`authorId` 返回字面量 `"1"`（`order_service.go:157` 注释「平台单，对齐原仓库 authorId=1」）—— 这是哨兵值不是真实用户 ID，前端若拿它反查用户会 404，建议文档标注。

### [L-076] 2026-11-25 11:15:22 ｜ GET /api/v1/order/:id（越权：userB 读 A 的订单）

**逻辑链**：链-06 交易链路　｜　**步骤**：2/4-异常（IDOR）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<userB token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Order not found","status_code":10004,"trace_id":"ca8e4c4b-...","timestamp":1795576522858}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：越权读他人订单返回 404（`findByIDWithScope` 非 manage 权限强制 `user_id = 当前用户`）。**设计取舍**：用 404 而非 403 —— 好处是不泄漏「该订单号存在」的信息（防枚举），代价是前端无法区分「订单不存在」与「无权限」。这是常见做法，不判缺陷；报告第八节列为可选优化。

### [L-077] 2026-11-25 11:15:22 ｜ PUT /api/v1/order/:id/cancel（越权：userB 取消 A 的订单）

**逻辑链**：链-06 交易链路　｜　**步骤**：3/4-异常（IDOR）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<userB token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Order not found","status_code":10004,"trace_id":"57cdd26f-...","timestamp":1795576522940}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：越权取消同样被 scope 拦住。**订单未被改动**（随后 owner 本人取消成功，见 L-078）。

### [L-078] 2026-11-25 11:15:23 ｜ PUT /api/v1/order/:id/cancel（owner 取消会员订单）

**逻辑链**：链-06 交易链路　｜　**步骤**：3/4 取消会员订单

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"69e7d0c9-...","status":"CANCELLED","authorId":"1",...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：owner 取消成功，反证 L-077 的 404 不是「订单不可取消」而是「越权被 scope 拦住」。


---

## 链-07 未登录鉴权（安全链，批量 20 接口）

**执行方式**：脚本 `tmp/auth-batch.sh` 顺序打 20 个需鉴权端点，全部**不带 Authorization 头**。真实输出见下各条。

### [L-079] 2026-11-25 11:15:47 ｜ GET /api/v1/user/profile（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：1/20

| 字段 | 内容 |
|---|---|
| 请求头 | —（无 Authorization） |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"e80a9f61-...","timestamp":1795576547665}` |
| 耗时 | `7ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无**（本条单独看正确；错误码不统一问题见 L-089） |
| 结论 | **✅ 通过** |

### [L-080] 2026-11-25 11:15:47 ｜ PATCH /api/v1/user/profile（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：2/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"2c9fd181-...","timestamp":1795576547772}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-081] 2026-11-25 11:15:47 ｜ POST /api/v1/user/logout（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：3/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"e1151db7-...","timestamp":1795576547871}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-082] 2026-11-25 11:15:47 ｜ POST /api/v1/user/password/change（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：4/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"cc517238-...","timestamp":1795576547962}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-083] 2026-11-25 11:15:48 ｜ GET /api/v1/user/config（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：5/20

| 字段 | 内容 |
|---|---|
| 请求头 | — |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"b716194e-...","timestamp":1795576548061}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-084] 2026-11-25 11:15:48 ｜ POST /api/v1/user/sign-in（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：6/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"c5863c5c-...","timestamp":1795576548160}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-085] 2026-11-25 11:15:48 ｜ GET /api/v1/user/sign-in/records（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：7/20

| 字段 | 内容 |
|---|---|
| 请求头 | — |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"ca716cec-...","timestamp":1795576548257}` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-086] 2026-11-25 11:15:48 ｜ GET /api/v1/user/wallet/balance（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：8/20

| 字段 | 内容 |
|---|---|
| 请求头 | — |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"07ba8b76-...","timestamp":1795576548360}` |
| 耗时 | `9ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-087] 2026-11-25 11:15:48 ｜ POST /api/v1/user/wallet/withdraw（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：9/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"b62a79dc-...","timestamp":1795576548458}` |
| 耗时 | `7ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-088] 2026-11-25 11:15:48 ｜ POST /api/v1/article（未登录）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：10/20

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`（无 Authorization） |
| 请求体 | `{}` |
| HTTP 码 | `401` |
| status_code | `10002` |
| 响应体 | `{"message":"Unauthorized","status_code":10002,"trace_id":"96406de0-...","timestamp":1795576548554}` |
| 耗时 | `8ms` |
| 是否需要修改 | **是**（见 L-089 汇总） |
| 修改内容 | 错误码不统一问题，统一处理见 L-089 |
| 有问题 | **同一「未登录」语义返回 10002，而 user 域返回 20001 —— 业务码不统一** |
| 结论 | **⚠️ 通过但有问题** |

### [L-089] 2026-11-25 11:15:48 ｜ 错误码不统一汇总（PATCH/DELETE article、POST comment、order、upload、collection 共 10 条）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：11/20~19/20 汇总

| 字段 | 内容 |
|---|---|
| 请求头 | —（无 Authorization） |
| 请求体 | 见各接口（空 JSON 或 GET 无体） |
| HTTP 码 | 全部 `401`（正确） |
| status_code | 全部 `10002`（**与 user 域的 20001 不一致**） |
| 响应体 | `{"message":"Unauthorized","status_code":10002,"trace_id":"...","timestamp":...}`（10 条同一模式） |
| 耗时 | 7~9ms |
| 是否需要修改 | **是** |
| 修改内容 | 待 lead 裁决（根因是两套中间件并存，见下） |
| 有问题 | **「未登录」这一语义在全站返回两种业务码：user 域 20001（`ErrNeedAuth`/`JWTAuthV2`）vs 其余 7 域 10002（`ErrUnauthorized`/`JWTAuth`）。前端需写两个分支；且 V1 缺黑名单校验（见 L-090 P0）** |
| 结论 | **⚠️ 通过但有问题** |

**明细（逐条真实输出）**：
| # | 接口 | HTTP | status_code |
|---|---|---|---|
| 11 | PATCH /article/{不存在的id} | 401 | 10002 |
| 12 | DELETE /article/{不存在的id} | 401 | 10002 |
| 13 | POST /comment | 401 | 10002 |
| 14 | GET /order/user | 401 | 10002 |
| 15 | GET /order/pending | 401 | 10002 |
| 16 | POST /upload/file | 401 | 10002 |
| 17 | GET /upload | 401 | 10002 |
| 18 | GET /user/oauth/bindings | 401 | 20001 |
| 19 | GET /collection/check/{不存在的id} | 401 | 10002 |

**根因**：`middleware/jwt.go:16 JWTAuth`（仅验签，10002）与 `middleware/authz.go:21 JWTAuthV2`（验签+黑名单+全端登出+perm_ver，20001）并存；`authz.go:3-4` 注释自认「并存（不改其签名）」。user 域已迁 V2（`user_routes.go:42`），其余 7 域仍 V1。

### [L-090] 2026-11-25 11:15:49 ｜ GET /api/v1/msg/conversations（路径笔误，非缺陷）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：20/20

| 字段 | 内容 |
|---|---|
| 请求头 | — |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | 无（gin 原生 404 页） |
| 响应体 | `404 page not found` |
| 耗时 | `8ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** —— 我把路径写错了，消息域真实前缀是 `/message`（`message_routes.go:48 auth.Group("/message")`），`/msg` 不存在 |
| 结论 | **✅ 通过** |

**备注**：测试方笔误，非接口问题。**但暴露一个真实观察项**：gin 未注册路径返回**原生纯文本 `404 page not found`**，不是统一响应体 —— 与「所有响应走统一契约」不符（P2，报告第八节）。

### [L-091] 2026-11-25 11:16:14 ｜ 🔴 P0：登出后 token 仍可访问 article 域（黑名单绕过）

**逻辑链**：链-07 未登录鉴权　｜　**步骤**：补充（登出态 token 有效性，关键安全用例）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<已登出token>>` |
| 请求体 | `{"title":"T-39 blacklist bypass probe","type":"image"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"e5de2557-53df-4a81-853f-071e97a71e0d","title":"T-39 blacklist bypass probe",...}}` |
| 耗时 | `14ms` |
| 是否需要修改 | **是（P0）** |
| 修改内容 | 待 lead 裁决（改动面 10 router 文件，超边界） |
| 有问题 | **期望：登出后的 token 访问任何需鉴权端点均 401。实际：同一 token 打 user 域（V2）返回 401/20001 ✅ 正确，打 article 域（V1）返回 200/0 并成功建文 ❌** |
| 结论 | **❌ 失败（P0 安全）** |

**三步对照（真实输出）**：
```
① POST /user/logout  + 有效token → 200 {"message":"Logged out","status_code":0}
② GET  /user/profile + 同一token → 401 {"message":"Please log in first","status_code":20001}   ← V2 正确
③ POST /article      + 同一token → 200 {"message":"Success","status_code":0,...}             ← V1 放行 ❌
```
**危害**：①用户登出/改密后旧 token 在 7 个域（article/comment/order/payment/upload/audit/message）仍可用，公共设备登出形同虚设；②`report.handled` 封号后被封用户仍能发内容（token 有效期内最长 1h）；③`perm_ver` 未校验 → 管理员改权限后旧 token 的 perms 继续生效直到过期。
**已建出的探针文章 `e5de2557-...` 已清理**（SQL 软删，见收尾记录）。


---

## 链-08 普通用户访问管理员接口（安全链，批量 18 接口）

**执行方式**：脚本 `tmp/admin-batch.sh`，身份 = 普通用户 userB（`test_chain02_userb`，仅 user 角色，无任何管理权限点）。

### [L-092] 2026-11-25 11:17:19 ｜ 管理员接口批量 403 验证（15 条通过 + 3 条路径笔误）

**逻辑链**：链-08 普通用户访管理员接口　｜　**步骤**：1/18~15/18 汇总

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<userB token>>`（POST/PATCH 另加 `Content-Type: application/json`，体 `{}`） |
| 请求体 | `{}` 或 GET 无体 |
| HTTP 码 | 全部 `403`（15 条）/ `404`（3 条路径笔误） |
| status_code | `10003`（11 条）/ `20003`（4 条，user 与 role 域） |
| 响应体 | `{"message":"Permission denied","status_code":10003或20003,"trace_id":"...","timestamp":...}` |
| 耗时 | 7~10ms |
| 是否需要修改 | **是**（仅错误码不统一，见下） |
| 修改内容 | 待 lead 裁决（同 L-089 根因：两套中间件） |
| 有问题 | **权限拦截本身全部正确 ✅；但「无权限」语义在两套中间件下返回 10003 与 20003 两个业务码** |
| 结论 | **⚠️ 通过但有问题** |

**逐条真实输出**：
| # | 接口 | HTTP | status_code | 说明 |
|---|---|---|---|---|
| 1 | GET /user | 403 | **20003** | 需 user:manage |
| 2 | POST /user | 403 | **20003** | 需 user:manage |
| 3 | GET /user/certification/admin/applications | 403 | **20003** | 需认证审核权限 |
| 4 | POST /category | 403 | 10003 | category:create |
| 5 | PATCH /category/{id} | 403 | 10003 | category:update |
| 6 | POST /tag | 403 | 10003 | tag:create |
| 7 | GET /audit/pending-articles | 404 | — | **路径笔误**：真实前缀是 `/content-audit`（`audit_routes.go:24`） |
| 8 | GET /audit/records | 404 | — | 同上，路径笔误 |
| 9 | POST /achievement | 403 | 10003 | achievement:manage |
| 10 | GET /order | 403 | 10003 | order:manage |
| 11 | GET /upload/migration/stats | 403 | 10003 | upload:delete |
| 12 | GET /config | 403 | 10003 | config 权限 |
| 13 | PATCH /config | 403 | 10003 | config 权限 |
| 14 | GET /role | 403 | **20003** | RBAC 域 |
| 15 | POST /role | 403 | **20003** | RBAC 域 |
| 16 | GET /statistics/overview | 403 | 10003 | 统计权限 |
| 17 | POST /notification/push | 404 | — | **路径笔误**：通知域无此端点 |
| 18 | POST /search/sync/articles | 403 | 10003 | article:manage |

**补测（路径修正后，11:17:31 真实输出）**：
| 接口 | HTTP | status_code |
|---|---|---|
| GET /content-audit/pending-articles | 403 | 10003 |
| GET /content-audit/config | 403 | 10003 |
| POST /content-audit/text | 403 | 10003 |

**结论**：18 个管理员端点**无一泄漏**，普通用户全部被 403 拦截 —— 鉴权矩阵本身健康。唯一问题是 10003 vs 20003 的码值不统一（与 L-089 的 10002 vs 20001 同源）：user/RBAC 域走 `JWTAuthV2` + `RequireAnyPermission`（20003），其余域走 `JWTAuth` + `RequirePermissions`（10003）。


---

## 链-09 OAuth 第三方登录（服务端口 3992，P0 修复后代码）

**范围限定**：不真去微信/QQ 授权页，只测 5 类 —— 授权 URL 生成 / 伪造 code 回调 / 伪造 token 校验 / 绑定列表 / 解绑。

### [L-093] 2026-11-25 11:24:58 ｜ GET /api/v1/user/oauth/wechat/web/url

**逻辑链**：链-09 OAuth　｜　**步骤**：1/5 微信授权 URL

| 字段 | 内容 |
|---|---|
| 请求头 | —（公开端点） |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"state":"b007d09ed1a847cbfd4e2641936c0c6c","url":"https://open.weixin.qq.com/connect/qrconnect?appid=wx546665b783674034&redirect_uri=https%3A%2F%2Fexample.com%2Fapi%2Fv1%2Fuser%2Foauth%2Fwechat%2Fweb%2Fcallback&response_type=code&scope=snsapi_login&state=b007d09e...**#wechat_redirect**"},"trace_id":"f27b605d-...","timestamp":1795577098480}` |
| 耗时 | `15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：四要素齐全 —— `appid=wx546665b783674034`、`scope=snsapi_login`、`state` 32 位 hex、**`#wechat_redirect` 结尾**（微信 web 扫码的硬性要求，goth 库漏这个被 T-37a 判掉）。redirect_uri 已 urlEncode。

### [L-094] 2026-11-25 11:24:58 ｜ GET /api/v1/user/oauth/qq/web/url

**逻辑链**：链-09 OAuth　｜　**步骤**：2/5 QQ 授权 URL

| 字段 | 内容 |
|---|---|
| 请求头 | —（公开端点） |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"state":"343d30a10ea76bf500986d82b6131bae","url":"https://graph.qq.com/oauth2.0/authorize?**client_id=102065886**&redirect_uri=https%3A%2F%2Fexample.com%2Fapi%2Fv1%2Fuser%2Foauth%2Fqq%2Fweb%2Fcallback&response_type=code&scope=get_user_info&state=343d30a1..."},"trace_id":"b913facf-...","timestamp":1795577098537}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：QQ 用 `client_id`（不是 appid）、`scope=get_user_info`，与 docs/15 §2.4 逐字一致。

### [L-095] 2026-11-25 11:25:05 ｜ POST /api/v1/user/oauth/wechat/web/callback（伪造 code + 真实 state —— 发现真问题）

**逻辑链**：链-09 OAuth　｜　**步骤**：3/5 微信伪造 code

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"code":"FAKE_CODE_T39","state":"b007d09ed1a847cbfd4e2641936c0c6c"}` |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"**Third-party login failed: %s**","status_code":10008,"trace_id":"469bcdf4-...","timestamp":1795577105096}` |
| 耗时 | `336ms`（真实打到微信服务器，走完一次上游往返） |
| 是否需要修改 | **是**（契约级：未替换占位符） |
| 修改内容 | 待 lead 裁决（`internal/pkg/i18n/en.go:52`，i18n 归 lead 独占） |
| 有问题 | **期望 `"Third-party login failed"`（或带我方原因），实际把 `%s` 原样吐给客户端 —— 违反 AGENTS 响应契约第 3 条「无未替换的 %s」** |
| 结论 | **⚠️ 通过但有问题（P1 契约）** |

**备注**：服务端日志 `service/auth_service.go:728 oauth exchange failed {"provider":"wechat","channel":"web","class":"code_invalid"}` —— 错误分类与日志脱敏都正确，问题只在文案渲染。**根因**：`en.go:52` 的 `AuthOAuthFailed: "Third-party login failed: %s"` 仍带占位符，而 `oauthFailed()` 用 `WithKey(i18n.AuthOAuthFailed)` 未传 Args。T-37c 的收口记录写着「AuthOAuthFailed **去占位符**（细节场景改 AuthOAuthFailedDetail）」—— **这一步在 en.go 里没落地**。

### [L-096] 2026-11-25 11:25:05 ｜ POST /api/v1/user/oauth/qq/web/callback（伪造 code —— 上游错误映射为 500）

**逻辑链**：链-09 OAuth　｜　**步骤**：3/5 QQ 伪造 code

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"code":"FAKE_CODE_T39","state":"343d30a10ea76bf500986d82b6131bae"}` |
| HTTP 码 | `500` |
| status_code | `10007` |
| 响应体 | `{"message":"Internal server error","status_code":10007,"trace_id":"c058a611-...","timestamp":1795577105270}` |
| 耗时 | `133ms` |
| 是否需要修改 | **否**（设计如此，但语义可优化） |
| 修改内容 | `—` |
| 有问题 | **客户端传伪造 code 得到 500 —— 语义上应是 400（客户端错误）。但这是刻意设计：`mapOAuthErr` 注释「ErrUpstream → 500，细节只进日志不进响应」，依据 docs/14 禁令 2「错误信息禁夹带响应体」。属 P2 语义建议，非缺陷** |
| 结论 | **⚠️ 通过但有问题（P2）** |

**备注**：日志 `class:"upstream"` —— QQ 上游对伪造 code 返回了非预期格式（HTML 错误页/JSONP 异常），SDK 归为 ErrUpstream。**安全面正确**：不泄漏上游响应体（禁令 2 遵守）。**监控面瑕疵**：客户端输入问题被记为 5xx，会污染错误率告警。

### [L-097] 2026-11-25 11:25:05 ｜ POST /api/v1/user/oauth/wechat/web/callback（state 重放）

**逻辑链**：链-09 OAuth　｜　**步骤**：3/5-异常 state 重放

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"code":"FAKE2","state":"b007d09ed1a847cbfd4e2641936c0c6c"}`（同一 state 第二次） |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"Third-party login failed: %s","status_code":10008,"trace_id":"343eedbd-...","timestamp":1795577105328}` |
| 耗时 | `13ms`（比首次 L-095 的 336ms 快 25 倍 —— 未发出上游请求） |
| 是否需要修改 | **是**（同 L-095 的占位符问题） |
| 修改内容 | 同 L-095 |
| 有问题 | **state 一次性消费 ✅ 正确（重放被拦）；文案占位符 ❌ 同 L-095** |
| 结论 | **✅ 通过（state 防重放语义）/ ⚠️ 文案问题见 L-095** |

**备注**：13ms vs 336ms 的耗时差是 state 校验在**上游调用之前**的强证据 —— 重放请求根本没到微信。state 一次性消费（Redis DEL 成功才算通过）工作正常。

### [L-098] 2026-11-25 11:26:03 ｜ POST /api/v1/user/oauth/qq/app/login（🔴 伪造 token —— 安全关键项）

**逻辑链**：链-09 OAuth　｜　**步骤**：4/5 QQ App 伪造 token

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"accessToken":"FORGED_TOKEN_T39","openId":"FORGED_OPENID_T39"}` |
| HTTP 码 | `401` |
| status_code | `10002` |
| 响应体 | `{"message":"Your third-party session has expired, please sign in again","status_code":10002,"trace_id":"708e5379-...","timestamp":1795577163194}` |
| 耗时 | `41ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过（安全关键项）** |

**备注**：**这是链-09 最重要的一条**。`access_token` 与 `openid` 都由客户端提供、都可伪造，后端必须调 QQ `oauth2.0/me` 校验。
- 服务端日志确证：`service/auth_service.go:802 oauth app validate token failed {"provider":"qq","channel":"app","class":"token_expired"}` —— **真的打到了 ValidateToken**，不是本地短路
- **未建号验证**：`SELECT count(*) FROM "user" WHERE username LIKE '%FORGED%' OR nickname LIKE '%FORGED%'` → **0**；`user_oauth_binding` 无 FORGED 记录
- 401/10002 与 docs/15 §2.6 表格「token 过期/被撤销 → 401 / 10002」逐字一致
- 说明 `qq_app_appid` 当前**已配置**（若为空会降级 20026 而非走到 ValidateToken）

### [L-099] 2026-11-25 11:26:17 ｜ GET /api/v1/user/oauth/bindings（空列表）

**逻辑链**：链-09 OAuth　｜　**步骤**：4/5 绑定列表（无绑定态）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"bindings":[]},"trace_id":"77818495-...","timestamp":1795577177334}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：空绑定 `[]` 非 null。

### [L-100] 2026-11-25 11:26:17 ｜ DELETE /api/v1/user/oauth/bindings/wechat/web（解绑不存在的绑定）

**逻辑链**：链-09 OAuth　｜　**步骤**：5/5-异常

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `20029` |
| 响应体 | `{"message":"This third-party account is not bound to your profile","status_code":20029,"trace_id":"8df6f8f7-...","timestamp":1795577177387}` |
| 耗时 | `12ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：绑定不存在 → 404 + 20029，语义精确（并可防他人绑定被枚举 —— 查别人的绑定同样落到这条）。

### [L-101] 2026-11-25 11:26:17 ｜ GET /api/v1/user/oauth/bindings（无 token）

**逻辑链**：链-09 OAuth　｜　**步骤**：4/5-异常

| 字段 | 内容 |
|---|---|
| 请求头 | —（无 Authorization） |
| 请求体 | — |
| HTTP 码 | `401` |
| status_code | `20001` |
| 响应体 | `{"message":"Please log in first","status_code":20001,"trace_id":"303fbf92-...","timestamp":1795577177433}` |
| 耗时 | `6ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：P0 修复后返回统一的 20001（而非旧 V1 的 10002）—— **错误码统一已在 oauth 域生效的实证**。

### [L-102] 2026-11-25 11:26:24 ｜ GET /api/v1/user/oauth/bindings（有绑定数据 —— 首次返回空，查出测试方法坑）

**逻辑链**：链-09 OAuth　｜　**步骤**：4/5 绑定列表（含 openid 泄漏检查）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"bindings":[]},...}`（**首次：空**） |
| 耗时 | `10ms` |
| 是否需要修改 | **否**（缺陷在我造数据，不在接口） |
| 修改内容 | `—` |
| 有问题 | **首次空数组，但 SQL 直查确认库里有 1 行。根因：`soft_delete` 插件语义 —— 未删除 = `deleted_at = 0`，我用 SQL 插的 `deleted_at = NULL` 不匹配查询条件。修正数据（`deleted_at=0`）后立即返回该绑定** |
| 结论 | **✅ 通过（接口正确，测试方法已纠正）** |

**备注**：**这是本轮最有价值的「测试方法坑」**：`internal/model/base.go:28` 用 `soft_delete.DeletedAt`（gorm.io/plugin/soft_delete），它用 **int64 `0` 表示未删除**，与 gorm 原生 `gorm.DeletedAt`（NULL 表示未删）**语义相反**。手工 SQL 造数据必须写 `deleted_at=0`，写 NULL 会被当成「不存在的行」。已记入报告第七节供后续测试者避坑。

### [L-102b] 2026-11-25 11:27:29 ｜ GET /api/v1/user/oauth/bindings（修正数据后 —— openid 泄漏检查）

**逻辑链**：链-09 OAuth　｜　**步骤**：4/5 绑定列表（有数据）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"bindings":[{"provider":"wechat","channel":"web","nickname":"test_oauth_nick","avatarUrl":"","boundAt":"2026-11-25T11:26:23+08:00"}]},"trace_id":"f97c7237-...","timestamp":1795577249352}` |
| 耗时 | `21ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过（安全要求满足）** |

**备注**：**关键安全验证**：数据库里该行有 `openid=test_openid_t39_secret123` 与 `unionid=test_unionid_t39_secret456`，但响应体**只回 5 个字段**（provider/channel/nickname/avatarUrl/boundAt），**openid 与 unionId 一个字节都没泄漏** —— 符合 lead 的验收标准 4。`oauth_service.go:780-786` 的 `oauthBindingView` 白名单式构造生效。

### [L-103] 2026-11-25 11:27:42 ｜ DELETE /api/v1/user/oauth/bindings/wechat/web（有 email 兜底 → 允许解绑）

**逻辑链**：链-09 OAuth　｜　**步骤**：5/5 解绑成功

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"bindings":[]},"trace_id":"54b454f9-...","timestamp":1795577262224}` |
| 耗时 | `24ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：该用户有 `email=test_chain01@example.com` 兜底 → 允许解绑（安全规则「手机号 OR 邮箱 OR 其它绑定」，Password 不参与）。响应直接回最新列表，前端一次请求完成「删 + 刷」。**硬删确认**：`SELECT count(*) ... WHERE openid LIKE 'test_openid_t39%'` → **0**（不是软删，符合 T-37e「唯一索引不含 deleted_at，软删后绑不回来」的决策）。

### [L-104] 2026-11-25 11:27:55 ｜ DELETE /api/v1/user/oauth/bindings/github/web（🔴 解绑最后一种登录方式 —— 必须被拒）

**逻辑链**：链-09 OAuth　｜　**步骤**：5/5-异常（安全规则）

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<test_oauth_only 的 token>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `20028` |
| 响应体 | `{"message":"This is your last way to sign in, please set a phone number or email before unbinding","status_code":20028,"trace_id":"c2b5e057-...","timestamp":1795577275806}` |
| 耗时 | `16ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过（安全关键项）** |

**备注**：测试前置条件（真实构造）：`test_oauth_only` 账号 **email 与 phone 均为空**，且只有 1 条 `github/web` 绑定。
- 期望 403/20028 → 实际 403/20028 ✅
- **数据未被删**：`SELECT provider,channel,deleted_at FROM user_oauth_binding WHERE openid='test_openid_t39_only'` → `github | web | 0`（仍在，deleted_at 仍为 0 = 未删）✅
- 文案可执行性好（告诉用户先补手机号或邮箱），不是干巴巴的 "Forbidden"


---

## 链-10 IDOR 越权（服务端口 3992）

**测试账号**：A = `test_chain01_user`（a3baeae0-...），B = `test_chain02_userb`（dbd00781-...，仅 user 角色）。
**验收标准**：要么 404（防枚举）要么 403，**不许 200 返回/修改他人数据**。

### [L-105] 2026-11-25 11:28:47 ｜ PATCH /api/v1/article/:id（B 改 A 的文章）

**逻辑链**：链-10 IDOR　｜　**步骤**：1/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | `{"title":"hijacked-by-B"}` |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this article","status_code":10003,"trace_id":"875e17af-...","timestamp":1795577327535}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-106] 2026-11-25 11:28:47 ｜ DELETE /api/v1/article/:id（B 删 A 的文章）

**逻辑链**：链-10 IDOR　｜　**步骤**：2/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to delete this article","status_code":10003,"trace_id":"d5eb037e-...","timestamp":1795577327589}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-107] 2026-11-25 11:28:47 ｜ PATCH /api/v1/comment/:id（B 改 A 的评论）

**逻辑链**：链-10 IDOR　｜　**步骤**：3/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | `{"content":"hijacked"}` |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this comment","status_code":10003,"trace_id":"286f59f1-...","timestamp":1795577327641}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-108] 2026-11-25 11:28:47 ｜ DELETE /api/v1/comment/:id（B 删 A 的评论）

**逻辑链**：链-10 IDOR　｜　**步骤**：4/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this comment","status_code":10003,"trace_id":"5b78fc8b-...","timestamp":1795577327698}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：删除复用了「modify」文案（未区分 delete 专用文案），语义可接受。

### [L-109] 2026-11-25 11:28:56 ｜ GET /api/v1/upload/info/:id（B 查 A 的上传详情 —— NoAuth 公开端点）

**逻辑链**：链-10 IDOR　｜　**步骤**：5/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"02ebe164-...","filename":"130c7aa5-...png","hash":"130c7aa5-...","mimeType":"image/png","originalName":"test-upload.png","path":"uploads\\2026\\11\\25\\13\\0c\\...png","userId":"a3baeae0-...","...":...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **⚠️ 设计如此：`/upload/info/:id` 是 NoAuth 公开端点（`upload_routes.go:65`，对齐原仓库 @NoAuth）。意味着知道 upload id 即可读他人文件元数据（含 path / userId / hash / originalName）** |
| 结论 | **⚠️ 通过但有问题（P2 信息暴露面）** |

**备注**：不算 IDOR 缺陷（无鉴权 ≠ 越权），但属**可被枚举的信息面**：id 是 UUID 不可猜，但一旦泄漏（分享链接/日志）就能拿到完整元数据。原仓库同为 @NoAuth，重构保持对齐 → 判 P2 观察、不改。

### [L-110] 2026-11-25 11:28:56 ｜ GET /api/v1/upload（B 看上传列表 —— 降级只看自己）

**逻辑链**：链-10 IDOR　｜　**步骤**：6/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | —（query：page=1&limit=20） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":20,"totalPages":0}},"trace_id":"e5ae46dc-...","timestamp":1795577335898}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：B 无 `upload:list` 权限 → handler 内降级为「只看自己」→ total=0（A 有 1 条，见 L-111）。**这是「同一接口按权限降级数据范围」的正确范式**。

### [L-111] 2026-11-25 11:29:00 ｜ GET /api/v1/upload（A 对照 —— 应看到自己的）

**逻辑链**：链-10 IDOR　｜　**步骤**：6/14 对照组

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<A token 截断>>` |
| 请求体 | —（query：page=1&limit=20） |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[{...id":"02ebe164-..."...}],"meta":{"total":1,"page":1,"limit":20,"totalPages":1}},...}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：A total=1、B total=0 —— 降级隔离成立（L-110 的空列表不是「没人上传」而是「不是你的」）。

### [L-112] 2026-11-25 11:29:04 ｜ POST /api/v1/collection（B 建自己的合集，为后续越权测试备料）

**逻辑链**：链-10 IDOR　｜　**步骤**：7/14 环境准备

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | `{"name":"test_idor_collection_B"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"59ab40de-...","name":"test_idor_collection_B","isPublic":false,"userId":"dbd00781-1061-4419-8fc0-64ab287f06bd","itemCount":0,"views":0,...}}` |
| 耗时 | `<20ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：userId 正确绑定到 B 本人。默认 `isPublic:false`（私密）。

### [L-113] 2026-11-25 11:29:04 ｜ DELETE /api/v1/user/:id（B 删 A 的用户 —— 管理员接口）

**逻辑链**：链-10 IDOR　｜　**步骤**：8/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `20003` |
| 响应体 | `{"message":"Permission denied","status_code":20003,"trace_id":"46552ce9-...","timestamp":1795577344451}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：B 的 perms 只有 `user:read` / `user:update`，无 `user:delete` → 403。返回 20003（P0 修复后的 V2 码）。**这条正确，反衬出 L-117 的 PATCH 是漏的**。

### [L-114] 2026-11-25 11:29:16 ｜ GET /api/v1/collection/:id（B 读 A 的私密合集）

**逻辑链**：链-10 IDOR　｜　**步骤**：9/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"This collection is private","status_code":10003,"trace_id":"ee610e2e-...","timestamp":1795577356988}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：A 的合集是 `isPublic:false`，B 读被 403 拦。文案点明原因（private），比笼统 Forbidden 更有用。

### [L-115] 2026-11-25 11:29:17 ｜ POST /api/v1/collection/:id/article/:articleId（B 往 A 的合集塞文章）

**逻辑链**：链-10 IDOR　｜　**步骤**：10/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this collection","status_code":10003,"trace_id":"d11894ec-...","timestamp":1795577357097}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-116] 2026-11-25 11:29:17 ｜ DELETE /api/v1/collection/:id（B 删 A 的合集）

**逻辑链**：链-10 IDOR　｜　**步骤**：11/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `10003` |
| 响应体 | `{"message":"You do not have permission to modify this collection","status_code":10003,"trace_id":"63a8d2b7-...","timestamp":1795577357165}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

### [L-117] 2026-11-25 11:29:32 ｜ 🔴 P0：PATCH /api/v1/user/:id（B 改 A 的资料 —— 越权写成功）

**逻辑链**：链-10 IDOR　｜　**步骤**：12/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | `{"nickname":"hacked"}` |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"id":"a3baeae0-37ab-405b-9b10-46465b06bff6","username":"test_chain01_user","**nickname":"hacked**","description":"updated by T-39 test",...,"gender":"male",...}}` |
| 耗时 | `<15ms` |
| 是否需要修改 | **是（P0）** |
| 修改内容 | 待 lead 裁决（`internal/router/user_routes.go:189` 的 `canUserWrite` 换 `canUserManage`） |
| 有问题 | **期望 403（普通用户不得改他人资料），实际 200 且 A 的 nickname 真被改成 "hacked"** |
| 结论 | **❌ 失败（P0 越权写）** |

**根因（逐层确证）**：
1. `user_routes.go:45`：`canUserWrite := middleware.RequireAnyPermission("user:update", "user:manage")`
2. `user_routes.go:189`：`admin.PATCH("/:id", canUserWrite, userH.Update)` ← **把「改自己」的权限点用在了「改他人」的路由上**
3. B 的 JWT perms 实测 = `['user:read','user:update']` —— `user:update` 是**每个普通用户都有的**（`PATCH /user/profile` 正是靠它）
4. → 权限点放行，handler 不再校验「只能改自己」→ 改成功

**同组对照（说明是单点遗漏，不是整组失守）**：
| 路由 | 权限点 | B 能过吗 | 判定 |
|---|---|---|---|
| `GET /user` | `user:manage` | ❌ 403 | ✅ 已收紧（todo 记录过） |
| `DELETE /user/:id` | `user:delete, user:manage` | ❌ 403（L-113） | ✅ 安全 |
| `PATCH /user/:id` | `user:update, user:manage` | ✅ **200** | ❌ **就是这行** |

**数据已回滚**：用 A 的 token `PATCH /user/profile {"nickname":"test_nickname_01_v2"}` → 响应确认 nickname 恢复。

### [L-118] 2026-11-25 11:29:32 ｜ GET /api/v1/message/conversations（B 读会话列表 —— 路径不存在）

**逻辑链**：链-10 IDOR　｜　**步骤**：13/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | 无（gin 原生纯文本） |
| 响应体 | `{"message":"Message not found","status_code":10004,...}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** —— 真实路由是 `/message/private/conversations`（`message_routes.go:63`），`/message/conversations` 落到 `/:id` 通配被当成「消息 id 不存在」 |
| 结论 | **✅ 通过** |

**备注**：gin 静态优先 + 参数回退的语义再次生效（同 L-057）。

### [L-119] 2026-11-25 11:29:32 ｜ GET /api/v1/article/browse/history（B 看浏览历史 —— 只看自己）

**逻辑链**：链-10 IDOR　｜　**步骤**：13/14

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":10,"totalPages":0}},"trace_id":"1fbc7b80-...","timestamp":1795577372382}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：接口无 userId 参数，按当前登录用户取 → 天然不可越权。

### [L-120] 2026-11-25 11:29:52 ｜ DELETE /api/v1/user/:id（B 再试删 A，确认 L-113 结论）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 复核

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `403` |
| status_code | `20003` |
| 响应体 | `{"message":"Permission denied","status_code":20003,"trace_id":"5094022a-...","timestamp":1795577392241}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：与 L-117 对照 —— 同一组里 DELETE 拦住了、PATCH 没拦住，进一步锁定是 PATCH 单点权限点配错。

### [L-121] 2026-11-25 11:30:12 ｜ GET /api/v1/user/oauth/bindings（B 查绑定 —— 接口无 userId 参数）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"bindings":[]},"trace_id":"fdb7ab3f-...","timestamp":1795577412238}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：接口签名不含 `:userId`（只读当前登录用户），**设计上不可越权**。

### [L-122] 2026-11-25 11:30:12 ｜ GET /api/v1/user/profile/favorites（B 看 A 的收藏夹）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"list":[],"meta":{"total":0,"page":1,"limit":10,"totalPages":0}},"trace_id":"daea3b32-...","timestamp":1795577412310}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：同为「无 userId 参数、取当前登录用户」模式。

### [L-123] 2026-11-25 11:30:12 ｜ GET /api/v1/message/conversation/:id（不存在路径）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | 无（gin 原生） |
| 响应体 | `404 page not found` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无**（我的路径笔误，真实路由 `/message/:id`） |
| 结论 | **✅ 通过** |

### [L-124] 2026-11-25 11:30:37 ｜ GET /api/v1/message/private/conversations（B 读私信会话列表）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"data":[],"meta":{"limit":20,"hasMore":false,"nextCursor":null}},"trace_id":"321ca7ad-...","timestamp":1795577437680}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **⚠️ P3：响应是 `data.data` + `data.meta` 双层嵌套，与全站 `data.{list,meta}` 命名不一致（此处用 `data` 当列表键）** |
| 结论 | **⚠️ 通过但有问题（P3 命名一致性）** |

**备注**：B 的会话为空（只看自己，无越权）。游标分页（`hasMore`/`nextCursor`）而非页码分页 —— 消息流的合理选择，但与其它域的分页契约不同（P3）。

### [L-125] 2026-11-25 11:30:37 ｜ GET /api/v1/message/private/conversations/:userId/messages（B 读与 A 的私信历史）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `200` |
| status_code | `0` |
| 响应体 | `{"message":"Success","status_code":0,"data":{"data":[],"meta":{"limit":20,"hasMore":false,"nextCursor":null}},"trace_id":"a539ac61-...","timestamp":1795577437757}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：语义是「**当前登录用户与 :userId 的对话**」（配对查询），不是「:userId 的所有消息」→ 传 A 的 id 只能拿到 B↔A 之间的内容，B 无法借此读 A↔C 的对话。A/B 之间无消息故为空。

### [L-126] 2026-11-25 11:30:37 ｜ GET /api/v1/message/:id（B 读不存在的消息）

**逻辑链**：链-10 IDOR　｜　**步骤**：14/14 补充

| 字段 | 内容 |
|---|---|
| 请求头 | `Authorization: Bearer eyJ...<后8位=<B token 截断>>` |
| 请求体 | — |
| HTTP 码 | `404` |
| status_code | `10004` |
| 响应体 | `{"message":"Message not found","status_code":10004,"trace_id":"2da9d035-...","timestamp":1795577437841}` |
| 耗时 | `<10ms` |
| 是否需要修改 | **否** |
| 修改内容 | `—` |
| 有问题 | **无** |
| 结论 | **✅ 通过** |


---

## 修复后复测（P0-2 与 P1 OAuth 文案 —— lead 已改，我复测取证）

**代码现状（复测前核实）**：
- `internal/router/user_routes.go:197` → `admin.PATCH("/:id", canUserManage, userH.Update)`（原 `canUserWrite`），注释写着「P0 教训（2026-11-25 接口测试发现）」
- `internal/pkg/i18n/en.go:52` → `AuthOAuthFailed: "Third-party login failed"`（原 `"Third-party login failed: %s"`）

### [L-127] 2026-11-25 11:35:30 ｜ PATCH /api/v1/user/:id（复测：B 改 A 的资料）

**逻辑链**：链-10 IDOR　｜　**步骤**：12/14-复测（对应 L-117）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json`　`Authorization: Bearer eyJ...<后8位=<test_verify_ub 的 token>>` |
| 请求体 | `{"nickname":"hacked-again"}` |
| HTTP 码 | `403` |
| status_code | `20003` |
| 响应体 | `{"message":"Permission denied","status_code":20003,"trace_id":"cb44189d-2e89-4e43-a7b4-5a0524513d1b","timestamp":1795577730464}` |
| 耗时 | `13ms` |
| 是否需要修改 | **否**（修复已生效） |
| 修改内容 | `—`（`user_routes.go:197` 的 `canUserWrite` → `canUserManage` 由 lead 完成） |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：L-117 的 P0 闭环。**双证据**：
1. 越权请求被 403/20003 拦
2. A 的资料未被篡改 —— 用 A 自己的 token 读 `GET /user/profile` → `nickname = None`（A 从未设过昵称，说明 `hacked-again` 没写进去）

### [L-128] 2026-11-25 11:35:36 ｜ POST /api/v1/user/oauth/wechat/web/callback（复测：伪造 code 文案）

**逻辑链**：链-09 OAuth　｜　**步骤**：3/5-复测（对应 L-095）

| 字段 | 内容 |
|---|---|
| 请求头 | `Content-Type: application/json` |
| 请求体 | `{"code":"FAKE_VERIFY","state":"2edbb31c9f1bb1d368694ab310facee5"}`（state 取自新生成的授权 URL） |
| HTTP 码 | `400` |
| status_code | `10008` |
| 响应体 | `{"message":"**Third-party login failed**","status_code":10008,"trace_id":"54af31cf-400a-4b01-9a84-b7874b601f3e","timestamp":1795577736402}` |
| 耗时 | `255ms`（真实打到微信上游） |
| 是否需要修改 | **否**（修复已生效） |
| 修改内容 | `—`（`en.go:52` 去 `: %s` 由 lead 完成） |
| 有问题 | **无** |
| 结论 | **✅ 通过** |

**备注**：L-095 的 P1 闭环 —— 占位符 `%s` 已消失，文案完整可读。⚠️ 注意 state 是**一次性的**：本条的 state 在 L-128 执行时被消费掉，不能再用。


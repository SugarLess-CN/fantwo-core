# Apifox 接口数据完善计划（对照电商项目范式）

> 日期：2026-11-24
> 输入：`docs/swagger/swagger.json`（237 paths / 309 operations，已转 OAS3 `tmp/swagger-oas3.json`）
> 范式来源：apifox-office（电商项目，**只读参考文档格式**，不取其业务数据）

---

## 一、电商项目的接口文档范式（学什么）

从 `/paths/_auth_login.json`、`/paths/_profile.json`、`/paths/_admin_users.json` 提炼：

### 1. 每个 operation 必须有

| 要素 | 范式示例 |
|---|---|
| `summary` | 「登录」（简短中文动词短语） |
| `operationId` | `post_auth_login`（method_模块_动作，snake_case） |
| `tags` | `["认证与账户"]`（按业务域分组的中文标签） |
| `description` | 接口行为说明（可为空，但复杂接口必须写） |
| `deprecated` | `false`（显式声明） |

### 2. 请求参数 / requestBody

| 要素 | 范式示例 |
|---|---|
| 每个 field 有 `description` | `"用户名（管理员/密码登录账号）"` |
| 每个 requestBody 有 `example` | `{"username":"admin","password":"Admin@123"}` |
| `required` 数组显式列出 | `["username","password"]` |
| 字段约束写在 description | `"密码（min=6）"` |

### 3. 响应（重中之重）

| 要素 | 范式示例 |
|---|---|
| **多个状态码** | 200 / 400 / 401 / 403 / 409 各一条，每条带 description（含业务码） |
| 响应 schema 完整 | 每个 field 有 type + description |
| **顶层响应 example** | 完整 JSON 示例，含真实感的模拟数据 |
| data 内结构化 | 嵌套对象展开写，不是空 `{}` |
| 业务码写进 description | `"参数校验失败（1002）"` / `"令牌失效（2001）"` |

### 4. 响应 example 的范式（电商项目的标杆）

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiJ9.access",
    "refresh_token": "eyJhbGciOiJIUzI1NiJ9.refresh",
    "token_type": "Bearer",
    "expires_in": 7200,
    "user": {
      "uid": "1",
      "username": "admin",
      "nickname": "超级管理员",
      "email": "sample@pinhoop.com",
      "phone": "138****5555",
      "avatar": "",
      "status": 1,
      "is_super": true,
      "created_at": "2026-10-27 13:32:26",
      "role": "SUPER_ADMIN",
      "roles": ["SUPER_ADMIN"]
    }
  },
  "trace_id": "8fa6e149-...",
  "timestamp": 1793598718641
}
```

特点：**数据是「看起来真实」的模拟值**（有长度的 token、脱敏手机号、具体日期时间），不是空串/0/null。

### 5. security

- 需登录的接口：`"security": [{"登录鉴权": []}]`
- 公开接口：`"security": []`

---

## 二、fantwo-core 的响应契约（与电商项目不同，写文档时用我们的）

```json
{"data": {...}, "message": "Success", "status_code": 0}
```

- `status_code`：0=成功；10001 参数 / 10002 未登录 / 10003 无权限 / 10004 不存在 / 10005 冲突 / 10006 限流 / 10007 内部；20000+ 用户域；70000+ RBAC
- data/message 为空时**整个字段省略**
- message 一律英文（i18n）
- **无 trace_id / timestamp**（我们的契约没有这两个字段）
- health 端点例外：只返回 `{"status_code":0}`

---

## 三、要修改的接口清单（309 个 operation）

### 分层策略（按优先级）

| 优先级 | 范围 | 数量 | 说明 |
|---|---|---|---|
| P0 | 高频核心域：auth / user / article / comment / config | ~85 | 前端最常用，先做精 |
| P1 | 互动与运营：interaction / collection / points / achievement / decoration | ~70 | 次高频 |
| P2 | 交易与消息：order / payment / message / upload | ~65 | 涉及资金与文件，需要精确错误码 |
| P3 | 其余：search / role / report / invite / category / tag / banner / emoji / stats | ~89 | 补全覆盖 |

### 每个 operation 要补的内容（对照范式差距）

| 项目 | 现状 | 目标 |
|---|---|---|
| `operationId` | 中文（如「成就列表」） | `get_achievement_list` 英文 snake_case |
| `tags` | ✅ 已有（中文域标签） | 保持 |
| `description` | ❌ 大多缺失 | 补接口行为说明 |
| `deprecated` | ❌ 缺失 | 补 `false` |
| 参数 `description` | ❌ 大多缺失 | 每个 query/path 参数补说明 |
| `requestBody.example` | ❌ 缺失 | 补真实感模拟数据 |
| 200 响应 `example` | ❌ 缺失 | 补完整响应示例（含 data 真实模拟值） |
| 4xx/5xx 响应 | ❌ 大多只有 200 | 补 400/401/403/404/409 等，每条带 example + 业务码 |
| `security` | ❌ 缺失 | 按接口权限补 `登录鉴权` 或 `[]` |

---

## 四、导入方式

- **受 MCP 限制**：每次 ≤1 operation；schema 先行；$ref 指向已导入 schema
- **批次文件**：`tmp/batches/o-001.json ~ o-309.json` 已生成（单 operation），但缺 example/多状态码——需重新生成增强版
- **增强版生成**：从 `docs/swagger/swagger.json` 提取结构 → 按范式补 example/多状态码 → 重新生成批次 → 逐个导入

---

## 五、执行计划

1. **先做 3 个样板接口**（auth login / user profile / article list），导入后到 Apifox 确认格式符合预期
2. 确认范式无误后，批量生成剩余接口的增强版批次
3. 按优先级 P0 → P1 → P2 → P3 逐个导入
4. 每完成一个域，用只读 MCP 核对

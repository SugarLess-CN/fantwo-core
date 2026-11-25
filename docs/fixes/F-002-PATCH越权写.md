# F-002 PATCH /user/:id 越权写（普通用户可改任何人）

| 项 | 内容 |
|---|---|
| 发现时间 | 2026-11-25 11:29 |
| 发现方式 | 接口测试（链-10 IDOR 越权） |
| 严重度 | **P0**（越权写 + 可自我提权） |
| 影响面 | 全部用户；可改 `status`/`wallet`/`roleIds` |
| 状态 | ✅ 已修复并复测 |
| 相关提交 | `9719919` |
| 相关文档 | `docs/19` §八（常见误用）、`reports/11` §三 |

## 一、现象

```
用户 B（普通 user 角色，JWT perms = ['user:read','user:update']）
PATCH /api/v1/user/{用户A的id}  {"nickname":"hacked"}
→ HTTP 200  {"status_code":0,"data":{"id":"<A>","nickname":"hacked",...}}
```

A 的 nickname 真的被改了。

## 二、复现步骤

```bash
# 造两个账号：A 普通用户，B 普通用户
TB=$(B 的 token)
curl -X PATCH .../user/{A.id} -H "Authorization: Bearer $TB" \
  -H "Content-Type: application/json" -d '{"nickname":"hacked"}'
# 改前：200 且真改成功
```

## 三、根因

```go
canUserWrite := middleware.RequireAnyPermission("user:update", "user:manage")
admin.PATCH("/:id", canUserWrite, userH.Update)   // ← 这里
```

**`user:update` 是「改自己资料」的权限点** —— `PATCH /user/profile` 用的就是它，
普通用户人人都有。挂在「管理员改他人」的 `PATCH /user/:id` 上 → 任何登录用户能改任何人。

同组其它路由的收紧口径都是对的，只有 PATCH 这行漏了：

| 路由 | 权限 | 普通用户 |
|---|---|---|
| `GET /user` | `user:manage` | 403 ✅ |
| `DELETE /user/:id` | `user:delete, user:manage` | 403 ✅ |
| `GET /user/:id` | `user:read, user:manage` | 200（公开主页，合理） |
| **`PATCH /user/:id`** | **`user:update, user:manage`** | **200 改成功 ❌** |

**危害远不止改昵称** —— `UserService.UpdateUser` 的字段白名单包含：

| 字段 | 后果 |
|---|---|
| `roleIds` | **给自己加管理员角色 → 提权** |
| `status` | **封禁/解封任何人** |
| `wallet` | **直接改钱包余额** |
| nickname/avatar/description/… | 篡改他人资料 |

这是「从普通用户到管理员」的完整提权链。

## 四、修复方案

```go
// 改前
admin.PATCH("/:id", canUserWrite, userH.Update)
// 改后
admin.PATCH("/:id", canUserManage, userH.Update)
```

同时删掉 `canUserWrite` 变量（只此一处使用，留着编译失败），
并在原处写清「`user:update` 不可用于改他人路由」的警示注释 —— 防后人再犯。

## 五、验证

```
① B 改 A 的资料：PATCH /user/{A.id} {"nickname":"hacked"}
   → HTTP 403 / 20003 "Permission denied"     （改前 200 且真改成功）
② B 给自己提权：PATCH /user/{B.id} {"roleIds":["admin"]}
   → HTTP 403                                  （改前会成功）
③ ★ 反向验证：B 改自己走 profile 接口
   PATCH /user/profile {"nickname":"self_ok2"} → HTTP 200 / 0   ✅ 未误伤
④ ★ 反向验证：管理员改他人
   admin PATCH /user/{user.id} → HTTP 200      ✅ 管理员没被拦
```

第 ③④ 条是必须的反向验证 —— 收紧权限最容易的副作用是把正主（本人、管理员）也拦掉。

## 六、影响与风险

**Breaking change**：无（普通用户本来就不该调这个接口）。

**同类审计**：全仓 `user:update` 只有这一处使用（已 `grep` 确认）。
其它域的 `xxx:update` 权限点是否也挂在「改他人」的路由上，需逐域复查 —— 已记 `todo.md`。

**复发预防**：权限点命名应能自解释。`user:update` 语义模糊（改自己？改他人？），
建议未来改成 `user:self:update` / `user:manage` 二分，让误用一眼可见。

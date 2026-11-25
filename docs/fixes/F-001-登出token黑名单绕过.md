# F-001 登出 token 黑名单绕过

| 项 | 内容 |
|---|---|
| 发现时间 | 2026-11-25 11:16 |
| 发现方式 | 接口测试（链-07 未登录鉴权，批量打点中发现） |
| 严重度 | **P0**（登出失效 + 封禁绕过） |
| 影响面 | 除 user 域外的 7 个业务域（article/comment/order/upload/payment/audit/message） |
| 状态 | ✅ 已修复并复测 |
| 相关提交 | `27f06e8` |
| 相关文档 | `docs/19` §四（20001）、`reports/11` §三 |

## 一、现象

```
1) POST /user/logout   + 有效 token → 200 Logged out
2) GET  /user/profile  + 同一 token → 401 / 20001   ← V2 中间件拦住
3) POST /article       + 同一 token → 200 / 0 成功  ← V1 放行，真建出了文章
```

## 二、复现步骤

```bash
T=$(登录拿 token)
curl -X POST .../user/logout -H "Authorization: Bearer $T" -H "Device-Id: x"
curl -X POST .../article -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d '{"title":"p0-check","content":"x","images":[]}'
# 改前：200 / status_code=0（文章真建出来了）
```

## 三、根因

两套鉴权中间件并存，各域用了不同版本：

| 中间件 | 校验内容 | 未登录返回 |
|---|---|---|
| `JWTAuth`（旧，`middleware/jwt.go:16`） | 只验签 | 10002 |
| `JWTAuthV2`（新，`middleware/authz.go:21`） | 验签 + 黑名单 + 全端登出时间 + perm_ver | 20001 |

只有 `user_routes.go` 用了 V2，其余 7 个 router 文件仍是 V1。
`authz.go` 的注释自己写着「需要黑名单与 perm_ver 校验的路由组用 JWTAuthV2」——
是已知技术债，但一直没迁完。

**危害**：
1. 登出形同虚设 —— 公共设备登出后旧 token 在 7 个域继续可用
2. 封禁绕过 —— `report.handled` 封号后，被封用户仍能发内容（token 有效期 1h 内）
3. 权限变更延迟 —— V1 不查 perm_ver，管理员改权限后旧 perms 继续有效

## 四、修复方案

10 处调用迁移到 V2（8 个文件）：

```go
// 改前
middleware.JWTAuth(opt.Signer)
// 改后
middleware.JWTAuthV2(opt.Signer, authtoken.NewRedisStore(opt.Redis))
```

**同时堵掉一个「修 P0 会引入的新 P0」**：

`authtoken.PermVer()` 在 Redis 出错时返回 `0`，而中间件原本写 `if cur != claims.PermVer`
→ 若用户 token 的 `PermVer` 是 1，Redis 挂时 `0 != 1` → **20002 → 该用户全站不可用**。
而同一中间件里 `Blacklisted` / `LogoutAllAt` 在 err 时都是 fail-open，降级方向矛盾。

改（`authz.go:133`）：

```go
// perm_ver 单调递增，0 只可能是「未设置」或「查询失败」；此时跳过比对（与黑名单同向 fail-open）
if cur := authtoken.PermVer(ctx, st, claims.UserID); cur != 0 && cur != claims.PermVer {
    return nil, "", errcode.ErrPermVersionStale
}
```

**为什么 cur==0 可以安全跳过**：真实的权限变更必然伴随 bump，key 一定存在且 > 0；
0 只出现在「从未 bump 过」（此时 token 里也是 0）或「Redis 故障」。两种情况都不该拒绝请求。

## 五、验证

```
① 登出后需登录接口全 401/20001：
   POST /article → 401   POST /comment → 401
   PATCH /user/profile → 401   POST /order/membership → 401
② 未登出正常访问：5 个域全 200（没一刀切）
③ 公开接口仍公开：GET /article → 200
④ 错误码统一：无 token 访问 5 个域全 20001（无 10002）
⑤ Redis 故障降级：docker stop redis → 登录成功 + 5 域全 200，**无 20002**
⑥ 垃圾数据：articles WHERE title LIKE 'p0-%' = 0 条

go build OK / go vet 零输出 / go test 16 包全绿
```

## 六、影响与风险

**Breaking change**：全站「未登录」统一为 `20001`（原 user 域 20001、其余 10002）。
前端需把 `if (code === 10002)` 改成 `if (code === 10002 || code === 20001)`。

**同类未修**：无（10 处全部迁完，`grep JWTAuth(` 已 0 命中）。

**复发预防**：新增 handler 一律用 `JWTAuthV2`；`JWTAuth` 只剩 `OptionalAuth` 场景在用。

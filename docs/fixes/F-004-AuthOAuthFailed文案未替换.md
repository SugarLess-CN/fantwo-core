# F-004 AuthOAuthFailed 文案带未替换的 %s

| 项 | 内容 |
|---|---|
| 发现时间 | 2026-11-25 11:50 |
| 发现方式 | 接口测试（链-09 OAuth，微信回调伪造 code） |
| 严重度 | P1（用户可见文案错） |
| 影响面 | 微信/QQ 回调失败、state 重放 |
| 状态 | ✅ 已修复并复测 |
| 相关提交 | `d18c513` 同批 |
| 相关文档 | `docs/19` §二（响应体形状） |

## 一、现象

```
POST /api/v1/user/oauth/wechat/web/callback  {"code":"fake","state":"..."}
→ HTTP 400  {"message":"Third-party login failed: %s","status_code":10008}
```

用户看到字面的 `%s`。

## 二、复现步骤

```bash
curl -s -X POST .../user/oauth/wechat/web/callback \
  -H "Content-Type: application/json" \
  -d '{"code":"fake","state":"<有效 state>"}'
# 改前：message 里出现 "Third-party login failed: %s"
```

## 三、根因

`internal/pkg/i18n/en.go:52`：

```go
AuthOAuthFailed: "Third-party login failed: %s",
```

而调用方 `oauth_service.go:73`：

```go
return errcode.ErrInvalidParam.WithKey(i18n.AuthOAuthFailed)   // 没传 Args
```

`WithKey` 只换 key 不传参 → `%s` 原样保留。

**并且这里设计上就不该带占位符**：按 docs/14 禁令 2，错误细节只进日志不进响应，
所以通用失败文案本来就不该有 `%s`。带细节的场景应该用另一个 key。

**为什么测试没抓到**：`TestNoChinese` / `TestNoEmptyText` 只管「有没有中文/空串」，
不管占位符数量与传参是否匹配。

## 四、修复方案

```go
// 改前
AuthOAuthFailed: "Third-party login failed: %s",
// 改后（通用失败，不带占位符）
AuthOAuthFailed: "Third-party login failed",
```

同时补上 `AuthOAuthFailedDetail`（带 `%s` 的变体）—— 这个 key 之前**只有常量没有文案**，
线上若被引用会返回裸 key 字符串。

## 五、验证

```bash
curl -s -X POST .../user/oauth/wechat/web/callback -d '{"code":"fake","state":"..."}'
→ {"message":"Third-party login failed","status_code":10008}    ✓ 无 %s
```

新增守卫测试 `internal/pkg/i18n/catalog_guard_test.go`：

```
=== RUN   TestEveryKeyConstantHasText
    catalog_guard_test.go:86: ✓ 332 个 Key 常量全部有文案
--- PASS
=== RUN   TestNoOrphanCatalogEntry
    catalog_guard_test.go:106: ✓ 无孤儿文案
--- PASS
```

## 六、影响与风险

**Breaking change**：文案从 `"Third-party login failed: %s"` 变 `"Third-party login failed"`。
前端若做了字符串匹配需同步（正常应只看 status_code）。

**顺带修掉的更严重问题**：旧的 `TestCatalogComplete` 是**同义反复** ——
`Keys()` 从 `enCatalog` 反向收集 key，再去 `enCatalog` 里查，永远通过。
所以 `AuthOAuthFailedDetail` 缺文案这件事，测试一直绿着。
新增的 `catalog_guard_test.go` 用 `go/parser` 做 AST 静态分析，真正能抓到。

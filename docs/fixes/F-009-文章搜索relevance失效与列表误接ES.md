# F-009 — 文章搜索 relevance 排序失效 + /article 列表被误接 ES

## 元信息

| 项 | 值 |
|---|---|
| 日期 | 2026-11-27 |
| 影响端点 | `GET /article/search`（排序 bug）、`GET /article`（误接，lead 自查发现未上线） |
| 严重级 | P1（默认排序语义错误，前端搜索页第一屏排序错） |
| 发现方式 | 真机对比 `sort_by=relevance` 与 `sort_by=latest` 返回顺序完全相同 |
| 状态 | 已修复并真机验证 |

## 一、现象

**修复前**（端口 4081 真机）：

```
sort_by=relevance: 4bf422(L1/V7) | 7dd4f5(L0/V2) | 554965(L0/V1) | 6628e7(L0/V0)
sort_by=likes:     4bf422(L1/V7) | 4be831(L1/V0) | 7dd4f5(L0/V2) | 554965(L0/V1)
sort_by=latest:    4bf422(L1/V7) | 7dd4f5(L0/V2) | 554965(L0/V1) | 6628e7(L0/V0)
sort_by=views:     4bf422(L1/V7) | 7dd4f5(L0/V2) | 554965(L0/V1) | 4ed95f(L0/V1)
```

`relevance`（**默认值**）与 `latest` 完全相同；`likes` 却出现不同的 `4be831`。
「按相关度」这个搜索的默认语义整个失效。

## 二、复现步骤

```bash
# 起服务（ES 可用或不可用均可复现 —— DB 路径的 bug）
curl "http://127.0.0.1:4081/api/v1/article/search?page=1&limit=4&sort_by=relevance" \
  -H "Authorization: Bearer $T" -H "Device-Id: t"
# 与 sort_by=latest 对比：返回完全相同的顺序与条目
```

## 三、根因

`internal/service/article.go` 的 `List` 里，`SortBy` → `OrderBy` 的映射 switch **缺 `relevance` 分支**：

```go
// 修复前：只有三个 case
switch q.SortBy {
case "latest":  orderBy = "latest"
case "views":   orderBy = "popular"
case "likes":   orderBy = "relevance"
}
// relevance 不匹配任何 case → orderBy 保持初值 "sort"（人工排序位 DESC）
```

`orderBy` 的初值是 `"sort"`（对齐 `GET /article` 的语义：人工置顶优先）。
对搜索而言，这个初值毫无意义 —— 但 handler 里 `DefaultQuery("sort_by", "relevance")`
让**每个不带参数的搜索请求都命中这个漏洞**。

`articleOrder()`（repository/article.go:78）本身**有** `relevance` 分支
（`likes DESC, views DESC, created_at DESC`），只是上游永远传不进来。

**衍生缺陷**（ES 接线时的失误，自查发现）：给 `/article/search` 接 `ListWithSearch` 时，
replace 命中了 `ArticleHandler.List`（`/article` 列表）里的同文调用行 ——
列表页被误接了 ES 优先逻辑。用临时日志确认 `ListWithSearch` 从未被 Search 调用后，
按函数体精确替换修正。

## 四、修复方案

1. **补 `relevance` 分支**，并把「likes 与 relevance 同义」写进注释：

```go
case "likes", "relevance":
    orderBy = "relevance"
```

选「合并映射」而不是「relevance 单独一档」：`articleOrder` 里 relevance 的定义就是
热度优先（likes→views→时间），与 likes 排序语义一致；ES 路径的 `_score` 相关性
在 DB 回落时没有对应物，用热度近似是既定语义（非本轮引入）。

2. **handler 误接修正**：`/article` 列表恢复 `h.svc.List`，`/article/search` 独占 `ListWithSearch`。

否掉的备选：把 `orderBy` 初值从 `"sort"` 改成 `"relevance"` —— 那会改变
`GET /article` 无 sort 时的行为（人工置顶优先是列表页的既定语义），影响面更大。

## 五、验证

**修复后**（端口 4082 真机）：

```
sort_by=relevance: 4bf422(L1/V7) | 4be831(L1/V0) | 7dd4f5(L0/V2) | 554965(L0/V1)
sort_by=likes:     4bf422(L1/V7) | 4be831(L1/V0) | 7dd4f5(L0/V2) | 554965(L0/V1)
sort_by=latest:    4bf422(L1/V7) | 7dd4f5(L0/V2) | 554965(L0/V1) | 6628e7(L0/V0)
sort_by=views:     4bf422(L1/V7) | 7dd4f5(L0/V2) | 554965(L0/V1) | 4ed95f(L0/V1)
```

- `relevance` ≡ `likes`（第 2 位出现 `4be831`，两篇 L1 排前）✓
- 四种排序两两可区分 ✓

**误接修正验证**：临时日志确认 `/article/search` 走 `ListWithSearch`
（`search_nil=false, search_enabled=true`）、`/article` 列表不再经过它；
验证后日志代码已删除。

**ES 双路径**（`/article/search` 与 `/search` 各两态）：

```
ES 正常（真实 9201）→ 走 ES，无回落告警
ES 指向死端口 9999  → 日志 "ES 检索失败，回落 DB"，响应仍 200 + 正确数据
```

## 六、影响与风险

- `GET /article/search` 默认排序**行为变更**：修复前默认按人工置顶位，修复后按热度。
  对前端是「搜索结果更合理」，无字段变化，无需适配。
- `GET /article` 列表语义未变（误接未上线即被拦下，无外部影响）。
- **同类隐患**：`articleRowToAPI`（map 字面量 key）输出 camelCase 违反全站 snake_case，
  `json-naming-check.py` 扫不到 struct tag 之外的 key —— 本轮一并改为 snake_case
  （`author_id`/`favorite_count`/`created_at`…），真机 `/search` 响应已确认。
  工具盲区已记入 todo（map 字面量 key 无 AST 级扫描）。

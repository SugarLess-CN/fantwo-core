# 代码规范（JSON 序列化）

> **对外 JSON 的空值规则。** 违反 = 前端要写一堆 `null` 判断，且字段类型不可信。
> AGENTS.md「响应契约与 i18n」有精简指针，本文件是完整规范。
>
> 最后更新：2026-11-25（用户指定）

---

## 一、四条硬规则

| 字段类型 | 空值时输出 | dto 里怎么定义 | 反例 |
|---|---|---|---|
| **字符串** | `""` | `string`（**值类型，不用指针**） | `"email": null` ❌ |
| **数组** | `[]` | `[]T`（**必须初始化，不能是 nil**） | `"roles": null` ❌ |
| **对象 / 嵌套结构** | **整个字段不传** | `*T` + `json:",omitempty"` | `"certification": null` ❌ |
| **时间 / 有 null 语义的标量** | **整个字段不传** | `*time.Time` + `json:",omitempty"` | `"birthDate": null` ❌ |
| 数字 / 布尔 | `0` / `false` | 值类型 | — |

### 为什么时间字段是「不传」而不是 `""`

时间**没有空字符串的自然表示**：
- 传 `""` → 前端 `new Date("")` 得到 `Invalid Date`，比 null 更难排查
- 传 `null` → 违反「不用 null」规则
- 传 `"0001-01-01T00:00:00Z"` → `time.Time` 零值，看着像有效数据，最糟

所以**不传**是唯一诚实的选择。前端用 `if (user.birthDate)` 判断即可。

### 为什么字符串必须用值类型而不是指针

`*string` 表达的是「这个字段有三种状态：未设置 / 空串 / 有值」。
但对外 JSON **只有两种状态对前端有意义**：有值 / 空。前端不需要区分「从没设过」和「设成空串」。

指针带来的唯一后果是 `null`。所以 dto 层一律用值类型。

**注意**：`model` 层的指针**保持不变** —— 数据库需要区分 SQL NULL 与空串（唯一索引、条件查询都依赖）。
转换只发生在 `dto` / View 组装层。

---

## 二、落地写法

### 2.1 dto 定义

```go
// internal/dto/user.go
type UserDto struct {
    ID       string  `json:"id"`
    Nickname string  `json:"nickname"`        // ✅ 值类型，空时 ""
    Avatar   string  `json:"avatar"`          // ✅
    Roles    []RoleBriefDto `json:"roles"`    // ✅ 数组，空时 []（去掉 omitempty）

    // 时间：空时不传
    BirthDate *time.Time `json:"birthDate,omitempty"`            // ✅
    MembershipEndDate *time.Time `json:"membershipEndDate,omitempty"` // ✅

    // 嵌套对象：空时不传
    Certification *CertificationDto `json:"certification,omitempty"` // ✅
}
```

### 2.2 组装层的三个辅助函数

放在 `internal/service/common.go`（公共工具唯一归属地）：

```go
// strOrEmpty 指针字符串 → 值；nil 时返回 ""。
func strOrEmpty(p *string) string {
    if p == nil {
        return ""
    }
    return *p
}

// sliceOrEmpty 保证返回非 nil 切片（JSON 序列化后是 [] 而不是 null）。
func sliceOrEmpty[T any](s []T) []T {
    if s == nil {
        return []T{}
    }
    return s
}

// orZeroTime 指针时间 → 指针（nil 保持 nil，配合 omitempty 不传字段）。
// 保留这个包装只是为了「显式表达意图」，实际 nil 直接赋值即可。
```

### 2.3 数组字段的陷阱

```go
// ❌ 错：nil 切片序列化成 null
var roles []RoleBriefDto
v.Roles = roles

// ✅ 对：显式初始化
v.Roles = make([]RoleBriefDto, 0, len(u.Roles))
```

**`json.Marshal` 对 nil 切片输出 `null`，对空切片输出 `[]`。** 这个差异是 Go 的坑，
不是配置能改的 —— 只能在赋值时保证非 nil。

---

## 三、自查清单（改完任何对外结构后跑）

```bash
# ① 起服务后请求几个典型接口，看有没有 null
curl -s http://127.0.0.1:PORT/api/v1/user/profile -H "Authorization: Bearer $T" \
  | python -c "import sys,json;d=json.load(sys.stdin);print(json.dumps(d,ensure_ascii=False,indent=1))" \
  | grep -n "null"

# 期望：无输出（除了刻意保留的极少数例外，见第四节）

# ② 空列表接口应返回 [] 而不是省略或 null
curl -s "http://127.0.0.1:PORT/api/v1/decoration/user/my" -H "Authorization: Bearer $T"
# 期望：{"list":[],"meta":{...}}

# ③ swagger 里的类型声明要与实际一致（string 不能声明成 nullable）
python -c "
import json;d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'))
s=d['definitions'].get('dto.UserDto',{})
for k,v in (s.get('properties') or {}).items():
    if v.get('type') is None and '\$ref' not in v:
        print('可疑无类型:', k, v)
"
```

---

## 四、允许的例外（只有这些）

| 场景 | 为什么可以 null / 省略 |
|---|---|
| 响应外壳的 `data` 字段 | 空时整个 `data` 省略，这是既有契约（`Body.MarshalJSON`） |
| 请求体里的可选字段 | 客户端不传 = 不改，语义不同（这是入参不是出参） |
| `interface{}` 类型的透传字段 | 如 `certification`（可能为任意结构），用 `omitempty` 省略 |
| 确实需要三态语义的字段 | 极少。用了必须在结构体注释里写清「为什么这个字段需要区分未设置/空值」 |

**除上表外，任何 `null` 都是缺陷。**

---

## 五、这次改动的范围与顺序

按影响面从小到大，**逐域改，每域改完立刻验证**：

| 批次 | 域 | 主要结构 |
|---|---|---|
| 1 | user | `UserDto` / `UserProfileDto` / `RoleBriefDto` / `CertificationDto` / `UserConfigDto` |
| 2 | article | `ArticleDto` / `CommentDto` |
| 3 | upload / emoji / decoration | 各自 Dto |
| 4 | points / order / payment | 各自 Dto |
| 5 | config / audit / role / permission | 各自 Dto |

**为什么逐域而不一次全改**：一次全改 189 个指针字段，出问题无法定位是哪个域引入的。
逐域改，每批都能独立验证、独立回滚。

---

## 六、与已有契约的关系

| 既有规则 | 是否受本规范影响 |
|---|---|
| `data` / `message` 空时整个字段省略 | 不变（这是外壳层，见第四节例外） |
| `status_code` / `trace_id` / `timestamp` 恒在 | 不变 |
| 数组空时 `[]` 不是 `null` | **强化**（原来只对 `thumbnails` 等个别字段做了，现在全量） |
| camelCase | 不变 |

---

## 七、记录这一决策

按 `docs/fixes/README.md` 的机制，本规范的实施**必须**有修复文档：

- `docs/fixes/F-007-空值序列化规范不符.md` —— 记录「改前一堆 null」的现象、根因、逐域修复过程

规范本身是「定规矩」，修复文档是「这一次改了什么」。两份都要有。

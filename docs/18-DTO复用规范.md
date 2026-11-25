# DTO 复用规范（Swagger 数据模型）

> 强制规则。违反 = 前端拿不到字段定义、Apifox 里看到一堆匿名 `{}`。
> AGENTS.md 有精简版指针，本文件是完整规范 + 落地步骤。
>
> 最后更新：2026-11-25

---

## 一、问题：现在的 swagger 只有"外壳复用"，业务 DTO 一层完全缺失

实测 `docs/swagger/swagger.json`（246 paths）现状：

```json
// ✅ 外壳已复用
"200": {"schema": {"$ref": "#/definitions/internal_handler.ResponseBody"}}

// ❌ 但 data 是空对象 —— 前端拿不到任何业务字段
"internal_handler.ResponseBody": {
  "properties": {
    "data": {},                       ← 零定义
    "message": {"type": "string"},
    "status_code": {"type": "integer"},
    "trace_id": {"type": "string"},
    "timestamp": {"type": "integer"}
  }
}
```

具体缺失：

| 问题 | 现状 | 影响 |
|---|---|---|
| `data` 无类型 | `{}` | 前端/Apifox 看不到业务字段，等于没有文档 |
| 分页 `items` 无类型 | 只有 `Inner/PageMeta` 骨架 | 列表接口的每条记录字段全缺 |
| 请求体内联 | 245 个接口只有 1 个具名（`loginRequest`） | 改一个字段要翻 245 处 |
| 命名带包名前缀 | `internal_handler.ResponseBody` | 进 Apifox 显示成一长串，难认 |
| 命名不规范 | `loginRequest` 小写开头 | 与 Go 导出规则冲突 |

---

## 二、目标结构

```
internal/dto/                     ← 新建独立包（前缀短：dto.）
├── common.go                     ← 外壳：ResponseBody / PageBody / PageMeta
├── user.go                       ← UserDto / CreateUserRequest / UpdateUserProfileRequest
├── article.go                    ← ArticleDto / CreateArticleRequest / UpdateArticleRequest
├── comment.go                    ← CommentDto / CreateCommentRequest
├── upload.go                     ← UploadDto / ConfirmUploadRequest
└── ...
```

**为什么单独建 `internal/dto` 包**：swag 生成的 schema 名 = `<包名末段>.<类型名>`。
放 `internal/handler` 里会变成 `internal_handler.ResponseBody`；放 `internal/dto` 里是
`dto.ResponseBody` —— 短、可读、Apifox 里能认。这是唯一能改善命名的办法（swag 不提供
自定义前缀参数）。

### 命名规范（强制）

| 类别 | 后缀 | 例 |
|---|---|---|
| 响应业务实体 | `Dto` | `ArticleDto`、`UserDto`、`CommentDto` |
| 请求体 | `Request` | `CreateArticleRequest`、`UpdateUserRequest` |
| 分页容器 | 复用 `dto.PageBody` | 不新建 |
| 嵌套子结构 | 语义命名 | `CommentAuthorDto`、`ThumbnailDto` |

**禁止**：小写开头（`loginRequest`）；`Dto`/`Response` 混用同一层；`Req`/`Resp` 缩写。

---

## 三、必须复用的三种结构

### 3.1 外壳（所有接口共用，只定义一次）

```go
// internal/dto/common.go
package dto

// ResponseBody 是所有非分页接口的统一响应外壳。
type ResponseBody struct {
    Data       interface{} `json:"data,omitempty"`
    Message    string      `json:"message,omitempty"`
    StatusCode int         `json:"status_code" example:"0"`
    TraceID    string      `json:"trace_id" example:"4c3abb48-ddf2-4943-b685-ec4ec35449ed"`
    Timestamp  int64       `json:"timestamp" example:"1795540372656"`
}

// ResponsePageBody 是所有分页接口的统一响应外壳。
type ResponsePageBody struct {
    Data       PageBody `json:"data,omitempty"`
    Message    string   `json:"message,omitempty"`
    StatusCode int      `json:"status_code" example:"0"`
    TraceID    string   `json:"trace_id"`
    Timestamp  int64    `json:"timestamp"`
}

// PageBody 分页数据体。
type PageBody struct {
    List  interface{} `json:"list"`
    Total int64       `json:"total" example:"128"`
    Page  int         `json:"page" example:"1"`
    Size  int         `json:"size" example:"20"`
}
```

### 3.2 业务 DTO（每个域一次）

```go
// internal/dto/article.go
package dto

// ArticleDto 文章对外视图。
type ArticleDto struct {
    ID       string `json:"id" example:"64b59ed4-1a2b-4c3d-8e9f-0123456789ab"`
    Title    string `json:"title" example:"我的第一篇作品"`
    Content  string `json:"content"`
    AuthorID string `json:"authorId"`
    LikeCount int   `json:"likeCount" example:"12"`
    // ...
}
```

### 3.3 handler 注解引用（关键：用 `{data=XxxDto}` 嵌套覆盖）

**非分页**：

```go
// @Success 200 {object} dto.ResponseBody{data=dto.ArticleDto} "文章详情"
```

**分页**（列表在 `data.list`）：

```go
// @Success 200 {object} dto.ResponsePageBody{data=dto.PageBody{list=[]dto.ArticleDto}} "文章列表"
```

**请求体**：

```go
// @Param body body dto.CreateArticleRequest true "创建文章"
```

这三条语法是 swag 支持的嵌套覆盖机制 —— **它让外壳只定义一次、业务字段各自具名**，
这就是"复用"的落地形态。不写 `{data=...}` 就等于 `data: {}`，白定义。

---

## 四、落地步骤（按域分批）

1. **先建 `internal/dto/common.go`**（外壳，一次）
2. **逐域抽 DTO**：`user` → `article` → `comment` → `upload` → `points` → `order` → `payment` → 其余
3. **每域完成后**：改该域所有 handler 的注解（`@Success` / `@Param`）→ `make swag` → 核对：
   ```bash
   # definitions 应只增不减（新增该域 DTO）
   python -c "import json;d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'));\
   print('paths:',len(d['paths']),'defs:',len(d.get('definitions',{})))"
   ```
4. **删掉旧的匿名/小写类型**：`internal_handler.loginRequest` → `dto.LoginRequest`
5. **一致性检查**（每批必做）：
   ```bash
   # data 不应再出现空 {}（除已知无 data 的接口如 logout/delete）
   python -c "
   import json;d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'))
   empty=[k for k,v in d.get('definitions',{}).items()
          if v.get('properties',{}).get('data')=={}]
   print('data 仍为空的 definitions:', empty or '无')"
   ```

---

## 五、边界：什么不该抽成 DTO

- **只有 1 个接口用的、且不会再变的内部结构** —— 抽了也没有复用收益，但**仍然要具名**
  （为了 swag 能生成可读 schema 名），只是不必强求跨文件复用。
- **错误响应的 data** —— 统一 `data` 省略（响应契约规定空字段省略），不定义错误 DTO。
- **文件流 / 二进制响应**（下载）—— 用 `@Produce octet-stream`，不定义 DTO。

---

## 六、验证清单

每批 DTO 抽取完成后逐条确认：

```
□ gofmt -l internal/ 空
□ go build ./... OK
□ go vet ./... 零输出
□ go test ./... -count=1 16 包全绿（注解改动不该影响行为，但要确认）
□ make swag 成功
□ definitions 数量增加（新增 DTO 生效）
□ data 不再是大面积空 {}
□ 新 DTO 命名符合 §二 的表格
□ 原来被替换的旧类型（如 internal_handler.loginRequest）已从 definitions 消失
□ 真机 curl 一个该域接口，响应字段与 DTO 声明一致
```

---

## 七、为什么这件事值得做

1. **前端**：按 swagger 生成 SDK 时能拿到真实字段类型，不用猜
2. **Apifox**：`{data=dto.ArticleDto}` 会展开成可读的数据模型，Mock 能自动生成
3. **改字段**：改 `ArticleDto` 一处，246 个引用它的接口文档同时更新 —— 这才是复用的价值
4. **测试**：`api-tester` 能按 DTO 声明双向核对（响应字段是否与文档一致），比人肉看强

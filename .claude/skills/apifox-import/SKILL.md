---
name: apifox-import
description: 把 swagger 接口文档导入 Apifox 并反查验证。当用户要求「导入接口到 Apifox」「同步 DTO 到 Apifox」「更新 Apifox 接口」「读取 Apifox 文档」时使用。包含单文档全量方案、6 个静默失败坑（$ref 前缀 / 拆批次 / 文件名前缀 / 注解不生效 / schema ignored / 分类不匹配）、导入前闸门命令、只读 MCP 反查方法。输入是 swagger.json。
---

# Apifox 导入与反查

**本 skill 记录的是实测有效路径**，不是理论。每条坑都标了「怎么发现的」，因为这类问题的共同特征是
**日志全部显示成功**。

---

## 零、先记住这一条

> **导入日志说「成功 580/580」不等于 Apifox 里是对的。**
>
> 本项目实测踩过 6 次「静默失败」——日志 100% 绿，但 Apifox 侧数据是坏的。
> **唯一可信的验收方式是「导入后只用读 MCP 反查实际结构」。**

判定顺序永远是：

```
改注解 → 跑 swag → 【读 swagger.json 验证注解真生效】→ 生成批次
       → 【读批次文件验证 $ref 正确】→ 导入 → 【读 MCP 验证 Apifox 侧结构】
```

**三个方括号是硬闸门，跳过一个就可能白跑一轮。** 每轮白跑至少 10 分钟。

---

## 一、工具链现状

| 用途 | 工具 | 说明 |
|---|---|---|
| **生成批次** | `scripts/gen-apifox-enhanced.py` | 本地 swagger → Apifox 批次文件 |
| **导入** | `scripts/apifox-import.mjs` | **自写，直调 Apifox Open API，不走第三方 MCP** |
| **反查** | 只读 MCP（工具名带 `apifox-` 且能读 OAS） | 唯一可信的验收手段 |

### 为什么不用 import MCP

历史用过第三方 import MCP，限制严重：**每次只能导 1 个 operation**、schema 必须先于 paths、
成对 method 会 422。用它导 320 个接口要 320 次调用。

**自写脚本一次请求完成**（单文档全量），**11 秒**。别回去用 MCP 导入。

### MCP 身份判定（重要）

存在多个 Apifox MCP 时：
- **只读**（读 OAS、列项目）—— 用来反查
- **import**（工具名含 `import-openapi`）—— 本 skill **不用**
- **名称带 office / 电商相关** —— **永久禁用**，曾两次误当本项目源

**只读 MCP 首次调用报 broken link，先 `refresh` 再 `read`。**

---

## 二、标准流程（4 步）

```bash
cd <项目根>

# ① 生成 swagger（改了注解/DTO 才需要）
"$(go env GOPATH)/bin/swag.exe" init -g cmd/server/main.go -o docs/swagger \
  --parseDependency --parseInternal --exclude tmp,internal/dto > tmp/swag.out 2>&1
echo "exit=$?"                                                      # 必须 0
grep -icE "unsupported|cannot find|declared multiple" tmp/swag.out  # 必须 0

# ② 生成批次（默认 --mode full = 单文档全量）
rm -rf tmp/apifox-enhanced
python scripts/gen-apifox-enhanced.py docs/swagger/swagger.json tmp/apifox-enhanced/

# ③ 闸门：验证批次文件自洽（跳过这步 = 大概率白跑）
python -c "
import json
d=json.load(open('tmp/apifox-enhanced/o-full.json',encoding='utf-8'))
schema=set(d.get('components',{}).get('schemas',{}))
refs=set()
def w(n):
    if isinstance(n,dict):
        for k,v in n.items():
            if k=='\$ref' and isinstance(v,str): refs.add(v.rsplit('/',1)[-1])
            else: w(v)
    elif isinstance(n,list):
        for i in n: w(i)
w(d)
print('schemas=',len(schema),'missing=',sorted(refs-schema) or '无 ✓')
"
# 期望 missing 为空。非空 → 停下，别导入

# ④ 导入
node scripts/apifox-import.mjs tmp/apifox-enhanced/
# 期望：总计 1 | 成功 1 | 失败 0
```

---

## 三、六个静默失败坑（全部实测踩过）

### 坑 1｜`$ref` 前缀错：`#/definitions/` 而非 `#/components/schemas/`

**症状**：schema 全进去了、数量对、顶层字段对，但**所有嵌套 `$ref` 在 Apifox 侧变成
`{"type":"object","properties":{}}` 空对象**。日志 100% 成功。

**根因**：`gen-apifox-enhanced.py` 把 swagger2 的 `definitions` 搬到 OAS3 的 `components.schemas`，
但没递归改写引用路径。而文档头声明 `openapi: 3.0.3` —— **OAS3 里没有 `#/definitions/` 这个路径**，
Apifox 解析不到就**静默降级，不报错**。

**发现方式**：反查时读某个 path 的 200 schema，发现 `allOf` 里的 `$ref` 变成了空 object。

**修法**（已在脚本里）：
```python
def _rewrite_refs(node):
    if isinstance(node, dict):
        for k, v in list(node.items()):
            if k == '$ref' and isinstance(v, str) and v.startswith('#/definitions/'):
                node[k] = '#/components/schemas/' + v[len('#/definitions/'):]
            else: _rewrite_refs(v)
    elif isinstance(node, list):
        for it in node: _rewrite_refs(it)
_rewrite_refs(o3)   # ★ 必须在 o3 完全构造完（return 前）调用
```

**闸门**：
```bash
grep -ohE '"\$ref" *: *"[^"]+"' tmp/apifox-enhanced/*.json | cut -d/ -f1-2 | sort | uniq -c
# 期望全部 #/components，出现 #/definitions 就别导
```

---

### 坑 2｜拆批次导致引用跨文档（**最隐蔽**）

**症状**：与坑 1 一样（引用全断成空对象），但 `$ref` 前缀已经是对的。

**根因**：Apifox 的 `import-openapi` **只认「同一次导入的文档内 `components.schemas`
里定义过的 schema」**，不认项目里已存在的同名 schema。

拆成 `1 schema + 1 operation` 批次时，**op 批次文档里 `schemas=0`**，
它引用的 `dto.Xxx` 一个都解析不到 → 全部降级。

**怎么发现的**：做了个受控实验 —— 造一个同时含 path 和它引用的 3 个 schema 的文档，
单独导入 → 引用完整保留。再查 `o-001.json` 的 `components.schemas` 数量 = **0**。

**修法**：`--mode full`（默认）单文档全量，1 个文件装 246 paths + 258 schemas。

| 模式 | 行为 | 何时用 |
|---|---|---|
| `--mode full` | **默认**，1 个文件全量 | 正常情况 |
| `--mode split` | 1 schema + 1 op 一批 | **已知无效，仅留作对比** |
| `--mode tag` | 按 tag 分组，每批带全量 schemas | 若 full 因超时失败才试 |

**副作用**：full 模式的文档约 **2 MB**（split 时每 op 只带自己的 path，
full 里每个 op 都补了 8 个状态码的 example，320×8 累计）。Apifox 接受这个体积。

---

### 坑 3｜文件名前缀：必须 `s-*` 或 `o-*`

**症状**：本地生成成功，但导入日志说「schema: 0 | operation: 1」，实际导入了 **0 个文件**。

**根因**：`apifox-import.mjs` 只扫 `s-*`（schema）和 `o-*`（operation）前缀。
叫 `full.json` 会被**静默跳过**。

**修法**：全量模式输出 `o-full.json`（**`o-` 前缀**）。

---

### 坑 4｜注解改了但没生效（两轮白跑都是这个）

**这是最容易犯的，因为脚本会说「改了 N 处」。**

#### 4a｜`@ID` 少了 `//` 前缀

```go
//	@Success	200	{object}	dto.ResponseBody{...}
@ID			AchievementController_create      ← 少了 //，这是 Go 语句不是 swag 注解
```

**swag 静默忽略**，`exit=0`、零报错。

**发现方式**：注入后立刻 `grep -A1 -B1` **看渲染出来的行**，不是看「注入 299 条成功」的计数。

#### 4b｜同一 handler 有两个 body 参数，swag 取第一个

```go
//	@Param		body body object true "achievementRequest"                      ← 旧的，被取用
//	@Success	200	{object}	dto.ResponseBody{data=dto.AchievementDto}
//	@ID			AchievementController_create
//	@Param		body body dto.CreateAchievementRequest true "创建成就定义"       ← 新的，被忽略
```

**症状**：PATCH 类接口都修好了（它们原本只有 `id path`），**POST 类全是空对象**。

**修法**：先删旧行再插新行，或直接替换而非追加。

#### 4c｜注解里要写 `dto.` 前缀

```
//	@Param		body body CreateTagRequest        ← swag 报 cannot find type definition
//	@Param		body body dto.CreateTagRequest    ← 正确
```

**swag 会报错**（`ParseComment error in file ... cannot find type definition`），
但**只有 `grep` 输出才看得见** —— 必须跑：
```bash
grep -icE "unsupported|cannot find|declared multiple" tmp/swag.out   # 必须 0
```

---

### 坑 5｜`schemaOverwriteBehavior` 对已存在 schema 实际走 `ignored`

**症状**：日志显示 `schema:+0 ~0 (ignored:258)` —— 258 个 schema 全部被忽略，一个没更新。

**结论**：这是 **Apifox 的既定行为**，脚本里的 `OVERWRITE_EXISTING` 对**已存在**的 schema 不生效。

**但它不影响引用解析**（实测：209 个 ignored + 48 个 updated 混着，314 个接口照样全对）。

**所以**：
- 首次导入新 schema → `schema:+N`（新建）
- 已存在的 schema → `ignored`，**内容不会更新**

**将来要改 schema 内容，必须先删再导**（只能界面手工删，import 不允许删）。

---

### 坑 6｜tag 名不匹配 → Apifox 自动建新目录

**症状**：原有目录变空，接口散落在同义但不同名的新目录里。

**根因**：Apifox 按 `tags` 字段**精确匹配**已有目录名。

**做法**：
1. **先拉取 Apifox 已有目录名**（只读 MCP 遍历全部 paths 收集 tags 值）—— 必做，不可跳过
2. 通读全部接口路径与语义，**自行归类**到已有目录名
3. 生成脚本里应用这套映射

映射表只在「接口文档本身带正确 tags」时才有意义。真实情况常是没 tags 或不规范，
此时必须按语义自行归类。

---

## 四、反查验收（唯一可信手段）

### 为什么必须反查

导入日志只反映「HTTP 请求成功」，不反映「Apifox 侧数据结构正确」。六个坑里**有五个日志全绿**。

### 反查方法

```
只读 MCP → refresh_project_oas → read_project_oas
```

**注意索引层不含细节**：`read_project_oas` 返回的 `paths` 每项只有
`{"$ref": "/paths/xxx.json", "get": {"summary": "..."}}` —— **看不到 requestBody、看不到 responses 结构**。

**要读具体 path 文件**：
```
read_project_oas_ref_resources → ["/paths/_tag.json"]
```

（路径里的 `/` 换成 `_`，`{}` 换成 `%7B...%7D`）

### 反查三项（每次必查）

| 项 | 期望 | 位置 |
|---|---|---|
| `operationId` 是否符合 `<Controller>_<method>` | 全合规 | path 文件的 `operationId` 字段 |
| 200 响应的 `data` 是否指向 `$ref` | 不是空 `{}` | `responses.200.content["application/json"].schema` |
| `requestBody` 是否有字段 | 不是 `{"type":"object","properties":{}}` | `requestBody.content...schema` |

### 抽验清单（至少这三个，覆盖三种形态）

```jsonc
// ① 分页接口 —— 验三层嵌套
GET /achievement  → allOf[$ref ResponsePageBody, {data: allOf[$ref PageBody, {list: items $ref AchievementDto}]}]

// ② 写接口 —— 验 requestBody
POST /tag         → requestBody.schema.$ref dto.CreateTagRequest，6 个字段

// ③ 大对象 —— 验字段完整性
GET /user/profile → data.$ref dto.UserProfileDto
```

### 已知的历史残留（不要试图修）

- `tmp_deploy-pkg_internal_handler.*` 5 个**污染 schema**：早期 swag 扫到 `tmp/deploy-pkg`
  整项目副本留下的。**只能在 Apifox 界面手工删**，import 不允许删。
- `/comment/{id}/replies`：本地 swagger 与代码都已无此路径，Apifox 侧是历史残留
  （导入只更新不删除）。

---

## 五、导入前的完整闸门（复制粘贴用）

```bash
cd <项目根>

# 1. swag 质量
"$(go env GOPATH)/bin/swag.exe" init -g cmd/server/main.go -o docs/swagger \
  --parseDependency --parseInternal --exclude tmp,internal/dto > tmp/swag.out 2>&1
echo "swag exit=$? (必须 0)"
grep -icE "unsupported|cannot find|declared multiple" tmp/swag.out   # 必须 0

# 2. swagger 自身健全
python -c "
import json,re
d=json.load(open('docs/swagger/swagger.json',encoding='utf-8'))
defs=list(d['definitions'])
print('paths:',len(d['paths']),'defs:',len(defs))
print('污染:',[x for x in defs if 'tmp' in x] or '无')
ops=[op.get('operationId','') for p,it in d['paths'].items() for m,op in it.items() if m in ('get','post','put','patch','delete')]
good=[o for o in ops if re.fullmatch(r'[A-Z]\w+Controller_\w+',o or '')]
print(f'operationId 合规: {len(good)}/{len(ops)}')
empty=0
for p,it in d['paths'].items():
    for m,op in it.items():
        if m not in ('post','put','patch'): continue
        rb=op.get('requestBody') or next(({'content':{'application/json':{'schema':x.get('schema',{})}}} for x in op.get('parameters',[]) if x.get('in')=='body'), None)
        if not rb: continue
        sch=rb.get('content',{}).get('application/json',{}).get('schema',{})
        if sch=={} or (sch.get('type')=='object' and not sch.get('properties') and not sch.get('\$ref')): empty+=1
print('空 requestBody:',empty)
"

# 3. 生成 + 批次自洽
rm -rf tmp/apifox-enhanced
python scripts/gen-apifox-enhanced.py docs/swagger/swagger.json tmp/apifox-enhanced/
grep -ohE '"\$ref" *: *"[^"]+"' tmp/apifox-enhanced/*.json | cut -d/ -f1-2 | sort | uniq -c
# 期望：全部 #/components

# 4. 导入
node scripts/apifox-import.mjs tmp/apifox-enhanced/
# 期望：总计 1 | 成功 1 | 失败 0
```

---

## 六、常见症状 → 病因对照

| 症状 | 病因 | 处置 |
|---|---|---|
| 嵌套 `$ref` 全是空 `{}` | 坑 1 或坑 2 | 查 `$ref` 前缀；查 op 批次是否带 schema |
| 日志「成功 580/580」但 Apifox 没变 | 坑 2 | 改用 `--mode full` |
| 日志「schema: 0 \| operation: 1」但内容没进去 | 坑 3 | 文件名必须 `o-*` / `s-*` |
| 注解改了但 swagger 里没变 | 坑 4a/4b | `grep -A1` 看渲染行；查是否两个 body 参数 |
| swag 报 `cannot find type definition` | 坑 4c | 补 `dto.` 前缀 |
| `schema:+0 ~0 (ignored:N)` | 坑 5 | 正常行为，不影响引用；要改内容需先删 |
| 目录变空、接口散落 | 坑 6 | 先拉 Apifox 目录名再归类 |
| `requestBody` 是空 `{}` | 缺 `@Param body body dto.Xxx` | 补注解（见坑 4） |

---

## 七、配套文档

本 skill 是操作手册。**原理与完整决策记录**见：

- `docs/18-DTO复用规范.md` §五之三 —— Apifox 同步流程与 DTO 规范
- `docs/16-开发工作流.md` —— 七步标准流程（Step 4 是 Apifox 导入）

---

## 八、一句话总结

> **六个坑的共同特征：日志全部显示成功。**
>
> 所以判定的唯一依据是**生成产物**，不是**脚本输出**。
> 每改一次注解，读一次 swagger.json；每导一次，读一次 Apifox OAS。

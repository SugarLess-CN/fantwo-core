---
name: apifox-import
description: 通过 Apifox 的 import MCP 把接口数据导入 Apifox 项目。当用户要求「导入接口到 Apifox」「更新 Apifox 接口」时使用。包含已实测的 MCP 限制（每次 ≤1 operation、schema 先行、$ref 完整性）与分批导入流程、422 排查表、分类保护机制。输入是任一份符合 OpenAPI 3.x 的 JSON（来源不限）。
---

# Apifox 接口导入（import MCP）

通过 Apifox 的 import MCP（工具名含 `import-openapi` 的那个）把接口数据导入 Apifox 项目。

## 前置检查

1. **MCP 选择**：确认当前环境已接入 Apifox 的 import MCP（工具名含 `import-openapi`）。若存在多个 Apifox MCP（只读查看用 / import 导入用），导入用 import 那个；名称与目标项目无关的 MCP 不可用。
2. **输入要求**：一份符合 **OpenAPI 3.x** 的 JSON（`"openapi": "3.0.x"`）。输入来源不限——由调用方准备好；若手里的文档是其它格式，先转换成 3.x 再进入本流程（转换不属于本 skill 的职责）。

## 已实测的 MCP 限制（422 Invalid Parameter 的根因，务必遵守）

| 限制 | 规则 | 违反后果 |
|---|---|---|
| 每次调用 ≤ **1 个 operation** | 2 个及以上大概率 422（带 requestBody 必 422） | 拆批：每个 operation 一次调用（实测 309 次全部成功） |
| **schema 必须先于 paths** | `$ref` 指向未导入的 schema → 422 | 第 0 步：逐个导入 components.schemas（每次 1 个） |
| schema **重名** → 覆盖 | 服务端按名 upsert | 换名会新增，同名 AUTO_MERGE 会「更新 1」 |
| paths 内 `$ref` 必须指向**已导入** schema | 引用完整性校验 | 导入 paths 前先 `grep` 全部 `$ref`，逐一核对已存在 |
| **成对调用会 422** | 同一 path 的两个 method 放一次请求 → 422 | 即使同一 path，get/post 也要分两次调用 |

## ⚠️ 分类保护（实测踩过：tag 名不匹配导致 Apifox 自动创建新目录，原有目录变空）

**Apifox 按 `tags` 字段自动匹配已有目录。tag 名必须与 Apifox 原有目录名**完全一致****，否则 Apifox 会新建一个同义但不同名的目录，导致原有目录变空、接口散落在错误位置。

分类的正确做法不是「做映射表」——映射表只在「接口文档本身带有正确 tags」时才有意义。真实情况是：**很多接口文档没有 tags 或 tags 不规范**，此时需要**通读全部接口的路径与操作语义，自行归类**，生成一套与 Apifox 已有目录对齐的 tags。

### 第 -1 步：获取 Apifox 原有目录名（必做，不可跳过）

用只读 MCP 拉取当前 Apifox 项目的 OAS，提取所有已有 tag/目录名：

```
1. 调 Apifox 只读 MCP 的「读取 OAS」工具
2. 遍历全部 paths，收集所有 operation 的 tags 值 → 这就是 Apifox 已有的目录名集合
```

或者直接在 Apifox 面板截图/复制目录列表。

### 第 -2 步：通读全部接口，按语义归类

**这一步是人工/LLM 智能判断，不是机械映射。**

逐个阅读每个 operation 的 `path`、`method`、`summary`、`description`，理解它的业务含义，然后归入 Apifox 已有目录中最匹配的那个。判断依据：

1. **路径前缀**：`/article/*` → 文章类；`/user/*` → 用户类；`/order/*` → 订单类
2. **summary 语义**：「获取文章列表」→ 文章类；「用户登录」→ 认证类
3. **操作类型**：CRUD 归到对应资源；管理端操作归到管理类

归类完成后输出一张清单（接口路径 → 分配的 tag），人工抽查确认无误。

**注意事项**：
- 如果某个接口实在无法归入已有目录，可以新建一个 tag（Apifox 会自动创建新目录），但必须在汇报中明确说明，让用户确认。
- 不要凭接口路径的字面前缀硬套——要理解业务语义。例如 `/user/certification/*` 虽然前缀是 user，但业务上是「认证」而非「用户资料」。

### 第 -3 步：在生成脚本里应用归类结果

```python
# 从第 -2 步的归类结果生成 接口路径 → tag 的映射
TAG_MAP = {
    # 由通读归类填入，例：
    # "/user/login": "用户认证",
    # "/article/{id}": "文章管理",
}

def get_tag(path):
    return TAG_MAP.get(path, "未分类")  # 未命中的给默认
```

**验证**：生成完后抽查几个批次文件，确认 tags 与 Apifox 原有目录名一致。如仍有新目录名，归类有误，重新归类。

---

## 流程

### 第 0 步：拆分 schema（每次 1 个，不带 paths）

```python
import json
d = json.load(open('input.json', encoding='utf-8'))   # OpenAPI 3.x 输入
for i, (name, schema) in enumerate(d['components']['schemas'].items()):
    doc = {"openapi": "3.0.3", "info": {"title": "t", "version": "1"},
           "paths": {}, "components": {"schemas": {name: schema}}}
    json.dump(doc, open(f'tmp/schemas/s-{i:02d}.json', 'w', encoding='utf-8'), ensure_ascii=False)
```

对每个文件调 import MCP 的 `import-openapi` 工具（`endpointOverwriteBehavior: "AUTO_MERGE"`）。期望输出：`数据模型: 新增 1`（已存在则 `更新 1`，同样算成功）。

**注意**：schema 内部的 `$ref` 必须指向同批已导入的 schema——有依赖顺序的（如 PageBody → PageInner → PageMeta）按依赖序导入。

### 第 1 步：导入 paths（每个 operation 一次调用，tags 已归类）

```python
import json, os
d = json.load(open('input.json', encoding='utf-8'))
os.makedirs('tmp/batches', exist_ok=True)
n = 0
for path, item in d['paths'].items():
    for method, op in item.items():
        if method == 'parameters': continue
        # ⚠️ tags 必须在此处已完成归类（第 -3 步）
        n += 1
        doc = {"openapi": "3.0.3", "info": {"title": "t", "version": "1"},
               "paths": {path: {method: op}}}
        json.dump(doc, open(f'tmp/batches/o-{n:03d}.json', 'w', encoding='utf-8'), ensure_ascii=False)
```

**$ref 修正**：所有 `$ref` 必须指向第 0 步已导入的 schema。导入前批量核对：

```python
import re, glob
schemas = set(d['components']['schemas'].keys())
missing = set()
for f in glob.glob('tmp/batches/*.json'):
    for ref in re.findall(r'#/components/schemas/([^"]+)', open(f, encoding='utf-8').read()):
        if ref not in schemas: missing.add(ref)
assert not missing, f"未导入的 schema: {missing}"
```

然后**逐个**调用 MCP。309 个 operation = 309 次调用，实测全部成功。

### 第 2 步：核对（含分类核对）

导入完成后用只读 MCP 交叉校验：

```
1. 调 Apifox 只读 MCP 的「重新拉取 OAS」工具，刷新服务端缓存
2. 调「读取 OAS」工具，核对 paths 数量与新增接口是否符合预期
3. **分类核对**：遍历 OAS 中每个 operation 的 tags，确认全部命中 Apifox 原有目录名集合——
   出现新 tag 名说明归类有误，该接口会被放到错误目录
```

同时在 Apifox 面板上检查：
- 原有目录的接口数是否与预期一致
- 是否出现了新的非预期目录（有 → 归类遗漏，需修归类重新导入）

---

## 422 排查表

| 症状 | 原因 | 解法 |
|---|---|---|
| 2+ 个 operation 一起报 422 | 超过每次 1 个的安全限额（带 requestBody 必炸） | 拆成单 operation |
| 同一 path 两个 method 一起 422 | 成对调用不支持 | get/post 分两次调用 |
| `$ref` 相关 422 | 引用的 schema 未导入 | 先导 schema，再导 paths |
| schema 导入报 422 但内容看起来合法 | 请求里同时带了 paths | schema 导入时 `"paths": {}` 必须为空 |
| 部分路径持续 422 无明显规律 | 实测存在 | 单独逐个导；仍失败则简化该 op 的 schema 后重试 |

## 分类错误排查表（实测踩过的坑）

| 症状 | 原因 | 解法 |
|---|---|---|
| Apifox 出现新的同义不同名目录 | tags 未按 Apifox 原有目录名归类 | 通读接口按语义归类，重新导入；手动删错误目录 |
| Apifox 原有目录变空（0 个接口） | 接口被分到了新目录 | 同上 |
| 接口数量对但目录对不上 | 同上 | 同上 |

## 实操建议

- 导入是幂等的（AUTO_MERGE 下同 path+method = 更新），中断后重跑即可，不需要去重。
- 导入完成前**不要**在 Apifox 里手工改这些接口（会被下次 AUTO_MERGE 覆盖）。
- 大批量导入（数百 operation）时按域/分批核对「新增/更新」计数，便于中断后定位断点。
- **重新导入不会自动删除 Apifox 里旧目录**——归类修正后，接口会移到正确目录，但旧目录（空壳）需要手动在 Apifox 面板删除。
- **首次导入到已有 Apifox 项目时，务必先做分类保护（第 -1 ~ -3 步）**——归类不匹配的代价是大量空目录需要手工清理。

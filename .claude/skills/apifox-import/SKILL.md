---
name: apifox-import
description: 通过 Apifox 的 import MCP 把接口数据导入 Apifox 项目。当用户要求「导入接口到 Apifox」「更新 Apifox 接口」时使用。包含已实测的 MCP 限制（每次 ≤1 operation、schema 先行、$ref 完整性）与分批导入流程、422 排查表。输入是任一份符合 OpenAPI 3.x 的 JSON（来源不限）。
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

### 第 1 步：导入 paths（每个 operation 一次调用）

```python
import json, os
d = json.load(open('input.json', encoding='utf-8'))
os.makedirs('tmp/batches', exist_ok=True)
n = 0
for path, item in d['paths'].items():
    for method, op in item.items():
        if method == 'parameters': continue
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

### 第 2 步：核对

导入完成后用只读 MCP 交叉校验：

```
1. 调 Apifox 只读 MCP 的「重新拉取 OAS」工具，刷新服务端缓存
2. 调「读取 OAS」工具，核对 paths 数量与新增接口是否符合预期
```

---

## 422 排查表

| 症状 | 原因 | 解法 |
|---|---|---|
| 2+ 个 operation 一起报 422 | 超过每次 1 个的安全限额（带 requestBody 必炸） | 拆成单 operation |
| 同一 path 两个 method 一起 422 | 成对调用不支持 | get/post 分两次调用 |
| `$ref` 相关 422 | 引用的 schema 未导入 | 先导 schema，再导 paths |
| schema 导入报 422 但内容看起来合法 | 请求里同时带了 paths | schema 导入时 `"paths": {}` 必须为空 |
| 部分路径持续 422 无明显规律 | 实测存在 | 单独逐个导；仍失败则简化该 op 的 schema 后重试 |

## 实操建议

- 导入是幂等的（AUTO_MERGE 下同 path+method = 更新），中断后重跑即可，不需要去重。
- 导入完成前**不要**在 Apifox 里手工改这些接口（会被下次 AUTO_MERGE 覆盖）。
- 大批量导入（数百 operation）时按域/分批核对「新增/更新」计数，便于中断后定位断点。

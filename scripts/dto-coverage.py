#!/usr/bin/env python
"""检查 swagger 里每个接口的 data 是否真的具名（而非空 {} 或裸外层 $ref）。

为什么不能只看"有没有 $ref"：
    所有接口的 200 都 $ref 到 internal_handler.ResponseBody / ResponsePageBody，
    而这两个结构的 data 字段是空 {} —— 外层有 $ref 但业务字段零定义。
    真正要看的是：**schema 里是否为 data 声明了具体类型**。

判定逻辑：
    1) 空 schema 或无 responses.200 → 无类型
    2) 只有外层 $ref、没有 allOf 覆盖 → 看该 $ref 目标的 data 是否空
    3) allOf 里带 data 覆盖 → 有类型
"""
import json, re, sys
from collections import Counter

args = [a for a in sys.argv[1:] if not a.startswith('--')]
SRC = args[0] if args else 'docs/swagger/swagger.json'
d = json.load(open(SRC, encoding='utf-8'))
defs = d.get('definitions', {})


def data_typed(schema, depth=0):
    """判断 schema 是否为 data 声明了具体类型。"""
    if not schema or depth > 3:
        return False
    # allOf 覆盖（swag 的 {data=dto.Xxx} 语法生成这种）
    if 'allOf' in schema:
        for part in schema['allOf']:
            props = part.get('properties') or {}
            if 'data' in props:
                dv = props['data']
                # data 指向 $ref 或 allOf（分页嵌套）才算具名
                if '$ref' in dv or 'allOf' in dv:
                    return True
                if dv.get('type') == 'array' and ('items' in dv):
                    return True
        # allOf 里只有外层 $ref → 递归看外层
        for part in schema['allOf']:
            if '$ref' in part and data_typed_named(part['$ref']):
                return True
        return False
    if '$ref' in schema:
        return data_typed_named(schema['$ref'])
    props = schema.get('properties') or {}
    if 'data' in props:
        dv = props['data']
        return bool(dv)
    return False


def data_typed_named(ref):
    """$ref 指向的定义里 data 是否有实质类型。"""
    name = ref.rsplit('/', 1)[-1]
    target = defs.get(name)
    if not target:
        return False
    props = target.get('properties') or {}
    if 'data' not in props:
        return False
    dv = props['data']
    # 空 {} 表示零定义
    if dv == {}:
        return False
    return bool(dv)


bad = []
tot = 0
for p, item in d['paths'].items():
    if '/notify/' in p or p == '/health':
        continue
    for m, op in item.items():
        if m not in ('get', 'post', 'put', 'patch', 'delete'):
            continue
        tot += 1
        sch = op.get('responses', {}).get('200', {}).get('schema', {})
        if not data_typed(sch):
            bad.append((m.upper(), p))

print(f'总操作数      {tot}')
print(f'data 未具名   {len(bad)}  ({len(bad)*100//max(tot,1)}%)')
print(f'已具名        {tot-len(bad)}  ({(tot-len(bad))*100//max(tot,1)}%)')
print()
dom = Counter(p.split('/')[1] if len(p.split('/')) > 1 else 'root' for _, p in bad)
print('未具名按域:')
for k, v in dom.most_common():
    print(f'  /{k:<24} {v}')
if '--list' in sys.argv:
    print('\n明细:')
    for m, p in sorted(bad, key=lambda x: x[1]):
        print(f'  {m:<7} {p}')

# ---------- 契约漂移检查 ----------
# 覆盖率只反映 swagger 注解，看不出 service 层实际返回什么类型。
# 有 42 个函数注解写 dto.Xxx 但实际返回 map[string]interface{} ——
# 文档说一套、运行时另一套，前端按 dto 生成 SDK 会拿到不存在的字段。
if '--drift' in sys.argv or '--all' in sys.argv:
    import subprocess, glob

    print('\n' + '=' * 62)
    print('契约漂移检查：service 层返回 map[string]interface{} 的函数')
    print('=' * 62)

    # 只认「map 是返回值」的情况，不是「map 是参数」。
    # 误报来源：`func (x) Update(fields map[string]interface{}) error` ——
    # 这是把 map 传给 GORM 更新，函数本身返回 error，不是契约漂移。
    #
    # 判别方式：抓函数签名里 `(map[string]interface{}, error)` 或
    # `(map[string]interface{}, *errcode.Err)` 这种**返回位置**的形态。
    pat = re.compile(
        r'\)\s*\(\s*map\[string\]interface\{\}\s*,\s*(error|\*errcode\.Err)\s*\)'
    )
    drift = {}
    for f in glob.glob('internal/service/*.go'):
        if f.endswith('_test.go'):
            continue
        try:
            content = open(f, encoding='utf-8').read()
        except Exception:
            continue
        n = len(pat.findall(content))
        if n:
            drift[f] = n

    total = sum(drift.values())
    if total == 0:
        print('✓ 无契约漂移')
    else:
        print(f'⚠️  {total} 个函数返回 map 而非具名结构：')
        for f, n in sorted(drift.items(), key=lambda x: -x[1]):
            print(f'  {f.replace("internal/service/", ""):<32} {n}')
        print()
        print('这些函数的 handler 注解可能已声明 dto.Xxx，但运行时抛的是 map。')
        print('修法：返回具名 dto。详见 docs/21 §四之三。')

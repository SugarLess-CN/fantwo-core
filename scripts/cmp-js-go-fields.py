#!/usr/bin/env python3
"""对比 JS 版 openapi.json 与 Go 版 exports/openapi.json 的字段差异。

用途：找出「JS 版有、Go 版缺」的字段与参数，产出对照清单。
两份文档命名风格不同（JS camelCase / Go snake_case），统一归一化到 snake_case 再比。

用法：
  python scripts/cmp-js-go-fields.py                 # 汇总
  python scripts/cmp-js-go-fields.py --detail        # 逐字段明细
  python scripts/cmp-js-go-fields.py --schema Login  # 只查某个 schema
"""
import json
import re
import sys
import collections

JS = 'tmp/js/openapi.json'
GO = 'exports/openapi.json'


def snake(name):
    """camelCase / PascalCase → snake_case。已是 snake 的原样返回。"""
    s1 = re.sub(r'([A-Z]+)([A-Z][a-z])', r'\1_\2', name)
    s2 = re.sub(r'([a-z0-9])([A-Z])', r'\1_\2', s1)
    return s2.lower()


def load(path):
    return json.load(open(path, encoding='utf-8'))


def resolve(schema, all_schemas, depth=0):
    """展开 allOf / $ref，返回合并后的 properties 与 required。

    swag 与 Apifox 都会用 allOf 做「继承 + 覆盖」，只看顶层 properties 会漏字段。
    """
    if depth > 8 or not isinstance(schema, dict):
        return {}, set()
    props, req = {}, set()

    for key in ('allOf', 'oneOf', 'anyOf'):
        for sub in schema.get(key, []) or []:
            p, r = resolve(sub, all_schemas, depth + 1)
            props.update(p)
            req |= r

    ref = schema.get('$ref')
    if ref:
        target = all_schemas.get(ref.rsplit('/', 1)[-1])
        if target:
            p, r = resolve(target, all_schemas, depth + 1)
            props.update(p)
            req |= r

    props.update(schema.get('properties') or {})
    req |= set(schema.get('required') or [])
    return props, req


def field_types(schema, all_schemas):
    """schema → {snake_field: (原始名, 类型描述)}"""
    props, _ = resolve(schema, all_schemas)
    out = {}
    for name, pv in props.items():
        t = pv.get('type') or ('ref' if '$ref' in pv else '?')
        out[snake(name)] = (name, t)
    return out


def norm_path(p):
    """路径参数名归一：/{userId} → /{x}，避免 userId vs user_id 的假差异。"""
    return re.sub(r'\{[^}]+\}', '{x}', p.rstrip('/'))


def main():
    detail = '--detail' in sys.argv
    only = None
    if '--schema' in sys.argv:
        only = sys.argv[sys.argv.index('--schema') + 1].lower()

    js, go = load(JS), load(GO)
    js_s = js['components']['schemas']
    go_s = go['components']['schemas']

    # ---------- ① schema 级：按归一化名匹配 ----------
    # JS: CreateUserDto → create_user；Go: dto.CreateUserRequest → create_user_request
    # 去掉 dto. 前缀 + 去掉 request/dto 后缀后比对
    def key_of(n):
        n = n.split('.')[-1]
        n = snake(n)
        for suf in ('_request', '_dto', '_response', '_input'):
            if n.endswith(suf):
                n = n[:-len(suf)]
        return n

    js_by_key = collections.defaultdict(list)
    for n in js_s:
        js_by_key[key_of(n)].append(n)
    go_by_key = collections.defaultdict(list)
    for n in go_s:
        go_by_key[key_of(n)].append(n)

    matched, unmatched = [], []
    for k, js_names in sorted(js_by_key.items()):
        if only and only not in k:
            continue
        go_names = go_by_key.get(k)
        if go_names:
            matched.append((k, js_names, go_names))
        else:
            unmatched.append((k, js_names))

    # ---------- ② 字段级：逐匹配 schema 比对 ----------
    MISSING = []   # (js_schema, go_schema, field, type, originalName)
    for k, js_names, go_names in matched:
        # 合并该 key 下 JS 所有 schema 的字段（一个 key 可能对应多个变体）
        js_fields = {}
        for n in js_names:
            js_fields.update(field_types(js_s[n], js_s))
        go_fields = {}
        for n in go_names:
            go_fields.update(field_types(go_s[n], go_s))

        for f, (orig, t) in sorted(js_fields.items()):
            if f not in go_fields:
                MISSING.append((js_names[0], go_names[0], f, t, orig))

    # ---------- ③ 参数级：同路径同方法比对 query/path/header 参数 ----------
    def param_map(doc):
        out = {}
        for p, item in doc['paths'].items():
            for m, op in item.items():
                if m not in ('get', 'post', 'put', 'patch', 'delete'):
                    continue
                params = {}
                for x in op.get('parameters', []) or []:
                    if x.get('in') == 'body':
                        continue
                    params[snake(x.get('name', ''))] = x.get('in')
                out[(norm_path(p), m.upper())] = params
        return out

    js_p, go_p = param_map(js), param_map(go)
    common_paths = set(js_p) & set(go_p)

    # JS 版把 4 个全局头逐操作声明，Go 版走 @Security 全局声明 + 只显式写 Device-Id。
    # 这不是真缺失，单独统计，避免淹没真业务参数差异。
    GLOBAL_HEADERS = {'authorization', 'device_id', 'device_name', 'device_type'}
    P_MISSING, P_GLOBAL = [], []
    for k in sorted(common_paths):
        for name, loc in js_p[k].items():
            if name in go_p[k]:
                continue
            if loc == 'header' and name in GLOBAL_HEADERS:
                P_GLOBAL.append((k[1], k[0], name))
            else:
                P_MISSING.append((k[1], k[0], name, loc))

    js_only_paths = set(js_p) - set(go_p)

    # ---------- 输出 ----------
    print(f'JS: {len(js["paths"])} paths / {len(js_s)} schemas')
    print(f'Go: {len(go["paths"])} paths / {len(go_s)} schemas')
    print()
    print(f'① schema 匹配: {len(matched)} 个 key 匹配上，{len(unmatched)} 个 JS schema 无对应')
    if unmatched:
        print('   无对应:', ', '.join(n for _, ns in unmatched for n in ns))
    print()
    print(f'② 字段缺失: {len(MISSING)} 处')
    by_schema = collections.Counter(m[0] for m in MISSING)
    for s, n in by_schema.most_common():
        print(f'   {s:34} {n} 个字段缺失')
    print()
    print(f'③ 业务参数缺失: {len(P_MISSING)} 处（比对 {len(common_paths)} 个共同路径）')
    print(f'   全局头差异: {len(P_GLOBAL)} 处（JS 逐操作声明，Go 走 @Security；非真缺失）')
    print(f'   只在 JS 的路径: {len(js_only_paths)}')

    if detail and MISSING:
        print('\n' + '=' * 90)
        print('字段缺失明细')
        print('=' * 90)
        for js_n, go_n, f, t, orig in MISSING:
            print(f'  {js_n:32} → {go_n:34} {f:26} {t:8} (JS名: {orig})')

    if detail and P_MISSING:
        print('\n' + '=' * 90)
        print('参数缺失明细')
        print('=' * 90)
        for verb, p, name, loc in P_MISSING:
            print(f'  {verb:7} {p:52} {name:22} {loc}')


if __name__ == '__main__':
    main()

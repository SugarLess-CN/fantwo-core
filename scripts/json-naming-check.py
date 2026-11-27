#!/usr/bin/env python3
"""JSON 字段命名检查：全站一律 snake_case（入参、出参统一）。

规则（AGENTS「JSON 字段命名口径」2026-11-27）：
  XxxDto / XxxRequest / XxxInput / 其他  → 全部必须 snake_case

豁免（不是「对外 JSON」，是协议层标识）：
  - WS 事件名常量（newMessage 等）：前端硬编码，不在 struct tag 里，本脚本不扫
  - gorm column tag：数据库列名，本来就是 snake_case，本脚本不扫

用法：
  python scripts/json-naming-check.py           # 检查，违规退出码 1
  python scripts/json-naming-check.py --fix     # 自动改写（仅 Dto）
  python scripts/json-naming-check.py --list    # 只列违规
"""
import re
import sys
import glob
import argparse
import collections

DTO_DIR = 'internal/dto'


def camel_to_snake(name: str) -> str:
    """articleCount -> article_count；ID -> id；isMember -> is_member。

    注意连续大写（UserID -> user_id）不能拆成 user_i_d。
    """
    s1 = re.sub(r'([A-Z]+)([A-Z][a-z])', r'\1_\2', name)
    s2 = re.sub(r'([a-z0-9])([A-Z])', r'\1_\2', s1)
    return s2.lower()


def snake_to_camel(name: str) -> str:
    parts = name.split('_')
    return parts[0] + ''.join(p[:1].upper() + p[1:] for p in parts[1:])


def iter_structs(path):
    """产出 (类型名, body 起始偏移, body 结束偏移, body 文本)。"""
    src = open(path, encoding='utf-8').read()
    for m in re.finditer(r'^type (\w+)(?: struct)?\s*\{', src, re.M):
        name = m.group(1)
        i, depth = m.end(), 1
        while i < len(src) and depth:
            if src[i] == '{':
                depth += 1
            elif src[i] == '}':
                depth -= 1
            i += 1
        yield name, m.end(), i - 1, src[m.end():i - 1]


def body_offset_to_abs(src, body_start, inner_offset):
    return body_start + inner_offset


def classify(type_name: str) -> str:
    """2026-11-27 起全站统一，分类仅用于报告分组。"""
    if type_name.endswith('Dto'):
        return 'out'
    if type_name.endswith('Request') or type_name.endswith('Input'):
        return 'in'
    return 'other'


def scan_form_tags():
    """扫描 form:"xxx"（query 参数），同样要求 snake_case。"""
    issues = []
    for f in sorted(glob.glob(f'{DTO_DIR}/*.go')) + sorted(glob.glob('internal/model/*.go')):
        if f.endswith('_test.go'):
            continue
        src = open(f, encoding='utf-8').read()
        for m in re.finditer(r'form:"([a-zA-Z][^",]*)"', src):
            name = m.group(1)
            if '_' in name or not re.search(r'[a-z][A-Z]', name):
                continue
            issues.append((f, '(form)', 'query', name, camel_to_snake(name),
                           m.start(1), m.end(1)))
    return issues


def scan():
    """返回 [(file, type, kind, field_json_name, expected_name, abs_start, abs_end)]"""
    issues = []
    for f in sorted(glob.glob(f'{DTO_DIR}/*.go')):
        src = open(f, encoding='utf-8').read()
        for tname, bstart, bend, body in iter_structs(f):
            kind = classify(tname)
            for m in re.finditer(r'json:"([^",]+)([^"]*)"', body):
                jname = m.group(1)
                if jname == '-' or not jname:
                    continue
                # 全站统一：含小写→大写跃变即为违规
                has_camel = bool(re.search(r'[a-z][A-Z]', jname))
                bad = camel_to_snake(jname) if has_camel else None
                if bad:
                    issues.append((
                        f, tname, kind, jname, bad,
                        bstart + m.start(1), bstart + m.end(1),
                    ))
    return issues


def apply_fix(issues):
    """按文件分组，倒序替换，避免偏移错乱。"""
    by_file = collections.defaultdict(list)
    for it in issues:
        by_file[it[0]].append(it)

    total = 0
    for f, items in by_file.items():
        src = open(f, encoding='utf-8').read()
        for _file, _t, _k, old, new, s, e in sorted(items, key=lambda x: -x[5]):
            src = src[:s] + new + src[e:]
            total += 1
        open(f, 'w', encoding='utf-8', newline='\n').write(src)
    return total


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--fix', action='store_true', help='自动改写')
    ap.add_argument('--list', action='store_true', help='只列违规')
    args = ap.parse_args()

    issues = scan() + scan_form_tags()
    if not issues:
        print('✓ 命名全部合规（全站 snake_case）')
        return 0

    by_kind = collections.Counter(i[2] for i in issues)
    print(f'违规 {len(issues)} 处（出参 {by_kind["out"]} / 入参 {by_kind["in"]}）\n')

    if args.fix:
        n = apply_fix(issues)
        print(f'已自动改写 {n} 个 json tag')
        return 0

    shown = collections.defaultdict(list)
    for f, t, k, old, new, _s, _e in issues:
        shown[t].append(f'{old} → {new}')
    for t in sorted(shown):
        kind = classify(t)
        print(f'  [{kind}] {t}')
        for x in shown[t][:6]:
            print(f'        {x}')
        if len(shown[t]) > 6:
            print(f'        ... 另 {len(shown[t]) - 6} 个')
    return 1


if __name__ == '__main__':
    sys.exit(main())

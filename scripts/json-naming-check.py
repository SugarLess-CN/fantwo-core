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


# --- 工具盲区补丁（2026-11-27）---
# 前两轮全站改名只扫了 struct tag（json:/form:），漏掉 handler 里 c.Query("isRead")
# 这类**硬编码字符串** —— 它们同样是对外 query 契约的一部分，前端传 is_read 才生效。
# 实测漏了 handler/message_handler.go 的 isRead / isBroadcast 一整轮没被发现。
#
# 第二轮补丁（同批）：只扫 `c.Query("x")` 仍然不够 —— 项目里有辅助函数把 key 当**普通字符串参数**
# 传进去（`queryBoolPtr(c, "isRead")`、`boolQuery(c, "onlyAuthor")`、`queryBool(c, "isActive")`），
# 正则匹配不到 `c.Query`。实测这样又漏了 2 处（comment.go / role.go）。
# 因此改为扫**任意函数调用中以字面量形式传给 (c, "key") 的第二个参数** ——
# 只要形如 `f(c, "someKey")` 就视为 query 契约的一部分。
QUERY_CALL_RE = re.compile(
    r'\bc\.(Query|GetQuery|DefaultQuery)\(\s*"([A-Za-z_][A-Za-z0-9_]*)"'
)
PARAM_CALL_RE = re.compile(r'\bc\.Param\(\s*"([A-Za-z_][A-Za-z0-9_]*)"')
# f(c, "key") / f(c, "key", default) —— 辅助函数的 key 实参
HELPER_KEY_RE = re.compile(
    r'\b([A-Za-z_]\w*)\(\s*c\s*,\s*"([A-Za-z_][A-Za-z0-9_]*)"'
)
# 不算 query 契约的辅助函数（与命名无关：它们是响应/日志等）
HELPER_ALLOW = set()


def scan_handler_query_keys():
    """扫描 internal/handler/*.go 里的 query / path 参数字面量，要求 snake_case。

    这是 json-naming-check 的**盲区**：参数名不在 struct tag 里，而在函数调用实参里。
    覆盖三种写法：
      1. `c.Query("x")` / `c.GetQuery("x")` / `c.DefaultQuery("x", ...)`
      2. `helper(c, "x")` —— 项目自建的 query 辅助函数（queryBoolPtr / boolQuery / queryBool ...）
      3. `c.Param("x")` —— 路径参数名同样前后端可见

    为什么只扫字面量：`c.Query(key)` 这种变量传参无法静态判定，留给人工；
    宁可有盲区也不要误报（误报会让脚本失去可信度）。
    """
    issues = []
    files = sorted(glob.glob('internal/handler/*.go'))
    for f in files:
        if f.endswith('_test.go'):
            continue
        src = open(f, encoding='utf-8').read()
        is_comment = _comment_mask(src)  # 每个文件只构建一次掩码（O(n)）
        for m in QUERY_CALL_RE.finditer(src):
            name = m.group(2)
            if not re.search(r'[a-z][A-Z]', name) or is_comment(m.start()):
                continue
            issues.append((f, f'(c.{m.group(1)})', 'query', name, camel_to_snake(name),
                           m.start(2), m.end(2)))
        for m in HELPER_KEY_RE.finditer(src):
            helper, name = m.group(1), m.group(2)
            if helper in HELPER_ALLOW or helper == 'c':
                continue
            if not re.search(r'[a-z][A-Z]', name) or is_comment(m.start()):
                continue
            issues.append((f, f'({helper})', 'query', name, camel_to_snake(name),
                           m.start(2), m.end(2)))
        for m in PARAM_CALL_RE.finditer(src):
            name = m.group(1)
            if not re.search(r'[a-z][A-Z]', name) or is_comment(m.start()):
                continue
            issues.append((f, '(c.Param)', 'path', name, camel_to_snake(name),
                           m.start(1), m.end(1)))
    return issues


def _comment_mask(src: str):
    """返回一个函数：给定位置，判断该位置是否在 Go 注释内。

    为什么不能简单数 `src[:pos].count('/*') > count('*/')`：
    `/**` 出现在 `//` 行注释里（如 `// 路径对齐 docs/02：/role**、/permission**`）
    会被误判成块注释开头，之后 whole 文件都被当成注释 —— 实测把 role.go 的
    `queryBool(c, "isActive")` 漏掉了。

    正确做法：单遍词法扫描，识别 `//` 行注释、`/* */` 块注释、`"..."`、`'...'`、
    反引号原始字符串，并把注释区间记成掩码。字符串里的 `//` 不算注释。
    """
    n = len(src)
    in_comment = [False] * n
    i = 0
    while i < n:
        ch = src[i]
        if ch == '"':  # 普通字符串（跳过转义）
            i += 1
            while i < n and src[i] != '"':
                i += 2 if src[i] == '\\' else 1
            i += 1
        elif ch == '`':  # 原始字符串
            i += 1
            while i < n and src[i] != '`':
                i += 1
            i += 1
        elif ch == "'":  # rune
            i += 1
            while i < n and src[i] != "'":
                i += 2 if src[i] == '\\' else 1
            i += 1
        elif ch == '/' and i + 1 < n and src[i + 1] == '/':
            j = src.find('\n', i)
            j = n if j < 0 else j
            for k in range(i, j):
                in_comment[k] = True
            i = j
        elif ch == '/' and i + 1 < n and src[i + 1] == '*':
            j = src.find('*/', i + 2)
            j = n if j < 0 else j + 2
            for k in range(i, j):
                in_comment[k] = True
            i = j
        else:
            i += 1
    return lambda pos: in_comment[pos] if 0 <= pos < n else False


def _in_comment(src: str, pos: int) -> bool:
    return _comment_mask(src)(pos)


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

    issues = scan() + scan_form_tags() + scan_handler_query_keys()
    if not issues:
        print('✓ 命名全部合规（全站 snake_case）')
        return 0

    by_kind = collections.Counter(i[2] for i in issues)
    print(f'违规 {len(issues)} 处（出参 {by_kind["out"]} / 入参 {by_kind["in"]} '
          f'/ query {by_kind["query"]} / path {by_kind["path"]}）\n')

    if args.fix:
        n = apply_fix(issues)
        print(f'已自动改写 {n} 个 json tag（handler 里的 c.Query 实参不自动改，需人工）')
        return 0

    shown = collections.defaultdict(list)
    for f, t, k, old, new, _s, _e in issues:
        shown[t].append(f'{old} → {new}  ({f})')
    for t in sorted(shown):
        kind = classify(t) if not t.startswith('(c.') and t != '(form)' else t
        print(f'  [{kind}] {t}')
        for x in shown[t][:6]:
            print(f'        {x}')
        if len(shown[t]) > 6:
            print(f'        ... 另 {len(shown[t]) - 6} 个')
    return 1


if __name__ == '__main__':
    sys.exit(main())

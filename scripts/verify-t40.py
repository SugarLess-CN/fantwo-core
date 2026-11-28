#!/usr/bin/env python3
"""T-40 验收：前端交接单第三版的 9 项字段/参数缺口。

对应 docs/api-compat-handover.md §二 的 9 条。
判据来自**产物**（swagger + dto 源码 + handler 源码），不读 git diff、不看成员报告 ——
成员说「已补齐」但字段没进 swagger 的情况真实发生过。

用法：python scripts/verify-t40.py
退出码：0 全过 / 1 有未通过
"""
import json
import re
import sys

SWAGGER = 'docs/swagger/swagger.json'
ok = fail = 0
failures = []


def check(name, cond, detail=''):
    global ok, fail
    if cond:
        ok += 1
        print(f'  ✓ {name}')
    else:
        fail += 1
        failures.append(f'{name}  [{detail}]')
        print(f'  ✗ {name}  [{detail}]')


def props(sw, name):
    """取 definitions 里某个 schema 的属性名集合。"""
    s = sw.get('definitions', {}).get(name) or {}
    return set((s.get('properties') or {}).keys())


def op_params(sw, path, method='get'):
    op = (sw.get('paths', {}).get(path) or {}).get(method) or {}
    return op.get('parameters') or []


def find_param(sw, path, name, method='get'):
    for p in op_params(sw, path, method):
        if p.get('name') == name:
            return p
    return None


def handler_reads(path, param):
    """handler 是否读了某个 query 参数（覆盖 c.Query / helper(c,"x") / c.Param）。"""
    src = open(path, encoding='utf-8').read()
    pats = [
        r'(?:Query|DefaultQuery|GetQuery)\(\s*"' + param + r'"',
        r'\w+\(\s*c\s*,\s*"' + param + r'"',
        r'Param\(\s*"' + param + r'"',
    ]
    return any(re.search(p, src) for p in pats)


def main():
    sw = json.load(open(SWAGGER, encoding='utf-8'))

    # ---------- §二.1 评论内嵌 article / parent ----------
    cp = props(sw, 'dto.CommentDto')
    check('1a CommentDto 有 article', 'article' in cp, f'现: {sorted(cp)}')
    check('1b CommentDto 有 parent', 'parent' in cp, f'现: {sorted(cp)}')

    # ---------- §二.2 用户资料 ----------
    up = props(sw, 'dto.UserProfileDto')
    ud = props(sw, 'dto.UserDto')
    check('2a UserProfileDto 有 equipped_decorations',
          'equipped_decorations' in up or 'equipped_decorations' in ud,
          f'Profile={sorted(up)}')
    check('2b UserProfileDto/UserDto 有 is_followed',
          'is_followed' in up or 'is_followed' in ud,
          f'Profile={sorted(up)}')

    # ---------- §二.3 用户列表项 is_followed ----------
    # 列表元素是 UserDto，若字段上提到了 UserDto 就等于覆盖列表
    check('3 UserDto 有 is_followed', 'is_followed' in ud, f'现: {sorted(ud)}')

    # ---------- §二.4 装饰活动内嵌 ----------
    da = props(sw, 'dto.DecorationActivityDto')
    check('4a DecorationActivityDto 有 article', 'article' in da, f'现: {sorted(da)}')
    check('4b DecorationActivityDto 有 decoration', 'decoration' in da, f'现: {sorted(da)}')

    # ---------- §二.5 装饰归属状态 ----------
    dd = props(sw, 'dto.DecorationDto')
    check('5a DecorationDto 有 is_owned', 'is_owned' in dd, f'现: {sorted(dd)}')
    check('5b DecorationDto 有 is_using', 'is_using' in dd, f'现: {sorted(dd)}')

    # ---------- §二.6 浏览历史 id ----------
    bh = props(sw, 'dto.BrowseHistoryDto')
    check('6 BrowseHistoryDto 有 id', 'id' in bh, f'现: {sorted(bh)}')

    # ---------- §二.7 评论列表 sort_by ----------
    sb = find_param(sw, '/comment/article/{id}', 'sort_by')
    check('7a swagger 声明 /comment/article/{id} 的 sort_by', sb is not None,
          '缺参数')
    if sb:
        check('7b sort_by 有 enum', bool(sb.get('enum')), f'enum={sb.get("enum")}')
    check('7c handler 真读 sort_by', handler_reads('internal/handler/comment.go', 'sort_by'))

    # ---------- §二.8 read-all 的 type（已交付，防止回归）----------
    rp = find_param(sw, '/message/read-all', 'type', method='post')
    check('8a swagger 声明 POST /message/read-all 的 type', rp is not None, '缺参数')
    if rp:
        check('8b type 有 enum', bool(rp.get('enum')), f'enum={rp.get("enum")}')
    check('8c handler 真读 type',
          handler_reads('internal/handler/message_handler.go', 'type'))

    # ---------- §二.9 登录带 certification ----------
    lr = props(sw, 'dto.LoginResultDto')
    check('9 LoginResultDto 有 certification', 'certification' in lr, f'现: {sorted(lr)}')

    # ---------- 附加：全局零重复 oid（交接单 §三.1 的根本判据）----------
    oid = {}
    dup = []
    for path, item in sw.get('paths', {}).items():
        for m, op in item.items():
            if m not in ('get', 'post', 'put', 'patch', 'delete'):
                continue
            k = op.get('operationId', '')
            if not k:
                continue
            if k in oid:
                dup.append(k)
            oid[k] = path
    check('A1 全站 operationId 无重复', not dup, f'重复: {dup[:5]}')
    cams = [p for p in sw.get('paths', {}) if any(c.isupper() for c in p)]
    check('A2 无 camelCase 路径', not cams, f'现: {cams[:5]}')

    print()
    print('=' * 70)
    print(f'T-40 验收：{ok} 通过 / {fail} 未通过')
    print('=' * 70)
    if failures:
        print('未通过项：')
        for f in failures:
            print(f'  · {f}')
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
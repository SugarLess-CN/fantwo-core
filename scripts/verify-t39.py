#!/usr/bin/env python3
"""T-39 验收脚本：逐项核对 JS→Go 字段补齐的落实情况。

用法：python scripts/verify-t39.py
退出码 0 = 全过；1 = 有未通过项。

设计原则：**读产物，不读意图**。不看 git diff、不看成员报告，
只读 swagger / dto 源码 / handler 源码这些真实产物。
"""
import json
import re
import subprocess
import sys
import glob

OK, FAIL = [], []


def check(name, cond, detail=''):
    (OK if cond else FAIL).append((name, detail))


def dto_fields(path, type_name):
    """从 dto 源码提取某类型的 json 字段名列表。"""
    src = open(path, encoding='utf-8').read()
    m = re.search(r'type ' + type_name + r' struct \{(.*?)\n\}', src, re.S)
    if not m:
        return None
    return [x.group(1) for l in m.group(1).split('\n')
            if (x := re.search(r'json:"([^",]+)', l))]


def handler_reads(path, param):
    """handler 是否读了某个 query 参数。

    要覆盖三种写法，漏一种就误判：
      1. c.Query("x") / c.DefaultQuery("x", def) / c.GetQuery("x")
      2. helper(c, "x") —— queryBoolPtr / boolQuery / queryInt / queryString ...
      3. c.Param("x")
    """
    src = open(path, encoding='utf-8').read()
    pats = [
        r'(?:Query|DefaultQuery|GetQuery)\(\s*"' + param + r'"',
        r'\w+\(\s*c\s*,\s*"' + param + r'"',   # helper(c, "x")
        r'Param\(\s*"' + param + r'"',
    ]
    return any(re.search(p, src) for p in pats)


def annotated_params(handler_path, router_line):
    """从 handler 源码的注解块直接提取 @Param 名。

    为什么不查 swagger.json：它要跑过 swag init 才更新，
    校验源码改动时读它会把「已改好」误判成「没改」。
    """
    src = open(handler_path, encoding='utf-8').read().split('\n')
    # 注解里的 tab 数随 swag 排版浮动（实测 2~3 个），归一化后再比，
    # 否则「@Router\t\t/x」匹配不到「@Router\t\t\t/x」。
    norm = lambda t: re.sub(r'\s+', ' ', t).strip()
    target = norm(router_line)
    for i, ln in enumerate(src):
        if target not in norm(ln):
            continue
        names = set()
        for j in range(i - 1, max(i - 30, -1), -1):
            if not src[j].lstrip().startswith('//'):
                break
            m = re.search(r'@Param\s+(\S+)\s+(?:query|path|header|formData)\s', src[j])
            if m:
                names.add(m.group(1))
        return names
    return None


def swagger_params(swagger, path, method):
    """从 swagger.json（v2 格式）取某操作的参数名集合。"""
    op = (swagger.get('paths', {}).get(path) or {}).get(method)
    if not op:
        return None
    return {x.get('name') for x in op.get('parameters', []) or [] if x.get('in') != 'body'}


def go_test_ok():
    r = subprocess.run(['go', 'test', './...', '-count=1'],
                       capture_output=True, text=True, encoding='utf-8', errors='replace')
    fails = [l for l in r.stdout.split('\n') if 'FAIL' in l and 'ok ' not in l]
    return r.returncode == 0, ' | '.join(fails[:3])


def main():
    sw = json.load(open('docs/swagger/swagger.json', encoding='utf-8'))

    # ---------- T-39a 用户域 ----------
    f = dto_fields('internal/dto/user.go', 'UpdateUserConfigRequest')
    if f is None:
        check('A1 UpdateUserConfigRequest 存在', False, '类型不存在')
    else:
        need = [
            'enable_system_notification', 'enable_comment_notification', 'enable_like_notification',
            'enable_follow_notification', 'enable_message_notification', 'enable_order_notification',
            'enable_payment_notification', 'enable_invite_notification', 'enable_email_notification',
            'enable_sms_notification', 'enable_push_notification',
            'hide_comments', 'hide_favorites', 'hide_collections', 'hide_followers',
            'hide_followings', 'hide_tags',
            'watermark_enabled', 'remark',
        ]
        miss = [x for x in need if x not in f]
        check(f'A1 UpdateUserConfigRequest 补 23 字段（现 {len(f)}）', not miss,
              f'缺: {miss}')

    f = dto_fields('internal/dto/user.go', 'UserDto')
    check('A2 UserDto 有 config 字段', f is not None and 'config' in f,
          f'字段数 {len(f) if f else 0}')

    f = dto_fields('internal/dto/user.go', 'RegisterRequest')
    check('A3 RegisterRequest 有 invite_code', f is not None and 'invite_code' in f)
    check('A3 RegisterRequest 有 verification_code', f is not None and 'verification_code' in f)

    f = dto_fields('internal/dto/user.go', 'UpdateContactRequest')
    check('A4 UpdateContactRequest 存在且有 verification_code',
          f is not None and 'verification_code' in f)

    # 隐私字段真的被写入（不是只在 DTO 里）
    wrote = subprocess.run(
        ['grep', '-rl', 'hide_comments', 'internal/service/', 'internal/repository/'],
        capture_output=True, text=True, encoding='utf-8', errors='replace')
    check('A5 hide_comments 有服务层写入点', bool(wrote.stdout.strip()),
          'service/repository 里找不到')

    # ---------- T-39b 内容域 ----------
    check('B1 GET /article 读 sort',
          handler_reads('internal/handler/article.go', 'sort')
          or handler_reads('internal/handler/article.go', 'sort_by'))
    ap = annotated_params('internal/handler/article.go', '@Router\t\t/article [get]')
    check('B1 /article 注解声明 sort', ap is not None and 'sort' in ap,
          f'现: {sorted(ap) if ap else "解析失败"}')

    ap = annotated_params('internal/handler/article.go', '@Router\t\t/article/search [get]')
    miss = [p for p in ('keyword', 'category_id', 'sort_by') if not ap or p not in ap]
    check('B2 /article/search 注解声明 3 参数', not miss, f'缺 {miss}')

    check('B3 browse/history 读 order',
          handler_reads('internal/handler/article_interaction.go', 'order'))

    src = open('internal/service/comment.go', encoding='utf-8').read()
    m = re.search(r'func \(s \*CommentService\) FindCommentDetail\(([^)]*)\)', src)
    check('B4 FindCommentDetail 有分页参数',
          bool(m) and 'page' in m.group(1) and 'limit' in m.group(1),
          f'签名: {m.group(1) if m else "找不到"}')

    check('B5 /tag 读 keyword',
          handler_reads('internal/handler/tag.go', 'keyword'))

    # ---------- T-39c 消息域 ----------
    ap = annotated_params('internal/handler/message_handler.go', '@Router\t\t/message [get]')
    check('C1 /message 注解声明 type', ap is not None and 'type' in ap,
          f'现: {sorted(ap) if ap else "解析失败"}')

    src = open('internal/handler/message_handler.go', encoding='utf-8').read()
    check('C2 无 isRead camelCase', 'GetQuery("isRead")' not in src)
    check('C2 无 isBroadcast camelCase', 'GetQuery("isBroadcast")' not in src)
    # 读参数的写法有多种：Query / GetQuery / DefaultQuery / queryBoolPtr(c, "x") / queryInt(c, "x")
    def reads_param(source, name):
        return bool(re.search(r'"' + name + r'"\s*\)', source))

    check('C2 有 is_read', reads_param(src, 'is_read'))
    check('C2 有 is_broadcast', reads_param(src, 'is_broadcast'))

    f = dto_fields('internal/dto/message.go', 'ConversationDto')
    check('C3 ConversationDto 有 counterpart_id',
          f is not None and 'counterpart_id' in f)

    # json-naming-check 增强
    chk = open('scripts/json-naming-check.py', encoding='utf-8').read()
    check('C4 json-naming-check 扫 c.Query 硬编码',
          'GetQuery' in chk or 'Query(' in chk and 'handler' in chk)

    # ---------- 全局：无 camelCase query ----------
    r = subprocess.run(
        ['bash', '-c',
         r'''grep -rhoE 'Query\("[a-z]+[A-Z][a-zA-Z]*"\)|GetQuery\("[a-z]+[A-Z][a-zA-Z]*"\)' internal/handler/*.go | sort -u'''],
        capture_output=True, text=True, encoding='utf-8', errors='replace')
    check('G1 全站无 camelCase query 参数', not r.stdout.strip(), r.stdout.strip()[:120])

    # ---------- 全局：质量门 ----------
    ok, detail = go_test_ok()
    check('G2 go test 全绿', ok, detail)

    r = subprocess.run(['gofmt', '-l', 'internal/', 'migrations/', 'cmd/'],
                       capture_output=True, text=True, encoding='utf-8', errors='replace')
    check('G3 gofmt 无未格式化文件', not r.stdout.strip(), r.stdout.strip()[:120])

    # ---------- 输出 ----------
    print(f'{"=" * 70}\nT-39 验收：{len(OK)} 通过 / {len(FAIL)} 未通过\n{"=" * 70}')
    for n, d in OK:
        print(f'  ✓ {n}')
    if FAIL:
        print()
        for n, d in FAIL:
            print(f'  ✗ {n}' + (f'   [{d}]' if d else ''))
    return 0 if not FAIL else 1


if __name__ == '__main__':
    sys.exit(main())

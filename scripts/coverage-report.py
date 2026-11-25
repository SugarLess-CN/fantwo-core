#!/usr/bin/env python
"""对比 apifox/swagger 的 operations 与测试日志实际覆盖，输出覆盖率与未测清单。

用法：
    python scripts/coverage-report.py
    python scripts/coverage-report.py --src docs/swagger/swagger.json --log reports/api-test-log.md
"""
import json, re, sys, os
from collections import Counter

SRC = 'docs/swagger/swagger.json'
LOG = 'reports/api-test-log.md'
args = sys.argv[1:]
for i, a in enumerate(args):
    if a == '--src' and i + 1 < len(args):
        SRC = args[i + 1]
    if a == '--log' and i + 1 < len(args):
        LOG = args[i + 1]

sw = json.load(open(SRC, encoding='utf-8'))
allops = set()
for path, item in sw.get('paths', {}).items():
    for m in item:
        if m in ('get', 'post', 'put', 'patch', 'delete'):
            allops.add((m.upper(), path))


def norm(p):
    """归一化路径用于比较。

    要处理三种写法差异：
      - 去 /api/v1 前缀
      - swagger 用 {id}（花括号），日志用 :id（gin 冒号）→ 统一成 {}
      - 去尾斜杠
    """
    p = re.sub(r'^/api/v1', '', p)
    p = re.sub(r'\{[^}]+\}', '{}', p)   # {id} → {}
    p = re.sub(r':[A-Za-z_][A-Za-z0-9_]*', '{}', p)  # :id → {}
    return p.rstrip('/') or '/'


# 从日志标题行提取 (method, path)
# 形如：### [L-001] 2026-11-25 10:52:48 ｜ POST /api/v1/user/register
# 后面可能跟「（异常：xxx）」注解，需要截掉
pat = re.compile(r'^###\s+\[L-[\w]+\].*?｜\s*([A-Za-z]+)\s+(\S+)', re.M)
tested_raw = set()
for m in pat.finditer(open(LOG, encoding='utf-8').read()):
    method = m.group(1).upper()
    path = m.group(2)
    # 截掉中文括号或英文括号后的注解
    path = re.split(r'[（(]', path)[0]
    path = path.split('?')[0]
    tested_raw.add((method, path))

sw_norm = {}
for m, p in allops:
    sw_norm.setdefault((m, norm(p)), (m, p))

tested_norm = {(m, norm(p)) for m, p in tested_raw}

covered = [orig for key, orig in sw_norm.items() if key in tested_norm]
untested = [orig for key, orig in sw_norm.items() if key not in tested_norm]

# 日志里出现但 swagger 里没有的（可能是路径写法差异，或文档缺失）
sw_norm_keys = set(sw_norm)
extra = [(m, p) for m, p in tested_raw if (m, norm(p)) not in sw_norm_keys]

total = len(allops)
log_text = open(LOG, encoding='utf-8').read()
log_entries = len(re.findall(r'^### \[L-', log_text, re.M))
print('=' * 62)
print(f'{"swagger operations":<28} {total}')
print(f'{"日志条目":<28} {log_entries}')
print(f'{"日志唯一 (METHOD,path)":<28} {len(tested_raw)}')
print('-' * 62)
print(f'{"已覆盖":<28} {len(covered):>4}  ({len(covered)*100/total:.1f}%)')
print(f'{"未覆盖":<28} {len(untested):>4}  ({len(untested)*100/total:.1f}%)')
if extra:
    print(f'{"日志有但 swagger 无":<28} {len(extra)}  ← 路径写法差异或文档缺失')
print('=' * 62)

if untested:
    dom = Counter(p.split('/')[1] if len(p.split('/')) > 1 else 'root' for _, p in untested)
    print('\n未覆盖按域（count 降序）：')
    for k, v in dom.most_common():
        print(f'  /{k:<24} {v}')

    print('\n未覆盖明细（全部）：')
    for m, p in sorted(untested, key=lambda x: (x[1], x[0])):
        print(f'  {m:<7} {p}')

if extra:
    print('\n日志里出现但 swagger 未声明的：')
    for m, p in sorted(extra):
        print(f'  {m:<7} {p}')

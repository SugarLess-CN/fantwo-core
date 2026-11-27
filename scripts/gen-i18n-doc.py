#!/usr/bin/env python3
"""i18n key → 多语言对照文档生成器。

从 internal/pkg/i18n/{keys*.go,en.go} 提取全部 key 与英文文案，
生成 docs/27-i18n文案对照表.md。

新增语言：在 LANGS 里加一项（name + 翻译函数或人工映射文件）。
带占位符的文案翻译时保留 %s / %d / %.1f 原样。
"""
import re
import glob
import json
import collections
from datetime import date
import sys as _sys
from pathlib import Path as _Path
_sys.path.insert(0, str(_Path(__file__).parent))
from i18n_translations import T as FULL_T

UUID = '9f1d2e34'


def extract():
    const2key = {}
    for f in glob.glob('internal/pkg/i18n/keys*.go'):
        for m in re.finditer(r'^\s+(\w+)\s+Key\s*=\s*"([^"]+)"', open(f, encoding='utf-8').read(), re.M):
            const2key[m.group(1)] = m.group(2)
    ensrc = open('internal/pkg/i18n/en.go', encoding='utf-8').read()
    en = {}
    for m in re.finditer(r'^\t(\w+):\s+"((?:[^"\\]|\\.)*)",?\s*$', ensrc, re.M):
        en[m.group(1)] = m.group(2)
    out = {const2key[c]: en.get(c, '') for c in const2key}
    return out


FALLBACK = ('（待翻译）', '（待翻譯）', '（未翻訳）', '（번역 예정）')


def tr(en_text):
    """查完整翻译表（scripts/i18n_translations.py）；未命中给「待翻译」。"""
    if en_text in FULL_T:
        return FULL_T[en_text]
    return FALLBACK


def main():
    data = extract()
    by_domain = collections.defaultdict(list)
    for k, en in sorted(data.items()):
        dom = k.split('.')[0]
        by_domain[dom].append((k, en))

    lines = [
        '# i18n 文案对照表（key → 多语言）',
        '',
        f'> 生成时间：{date.today().isoformat()}　｜　key 总数：{len(data)}　｜　'
        '生成器：`scripts/gen-i18n-doc.py`（勿手改，重跑即可更新）',
        '',
        '**响应里的 message 已改为直接返回 key**（用户 2026-11-27 指定），翻译由前端完成。',
        '本文是后端维护的**权威对照表**，前端语言包以此为基准；文案变更只需重跑生成器。',
        '',
        '占位符 `%s` / `%d` / `%.1f` 前端需自行替换（或后端后续提供带参接口）。',
        '',
        '## 语言',
        '',
        '| 代码 | 语言 | 状态 |',
        '|---|---|---|',
        '| en | English | ✅ 权威（`en.go`） |',
        '| zh-CN | 简体中文 | ✅ 本文 |',
        '| zh-TW | 繁體中文 | ✅ 本文 |',
        '| ja | 日本語 | ✅ 本文 |',
        '| ko | 한국어 | ✅ 本文 |',
        '',
    ]

    n_missing = 0
    for dom in sorted(by_domain):
        lines.append(f'## {dom}')
        lines.append('')
        lines.append('| key | en | zh-CN | zh-TW | ja | ko |')
        lines.append('|---|---|---|---|---|---|')
        for k, en in sorted(by_domain[dom]):
            zh, zhtw, ja, ko = tr(en)
            if zh.startswith('（待'):
                n_missing += 1
            # 表格里的 | 和换行要转义
            esc = lambda s: s.replace('|', '\\|').replace('\n', ' ')
            lines.append(f'| `{k}` | {esc(en)} | {esc(zh)} | {esc(zhtw)} | {esc(ja)} | {esc(ko)} |')
        lines.append('')

    lines.append('---')
    lines.append('')
    lines.append('## 前端接入说明')
    lines.append('')
    lines.append('1. **响应 message 是 key**（如 `"auth.need_login"`），前端按本文查表翻译；')
    lines.append('2. `status_code` 才是逻辑判断依据（如 20001 → 跳登录页），**不要按 message 判断**；')
    lines.append('3. 带占位符的 key（38 个）需 `message.replace(\'%s\', 实际值)`；')
    lines.append('4. 新增 key 的流程：`keys*.go` 加常量 → `en.go` 加英文 → 重跑本文生成器。')
    if n_missing:
        lines.append(f'5. ⚠️ 当前有 {n_missing} 条未翻译（标「待翻译」），补充 `T` 表后重跑。')

    out = 'docs/27-i18n文案对照表.md'
    open(out, 'w', encoding='utf-8', newline='\n').write('\n'.join(lines) + '\n')
    print(f'生成 {out}：{len(data)} key，{len(by_domain)} 个域，未翻译 {n_missing} 条')


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""把 @Param 注解里的 `example(...)` 从描述引号内移到引号外。

## 为什么必须移

swag v1.16 的 `findAttr`（operation.go:495）用 `regexp` `\\s+example\\(.*\\)` 提取属性，
**要求 `example` 前有空白字符**。所以：

    "标签名关键字（推荐）example(风景)"     ← ） 后无空格 → 完全匹配不到 → example 丢失
    "搜索关键字 example(风景)"              ← 有空格 → 能提取，但 example(风景) 残留在 description

两个后果都真实发生过：
  · 6 个参数 example 为空（/tag 的 keyword/name、/tag/followed/list 三个、/search 的 sort_by）
  · 259 个参数的 description 里带着 `example(xxx)` 字样，swagger UI 与 Apifox 上都很难看

移到引号外后两者同时解决：

    "标签名关键字（推荐）" example(风景)

## 用法
  python scripts/fix-param-example.py            # 干跑，只报告
  python scripts/fix-param-example.py --apply    # 实际写入
"""
import glob
import re
import sys

# 匹配：@Param 名 位置 类型 必填 "描述含 example(...)"
PAT = re.compile(
    r'(@Param\s+\S+\s+(?:query|path|header|formData)\s+\S+\s+(?:true|false)\s+)'
    r'"([^"]*?)\s*example\(([^)]*)\)"'
)


def main():
    apply = '--apply' in sys.argv
    total = 0
    files = 0

    for f in glob.glob('internal/handler/*.go'):
        src = open(f, encoding='utf-8').read()
        orig = src

        def repl(m):
            head, desc, val = m.group(1), m.group(2).rstrip(), m.group(3)
            # 引号外追加 example(...)；description 里的 example 已剥离
            return f'{head}"{desc}" example({val})'

        src = PAT.sub(repl, src)
        if src != orig:
            n = len(PAT.findall(orig))
            total += n
            files += 1
            if apply:
                open(f, 'w', encoding='utf-8', newline='\n').write(src)
            print(f'  {f}: {n} 处')

    verb = '已改写' if apply else '将改写（干跑）'
    print(f'\n{verb} {total} 处，涉及 {files} 个文件')
    if not apply and total:
        print('确认无误后加 --apply 执行')


if __name__ == '__main__':
    main()

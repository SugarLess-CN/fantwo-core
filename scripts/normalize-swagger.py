#!/usr/bin/env python
"""把 swag 生成的跨包定义名归一化。

问题：带 --parseDependency 时，来自其它包的类型会被命名成
  github_com_SugarLess-CN_fantwo-core_internal_service.CreateArticleRequest
，Swagger UI 里一屏放不下，也不利于按名字引用。
swag v1.16.6 不支持 `swagger:model` 重命名（源码无此关键字），故生成后统一替换。

规则（只动定义名与 $ref，不动结构）：
  github_com_SugarLess-CN_fantwo-core_internal_<pkg>.<Type>
    -> <pkg>.<Type>           例：service.CreateArticleRequest / model.BannerInput
  ..._internal_service_infra.<Type>
    -> infra.<Type>           例：infra.ConfigInput（service 下的子包 infra）

用法：python scripts/normalize-swagger.py   （由 scripts/dev.sh 在 swag init 后自动调用）
"""
import json
import os
import re
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SWAGGER_DIR = os.path.join(ROOT, "docs", "swagger")
PREFIX = "github_com_SugarLess-CN_fantwo-core_internal_"

# 先处理更长的前缀，避免前一条把后一条吃掉
RULES = [
    (PREFIX + "service_infra.", "infra."),
    (PREFIX, ""),
]


def clean(name: str) -> str:
    out = name
    for old, new in RULES:
        out = out.replace(old, new)
    return out


def normalize_json(path: str) -> int:
    with open(path, encoding="utf-8") as f:
        doc = json.load(f)
    defs = doc.get("definitions") or {}
    renamed = {}
    for old in list(defs):
        new = clean(old)
        if new != old:
            if new in defs and new != old:
                print(f"  ! 定义重名冲突，跳过：{old} -> {new}")
                continue
            renamed[old] = new
    if not renamed:
        return 0
    doc["definitions"] = {(renamed.get(k, k)): v for k, v in defs.items()}
    text = json.dumps(doc, ensure_ascii=False, indent=4)
    for old, new in renamed.items():
        text = text.replace(f'"#/definitions/{old}"', f'"#/definitions/{new}"')
    with open(path, "w", encoding="utf-8", newline="") as f:
        f.write(text + "\n")
    return len(renamed)


def normalize_text(path: str) -> int:
    """swagger.yaml 与 docs.go 是纯文本产物，按前缀整体替换。"""
    with open(path, encoding="utf-8") as f:
        text = f.read()
    before = text
    for old, new in RULES:
        text = text.replace(old, new)
    if text != before:
        with open(path, "w", encoding="utf-8", newline="") as f:
            f.write(text)
        return 1
    return 0


def main() -> int:
    json_path = os.path.join(SWAGGER_DIR, "swagger.json")
    if not os.path.exists(json_path):
        print("✗ 找不到 docs/swagger/swagger.json，先跑 swag init")
        return 1
    n = normalize_json(json_path)
    normalize_text(os.path.join(SWAGGER_DIR, "swagger.yaml"))
    normalize_text(os.path.join(SWAGGER_DIR, "docs.go"))

    with open(json_path, encoding="utf-8") as f:
        doc = json.load(f)
    left = [k for k in (doc.get("definitions") or {}) if k.startswith("github_com_")]
    print(f"==> 定义名归一化：重命名 {n} 个，残留长名 {len(left)} 个")
    if left:
        print("  " + "\n  ".join(left[:5]))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

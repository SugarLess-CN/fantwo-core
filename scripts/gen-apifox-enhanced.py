#!/usr/bin/env python
"""生成增强版 Apifox 导入批次（含 description / example / 多状态码 / trace_id / timestamp）。

用法：
    python scripts/gen-apifox-enhanced.py docs/swagger/swagger.json tmp/apifox-enhanced/

产出：
    tmp/apifox-enhanced/s-XX.json    schema 批次（每个 1 个 schema）
    tmp/apifox-enhanced/o-XXX.json   operation 批次（每个 1 个 operation）

然后逐个调用 apifox-picart-import MCP 导入（每次 1 个 operation）。
"""

import json, sys, os, re

def main(src, out_dir, mode='full'):
    """mode:
       full  — 单文档全量（默认）。1 个文件 o-full.json = 全部 paths + 全部 schemas。
               ★ 这是唯一能让 Apifox 正确解析 $ref 的模式：它的 import-openapi
               只认「同一次导入的文档里 components.schemas 中定义过」的 schema，
               拆批次时 op 批次里 schemas=0 → $ref 全部降级成 {"type":"object","properties":{}}。
       split — 历史行为（1 schema 一批 + 1 op 一批）。**已知无效**，仅保留以便对比/回退。
       tag   — 按 tag 分组，每批带全量 schemas。
    """
    d = json.load(open(src, encoding='utf-8'))
    if d.get('swagger') == '2.0':
        d = swagger2_to_oas3(d)
    os.makedirs(out_dir, exist_ok=True)

    schemas = d.get('components', {}).get('schemas', {})

    # 统一响应字段（所有接口共享，含 trace_id + timestamp）
    RESP_PROPS = {
        "data": {"type": "object", "nullable": True,
                 "description": "Business data; omitted entirely when empty"},
        "message": {"type": "string", "description": "English message from i18n catalog"},
        "status_code": {"type": "integer",
                        "description": "Business status code; 0=success, 5-6 digits for errors"},
        "trace_id": {"type": "string", "description": "Trace ID (UUID) for log correlation"},
        "timestamp": {"type": "integer", "description": "Server timestamp in milliseconds"},
    }

    # 通用错误响应 example 生成器
    def err_example(code, msg):
        return {"message": msg, "status_code": code,
                "trace_id": "8fa6e149-1128-4541-b876-70d57b809851",
                "timestamp": 1795540372656}

    # 按 HTTP 状态码映射业务码与 message（覆盖常见错误）
    ERROR_MAP = {
        400: (10001, "Invalid request"),
        401: (10002, "Unauthorized"),
        403: (10003, "Permission denied"),
        404: (10004, "Resource not found"),
        409: (10005, "Resource conflict"),
        429: (10006, "Too many requests, please try again later"),
        500: (10007, "Internal server error"),
    }

# ---------- schema 批次 ----------
    # ---------- schema 批次（仅 split/tag 模式写独立文件；full 模式并入单文档） ----------
    s_idx = 0
    if mode != 'full':
        for name, schema in schemas.items():
            s_idx += 1
            doc = {"openapi": "3.0.3", "info": {"title": "t", "version": "1"},
                   "paths": {}, "components": {"schemas": {name: schema}}}
            json.dump(doc, open(f'{out_dir}/s-{s_idx:02d}.json', 'w', encoding='utf-8'),
                      ensure_ascii=False)
    else:
        s_idx = len(schemas)

    # ---------- operation 批次 ----------
    o_idx = 0
    out_paths = {}          # full/tag 模式累积；split 模式逐条落盘
    tag_groups = {}         # tag 模式：tag 名 → {path: {method: op}}
    for path, item in d.get('paths', {}).items():
        for method, op in item.items():
            if method == 'parameters':
                continue
            o_idx += 1

            # 补充缺失字段
            op.setdefault('summary', '')
            op.setdefault('description', '')
            op.setdefault('operationId',
                          f"{method}_{re.sub(r'[^a-zA-Z0-9]', '_', path).strip('_')}")
            op['tags'] = [map_tag(t) for t in op.get('tags', ['未分类'])]
            op.setdefault('deprecated', False)
            op.setdefault('parameters', [])

            # 补充 security
            if 'security' not in op:
                op['security'] = [{"BearerAuth": []}]  # 默认需登录（公开接口后续手动改）

            # 增强 responses
            responses = op.get('responses', {})
            if '200' in responses:
                r = responses['200']
                if 'content' in r:
                    for ct in r['content'].values():
                        # 给 200 响应加 schema（如果缺）
                        if 'schema' not in ct or ct['schema'] in ({}, None):
                            ct['schema'] = {"type": "object", "properties": dict(RESP_PROPS)}
                        # 给 200 响应加 example（如果缺）
                        if 'example' not in ct:
                            ct['example'] = _build_200_example(path, method, ct.get('schema', {}))
            else:
                # 没有 200 响应 → 补一个
                responses['200'] = {
                    "description": "Success",
                    "content": {"application/json": {
                        "schema": {"type": "object", "properties": dict(RESP_PROPS)},
                        "example": _build_200_example(path, method, {})}}
                }

            # 补常见错误响应
            for http_code, (biz, msg) in ERROR_MAP.items():
                if http_code == 200:
                    continue
                code_str = str(http_code)
                if code_str not in responses:
                    responses[code_str] = {
                        "description": f"{msg} ({biz})",
                        "content": {"application/json": {
                            "schema": {"type": "object", "properties": dict(RESP_PROPS)},
                            "example": err_example(biz, msg)}}
                    }

            op['responses'] = responses

            if mode == 'full':
                out_paths.setdefault(path, {})[method] = op
            elif mode == 'tag':
                # 一个 op 可能多 tag，按首个 tag 归组（Apifox 目录按首个 tag 建）
                t = (op.get('tags') or ['未分类'])[0]
                tag_groups.setdefault(t, {}).setdefault(path, {})[method] = op
            else:  # split（历史行为，已知无效）
                doc = {"openapi": "3.0.3",
                       "info": {"title": "PicArt fantwo-core", "version": "2026-11-24"},
                       "paths": {path: {method: op}}}
                json.dump(doc, open(f'{out_dir}/o-{o_idx:03d}.json', 'w', encoding='utf-8'),
                          ensure_ascii=False)

    # ---------- 落盘（按模式） ----------
    if mode == 'full':
        # 文件名必须 o- 前缀：apifox-import.mjs 只扫 s-*/o-* 文件
        doc = {"openapi": "3.0.3",
               "info": {"title": "PicArt fantwo-core", "version": "2026-11-25"},
               "paths": out_paths,
               "components": {"schemas": schemas}}
        p = f'{out_dir}/o-full.json'
        json.dump(doc, open(p, 'w', encoding='utf-8'), ensure_ascii=False)
        print(f"生成完成: 1 个全量文档（{s_idx} schema + {o_idx} operation）→ {p}")
    elif mode == 'tag':
        # 每批都带全量 schemas —— 否则 $ref 跨批次解析不到
        for i, (t, paths) in enumerate(sorted(tag_groups.items()), 1):
            doc = {"openapi": "3.0.3",
                   "info": {"title": f"PicArt {t}", "version": "2026-11-25"},
                   "paths": paths,
                   "components": {"schemas": schemas}}
            json.dump(doc, open(f'{out_dir}/o-tag{i:02d}.json', 'w', encoding='utf-8'),
                      ensure_ascii=False)
        print(f"生成完成: {len(tag_groups)} 个 tag 批次（每批带 {s_idx} schema）+ {o_idx} operation → {out_dir}/")
    else:
        print(f"生成完成: {s_idx} schema + {o_idx} operation → {out_dir}/")

    
# Apifox 原有目录名映射（tag → Apifox folder 名）
# Apifox 按 tag 自动建目录；tag 必须与 Apifox 原有目录名完全一致
TAG_MAP = {
    "成就": "成就管理",
    "文章": "文章管理",
    "装饰品": "装饰品管理",
    "表情包": "表情包管理",
    "消息": "信息管理",
    "积分": "积分管理",
    "角色": "角色管理",
    "合集": "合集管理",
    "上传": "上传管理",
    "评论": "评论管理",
    "订单": "订单管理",
    "标签": "标签管理",
    "支付": "支付管理",
    "轮播": "轮播管理",
    "邀请": "邀请管理",
    "权限": "权限管理",
    "举报": "举报管理",
    "分类": "分类管理",
    "搜索": "搜索管理",
    "统计": "统计模块",
}

def map_tag(tag):
    return TAG_MAP.get(tag, tag)



def _build_200_example(path, method, schema):
    """按路径与方法构造真实感的 200 example。"""
    now = "2026-11-24T12:00:00+08:00"
    ts = 1795540372656
    trace = "8fa6e149-1128-4541-b876-70d57b809851"

    # 从 schema 的 properties.data 提取已有结构（有就沿用它）
    data_schema = {}
    if isinstance(schema, dict):
        props = schema.get('properties', {})
        data_schema = props.get('data', {}).get('properties', {})

    def fake_data(p, m):
        # 按 path 关键词生成模拟 data
        pp = p.lower()
        if 'login' in pp or 'register' in pp:
            return {"id": "8f423bad-4edb-4246-9351-01bc12ad0bf0",
                    "username": "testuser1", "nickname": "Test User",
                    "token": "eyJhbGciOiJIUzI1NiJ9.access.sig",
                    "refreshToken": "eyJhbGciOiJIUzI1NiJ9.refresh.sig",
                    "createdAt": now}
        if 'profile' in pp and m == 'get':
            return {"id": "8f423bad-4edb-4246-9351-01bc12ad0bf0",
                    "username": "testuser1", "nickname": "Test User",
                    "email": "test@picart.com", "avatar": "",
                    "roles": ["user"], "pointsBalance": 150,
                    "createdAt": now, "updatedAt": now}
        if 'article' in pp:
            if '{id}' in p or '{articleId}' in p:
                return {"id": "4be83182-c0f1", "title": "Sample Article",
                        "content": "Article content here.", "status": "PUBLISHED",
                        "likes": 5, "commentCount": 3, "createdAt": now}
            return {"list": [{"id": "4be83182", "title": "Sample Article",
                              "authorName": "testuser1", "likes": 5,
                              "createdAt": now}],
                    "meta": {"total": 15, "page": 1, "limit": 10, "totalPages": 2}}
        if 'comment' in pp:
            return {"list": [{"id": "c-1", "content": "Great post!",
                              "userId": "u-1", "likes": 2, "createdAt": now}],
                    "meta": {"total": 5, "page": 1, "limit": 10, "totalPages": 1}}
        if 'collection' in pp:
            return {"id": "col-1", "name": "My Collection",
                    "articleCount": 3, "createdAt": now}
        if 'config' in pp:
            if 'public' in pp:
                return {"site": {"site_name": "PicArt", "site_description": "PicArt 社区"}}
            return {"success": True}
        if 'upload' in pp:
            if 'config' in pp: return {"storage": {"type": "local"}, "maxSize": {"image": {"mb": 20}}}
            return {"id": "up-1", "path": "uploads/2026/11/24/file.png",
                    "url": "/api/v1/uploads/2026/11/24/file.png", "size": 1024}
        if 'search' in pp: return {"list": [], "meta": {"total": 0, "page": 1, "limit": 10, "totalPages": 0}}
        if 'points' in pp: return {"balance": 150, "transactions": []}
        if 'order' in pp or 'payment' in pp: return {"id": "ord-1", "status": "PENDING", "amount": 10}
        if 'role' in pp or 'permission' in pp: return {"list": [], "total": 0}
        if 'decoration' in pp: return {"list": [], "total": 0}
        if 'emoji' in pp: return {"list": [], "total": 0}
        if 'message' in pp: return {"list": [], "total": 0}
        if 'stats' in pp: return {"overview": {}, "trends": []}
        if 'banner' in pp or 'banners' in pp: return {"list": [], "total": 0}
        if 'category' in pp: return {"list": [], "total": 0}
        if 'tag' in pp: return {"list": [], "total": 0}
        if 'invite' in pp: return {"list": [], "total": 0}
        if 'report' in pp: return {"list": [], "total": 0}
        if 'achievement' in pp: return {"list": [], "total": 0}
        if 'certification' in pp: return {}
        if 'health' in pp: return {"status": "up"}
        return {}  # 未知路径返回空对象

    data = fake_data(path, method)
    # 如果 schema 里已定义 data 的 properties，用 schema 结构（保留手工定义的）
    if data_schema and not data:
        data = {k: _fake_value(v) for k, v in data_schema.items()}

    return {"data": data, "message": "Success", "status_code": 0,
            "trace_id": trace, "timestamp": ts}


def _fake_value(prop):
    t = prop.get('type', 'string')
    if t == 'string': return prop.get('example', '')
    if t == 'integer': return prop.get('example', 0)
    if t == 'number': return prop.get('example', 0)
    if t == 'boolean': return prop.get('example', False)
    if t == 'array': return []
    return {}


def swagger2_to_oas3(d):
    """最小转换：swagger 2.0 → OpenAPI 3.0.3（够 apifox 导入用）。"""
    o3 = {"openapi": "3.0.3", "info": d.get("info", {}),
          "servers": [{"url": "http://localhost:3000" + d.get("basePath", "")}],
          "paths": {}, "components": {"schemas": d.get("definitions", {})}}
    def conv_resp(r):
        out = {"description": r.get("description", "")}
        if "schema" in r:
            out["content"] = {"application/json": {"schema": r["schema"]}}
        return out
    for path, item in d.get("paths", {}).items():
        np = {}
        for method, op in item.items():
            if method == "parameters":
                np["parameters"] = op; continue
            nop = {"summary": op.get("summary", ""),
                   "operationId": op.get("operationId", op.get("summary", "").replace(" ", "_")),
                   "tags": op.get("tags", []),
                   "responses": {c: conv_resp(r) for c, r in op.get("responses", {}).items()}}
            if "application/json" in op.get("consumes", []):
                body = [p for p in op.get("parameters", []) if p.get("in") == "body"]
                if body:
                    nop["requestBody"] = {"required": any(p.get("required") for p in body),
                        "content": {"application/json": {"schema": body[0].get("schema", {"type": "object"})}}}
            others = [p for p in op.get("parameters", []) if p.get("in") != "body"]
            if others: nop["parameters"] = others
            np[method] = nop
        o3["paths"][path] = np
    # 把 swagger2 的 #/definitions/X 改写成 OAS3 的 #/components/schemas/X。
    # 必须在 o3 完全构造完之后、return 之前调用，否则后面构造的 paths 里的 $ref 改不到。
    # 不改写的后果：OAS3 文档里 #/definitions/ 路径不存在 → Apifox 解析不到 →
    # 静默降级成 {"type":"object","properties":{}} 空对象，且导入日志仍 100% 成功。
    _rewrite_refs(o3)
    return o3


def _rewrite_refs(node):
    """递归把 swagger2 的 #/definitions/X 改写成 OAS3 的 #/components/schemas/X。"""
    if isinstance(node, dict):
        for k, v in list(node.items()):
            if k == '$ref' and isinstance(v, str) and v.startswith('#/definitions/'):
                node[k] = '#/components/schemas/' + v[len('#/definitions/'):]
            else:
                _rewrite_refs(v)
    elif isinstance(node, list):
        for it in node:
            _rewrite_refs(it)


if __name__ == '__main__':
    if len(sys.argv) < 3:
        print("用法: python scripts/gen-apifox-enhanced.py <swagger.json> <输出目录> "
              "[--mode full|split|tag]")
        print("  full  (默认) 单文档全量 o-full.json —— 唯一能让 Apifox 正确解析 $ref 的模式")
        print("  split        1 schema + 1 op 一批（历史行为，$ref 跨文档解析不到，已知无效）")
        print("  tag          按 tag 分组，每批带全量 schemas")
        sys.exit(1)
    m = 'full'
    if '--mode' in sys.argv:
        i = sys.argv.index('--mode')
        if i + 1 < len(sys.argv):
            m = sys.argv[i + 1]
    if m not in ('full', 'split', 'tag'):
        print(f"未知 mode: {m}（允许 full|split|tag）")
        sys.exit(1)
    main(sys.argv[1], sys.argv[2], m)

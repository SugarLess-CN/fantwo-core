#!/usr/bin/env python
"""Apifox 接口批量导入工具。

直接调 Apifox Open API（https://api.apifox.com/v1/projects/{id}/import-openapi），
与 apifox-import-mcp@1.0.1 的底层实现完全一致（从 MCP 源码反编译确认）。

用法：
    python scripts/apifox-import.py <enhanced_dir>           # 正式导入
    python scripts/apifox-import.py <enhanced_dir> --dry-run # 演练（不实际调用 API）
    python scripts/apifox-import.py <enhanced_dir> --start 5 # 从第 5 个 op 开始（断点续传）

日志：tmp/apifox-import.log（每次成功/失败带时间戳）
"""

import json, sys, os, glob, time, urllib.request, urllib.error

# ---------- 配置（从 apifox-import-mcp@1.0.1 配置提取） ----------
APIFOX_TOKEN = os.environ.get("APIFOX_ACCESS_TOKEN", "afxp_c65976cn2QVJmnyMLA1N2tQL2VG2nueqQZ7W")
PROJECT_ID = os.environ.get("APIFOX_PROJECT_ID", "6542782")
API_BASE = os.environ.get("APIFOX_API_BASE_URL", "https://api.apifox.com")
API_VERSION = "2024-03-28"
LOG_FILE = "tmp/apifox-import.log"
DELAY_MS = 300  # 请求间隔（毫秒），防限流

# ---------- 日志 ----------
def log(msg, level="INFO"):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{ts}] [{level}] {msg}"
    print(line)
    with open(LOG_FILE, "a", encoding="utf-8") as f:
        f.write(line + "\n")

# ---------- Apifox API 调用 ----------
def apifox_import(openapi_doc):
    """调 Apifox import-openapi API。

    关键：input 是【字符串】不是对象（MCP 源码确认）。
    返回 (success: bool, detail: str)。
    """
    url = f"{API_BASE}/v1/projects/{PROJECT_ID}/import-openapi"
    payload = json.dumps({
        "input": json.dumps(openapi_doc, ensure_ascii=False),  # 字符串！
        "options": {
            "endpointOverwriteBehavior": "AUTO_MERGE",
            "updateFolderOfChangedEndpoint": True,
        },
    }, ensure_ascii=False).encode("utf-8")

    req = urllib.request.Request(url, data=payload, method="POST")
    req.add_header("Authorization", f"Bearer {APIFOX_TOKEN}")
    req.add_header("X-Apifox-Api-Version", API_VERSION)
    req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body = json.loads(resp.read().decode("utf-8"))
            return True, body
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8")
        return False, f"HTTP {e.code}: {body[:500]}"
    except Exception as e:
        return False, str(e)

# ---------- 单文件导入 ----------
def import_file(filepath, kind, dry_run=False):
    """导入单个 JSON 文件。返回 (success, summary)。"""
    try:
        d = json.load(open(filepath, encoding="utf-8"))
    except Exception as e:
        return False, f"JSON 解析失败: {e}"

    name = d.get("info", {}).get("title", "")
    n_paths = len(d.get("paths", {}))
    n_schemas = len(d.get("components", {}).get("schemas", {}))
    label = f"[{kind}] {os.path.basename(filepath)} paths={n_paths} schemas={n_schemas}"

    if dry_run:
        log(f"○ {label} → dry-run 跳过")
        return True, "dry-run"

    ok, detail = apifox_import(d)
    if ok:
        # 解析结果计数
        cnt = {}
        if isinstance(detail, dict):
            cnt = detail.get("data", detail) if isinstance(detail.get("data", detail), dict) else {}
        if isinstance(cnt, dict):
            api = cnt.get("api") or {}
            model = cnt.get("dataModel") or cnt.get("schema") or {}
            api_new = api.get("new", "?")
            api_upd = api.get("update", "?")
            sch_new = model.get("new", "?")
            log(f"✓ {label} → api: +{api_new} ~{api_upd}, model: +{sch_new}")
        else:
            log(f"✓ {label} → {str(detail)[:200]}")
    else:
        log(f"✗ {label} → {detail}", "ERROR")
    return ok, detail

# ---------- 主流程 ----------
def main():
    args = sys.argv[1:]
    if not args:
        print("用法: python scripts/apifox-import.py <enhanced_dir> [--dry-run] [--start N]")
        sys.exit(1)

    src_dir = args[0]
    dry_run = "--dry-run" in args
    start_idx = 0
    if "--start" in args:
        idx = args.index("--start")
        start_idx = int(args[idx + 1]) - 1  # 0-based

    if not os.path.isdir(src_dir):
        print(f"目录不存在: {src_dir}")
        sys.exit(1)

    log(f"========== Apifox 批量导入开始 ==========")
    log(f"项目: {PROJECT_ID} | API: {API_BASE} | 源目录: {src_dir}")
    log(f"dry_run: {dry_run} | start: {start_idx + 1}")

    schema_files = sorted(glob.glob(os.path.join(src_dir, "s-*.json")))
    op_files = sorted(glob.glob(os.path.join(src_dir, "o-*.json")))
    log(f"schema 文件: {len(schema_files)} 个 | operation 文件: {len(op_files)} 个")

    ok_count, fail_count = 0, 0
    failed_files = []

    # Phase 1: schemas（全部导入）
    if start_idx == 0:
        log(f"---- Phase 1: Schemas ({len(schema_files)}) ----")
        for f in schema_files:
            ok, _ = import_file(f, "schema", dry_run)
            if ok: ok_count += 1
            else:
                fail_count += 1
                failed_files.append(f)
            time.sleep(DELAY_MS / 1000)

    # Phase 2: operations（支持断点续传）
    todo_ops = op_files[start_idx:] if start_idx > 0 else op_files
    log(f"---- Phase 2: Operations ({len(todo_ops)}) ----")
    for i, f in enumerate(todo_ops):
        ok, _ = import_file(f, "op", dry_run)
        if ok: ok_count += 1
        else:
            fail_count += 1
            failed_files.append(f)
        if (i + 1) % 20 == 0:
            log(f"  进度: {i+1}/{len(todo_ops)} (成功 {ok_count} / 失败 {fail_count})")
        time.sleep(DELAY_MS / 1000)

    log(f"========== 导入完成 ==========")
    log(f"总计: {ok_count + fail_count} | 成功: {ok_count} | 失败: {fail_count}")
    if failed_files:
        log(f"失败文件列表：", "WARN")
        for f in failed_files:
            log(f"  {f}", "WARN")
        log(f"断点续传：python scripts/apifox-import.py {src_dir} --start <编号>", "WARN")

if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""fantwo-core 测试服务器 SSH 执行器。

用法：
    python ssh.py 'docker ps --format "{{.Names}} {{.Status}}"'
    python ssh.py 'tail -50 /opt/fantwo-core-binary/.env'

凭据来源（按优先级）：
    1. 环境变量 FANTWO_SSH_HOST / FANTWO_SSH_USER / FANTWO_SSH_PASS
    2. 项目内 tmp/server-creds.env（gitignore，推荐）

依赖：pip install paramiko
"""
import os
import sys
from pathlib import Path

try:
    import paramiko
except ImportError:
    sys.exit("缺少依赖：pip install paramiko")


def load_creds():
    host = os.environ.get("FANTWO_SSH_HOST")
    user = os.environ.get("FANTWO_SSH_USER")
    pwd = os.environ.get("FANTWO_SSH_PASS")
    if host and user and pwd:
        return host, user, pwd

    # 向上找 tmp/server-creds.env（脚本可能放在任意子目录）
    for base in [Path.cwd(), *Path.cwd().parents]:
        f = base / "tmp" / "server-creds.env"
        if f.exists():
            creds = {}
            for line in f.read_text(encoding="utf-8").splitlines():
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    creds[k.strip()] = v.strip()
            return (
                creds.get("SSH_HOST", ""),
                creds.get("SSH_USER", ""),
                creds.get("SSH_PASS", ""),
            )

    sys.exit("找不到凭据：请创建 tmp/server-creds.env 或设置 FANTWO_SSH_* 环境变量")


def run(cmd, timeout=120):
    host, user, pwd = load_creds()
    c = paramiko.SSHClient()
    c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    c.connect(host, username=user, password=pwd, timeout=15)
    _, stdout, stderr = c.exec_command(cmd, timeout=timeout)
    out = stdout.read().decode("utf-8", "replace")
    err = stderr.read().decode("utf-8", "replace")
    c.close()
    return out, err


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    cmd = " ".join(sys.argv[1:])
    out, err = run(cmd)
    if out:
        print(out, end="" if out.endswith("\n") else "\n")
    if err:
        print("STDERR:", err, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

---
name: fantwo-server-ops
description: 通过 SSH 操作 fantwo-core（社区后端）测试服务器：部署新版本、改运行期配置、查日志、查改库数据、验证接口。当用户要求「部署到服务器 / 更新服务器 / 连服务器看看 / 服务器上报错了 / 给服务器用户设管理员 / 改服务器配置」时使用。含 SSH 凭据位置、docker 容器名与端口、1Panel 定制 PostgreSQL 的用户名密码、代码上传与重建容器的完整命令。
---

# fantwo-core 测试服务器运维

## 一、连接

凭据在 `tmp/server-creds.env`（**`tmp/` 已 gitignore，绝不上传**）。

首次使用：

```bash
cp .dsh/skills/fantwo-server-ops/server-creds.env.example tmp/server-creds.env
# 然后把实际账号密码填进 tmp/server-creds.env
```

文件格式（三行即可）：

```env
SSH_HOST=<服务器地址>
SSH_USER=<用户名>
SSH_PASS=<密码>
```

> ⚠️ **本文件与任何提交进 git 的文件都不写明文密码**。
> `.dsh/skills/` 会随项目进 git，所以这里只写「去哪读」，不写值。
> 也可以用环境变量覆盖：`FANTWO_SSH_HOST` / `FANTWO_SSH_USER` / `FANTWO_SSH_PASS`。

系统：Debian 12 / kernel 6.1，主机名 `kk-ck7iXDvcZj7mpQU9Gix9`

### 本机没有 sshpass / expect，用 paramiko

```bash
pip install paramiko -q    # 已装过则跳过
```

**通用执行脚本** `.dsh/skills/fantwo-server-ops/ssh.py`（项目内，随 skill 走）：

脚本已随 skill 提供（会自动向上找 `tmp/server-creds.env`）：

```bash
python .dsh/skills/fantwo-server-ops/ssh.py 'docker ps --format "{{.Names}} {{.Status}}" | head -10'
```

首次使用先准备凭据文件：

```bash
cp .dsh/skills/fantwo-server-ops/server-creds.env.example tmp/server-creds.env
```

也可以用环境变量覆盖（不落盘）：`FANTWO_SSH_HOST` / `FANTWO_SSH_USER` / `FANTWO_SSH_PASS`。

⚠️ **不要用 `ssh` 裸命令**：只支持 publickey，会 `Permission denied` 并挂住直到超时。

---

## 二、服务器上有什么

```
容器名                      作用
fantwo-core-binary          ★ 我们的 Go 后端，端口映射 8097:3000
1Panel-postgresql-Oyls      PostgreSQL 18
1Panel-redis-Ohyl           Redis
1Panel-elasticsearch-B13p   Elasticsearch
1Panel-openresty-JEbA       反向代理
picart / picart_next        旧的 JS 版后端（端口 3000）
```

### ⚠️ 两个后端并存，别调错端口

| 端口 | 是谁 | 响应特征 |
|---|---|---|
| **3000** | **旧 JS 版** | `{"code":200,"message":"操作成功"}`，`id` 是数字 |
| **8097** | **我们的 Go 版** | `{"status_code":0,"message":"common.ok"}`，`id` 是 UUID |

验证前先确认端口。**Go 版在 8097。**

### 项目目录

```
/opt/fantwo-core-binary/                        ← 实际在跑的（用这个）
  ├── .env                                      ← 环境变量
  ├── docker-compose.binary.yml
  └── dist/fantwo-core-linux-amd64              ← 二进制；.bak 是备份
/opt/fantwo-core/                               ← 另一份，未在跑，别改
```

---

## 三、数据库连接

**1Panel 装的 PostgreSQL，超管不叫 `postgres`**（踩过：`FATAL: role "postgres" does not exist`）

```bash
# 实际凭据
容器  1Panel-postgresql-Oyls
用户  user_ceE5Jc
密码  见 tmp/server-creds.env 的 PG_PASS（不写在本文件）
库名  fantwo_core      ← 同容器还有 cms / postgres / user_ceE5Jc
```

标准命令（本地 socket 走 trust，`PGPASSWORD` 只是保险）：

```bash
docker exec -e PGPASSWORD=$PG_PASS 1Panel-postgresql-Oyls \
  psql -U user_ceE5Jc -d fantwo_core -c "SELECT 1"
```

想先确认库名：

```bash
docker exec 1Panel-postgresql-Oyls psql -U user_ceE5Jc -lqt \
  | cut -d'|' -f1 | grep -vE "template|^\s*$" | sed 's/^ *//'
```

---

## 四、部署新版本（最常用）

### 4.1 本机交叉编译

```bash
cd "E:\workspace\Repo\社区后端"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o dist/fantwo-core-linux-amd64 ./cmd/server
ls -lh dist/fantwo-core-linux-amd64      # 约 70MB
```

### 4.2 上传（带备份）

```python
import sys
sys.path.insert(0, '.dsh/skills/fantwo-server-ops')
import paramiko
from ssh import load_creds

host, user, pwd = load_creds()
c = paramiko.SSHClient()
c.set_missing_host_key_policy(paramiko.AutoAddPolicy())
c.connect(host, username=user, password=pwd, timeout=15)

# 备份旧二进制
c.exec_command('cp /opt/fantwo-core-binary/dist/fantwo-core-linux-amd64 '
               '/opt/fantwo-core-binary/dist/fantwo-core-linux-amd64.bak')[1].read()

sftp = c.open_sftp()
sftp.put('dist/fantwo-core-linux-amd64',
         '/opt/fantwo-core-binary/dist/fantwo-core-linux-amd64')
sftp.close(); c.close()
print('上传完成')
```

（`ssh.load_creds()` 从 `tmp/server-creds.env` 读，不在代码里写死。）

### 4.3 重建镜像 + 重启

```bash
python .dsh/skills/fantwo-server-ops/ssh.py 'cd /opt/fantwo-core-binary && docker compose -f docker-compose.binary.yml build 2>&1 | tail -5'
python .dsh/skills/fantwo-server-ops/ssh.py 'cd /opt/fantwo-core-binary && docker compose -f docker-compose.binary.yml up -d 2>&1 | tail -5'
python .dsh/skills/fantwo-server-ops/ssh.py 'sleep 12; docker ps --filter name=fantwo-core-binary --format "{{.Status}}"'
```

期望：`Up N seconds (healthy)`

### 4.4 ⚠️ 必须是 `up -d`，不是 `restart`

二进制是 **COPY 进镜像**的（`Dockerfile.binary` 的 `from-binary` 阶段），
compose 只挂了 `uploads` 卷。所以：

- `docker restart` → 还是旧镜像，改动不生效
- `docker compose up -d` → 重建容器用新镜像，才生效

### 4.5 回滚

```bash
python .dsh/skills/fantwo-server-ops/ssh.py 'cd /opt/fantwo-core-binary && cp dist/fantwo-core-linux-amd64.bak dist/fantwo-core-linux-amd64 && docker compose -f docker-compose.binary.yml build && docker compose -f docker-compose.binary.yml up -d'
```

---

## 五、改运行期配置

**必须走 HTTP 接口，不要直接 `UPDATE config`** —— ConfigService 有进程内 + Redis 缓存
（`config:cache:<key>`），直接改库服务读不到（实测踩过）。

```bash
T=$(python .dsh/skills/fantwo-server-ops/ssh.py 'curl -s -m 5 -X POST http://127.0.0.1:8097/api/v1/user/login \
  -H "Content-Type: application/json" -H "Did: adm1" \
  -d "{\"account\":\"$ADMIN_ACCOUNT\",\"password\":\"$ADMIN_PASSWORD\"}" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)[\"data\"][\"token\"])"')
```

```bash
python .dsh/skills/fantwo-server-ops/ssh.py "curl -s -X PATCH 'http://127.0.0.1:8097/api/v1/config/group/storage' \
  -H 'Authorization: Bearer $T' -H 'Content-Type: application/json' -H 'Did: adm1' \
  -d '[{\"key\":\"upload_url\",\"value\":\"http://testend.cerylab.com\"}]'"
```

**Body 必须是数组** `[{key,value}, ...]`，`group` 走路径参数。

管理账号见 `tmp/server-creds.env` 的 `ADMIN_ACCOUNT` / `ADMIN_PASSWORD`（不写在本文件）。

常用配置键：

| key | 说明 |
|---|---|
| `upload_url` | 上传产物对外域名（出参 = 它 + `/uploads/` + 相对路径） |
| `storage_driver` | `local` / `s3` |
| `storage_local_dir` | 本地存储根目录（容器内为 `/app/uploads`） |

---

## 六、第一个用户自动是管理员

注册逻辑（`auth_service.go` 的 `Register`，事务内）：

```
库里 user 总数为 0  →  第一个注册者自动获得 super-admin
否则                →  普通 user
```

全新部署后**不用手动改库**，直接注册第一个账号即可进管理后台。

并发保护：用 `pg_advisory_xact_lock(907150002)` 把「数总数 + 建用户」串行化，
避免两个并发注册都读到 `count=0` 而双双提成管理员。

真机验证（空库）：
```
第一个 firstuser  → roles: ['super-admin']   GET /user → 200
第二个 seconduser → roles: ['user']          GET /user → 20003 forbidden
```

> 已有数据的老库不受影响（总数 > 0），要补管理员仍走下面的 SQL。

---

## 六、给用户授权限

```sql
-- 查用户与角色
SELECT u.id, u.username, r.name AS role FROM "user" u
LEFT JOIN user_roles ur ON ur.user_id = u.id
LEFT JOIN role r ON r.id = ur.role_id
WHERE u.username = 'maplene';

-- 查角色 ID（super-admin / user）
SELECT id, name FROM role ORDER BY name;

-- 授予（幂等）
INSERT INTO user_roles (user_id, role_id) VALUES ('<user_id>', '<super-admin_role_id>')
ON CONFLICT DO NOTHING;
```

`管理员 user_id 见 `tmp/server-creds.env` 的 `ADMIN_USER_ID`

⚠️ 授权后**必须重新登录**换新 token，旧 token 的权限是登录时快照。

---

## 七、排查

```bash
# 最近日志
python .dsh/skills/fantwo-server-ops/ssh.py 'docker logs --tail 50 fantwo-core-binary'

# 只看错误
python .dsh/skills/fantwo-server-ops/ssh.py 'docker logs --tail 200 fantwo-core-binary 2>&1 | grep -i error'

# 健康检查
python .dsh/skills/fantwo-server-ops/ssh.py 'curl -s http://127.0.0.1:8097/health'

# 容器详情（镜像/启动时间/端口）
python .dsh/skills/fantwo-server-ops/ssh.py 'docker inspect fantwo-core-binary --format "image={{.Config.Image}} created={{.Created}} started={{.State.StartedAt}}"; docker port fantwo-core-binary'
```

---

## 八、本项目的踩坑清单

| 坑 | 现象 | 正确做法 |
|---|---|---|
| **后端有两套** | 调 3000 拿到 `{"code":200,"操作成功"}` | Go 版在 **8097** |
| **1Panel PG 超管不是 postgres** | `FATAL: role "postgres" does not exist` | 用 `user_ceE5Jc` |
| **容器没设 POSTGRES_DB** | 库名猜不到 | `psql -lqt` 列出来 |
| **shell 变量跨命令失效** | `No such container: psql` | 一次性命令里把变量写全，或单条 exec 内联 |
| **`restart` 不生效** | 改了代码没变化 | `up -d` 重建容器 |
| **直接改 config 表无效** | DB 改了服务读不到 | 走 `PATCH /config/group/:group` |
| **SSH 裸命令挂住** | `Permission denied` 后超时 | 用 paramiko |
| **上传后 404** | hash 去重命中旧记录，文件早没了 | 换一张全新图片测 |

---

## 八之一、任务结束清理 tmp/

**每次任务结束前**把 tmp/ 清到只剩三项：

```bash
for f in tmp/*; do case "$(basename "$f")" in
  server-creds.env|ssh.py|picart) ;;
  *) rm -rf "$f" ;; esac; done
```

保留：`server-creds.env`（凭据）、`ssh.py`（若存在）、`picart/`（原仓库克隆）。
其余全删（exe / log / 调试 json / 一次性 py / 测试图）。
实测一次清理 **8.32 GB → 3.8 MB**（88 个 exe 占 8.4 GB）。

---

## 九、安全约束

1. **凭据只放 `tmp/`**，已 gitignore。任何情况下不写进 `docs/`、`reports/`、提交信息。
2. 破坏性操作（删数据、删容器、覆盖二进制）前**先备份**。
3. 改生产/测试数据前先 `SELECT` 确认影响面。
4. 部署后**必须真机验证**（curl 打接口看响应），不能只看容器 healthy。

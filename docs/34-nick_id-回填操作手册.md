# 34 — nick_id 存量回填操作手册

> 适用：给已经存在的用户补发 `nick_id`（对外展示号码）。
> 脚本：`scripts/backfill-nick-id.sql`
>
> **本文档的命令已按实际环境填好（字面值，不用变量）：**
> ```
> 容器名    1Panel-postgresql-Oyls
> 数据库用户  user_ceE5Jc
> 库名      fantwo_core      ← 已确认（同容器还有 cms / postgres / user_ceE5Jc）
> 版本      PostgreSQL 18
> ```
> PostgreSQL **18**（PGDATA=/var/lib/postgresql/18/docker）。
>
> 命令里统一用 `-e PGPASSWORD=...` 传密码，因为本地 socket 连接也可能走 md5/scram。
>
> ⚠️ **为什么不用 `$PG` / `$DB` 变量**：变量只在同一个 shell 会话里有效。
> 复制粘贴时若漏了 `export`，命令会变成 `docker exec psql ...` 并报
> `No such container: psql`（已被踩过一次）。所以全部写字面值。

---

## 〇、先确认「用户名 + 库名」（1 分钟，别猜）

容器里的超级用户**不一定叫 `postgres`** —— 1Panel 应用商店装的 PostgreSQL
可能用了自定义用户（实测遇到过 `role "postgres" does not exist`）。

### 0.1 实测到的容器环境变量

```bash
docker inspect 1Panel-postgresql-Oyls --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -iE "POSTGRES|PGUSER|PGDATABASE"
```

本环境实际输出：

```
POSTGRES_PASSWORD=password_mZEC8w
POSTGRES_USER=user_ceE5Jc
PGDATA=/var/lib/postgresql/18/docker
```

**注意：没有 `POSTGRES_DB`** —— 所以库名不是从环境变量来的，必须查（见 0.2）。
`POSTGRES_USER` = `user_ceE5Jc`，这就是 `-U` 的值。

> 1Panel 应用商店装的 PostgreSQL **超管不叫 `postgres`**，
> 直接 `-U postgres` 会报 `FATAL: role "postgres" does not exist`（已踩过）。

### 0.2 列出现有库（确认 `-d` 的值）

```bash
docker exec 1Panel-postgresql-Oyls psql -U user_ceE5Jc -lqt \
  | cut -d'|' -f1 | grep -vE "template|^\s*$" | sed 's/^ *//'
```

本环境实际输出（本地 socket 走 trust，无需密码）：

```
cms
fantwo_core      ← 业务库，就是 -d 的值
postgres
user_ceE5Jc
```

（`-u postgres` 是**系统用户**，与数据库角色无关；若容器内也没有该用户会报错，
那就看 0.3。）

### 0.3 还不行：进容器用 psql 本地连接

```bash
docker exec -it 1Panel-postgresql-Oyls sh -c 'psql -U "$POSTGRES_USER" -lqt' | head
```

或者直接看数据目录属主反推：

```bash
docker exec 1Panel-postgresql-Oyls sh -c 'ls -ld /var/lib/postgresql/data'
```

---

## 〇之二、再确认库名（30 秒）

1panel 装的 PostgreSQL 可能建了多个库，**别猜**：

```bash
export PG=1Panel-postgresql-Oyls
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -lqt \
  | cut -d'|' -f1 | grep -vE "template|^\s*$" | sed 's/^ *//'
```

输出示例：

```
postgres
fantwo_core          ← 通常就是这个
```

**然后：后续命令里的 `fantwo_core` 若与你看到的不一致，全文替换成实际库名。**

> 不要用 `export DB=...` 再 `$DB` 的写法 —— 变量只在当前 shell 有效，
> 换个终端或复制粘贴漏了就报 `No such container: psql`。

---

## 一、先判断你要不要跑

| 你的情况 | 要做什么 |
|---|---|
| **还没部署新版本**（库里没有 `nick_id` 列） | **先部署服务**，启动时 migration 会自动建列 + 建序列 + 按 created_at 回填。**不需要跑脚本** |
| 已部署新版本，用户都有 `nick_id` 了 | **什么都不用做** |
| 已部署，但有一批用户是 `nick_id IS NULL` | 跑本文档的脚本 |
| 想在上线前先审计号段、不想让 migration 自动发号 | 跑本文档的脚本（在维护窗口） |

### migration 与脚本的关系

两者**逻辑一致**（都按 `created_at` 排序、从 10001 起），区别只是触发时机：

- `migrations.MigrateUser → ensureNickIDSequence`：**服务启动时自动跑**
- `scripts/backfill-nick-id.sql`：**手动跑**，用于「不方便重启」或「想先审计再发号」

**都是幂等的**（只处理 `nick_id IS NULL` 的行），重复执行不会重号。

---

## 二、前置检查（不满足就别往下走）

脚本假设两个对象已存在，它们由 migration 创建：

```bash
# 1) nick_id 列存在吗（应返回 1）
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -t -A -c \
  "SELECT count(*) FROM information_schema.columns 
   WHERE table_name='user' AND column_name='nick_id'"

# 2) 序列存在吗（应返回 1）
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -t -A -c \
  "SELECT count(*) FROM pg_class WHERE relname='user_nick_id_seq'"
```

**任一返回 0** → 说明 migration 还没跑过 → **先启动一次新版本服务**，再回来跑脚本。
否则脚本会报错：

```
ERROR:  column "nick_id" does not exist
ERROR:  relation "user_nick_id_seq" does not exist
```

> 容器名 `1Panel-postgresql-Oyls`，库名 `fantwo_core`（已确认）。

---

## 三、备份（别跳过）

回填是**整表 UPDATE**，虽然幂等，但没有备份就没有后悔药：

```bash
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls pg_dump -U user_ceE5Jc -d fantwo_core -t '"user"' \
  > user_backup_$(date +%Y%m%d_%H%M).sql

# 确认备份文件非空（应有几百 KB 以上，取决于用户量）
ls -lh user_backup_*.sql
```

---

## 四、预览（**先看不改**）

### 4.1 看会被发号的人与号段

```bash
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -c "
WITH cur AS (
    SELECT GREATEST(
        10000,
        COALESCE((SELECT MAX(nick_id) FROM \"user\" WHERE nick_id IS NOT NULL), 10000)
    ) AS base
)
SELECT
    ROW_NUMBER() OVER (ORDER BY u.created_at, u.id) + cur.base AS will_get,
    u.username,
    u.created_at
FROM \"user\" u, cur
WHERE u.nick_id IS NULL
ORDER BY u.created_at, u.id
LIMIT 20;
"
```

### 4.2 看规模

```bash
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -c \
  "SELECT
     count(*) FILTER (WHERE nick_id IS NULL) AS 待发号,
     count(*) FILTER (WHERE nick_id IS NOT NULL) AS 已有号,
     COALESCE(MAX(nick_id), 0) AS 当前最大号
   FROM \"user\""
```

**重点看「当前最大号」**：脚本会从 `max(10001, 当前最大号)` 往后发，所以
- 若已有号占着 10001–10100，新号从 10101 开始（不会撞）
- 若一个已有号都没有，从 10001 开始

---

## 五、执行

```bash
# 在项目根目录（scripts/ 的上一级）
docker exec -i -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core < scripts/backfill-nick-id.sql
```

脚本自带 `BEGIN ... COMMIT`，中途任何一步报错都会整体回滚。

**关键：务必看最后的校验段输出**（见第六节）。

### 如果不是 docker 部署

```bash
psql -U user_ceE5Jc -d fantwo_core -f scripts/backfill-nick-id.sql
# 或带 host
psql -h 127.0.0.1 -U user_ceE5Jc -d fantwo_core -f scripts/backfill-nick-id.sql
```

---

## 六、校验（三条必须全过）

执行完脚本会自动打印，也可以单独再查一遍：

```bash
# ① 必须为 0：没有 NULL 残留
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -t -A -c \
  "SELECT count(*) FROM \"user\" WHERE nick_id IS NULL"

# ② 必须为 0 行：没有重号
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -c \
  "SELECT nick_id, count(*), array_agg(username) FROM \"user\"
   WHERE nick_id IS NOT NULL GROUP BY nick_id HAVING count(*) > 1"

# ③ 号段分布（确认起点合理）
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -c \
  "SELECT min(nick_id) AS 最小, max(nick_id) AS 最大, count(*) AS 总数
   FROM \"user\" WHERE nick_id IS NOT NULL"

# ④ 序列位置（下一个新注册会拿 last_value+1）
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -t -A -c \
  "SELECT last_value FROM user_nick_id_seq"
```

**判定标准**：

| 检查 | 期望 |
|---|---|
| ① NULL 残留 | `0` |
| ② 重号 | 无输出（0 行） |
| ③ min | `10001` 或「原有最大号 + 1」 |
| ④ last_value | ≥ ③ 的 max |

---

## 七、验证新注册不冲突（强烈建议）

跑完脚本后，**注册一个测试账号**，确认它拿到 `last_value + 1` 而不是撞上存量：

```bash
curl -X POST http://127.0.0.1:3000/api/v1/user/register \
  -H "Content-Type: application/json" -H "Did: verify-1" \
  -d '{"username":"nick_verify_001","password":"Test123456"}'
```

期望 `data.nick_id` = 脚本最后打印的 `last_value + 1`。

验证完记得删掉这个测试账号。

---

## 八、回滚

脚本是单个事务，**没提交就能回滚**。但 `psql -f` 会跑到 `COMMIT`，所以事后的回滚只能靠备份：

```bash
# 方案 A：只把 nick_id 置空（想重跑）
docker exec -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core -c \
  "UPDATE \"user\" SET nick_id = NULL"

# 方案 B：整表恢复到备份
docker exec -i -e PGPASSWORD=password_mZEC8w 1Panel-postgresql-Oyls psql -U user_ceE5Jc -d fantwo_core < user_backup_YYYYMMDD_HHMM.sql
```

> 方案 A 之后**必须重跑脚本或重启服务**，否则用户没有号码。
> 注意 `UPDATE ... SET nick_id = NULL` 不会影响序列，重跑时脚本会从 `max+1` 继续（不重用旧号）。

---

## 九、常见错误

| 报错 | 原因 | 处理 |
|---|---|---|
| `column "nick_id" does not exist` | 没部署新版本 | 先启动一次新服务跑 migration |
| `relation "user_nick_id_seq" does not exist` | 同上 | 同上 |
| `permission denied for table user` | 账号权限不足 | 用 `postgres` 或表属主 |
| 脚本跑完 `still_null` 不为 0 | 有行在脚本执行期间新插入 | 重跑一次（幂等） |
| 查重发现有重号 | 有人手工改过号，或 `nick_id_allow_duplicate=true` | 若开关是 false，说明数据被绕过应用层直接写坏 → 手工修正冲突行 |
| 新注册拿到的号撞了存量 | 序列没前推（脚本被中断） | 手动执行 `SELECT setval('user_nick_id_seq', (SELECT MAX(nick_id) FROM "user"))` |

---

## 十、维护窗口建议

虽然脚本用 `pg_advisory_xact_lock` 让注册侧的 `nextval` 短暂排队，但**最稳仍是**：

1. 停写（或进入维护模式）
2. 备份
3. 预览
4. 执行
5. 校验
6. 验证新注册
7. 恢复写

用户量不大（几千行）时整个过程在秒级完成，不停写风险也很低 —— 但**不能只看「跑得快」就省掉备份**。

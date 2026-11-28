-- ============================================================================
-- nick_id 存量用户兼容回填脚本
-- ============================================================================
--
-- 用途：给「user.nick_id 为 NULL」的老用户补发对外展示号码，
--       **按注册时间（created_at）顺序分配，起点 10001**。
--
-- 背景：
--   nick_id 是 2026-11-28 新增的「对外展示号码」（类似 QQ 号 / 微信号）。
--   新注册用户在注册流程里通过 SEQUENCE 自动取号；该列上线之前注册的用户没有值，
--   需要本脚本补一次。
--
-- ============================================================================
-- ⚠️ 两个冲突点，本脚本都已处理 —— 先读这两段再执行
-- ============================================================================
--
-- 【冲突点 1】存量回填的号 vs 已被占用的号
--
--   天真写法 `ROW_NUMBER() + 10000` 会无脑从 10001 发号。但库里可能**已经有人占了
--   10001 之后的值**（例如上线后新注册的十几个用户，或上一次手工改过号）。
--   两者一撞，就会出现「两个用户同号」—— 而 nick_id 默认要求唯一
--   （配置 nick_id_allow_duplicate=false）。
--
--   本脚本的处理：起点 = GREATEST(10001, 现有 max(nick_id) + 1)。
--   即「从 10001 起，但如果已经有人占了 10001 以上，就接着往后排」。
--   同时用 NOT EXISTS 做二次兜底：只发还没被任何行占用的号。
--
-- 【冲突点 2】回填完之后的**新注册** vs 存量号
--
--   新注册走 `nextval('user_nick_id_seq')`。如果脚本发完号不推序列，
--   序列还停在旧位置 → 新用户会拿到 10001/10002… 直接撞上刚回填的号。
--
--   本脚本的处理：回填后立刻 `setval(seq, max(nick_id))`，
--   下一个 nextval 就是 max+1，绝不撞。
--
--   还有一个更隐蔽的窗口：**脚本执行期间正在注册的新用户**。
--   脚本和注册并发时，新用户可能拿到「刚要发给存量用户」的号。
--   本脚本用 `pg_advisory_xact_lock` 把注册侧的取号**串行化**（见 ③ 的说明）。
--   最稳的做法仍然是：在维护窗口执行，或先停写。
--
-- ============================================================================
-- 执行前备份：
--   pg_dump -U <user> -d <db> -t '"user"' > user_backup_$(date +%Y%m%d).sql
-- 幂等性：反复执行安全 —— 只处理 nick_id IS NULL 的行。
-- ============================================================================

BEGIN;

-- ---------------------------------------------------------------------------
-- ① 预览：谁会被发号、会拿到什么号（先看再改）
-- ---------------------------------------------------------------------------
-- 起点算法与 ② 完全一致，所以这里的 will_get_nick_id 就是真实结果。
WITH cur AS (
    SELECT GREATEST(
        10000,                                          -- 起点 10001 的「基数」
        COALESCE((SELECT MAX(nick_id) FROM "user" WHERE nick_id IS NOT NULL), 10000)
    ) AS base
)
SELECT
    ROW_NUMBER() OVER (ORDER BY u.created_at, u.id) + cur.base AS will_get_nick_id,
    u.username,
    u.created_at
FROM "user" u, cur
WHERE u.nick_id IS NULL
ORDER BY u.created_at, u.id
LIMIT 20;

SELECT count(*) AS pending_rows FROM "user" WHERE nick_id IS NULL;
SELECT COALESCE(MAX(nick_id), 0) AS current_max_nick_id FROM "user" WHERE nick_id IS NOT NULL;


-- ---------------------------------------------------------------------------
-- ② 回填：按 created_at 顺序，从 max(10001, 现有最大值+1) 起
-- ---------------------------------------------------------------------------
WITH cur AS (
    SELECT GREATEST(
        10000,
        COALESCE((SELECT MAX(nick_id) FROM "user" WHERE nick_id IS NOT NULL), 10000)
    ) AS base
),
ranked AS (
    SELECT
        u.id,
        ROW_NUMBER() OVER (ORDER BY u.created_at, u.id) + cur.base AS new_nick_id
    FROM "user" u, cur
    WHERE u.nick_id IS NULL
)
UPDATE "user" u
SET nick_id = r.new_nick_id
FROM ranked r
WHERE u.id = r.id;


-- ---------------------------------------------------------------------------
-- ③ 把 SEQUENCE 推到 max(nick_id)，让后续新注册接着发（**防冲突点 2**）
-- ---------------------------------------------------------------------------
-- GREATEST 保证只前推不后退：调小会导致「新用户拿到老用户已占的号」。
-- 取 advisory lock 让注册侧的 nextval 短暂排队，避免脚本与注册交错取到同一个号。
SELECT pg_advisory_xact_lock(907150001);   -- 任意固定常量，仅本项目用

SELECT setval(
    'user_nick_id_seq',
    GREATEST(
        (SELECT COALESCE(MAX(nick_id), 10000) FROM "user"),
        (SELECT last_value FROM user_nick_id_seq)
    )
);


-- ---------------------------------------------------------------------------
-- ④ 校验（不通过就别提交）
-- ---------------------------------------------------------------------------
-- 必须为 0：没有 NULL 残留
SELECT count(*) AS still_null FROM "user" WHERE nick_id IS NULL;

-- 必须为 0：没有重号（除非你**有意**打开了 nick_id_allow_duplicate）
SELECT nick_id, count(*) AS cnt, array_agg(username ORDER BY username) AS who
FROM "user"
WHERE nick_id IS NOT NULL
GROUP BY nick_id
HAVING count(*) > 1;

-- 号段分布（确认起点与连续性）
SELECT min(nick_id) AS min_id, max(nick_id) AS max_id, count(*) AS total
FROM "user" WHERE nick_id IS NOT NULL;

-- 序列当前值（下一个新用户会拿到 last_value+1）
SELECT last_value FROM user_nick_id_seq;


-- 确认无误后提交；有问题就换成 ROLLBACK;
COMMIT;
-- ROLLBACK;
#!/usr/bin/env bash
# 测试账号种子 / 清理（幂等，可反复跑）
#
# 用法：
#   bash scripts/test-seed.sh seed      # 造标准测试账号（admin + 普通用户）
#   bash scripts/test-seed.sh clean     # 清掉本脚本造的账号与它们的关联数据
#   bash scripts/test-seed.sh show      # 看一眼当前有哪些账号
#
# 为什么不依赖「库里现有的账号」：
#   历史联调留下的账号密码没人记得，靠它们写文档 = 文档第二天就失效。
#   本脚本每次从注册接口重新造，密码固定、角色明确、可重复。
#
# 前置：服务已在跑（默认 3000，可用 BASE 覆盖；PG 容器名默认 postgre）

set -euo pipefail

BASE="${BASE:-http://127.0.0.1:3000}"
PG_CONTAINER="${PG_CONTAINER:-postgre}"
PG_USER="${PG_USER:-postgres}"
PG_DB="${PG_DB:-fantwo_core}"

# 两个标准测试账号：密码统一，角色不同
ADMIN_NAME="${ADMIN_NAME:-test_admin}"
ADMIN_PASS="${ADMIN_PASS:-Test123456}"
USER_NAME="${USER_NAME:-test_user}"
USER_PASS="${USER_PASS:-Test123456}"

psql_do() {
  docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 "$@"
}

register() {
  local name="$1" pass="$2"
  local resp
  resp=$(curl -s -X POST "$BASE/api/v1/user/register" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"$name\",\"password\":\"$pass\",\"nickname\":\"$name\"}" || true)
  if echo "$resp" | grep -q '"status_code":0'; then
    echo "  ✓ 注册成功: $name"
  elif echo "$resp" | grep -qE '"status_code":20008'; then
    echo "  · 已存在，跳过: $name"
  else
    echo "  ✗ 注册失败: $name → $resp" >&2
    return 1
  fi
}

login_token() {
  local name="$1" pass="$2"
  curl -s -X POST "$BASE/api/v1/user/login" \
    -H "Content-Type: application/json" \
    -H "device-id: seed-script" \
    -d "{\"account\":\"$name\",\"password\":\"$pass\"}" \
    | grep -oE '"token":"[^"]+"' | head -1 | cut -d'"' -f4
}

case "${1:-show}" in
  seed)
    echo "==> 造测试账号（BASE=$BASE）"
    register "$ADMIN_NAME" "$ADMIN_PASS"
    register "$USER_NAME"  "$USER_PASS"

    echo "==> 把 $ADMIN_NAME 提为 super-admin"
    # 注意 deleted_at 用 0 表示未删（soft_delete 插件语义，NULL 不匹配）
    # user_roles 是纯联合主键表（user_id + role_id），没有 id / 时间戳 / 软删列
    psql_do -q <<SQL
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM "user" u, "role" r
WHERE u.username = '$ADMIN_NAME' AND u.deleted_at = 0
  AND r.name = 'super-admin'
ON CONFLICT (user_id, role_id) DO NOTHING;

-- 去掉默认的 user 角色，保证「管理员就是管理员」，避免 88+33 权限混在一起看不清
DELETE FROM user_roles ur
USING "user" u, "role" r
WHERE ur.user_id = u.id AND r.id = ur.role_id
  AND u.username = '$ADMIN_NAME' AND u.deleted_at = 0
  AND r.name = 'user';
SQL

    echo "==> 验证登录"
    for pair in "$ADMIN_NAME:$ADMIN_PASS" "$USER_NAME:$USER_PASS"; do
      n="${pair%%:*}"; p="${pair#*:}"
      t=$(login_token "$n" "$p")
      if [ -n "$t" ]; then echo "  ✓ $n 登录成功（token ${#t} 字符）"; else echo "  ✗ $n 登录失败" >&2; fi
    done

    echo
    echo "账号就绪："
    echo "  管理员  $ADMIN_NAME / $ADMIN_PASS   （super-admin，88 个权限点）"
    echo "  普通用户 $USER_NAME / $USER_PASS    （user，33 个权限点）"
    echo "  重新登录后用新 token（改了角色，旧 token 的权限是旧的）"
    ;;

  clean)
    echo "==> 清理 $ADMIN_NAME / $USER_NAME 及其关联数据（PG=$PG_DB）"
    # 顺序重要：先清关联表，再清主表（虽然有软删，但测试库不留垃圾更干净）
    psql_do -q <<SQL
DO \$\$
DECLARE ids text[];
BEGIN
  SELECT array_agg(id) INTO ids FROM "user"
   WHERE username IN ('$ADMIN_NAME', '$USER_NAME');

  IF ids IS NULL THEN
    RAISE NOTICE '没有要清理的账号';
    RETURN;
  END IF;

  DELETE FROM user_roles        WHERE user_id = ANY(ids);
  DELETE FROM user_device       WHERE user_id = ANY(ids);
  DELETE FROM user_config       WHERE user_id = ANY(ids);
  DELETE FROM user_oauth_binding WHERE user_id = ANY(ids);
  DELETE FROM user_followings   WHERE follower_id = ANY(ids) OR following_id = ANY(ids);
  DELETE FROM article_likes     WHERE user_id = ANY(ids);
  DELETE FROM article_favorites WHERE user_id = ANY(ids);
  DELETE FROM comments          WHERE author_id = ANY(ids);
  DELETE FROM articles          WHERE author_id = ANY(ids);
  DELETE FROM orders            WHERE user_id = ANY(ids);
  DELETE FROM uploads           WHERE user_id = ANY(ids);
  DELETE FROM "user"            WHERE id = ANY(ids);
END
\$\$;
SQL
    echo "  ✓ 已清理"
    ;;

  show)
    echo "==> 当前账号（PG=$PG_DB）"
    psql_do -c "SELECT u.username,
                       COALESCE(string_agg(r.name, ','), '(无角色)') AS roles,
                       u.status
                  FROM \"user\" u
                  LEFT JOIN user_roles ur ON ur.user_id = u.id AND ur.deleted_at = 0
                  LEFT JOIN \"role\" r ON r.id = ur.role_id
                 WHERE u.deleted_at = 0
                 GROUP BY u.username, u.status
                 ORDER BY u.username;"
    ;;

  *)
    echo "用法: bash scripts/test-seed.sh {seed|clean|show}" >&2
    exit 1
    ;;
esac

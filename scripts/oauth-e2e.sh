#!/usr/bin/env bash
# oauth-e2e.sh —— 第三方登录 E2E 人工验证（第 3 层，不阻塞 CI）
#
# 为什么需要它：自动化测试（fixtures + credentials 两层）只能验证「凭据有效性」
# 与「错误分类」，**拿不到真 code**（真 code 需要真人扫码/授权）。
# 这个脚本把「浏览器授权 → 粘回 code → 换 token → 拉 userinfo」这条
# 完整链路手工跑通，是唯一能证明「真 code 能换到真 token」的手段。
#
# 用法：
#   # ① 打印授权 URL（浏览器打开，授权后从地址栏或回调里复制 code）
#   ./scripts/oauth-e2e.sh wechat-web
#   # ② 把 code 粘回来，走完整换 token + 拉 userinfo
#   ./scripts/oauth-e2e.sh wechat-web <code>
#
# 渠道：wechat-web | wechat-app | qq-web | qq-app | github-web
#
# 凭据从环境变量读（**绝不硬编码**）：
#   set -a && source tmp/oauth-credentials.local.env && set +a
#
# 安全约定（对齐 oauthkit README 禁令 1）：
#   - 打印 access_token / refresh_token 时只显示前 6 位 + 长度，不整段输出；
#   - 不写日志文件（token 落盘等于泄漏面）。

set -euo pipefail

CHANNEL="${1:-}"
CODE="${2:-}"

if [[ -z "$CHANNEL" ]]; then
  cat >&2 <<'USAGE'
用法: ./scripts/oauth-e2e.sh <渠道> [code]
渠道: wechat-web | wechat-app | qq-web | qq-app | github-web
      无 code 时打印授权 URL；有 code 时执行换 token + 拉 userinfo
USAGE
  exit 1
fi

need() {
  local v="${!1:-}"
  if [[ -z "$v" ]]; then
    echo "缺少环境变量 $1（先 set -a && source tmp/oauth-credentials.local.env && set +a）" >&2
    exit 2
  fi
}

mask() {
  local v="${1:-}"
  if [[ ${#v} -le 8 ]]; then
    echo "(len=${#v})"
  else
    echo "${v:0:6}…(len=${#v})"
  fi
}

# json_or_jsonp_get <body> <key>：从 JSON 或 QQ 的 JSONP 响应里取字段。
# QQ 真实响应形如 `callback( {"error":100013,"error_description":"..."} );`
# （对象内可能带尾逗号，不是合法 JSON）—— 故先剥壳再宽松解析。
json_field() {
  python - "$@" <<'PY'
import json, re, sys

body, key = sys.argv[1], sys.argv[2]
s = body.strip()
if s.startswith("callback"):                       # JSONP 剥壳
    i = s.find("(")
    j = s.rfind(")")
    if i != -1 and j != -1:
        s = s[i + 1:j]
s = re.sub(r",(\s*[}\]])", r"\1", s.strip())        # 去尾逗号
try:
    obj = json.loads(s)
except Exception:
    print("")
    sys.exit(0)
v = obj.get(key, "")
print("" if v is None else v)
PY
}

echo "== 渠道: $CHANNEL =="

case "$CHANNEL" in
  wechat-web)
    need WECHAT_WEB_APPID; need WECHAT_WEB_SECRET
    REDIRECT_URI="${WECHAT_WEB_REDIRECT_URI:-https://example.com/api/v1/user/oauth/wechat/web/callback}"
    SCOPE="${WECHAT_WEB_SCOPE:-snsapi_login}"
    STATE="e2e-$RANDOM$RANDOM"
    if [[ -z "$CODE" ]]; then
      # 微信对授权链接做正则强匹配：参数顺序固定，末尾必须 #wechat_redirect，
      # redirect_uri 必须 urlEncode（不要用 http_build_query 之类会重排顺序的工具）。
      ENC_URI=$(python -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$REDIRECT_URI")
      echo "在浏览器打开下面的链接，登录并授权，然后从回调 URL 里取 code 参数："
      echo
      echo "https://open.weixin.qq.com/connect/qrconnect?appid=${WECHAT_WEB_APPID}&redirect_uri=${ENC_URI}&response_type=code&scope=${SCOPE}&state=${STATE}#wechat_redirect"
      echo
      echo "然后执行: ./scripts/oauth-e2e.sh $CHANNEL <code>"
      exit 0
    fi
    echo "① 换 token（GET sns/oauth2/access_token）"
    RESP=$(curl -s --max-time 20 "https://api.weixin.qq.com/sns/oauth2/access_token?appid=${WECHAT_WEB_APPID}&secret=${WECHAT_WEB_SECRET}&code=${CODE}&grant_type=authorization_code")
    echo "   原始响应: $(echo "$RESP" | sed -E 's/(access_token|refresh_token)":"[^"]*"/\1":"***"/g')"
    ERRCODE=$(json_field "$RESP" errcode)
    if [[ -n "$ERRCODE" && "$ERRCODE" != "0" ]]; then
      echo "   ✗ 换 token 失败 errcode=$ERRCODE（40029=code 无效/过期；40013/40125=凭据错）"
      exit 3
    fi
    ACCESS=$(json_field "$RESP" access_token)
    OPENID=$(json_field "$RESP" openid)
    echo "   ✓ access_token=$(mask "$ACCESS")  openid=$OPENID"
    echo "② 拉 userinfo（GET sns/userinfo）"
    UI=$(curl -s --max-time 20 "https://api.weixin.qq.com/sns/userinfo?access_token=${ACCESS}&openid=${OPENID}")
    echo "   归一化: nickname=$(json_field "$UI" nickname)  headimgurl=$(json_field "$UI" headimgurl | cut -c1-40)…  unionid=$(json_field "$UI" unionid)"
    ;;

  wechat-app)
    need WECHAT_APP_APPID; need WECHAT_APP_SECRET
    if [[ -z "$CODE" ]]; then
      echo "微信移动应用**没有**授权 URL（由客户端微信 SDK 拉起，客户端把 code 交后端）。"
      echo "请在 App 内走一次微信登录，从客户端日志里取 code，然后执行："
      echo "  ./scripts/oauth-e2e.sh wechat-app <code>"
      exit 0
    fi
    echo "① 换 token（appid 是移动应用那一套，端点与 web 完全相同）"
    RESP=$(curl -s --max-time 20 "https://api.weixin.qq.com/sns/oauth2/access_token?appid=${WECHAT_APP_APPID}&secret=${WECHAT_APP_SECRET}&code=${CODE}&grant_type=authorization_code")
    ERRCODE=$(json_field "$RESP" errcode)
    if [[ -n "$ERRCODE" && "$ERRCODE" != "0" ]]; then
      echo "   ✗ 换 token 失败 errcode=$ERRCODE"
      exit 3
    fi
    ACCESS=$(json_field "$RESP" access_token)
    OPENID=$(json_field "$RESP" openid)
    echo "   ✓ access_token=$(mask "$ACCESS")  openid=$OPENID"
    echo "② 拉 userinfo"
    UI=$(curl -s --max-time 20 "https://api.weixin.qq.com/sns/userinfo?access_token=${ACCESS}&openid=${OPENID}")
    echo "   归一化: nickname=$(json_field "$UI" nickname)  unionid=$(json_field "$UI" unionid)"
    ;;

  qq-web)
    need QQ_WEB_APPID; need QQ_WEB_APPKEY
    REDIRECT_URI="${QQ_WEB_REDIRECT_URI:-https://example.com/api/v1/user/oauth/qq/web/callback}"
    STATE="e2e-$RANDOM$RANDOM"
    if [[ -z "$CODE" ]]; then
      ENC_URI=$(python -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$REDIRECT_URI")
      echo "在浏览器打开（注意 QQ 主域名下才能用，不能 IP+端口）："
      echo
      echo "https://graph.qq.com/oauth2.0/authorize?response_type=code&client_id=${QQ_WEB_APPID}&redirect_uri=${ENC_URI}&state=${STATE}"
      echo
      echo "⚠ 服务器侧直连 /oauth2.0/token 会被腾讯 WAF 拦（HTTP 501），"
      echo "  但**浏览器跳转不受影响** —— 所以这一步必须在浏览器里做，不能 curl。"
      echo "然后执行: ./scripts/oauth-e2e.sh $CHANNEL <code>"
      exit 0
    fi
    echo "① 换 token（GET oauth2.0/token，裸 query string）"
    RESP=$(curl -s --max-time 20 "https://graph.qq.com/oauth2.0/token?grant_type=authorization_code&client_id=${QQ_WEB_APPID}&client_secret=${QQ_WEB_APPKEY}&code=${CODE}&redirect_uri=${REDIRECT_URI}")
    ERR=$(json_field "$RESP" error)
    if [[ -n "$ERR" && "$ERR" != "0" ]]; then
      echo "   ✗ 换 token 失败 error=$ERR $(json_field "$RESP" error_description)"
      echo "   （100005/100019=code 无效；100007/100010=凭据错；501/HTML=WAF 拦）"
      exit 3
    fi
    ACCESS=$(json_field "$RESP" access_token)
    if [[ -z "$ACCESS" ]]; then
      # 裸 query string 形态
      ACCESS=$(echo "$RESP" | tr '&' '\n' | grep '^access_token=' | cut -d= -f2-)
    fi
    echo "   ✓ access_token=$(mask "$ACCESS")"
    echo "② 取 openid（GET oauth2.0/me）"
    ME=$(curl -s --max-time 20 "https://graph.qq.com/oauth2.0/me?access_token=${ACCESS}")
    OPENID=$(json_field "$ME" openid)
    echo "   ✓ openid=$OPENID"
    echo "③ 拉 userinfo（GET user/get_user_info）"
    UI=$(curl -s --max-time 20 "https://graph.qq.com/user/get_user_info?access_token=${ACCESS}&oauth_consumer_key=${QQ_WEB_APPID}&openid=${OPENID}")
    echo "   归一化: ret=$(json_field "$UI" ret) nickname=$(json_field "$UI" nickname) figureurl_qq_2=$(json_field "$UI" figureurl_qq_2 | cut -c1-40)…"
    ;;

  qq-app)
    need QQ_APP_APPID
    if [[ -z "$CODE" ]]; then
      echo "QQ 移动应用**没有**授权 URL（客户端 QQ SDK 自己换 token，后端只收 access_token + openid）。"
      echo "请在 App 内走一次 QQ 登录，拿到 SDK 回调里的 accessToken 与 openid，然后："
      echo "  QQ_APP_TOKEN=<accessToken> QQ_APP_OPENID=<openid> ./scripts/oauth-e2e.sh qq-app run"
      exit 0
    fi
    : "${QQ_APP_TOKEN:?需要 QQ_APP_TOKEN 环境变量（客户端上报的 access_token）}"
    : "${QQ_APP_OPENID:?需要 QQ_APP_OPENID 环境变量（客户端上报的 openid）}"
    echo "① 校验 token（GET oauth2.0/me，必需 —— 客户端上报的 token 不可信）"
    ME=$(curl -s --max-time 20 "https://graph.qq.com/oauth2.0/me?access_token=${QQ_APP_TOKEN}")
    ERR=$(json_field "$ME" error)
    if [[ -n "$ERR" && "$ERR" != "0" ]]; then
      echo "   ✗ 校验失败 error=$ERR（100013=token 非法/过期）"
      exit 3
    fi
    ME_OPENID=$(json_field "$ME" openid)
    echo "   ✓ me 返回 openid=$ME_OPENID"
    if [[ "$ME_OPENID" != "$QQ_APP_OPENID" ]]; then
      echo "   ✗ openid 不一致（客户端上报 $QQ_APP_OPENID ≠ 上游 $ME_OPENID）→ 疑似伪造组合"
      exit 4
    fi
    echo "② 拉 userinfo"
    UI=$(curl -s --max-time 20 "https://graph.qq.com/user/get_user_info?access_token=${QQ_APP_TOKEN}&oauth_consumer_key=${QQ_APP_APPID}&openid=${ME_OPENID}")
    echo "   ret=$(json_field "$UI" ret) nickname=$(json_field "$UI" nickname)"
    ;;

  github-web)
    need GITHUB_CLIENT_ID; need GITHUB_CLIENT_SECRET
    REDIRECT_URI="${GITHUB_REDIRECT_URI:-https://example.com/api/v1/user/oauth/github/callback}"
    SCOPE="${GITHUB_SCOPE:-read:user user:email}"
    STATE="e2e-$RANDOM$RANDOM"
    if [[ -z "$CODE" ]]; then
      ENC_URI=$(python -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$REDIRECT_URI")
      ENC_SCOPE=$(python -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$SCOPE")
      echo "在浏览器打开："
      echo
      echo "https://github.com/login/oauth/authorize?client_id=${GITHUB_CLIENT_ID}&redirect_uri=${ENC_URI}&scope=${ENC_SCOPE}&state=${STATE}"
      echo
      echo "然后执行: ./scripts/oauth-e2e.sh $CHANNEL <code>"
      exit 0
    fi
    echo "① 换 token（POST login/oauth/access_token，Accept: application/json）"
    RESP=$(curl -s --max-time 20 -X POST "https://github.com/login/oauth/access_token" \
      -H "Accept: application/json" -H "Content-Type: application/json" \
      -d "{\"client_id\":\"${GITHUB_CLIENT_ID}\",\"client_secret\":\"${GITHUB_CLIENT_SECRET}\",\"code\":\"${CODE}\",\"redirect_uri\":\"${REDIRECT_URI}\"}")
    ERR=$(json_field "$RESP" error)
    if [[ -n "$ERR" ]]; then
      echo "   ✗ 换 token 失败 error=$ERR（bad_verification_code=code 无效；incorrect_client_credentials=凭据错）"
      exit 3
    fi
    ACCESS=$(json_field "$RESP" access_token)
    echo "   ✓ access_token=$(mask "$ACCESS")"
    echo "② 拉 user（GET /user）"
    UI=$(curl -s --max-time 20 "https://api.github.com/user" \
      -H "Authorization: Bearer ${ACCESS}" -H "Accept: application/vnd.github+json" -H "User-Agent: oauth-e2e")
    echo "   归一化: id=$(json_field "$UI" id) login=$(json_field "$UI" login) email=$(json_field "$UI" email)"
    echo "   （email 为 null 是常态：用户把主邮箱设为 private，需要用 /user/emails 补拉）"
    ;;

  *)
    echo "未知渠道: $CHANNEL（可选 wechat-web | wechat-app | qq-web | qq-app | github-web）" >&2
    exit 1
    ;;
esac

echo
echo "== 完成 =="
echo "提示：本脚本只验证平台侧协议；本站的 state 校验 / 账号绑定 / 双 token 签发走"
echo "     go test ./internal/service/ -run OAuth 与 docs/15 的真机步骤。"
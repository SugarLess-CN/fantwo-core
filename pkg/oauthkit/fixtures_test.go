package oauthkit

// 真实上游响应 fixture（第 1 层：协议测试的「活体样本」）。
//
// 与其它 *_test.go 的区别：那些是按**官方文档**构造的响应；
// 本文件的每一条都是 **2026-11-25 从本机实测抓取的真实原文**，
// 除脱敏（无）与换行外未做任何改写。价值在于：
//
//  1. 官方文档的样例常常不完整（字段缺失、格式示例与正文矛盾）；
//     真实原文能证明「解析层面对真实字节流是work的」。
//  2. 反例比正例重要：QQ 从服务器侧直连时，`/oauth2.0/token` 会被腾讯 WAF
//     用 HTTP 501 + 一个 HTML 跳转脚本挡掉。这个 HTML 页**就是**
//     「能解析 ≠ 是平台的响应」这条禁令的活体样本 —— 它含 `=` 号，
//     url.ParseQuery 能「成功」把它解成一堆怪 key。
//
// 抓取命令（凭据不落代码，从环境变量读）：
//
//	# 微信（真 appid + 真 secret + 假 code → 凭据有效则报 40029）
//	curl "https://api.weixin.qq.com/sns/oauth2/access_token?appid=$WECHAT_WEB_APPID&secret=$WECHAT_WEB_SECRET&code=FAKE&grant_type=authorization_code"
//	# QQ me（假 token → 凭据无关，验证上游可达 + 错误分类）
//	curl "https://graph.qq.com/oauth2.0/me?access_token=FAKE"
//	# QQ token（真 client_id + 真 client_secret + 假 code）
//	curl "https://graph.qq.com/oauth2.0/token?grant_type=authorization_code&client_id=$QQ_WEB_APPID&client_secret=$QQ_WEB_APPKEY&code=FAKE&redirect_uri=..."

// realWechatWebInvalidCode 微信「网站应用」用假 code 换 token 的真实响应。
//
// 判读要点：40029 = invalid code → **凭据有效**（微信先校验 appid+secret 再查 code）。
// 若凭据错会得到 40013 invalid appid / 40125 invalid appsecret。
const realWechatWebInvalidCode = `{"errcode":40029,"errmsg":"invalid code, rid: 6b073046-0b5d90bb-64f870db"}`

// realWechatAppInvalidCode 微信「移动应用」同上（appid 是另一套）。
const realWechatAppInvalidCode = `{"errcode":40029,"errmsg":"invalid code, rid: 6b073047-538fe360-0e5f6abf"}`

// realQQMeIllegalToken QQ oauth2.0/me 对非法 access_token 的真实响应（JSONP 包裹）。
//
// 注意末尾的 `);` 与对象内尾逗号都不规范 —— 真实上游就是这样。
const realQQMeIllegalToken = `callback( {"error":100013,"error_description":"access token is illegal"} );`

// realQQUserInfoInvalidOpenID QQ user/get_user_info 对非法 openid 的真实响应。
//
// ret 非 0 时 **HTTP 仍是 200**，必须靠解析 ret 判失败（docs/14 §2.4 第 3 条）。
const realQQUserInfoInvalidOpenID = `{"ret":-22,"msg":"openid is invalid"}`

// realQQWAFBlockPage QQ `/oauth2.0/token` 与 `/oauth2.0/authorize` 被腾讯 WAF
// 拦下时的真实响应体（HTTP 501，326 字节）。
//
// 【这是本 SDK 最值钱的一条 fixture】它证明为什么「能解析 ≠ 是平台的响应」：
//   - 它不是 JSON，但含大量 `=` 号 → url.ParseQuery 返回 nil error + 一堆怪 key；
//   - 它不含任何 QQ 已知字段（access_token/refresh_token/error/ret/...）→ 必须拒。
//
// 若解析层只看「解析有没有报错」，这一页会被当成「成功但没 token」，
// 把「上游是不是挂了」误导成「QQ 为什么不给 token」。
const realQQWAFBlockPage = `<!DOCTYPE html><html><head><script>var i=location.href;var v=window.btoa?window.btoa(window.encodeURIComponent(i)):"";window.location.href="https://waf.tencent.com/501page.html?u="+location.origin+"&id=d1cd892fbe0d6015691a58e43159de4f-1782978915484717-433-140072907450112-48536715758008764&st=01&v="+v;</script></head></html>`

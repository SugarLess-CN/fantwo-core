// 第三方登录 HTTP 端点（微信 web/app、QQ web/app、GitHub web）。
//
// 设计依据：docs/14-第三方登录调研.md §8.4 渠道矩阵 + §8.5 问题 3 入口矩阵。
// 全部端点都走统一响应契约 {data, message, status_code, trace_id, timestamp}（AGENTS.md）。
//
// # 8 个核心端点
//
//	GET  /user/oauth/wechat/web/url        取授权链接（PC 扫码）
//	GET  /user/oauth/wechat/web/callback   回调（重定向模式，302 回前端带 token）
//	POST /user/oauth/wechat/web/callback   回调（SPA 模式，直接返回双 token）
//	POST /user/oauth/wechat/app/login      App 登录（客户端交 code）
//	GET  /user/oauth/qq/web/url            取授权链接
//	GET  /user/oauth/qq/web/callback       回调（重定向模式）
//	POST /user/oauth/qq/web/callback       回调（SPA 模式）
//	POST /user/oauth/qq/app/login          App 登录（客户端交 access_token，必须校验）
//
// 另有 GitHub 三条（url / callback POST / callback GET），路由沿用既有路径不变。
//
// # 为什么微信 App 交 code、QQ App 交 access_token（不抹平）
//
//   - 微信 App：`WXApi.sendAuthReq` 只回 code，后端用 code 换 token（可信）。
//   - QQ App：`Tencent.login` 直接回 access_token + openid，**两者都能被客户端伪造**，
//     后端必须调 oauth2.0/me 校验并比对 openid 一致性 —— 这是 `ValidateToken` 的存在理由。
//
// 抹平的代价：要么给微信加一次无意义的校验调用，要么直接信任 QQ 客户端的 token（漏洞）。
package handler

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// oauthLoginResult 组装登录成功的统一响应 data。
//
// **必须与既有 GitHub 端点逐字一致**（前端已按这个结构解析）：
// user 走 service.ViewUser 脱敏视图、附带 isMember、id 单独给一份（前端历史习惯）。
func oauthLoginResult(c *gin.Context, svc *service.AuthService, user *model.User, token, refresh string) *dto.LoginResultDto {
	return svc.LoginResultWithConfig(c.Request.Context(), user, token, refresh)
}

// ---------------------------------------------------------------------------
// 微信 - 网站应用（PC 扫码）
// ---------------------------------------------------------------------------

// WechatWebURL GET /user/oauth/wechat/web/url
//
//	@Summary	微信扫码登录（取授权链接）
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthURLDto}
//	@Param			did header string true "设备标识（**必填**，请求头 `did`，最长 128 字节）。OAuth 回调是浏览器 302 跳转、前端加不了自定义 header，所以必须在这一步随 state 存下，回调时后端取回用于签发双 token（refresh_token 与设备绑定）。缺失返回 20016 auth.device_id_required" example(dev-abc123)
//	@Router		/user/oauth/wechat/web/url [get]
//	@ID			WechatOAuthController_webURL
func (h *AuthHandler) WechatWebURL(c *gin.Context) {
	out, e := h.svc.WechatWebAuthURL(c.Request.Context(), deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// WechatWebCallback POST /user/oauth/wechat/web/callback
//
//	@Summary	微信扫码回调（SPA 模式换本站 token）
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Param		body body dto.OAuthCallbackRequest true "code + state"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@Router		/user/oauth/wechat/web/callback [post]
//	@ID			WechatOAuthController_webCallback
func (h *AuthHandler) WechatWebCallback(c *gin.Context) {
	var req dto.OAuthCallbackRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.WechatWebCallback(c.Request.Context(), req.Code, req.State, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(c, h.svc, user, token, refresh))
}

// WechatWebRedirect GET /user/oauth/wechat/web/callback
//
// **已废弃（2026-11-28）**：改用「redirect_uri 填前端域名 + 前端 POST 换 token」，
// 见 docs/15 §2.2.1。保留仅为兼容「redirect_uri 已配成后端域名」的存量部署。
// 废弃理由：token 走 URL 会留在浏览器历史 / Referer / 反代 access log / 埋点里。
//
//	@Summary	[已删除 2026-11-28] 微信扫码回调（重定向模式，302 回前端带 token）
//	@Tags		用户认证
//	@Produce	json
//	@Success	302	"重定向到前端，token 由 Location 的 query 携带（响应体为空，不是 JSON）"
//	@Header		302	{string}	Location	"前端回调地址?token=...&refreshToken=...（失败时带 error=...）"
//	@Deprecated
//	@Router		/user/oauth/wechat/web/callback [get]
//	@ID			WechatOAuthController_webRedirect
func (h *AuthHandler) WechatWebRedirect(c *gin.Context) {
	loc := h.svc.WechatWebRedirectURL(c.Request.Context(),
		c.Query("code"), c.Query("state"), c.Query("error"), deviceIDOf(c))
	c.Redirect(http.StatusFound, loc)
}

// ---------------------------------------------------------------------------
// 微信 - 移动应用（App 内微信 SDK 授权后把 code 交后端）
// ---------------------------------------------------------------------------

// WechatAppLogin POST /user/oauth/wechat/app/login
//
//	@Summary	微信 App 登录（客户端交 code）
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Param		body body dto.OAuthCodeRequest true "微信 SDK 回调的 code"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@Router		/user/oauth/wechat/app/login [post]
//	@ID			WechatOAuthController_appLogin
func (h *AuthHandler) WechatAppLogin(c *gin.Context) {
	var req dto.OAuthCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.WechatAppLogin(c.Request.Context(), req.Code, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(c, h.svc, user, token, refresh))
}

// ---------------------------------------------------------------------------
// QQ - 网站应用（网页登录）
// ---------------------------------------------------------------------------

// QQWebURL GET /user/oauth/qq/web/url
//
//	@Summary	QQ 网页登录（取授权链接）
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthURLDto}
//	@Param			did header string true "设备标识（**必填**，请求头 `did`，最长 128 字节）。OAuth 回调是浏览器 302 跳转、前端加不了自定义 header，所以必须在这一步随 state 存下，回调时后端取回用于签发双 token（refresh_token 与设备绑定）。缺失返回 20016 auth.device_id_required" example(dev-abc123)
//	@Router		/user/oauth/qq/web/url [get]
//	@ID			QqOAuthController_webURL
func (h *AuthHandler) QQWebURL(c *gin.Context) {
	out, e := h.svc.QQWebAuthURL(c.Request.Context(), deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// QQWebCallback POST /user/oauth/qq/web/callback
//
//	@Summary	QQ 网页回调（SPA 模式换本站 token）
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Param		body body dto.OAuthCallbackRequest true "code + state"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@Router		/user/oauth/qq/web/callback [post]
//	@ID			QqOAuthController_webCallback
func (h *AuthHandler) QQWebCallback(c *gin.Context) {
	var req dto.OAuthCallbackRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.QQWebCallback(c.Request.Context(), req.Code, req.State, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(c, h.svc, user, token, refresh))
}

// QQWebRedirect GET /user/oauth/qq/web/callback
//
// **已废弃（2026-11-28）**：同 WechatWebRedirect，改用前端域名回调 + POST 换 token。
//
//	@Summary	[已删除 2026-11-28] QQ 网页回调（重定向模式，302 回前端带 token）
//	@Tags		用户认证
//	@Produce	json
//	@Success	302	"重定向到前端，token 由 Location 的 query 携带（响应体为空，不是 JSON）"
//	@Header		302	{string}	Location	"前端回调地址?token=...&refreshToken=...（失败时带 error=...）"
//	@Deprecated
//	@Router		/user/oauth/qq/web/callback [get]
//	@ID			QqOAuthController_webRedirect
func (h *AuthHandler) QQWebRedirect(c *gin.Context) {
	loc := h.svc.QQWebRedirectURL(c.Request.Context(),
		c.Query("code"), c.Query("state"), c.Query("error"), deviceIDOf(c))
	c.Redirect(http.StatusFound, loc)
}

// ---------------------------------------------------------------------------
// QQ - 移动应用（QQ SDK 直接给 access_token + openid）
// ---------------------------------------------------------------------------

// QQAppLogin POST /user/oauth/qq/app/login
//
// 两种提交方式，二选一：
//   - 结构化：{"accessToken": "...", "openId": "..."}（推荐，字段显式便于校验）
//   - 原始载荷：{"payload": {...SDK 回调 JSON 原文...}}（走 ParseQQAppLoginPayload，
//     兼容 SDK 把 pay_token/pf/pfkey 一并回传、expires_in 是字符串的情况）
//
// 安全说明：**openid 与 access_token 都可被客户端伪造**，service 层必须调
// ValidateToken（内部打 oauth2.0/me 校验 + 比对 openid 一致性）后才算登录。
//
//	@Summary	QQ App 登录（客户端交 access_token）
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Param		body body dto.QqAppLoginRequest true "{accessToken, openId} 或 {payload}"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@ID			QqAppLoginController_login
//	@Router		/user/oauth/qq/app/login [post]
func (h *AuthHandler) QQAppLogin(c *gin.Context) {
	// Payload 字段在 handler 里从未使用（QQ 移动端原始回调走 QQAppLoginPayload 另一条路），
	// 保持 dto.QqAppLoginRequest 的三字段即可。
	var req dto.QqAppLoginRequest
	if !bindJSON(c, &req) {
		return
	}

	// 结构化字段优先；两者都没给时由 service 层的 ErrInvalidArgument 拦下。
	user, token, refresh, e := h.svc.QQAppLogin(c.Request.Context(),
		req.AccessToken, req.OpenID, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(c, h.svc, user, token, refresh))
}

// ---------------------------------------------------------------------------
// 账号绑定管理（T-37e）：三个端点全部需登录（路由挂 authGroup）。
//
// 与登录端点的本质区别：这里用户身份来自 JWT claims（middleware.CurrentUserID），
// 第三方身份只是**新增一条绑定**，不查找 / 不建号。响应契约与登录端点一致
// （{data, message, status_code, trace_id, timestamp}）。
//
// service 按请求现建（service.NewOAuthService(deps)）：OAuthService 无独立状态，
// 复用 AuthHandler 的 Deps，与路由注册处构造 NewOAuthService(deps) 是同一形态。
// ---------------------------------------------------------------------------

// OAuthBindingList GET /user/oauth/bindings
//
// 响应 data：{"bindings": [...]}。绑定视图不含 openid / unionid（PII）；
// 空列表是 [] 不是 null。
//
//	@Summary	我的第三方绑定列表
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthBindingListDto}
//	@Router		/user/oauth/bindings [get]
//	@ID			OAuthBindingController_listBindings
func (h *AuthHandler) OAuthBindingList(c *gin.Context) {
	svc := service.NewOAuthService(h.svc.Deps())
	bindings, e := svc.ListBindings(middleware.CurrentUserID(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"bindings": bindings})
}

// OAuthBindingUnbind DELETE /user/oauth/bindings/:provider/:channel
//
// 解绑安全规则（service 层实现）：可用登录方式 = 手机号 OR 邮箱 OR 其它第三方绑定；
// 全不剩 → 403（ErrOAuthUnbindLastMethod）。Password 不参与判定
// （第三方建号写的是随机密码，用户不知道它）。
// 绑定不存在（含别人的绑定）→ 404。
// 成功后回传最新绑定列表，前端一次请求完成「删 + 刷」。
//
//	@Summary	解绑第三方账号
//	@Tags		用户认证
//	@Produce	json
//	@Param		provider path string true "平台 wechat / qq / github" example(wechat)
//	@Param		channel path string true "渠道 web / app" example(web)
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthBindingListDto}
//	@Router		/user/oauth/bindings/{provider}/{channel} [delete]
//	@ID			OAuthBindingController_unbind
func (h *AuthHandler) OAuthBindingUnbind(c *gin.Context) {
	svc := service.NewOAuthService(h.svc.Deps())
	uid := middleware.CurrentUserID(c)
	if e := svc.Unbind(c.Request.Context(), uid, c.Param("provider"), c.Param("channel")); e != nil {
		response.Fail(c, e)
		return
	}
	bindings, e := svc.ListBindings(uid)
	if e != nil {
		// 列表刷新失败不影响解绑结果：解绑已生效，data 留空让前端自行重拉。
		response.Success(c, nil)
		return
	}
	response.Success(c, gin.H{"bindings": bindings})
}

// OAuthBindingLink POST /user/oauth/bindings/link
//
// 已登录状态下把第三方身份绑到当前用户。请求体与登录端点同构：
//   - web（微信/QQ/GitHub）：{"provider":"wechat","channel":"web","code":"...","state":"..."}
//   - QQ app：{"provider":"qq","channel":"app","accessToken":"...","openId":"..."}
//   - 微信 app：{"provider":"wechat","channel":"app","code":"..."}
//
// 该 openid 已被任何用户绑定 → 409。绝不建号：只插绑定到当前 user。
//
//	@Summary	绑定第三方账号到当前用户
//	@Tags		用户认证
//	@Accept		json
//	@Produce	json
//	@Param		body body dto.OAuthLinkRequest true "{provider, channel, code, state} 或 {provider, channel, accessToken, openId}"
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthBindingListDto}
//	@Router		/user/oauth/bindings/link [post]
//	@ID			OAuthBindingController_link
func (h *AuthHandler) OAuthBindingLink(c *gin.Context) {
	var req service.OAuthLinkInput
	if !bindJSON(c, &req) {
		return
	}
	svc := service.NewOAuthService(h.svc.Deps())
	bindings, e := svc.LinkToCurrentUser(c.Request.Context(), middleware.CurrentUserID(c), req)
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"bindings": bindings})
}

// 让 dto 包成为本文件的真实 import：swag 只按「注解所在文件的 import 列表」
// 解析跨包类型（docs/18 §三之一 约束 B）。本文件注解写了 dto.Xxx，这里引用一次。
// 不参与运行逻辑。
var _ = dto.ResponseBody{}

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

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// oauthLoginResult 组装登录成功的统一响应 data。
//
// **必须与既有 GitHub 端点逐字一致**（前端已按这个结构解析）：
// user 走 service.ViewUser 脱敏视图、附带 isMember、id 单独给一份（前端历史习惯）。
func oauthLoginResult(user *model.User, token, refresh string) gin.H {
	return gin.H{
		"user": service.ViewUser(user, true), "token": token,
		"refreshToken": refresh, "id": user.ID, "isMember": service.IsMember(user),
	}
}

// ---------------------------------------------------------------------------
// 微信 - 网站应用（PC 扫码）
// ---------------------------------------------------------------------------

// WechatWebURL GET /user/oauth/wechat/web/url
//
//	@Summary	微信扫码登录（取授权链接）
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_WechatWebURL
//	@Router		/user/oauth/wechat/web/url [get]
func (h *AuthHandler) WechatWebURL(c *gin.Context) {
	out, e := h.svc.WechatWebAuthURL(c.Request.Context())
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
//	@Param		body body OAuthCallbackRequest true "请求体 {code, state}"
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_WechatWebCallback
//	@Router		/user/oauth/wechat/web/callback [post]
func (h *AuthHandler) WechatWebCallback(c *gin.Context) {
	var req OAuthCallbackRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.WechatWebCallback(c.Request.Context(), req.Code, req.State, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(user, token, refresh))
}

// WechatWebRedirect GET /user/oauth/wechat/web/callback
//
//	@Summary	微信扫码回调（重定向模式，302 回前端带 token）
//	@Tags		用户认证
//	@Produce	json
//	@Success	302
//	@ID				AuthHandler_WechatWebRedirect
//	@Router		/user/oauth/wechat/web/callback [get]
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
//	@Param		body body WechatAppLoginRequest true "请求体 {code}"
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_WechatAppLogin
//	@Router		/user/oauth/wechat/app/login [post]
func (h *AuthHandler) WechatAppLogin(c *gin.Context) {
	var req WechatAppLoginRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.WechatAppLogin(c.Request.Context(), req.Code, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(user, token, refresh))
}

// ---------------------------------------------------------------------------
// QQ - 网站应用（网页登录）
// ---------------------------------------------------------------------------

// QQWebURL GET /user/oauth/qq/web/url
//
//	@Summary	QQ 网页登录（取授权链接）
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_QQWebURL
//	@Router		/user/oauth/qq/web/url [get]
func (h *AuthHandler) QQWebURL(c *gin.Context) {
	out, e := h.svc.QQWebAuthURL(c.Request.Context())
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
//	@Param		body body OAuthCallbackRequest true "请求体 {code, state}"
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_QQWebCallback
//	@Router		/user/oauth/qq/web/callback [post]
func (h *AuthHandler) QQWebCallback(c *gin.Context) {
	var req OAuthCallbackRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.QQWebCallback(c.Request.Context(), req.Code, req.State, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, oauthLoginResult(user, token, refresh))
}

// QQWebRedirect GET /user/oauth/qq/web/callback
//
//	@Summary	QQ 网页回调（重定向模式，302 回前端带 token）
//	@Tags		用户认证
//	@Produce	json
//	@Success	302
//	@ID				AuthHandler_QQWebRedirect
//	@Router		/user/oauth/qq/web/callback [get]
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
//	@Param		body body QQAppLoginRequest true "请求体 {accessToken, openId} 或 {payload}"
//	@Success	200	{object}	ResponseBody
//	@ID				AuthHandler_QQAppLogin
//	@Router		/user/oauth/qq/app/login [post]
func (h *AuthHandler) QQAppLogin(c *gin.Context) {
	var req QQAppLoginRequest
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
	response.Success(c, oauthLoginResult(user, token, refresh))
}

// ---- 请求体 DTO（注解引用，swag 按包内类型解析）----

// QQAppLoginRequest QQ App 登录请求体。
type QQAppLoginRequest struct {
	AccessToken string                 `json:"accessToken"`
	OpenID      string                 `json:"openId"`
	Payload     map[string]interface{} `json:"payload"`
}

// WechatAppLoginRequest 微信 App 授权码登录请求体。
type WechatAppLoginRequest struct {
	Code string `json:"code"`
}

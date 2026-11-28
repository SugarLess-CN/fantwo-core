// Package handler 认证与用户域 HTTP 处理层。
//
// 分层约定（docs/04 §2）：handler 只做参数绑定、调 service、统一响应；
// 业务判断全部在 internal/service。响应包装走 internal/pkg/response。
package handler

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// AuthHandler 认证端点（用户认证 tag 7 操作 + 登录/注册/刷新/登出/全端登出/短信）。
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler 构造。
func NewAuthHandler(svc *service.AuthService) *AuthHandler { return &AuthHandler{svc: svc} }

// loginRequest 登录请求体（account 支持用户名或邮箱）。
type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

// Login POST /user/login（另挂 POST /auth/login 别名）。
// device-id 头必填（原 refreshToken/logout 语义依赖；缺省时用 "default" 兼容脚本）。
//
//	@Summary		用户登录
//	@Description	账号（用户名或邮箱）+ 密码登录；Device-Id 头用于多端会话管理
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param		Device-Id header string false "设备标识，缺省 default" example(uuid-1)
//	@Param			body body dto.LoginRequest	true	"登录参数"
//	@Success		200			{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@Failure		401			{object}	dto.ResponseBody
//	@ID			UserController_login
//	@Router			/user/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	deviceID := deviceIDOf(c)
	user, token, refresh, e := h.svc.Login(c.Request.Context(), service.LoginInput{
		Account: req.Account, Password: req.Password, DeviceID: deviceID,
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, h.svc.LoginResultWithConfig(c.Request.Context(), user, token, refresh))
}

// Register POST /user/register（另挂 POST /auth/register）。
//
//	@Summary		用户注册
//	@Description	用户名 + 密码注册，可选昵称/邮箱/手机号/邀请码/邮箱验证码。**注册即登录**：响应与 /user/login 完全一致（含 token 与 refresh_token），前端无需再调一次 login
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param			device-id header string true "设备标识（与登录同口径，缺省 20016）" example(device-abc123)
//	@Param			body body dto.RegisterRequest	true	"registerRequest{username,nickname,password,email,phone,invite_code,verification_code}"
//	@Success		200		{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@Failure		409		{object}	dto.ResponseBody
//	@ID			UserController_registerUser
//	@Router			/user/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if !bindJSON(c, &req) {
		return
	}
	// 注册即登录：直接返回与 /user/login 完全一致的结构
	// （user 全字段 + config + equipped_decorations + certification + token + refresh_token）。
	// 前端不用再调一次 login，也不会出现「注册成功但登录失败」的中间态。
	out, e := h.svc.RegisterResult(c.Request.Context(), service.RegisterInput{
		Username: req.Username, Nickname: req.Nickname, Password: req.Password,
		Email: req.Email, Phone: req.Phone,
		// 这三个字段必须在这里显式转发 —— 漏了就等于「DTO 加了、service 支持了、
		// 但请求进不到业务层」（本项目已踩过同类：docs/21 §四之三 契约漂移）。
		InviteCode:       req.InviteCode,
		VerificationCode: req.VerificationCode,
		DeviceID:         deviceIDOf(c),
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// RefreshToken POST /user/refresh-token（另挂 POST /auth/refresh）。滑动过期：access 重签。
//
//	@Summary		刷新 access token
//	@Description	用 refreshToken 换取新的 access token；需 Device-Id 头（服务端据此校验设备会话）
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param		Device-Id header string true "设备标识" example(uuid-1)
//	@Param			body body dto.RefreshTokenRequest	true	"刷新令牌请求体"
//	@Success		200			{object}	dto.ResponseBody{data=dto.RefreshTokenResultDto}
//	@Failure		401			{object}	dto.ResponseBody
//	@ID			UserController_refreshToken
//	@Router			/user/refresh-token [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req dto.RefreshTokenRequest
	if !bindJSON(c, &req) {
		return
	}
	deviceID := deviceIDOf(c)
	if deviceID == "" {
		response.Fail(c, errcode.ErrDeviceIDRequired)
		return
	}
	token, e := h.svc.RefreshToken(c.Request.Context(), service.RefreshInput{
		RefreshToken: req.RefreshToken, DeviceID: deviceID,
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"token": token})
}

// Logout POST /user/logout（单设备）：设备删除 + token 进黑名单。
//
//	@Summary		退出登录
//	@Description	删除当前设备会话并把当前 token 加入黑名单
//	@Tags			用户认证
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@Failure		401	{object}	dto.ResponseBody
//	@ID			UserController_logout
//	@Router			/user/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	token := middleware.CurrentToken(c)
	if e := h.svc.Logout(c.Request.Context(), userID, token, deviceIDOf(c)); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthLogoutSuccess, gin.H{"success": true})
}

// LogoutAll POST /user/logout-all（另挂 POST /auth/logout-all）：全端登出。
//
//	@Summary	全端退出登录
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			AuthController_logoutAll
//	@Router		/user/logout-all [post]
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	if e := h.svc.LogoutAll(c.Request.Context(), userID); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthLogoutAllSuccess, gin.H{"success": true})
}

// SendSmsCode POST /auth/sms/send：发送短信验证码。
//
//	@Summary	发送短信验证码
//	@Tags		用户认证
//	@Produce	json
//	@Param		body body dto.SmsCodeRequest true "手机号与场景"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			AuthController_sendSmsCode
//	@Router		/auth/sms/send [post]
func (h *AuthHandler) SendSmsCode(c *gin.Context) {
	var req dto.SmsCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Scene == "" {
		req.Scene = "login"
	}
	if e := h.svc.SendCode(c.Request.Context(), service.SendCodeInput{Target: req.Phone, Scene: req.Scene}); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthCaptchaSent, gin.H{"success": true})
}

// SmsLogin POST /auth/sms/login：手机号 + 验证码登录（未注册自动建号）。
//
//	@Summary	手机验证码登录
//	@Tags		用户认证
//	@Produce	json
//	@Param		body body dto.SmsLoginRequest true "手机号与验证码"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@ID			AuthController_smsLogin
//	@Router		/auth/sms/login [post]
func (h *AuthHandler) SmsLogin(c *gin.Context) {
	var req dto.SmsLoginRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.SmsLogin(c.Request.Context(), req.Phone, req.Code, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, h.svc.LoginResultWithConfig(c.Request.Context(), user, token, refresh))
}

// SendEmailCode POST /user/email/verification：邮箱验证码。
//
//	@Summary	发送邮箱验证码
//	@Tags		用户认证
//	@Produce	json
//	@Param		body body dto.EmailCodeRequest true "邮箱与用途"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_sendVerificationCode
//	@Router		/user/email/verification [post]
func (h *AuthHandler) SendEmailCode(c *gin.Context) {
	var req dto.EmailCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	scene := req.Type
	if scene == "" {
		scene = "verification"
	}
	if e := h.svc.SendCode(c.Request.Context(), service.SendCodeInput{Target: req.Email, Scene: scene}); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthCaptchaSent, gin.H{"success": true})
}

// ResetPassword POST /user/password/reset：邮箱验证码重置密码。
//
//	@Summary	邮箱验证码重置密码
//	@Tags		用户认证
//	@Produce	json
//	@Param		body body dto.ResetPasswordRequest true "邮箱验证码重置参数"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_resetPassword
//	@Router		/user/password/reset [post]
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	if e := h.svc.ResetPassword(c.Request.Context(), req.Email, req.Code, req.NewPassword); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthPasswordReset, gin.H{"success": true})
}

// ChangePassword POST /user/password/change：原密码校验后改密。
//
//	@Summary	修改密码
//	@Tags		用户认证
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body dto.ChangePasswordRequest true "原密码与新密码"
//	@Success	200	{object}	dto.ResponseBody{data=dto.SuccessDto}
//	@ID			UserController_changePassword
//	@Router		/user/password/change [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	userID := middleware.CurrentUserID(c)
	if e := h.svc.ChangePassword(c.Request.Context(), userID, req.OldPassword, req.NewPassword); e != nil {
		response.Fail(c, e)
		return
	}
	response.SuccessMsg(c, i18n.AuthPasswordChanged, gin.H{"success": true})
}

// bindJSON 绑定并校验 JSON 体；失败时直接写 400 响应并返回 false。
// 对齐原 filter 的「只取第一条校验消息」语义。
func bindJSON(c *gin.Context, out interface{}) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		response.Fail(c, errcode.ErrInvalidParam.WithKey(i18n.CommonBadRequest))
		return false
	}
	return true
}

// deviceIDOf 读取 device-id 头（对齐原 getHeaderValue(headers,"device-id")）。
func deviceIDOf(c *gin.Context) string {
	if v := c.GetHeader("device-id"); v != "" {
		return v
	}
	return c.GetHeader("Device-Id")
}

// GithubURL GET /user/oauth/github/url：返回 GitHub 授权链接 + state。
// 未配置 client_id/secret 时返回 400 / code=4022（既有占位语义保留）。
//
//	@Summary	GitHub 授权链接
//	@Tags		用户认证
//	@Produce	json
//	@Success	200	{object}	dto.ResponseBody{data=dto.OAuthURLDto}
//	@ID			UserController_getGithubOAuthUrl
//	@Router		/user/oauth/github/url [get]
func (h *AuthHandler) GithubURL(c *gin.Context) {
	out, e := h.svc.GithubAuthURL(c.Request.Context())
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, out)
}

// GithubCallback POST /user/oauth/github/callback：SPA 模式，用 code 换本站双 token。
//
//	@Summary	GitHub 回调（SPA 模式换本站 token）
//	@Tags		用户认证
//	@Produce	json
//	@Param		body body dto.GithubCallbackRequest true "code + state"
//	@Success	200	{object}	dto.ResponseBody{data=dto.LoginResultDto}
//	@ID			UserController_githubOAuthCallback
//	@Router		/user/oauth/github/callback [post]
func (h *AuthHandler) GithubCallback(c *gin.Context) {
	var req dto.GithubCallbackRequest
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.GithubCallback(c.Request.Context(), req.Code, req.State, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, h.svc.LoginResultWithConfig(c.Request.Context(), user, token, refresh))
}

// GithubRedirect GET /user/oauth/github/callback：重定向模式，成功跳前端并带 token。
//
// 302 重定向，**响应体为空**（不是统一响应体）：token 走 Location 的 query。
// 因此不声明 data 的 $ref —— 硬编一个 200 的 data 会让前端/生成器以为这里有 JSON。
//
//	@Summary	GitHub 回调（重定向模式）
//	@Tags		用户认证
//	@Produce	json
//	@Success	302	"重定向到前端，token 由 Location 的 query 携带（响应体为空，不是 JSON）"
//	@Header		302	{string}	Location	"前端回调地址?token=...&refreshToken=...（失败时带 error=...）"
//	@ID			UserController_githubOAuthRedirectCallback
//	@Router		/user/oauth/github/callback [get]
func (h *AuthHandler) GithubRedirect(c *gin.Context) {
	loc := h.svc.GithubRedirectURL(c.Request.Context(),
		c.Query("code"), c.Query("state"), c.Query("error"), deviceIDOf(c))
	c.Redirect(http.StatusFound, loc)
}

// 让 dto 包成为本文件的真实 import：swag 按「注释所在文件的 import」解析跨包类型，
// 本文件的注解写了 dto.Xxx，这里必须引用一次（不参与运行逻辑）。
var _ = dto.ResponseBody{}

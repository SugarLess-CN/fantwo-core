// Package handler 认证与用户域 HTTP 处理层。
//
// 分层约定（docs/04 §2）：handler 只做参数绑定、调 service、统一响应；
// 业务判断全部在 internal/service。响应包装走 internal/pkg/response。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
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
//	@Param			Device-Id	header		string			false	"设备标识，缺省 default"
//	@Param			body		body		loginRequest	true	"登录参数"
//	@Success		200			{object}	ResponseBody
//	@Failure		401			{object}	ResponseBody
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
	out := service.ViewUser(user, true)
	response.Success(c, gin.H{"user": out, "token": token, "refreshToken": refresh,
		"username": user.Username, "id": user.ID})
}

// Register POST /user/register（另挂 POST /auth/register）。
//
//	@Summary		用户注册
//	@Description	用户名 + 密码注册，可选昵称/邮箱/手机号；返回用户信息
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param			body	body		object	true	"registerRequest{username,nickname,password,email,phone}"
//	@Success		200		{object}	ResponseBody
//	@Failure		409		{object}	ResponseBody
//	@Router			/user/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Password string `json:"password"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
	}
	if !bindJSON(c, &req) {
		return
	}
	user, e := h.svc.Register(c.Request.Context(), service.RegisterInput{
		Username: req.Username, Nickname: req.Nickname, Password: req.Password,
		Email: req.Email, Phone: req.Phone,
	})
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, service.ViewUser(user, false))
}

// RefreshToken POST /user/refresh-token（另挂 POST /auth/refresh）。滑动过期：access 重签。
//
//	@Summary		刷新 access token
//	@Description	用 refreshToken 换取新的 access token；需 Device-Id 头（服务端据此校验设备会话）
//	@Tags			用户认证
//	@Accept			json
//	@Produce		json
//	@Param			Device-Id	header		string	true	"设备标识"
//	@Param			body		body		object	true	"refreshRequest{refreshToken}"
//	@Success		200			{object}	ResponseBody
//	@Failure		401			{object}	ResponseBody
//	@Router			/user/refresh-token [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
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
//	@Success		200	{object}	ResponseBody
//	@Failure		401	{object}	ResponseBody
//	@Router			/user/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	token := middleware.CurrentToken(c)
	if e := h.svc.Logout(c.Request.Context(), userID, token, deviceIDOf(c)); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "message": "已退出登录"})
}

// LogoutAll POST /user/logout-all（另挂 POST /auth/logout-all）：全端登出。
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID := middleware.CurrentUserID(c)
	if e := h.svc.LogoutAll(c.Request.Context(), userID); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "message": "已从所有设备退出登录"})
}

// SendSmsCode POST /auth/sms/send：发送短信验证码。
func (h *AuthHandler) SendSmsCode(c *gin.Context) {
	var req struct {
		Phone string `json:"phone"`
		Scene string `json:"scene"`
	}
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
	response.Success(c, gin.H{"success": true, "message": "验证码已发送"})
}

// SmsLogin POST /auth/sms/login：手机号 + 验证码登录（未注册自动建号）。
func (h *AuthHandler) SmsLogin(c *gin.Context) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !bindJSON(c, &req) {
		return
	}
	user, token, refresh, e := h.svc.SmsLogin(c.Request.Context(), req.Phone, req.Code, deviceIDOf(c))
	if e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{
		"user": service.ViewUser(user, true), "token": token,
		"refreshToken": refresh, "id": user.ID,
	})
}

// SendEmailCode POST /user/email/verification：邮箱验证码。
func (h *AuthHandler) SendEmailCode(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
		Type  string `json:"type"`
	}
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
	response.Success(c, gin.H{"success": true, "message": "验证码已发送"})
}

// ResetPassword POST /user/password/reset：邮箱验证码重置密码。
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if e := h.svc.ResetPassword(c.Request.Context(), req.Email, req.Code, req.NewPassword); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "message": "密码重置成功"})
}

// ChangePassword POST /user/password/change：原密码校验后改密。
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if !bindJSON(c, &req) {
		return
	}
	userID := middleware.CurrentUserID(c)
	if e := h.svc.ChangePassword(c.Request.Context(), userID, req.OldPassword, req.NewPassword); e != nil {
		response.Fail(c, e)
		return
	}
	response.Success(c, gin.H{"success": true, "message": "密码修改成功"})
}

// bindJSON 绑定并校验 JSON 体；失败时直接写 400 响应并返回 false。
// 对齐原 filter 的「只取第一条校验消息」语义。
func bindJSON(c *gin.Context, out interface{}) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		response.Fail(c, errcode.ErrInvalidParam.With("请求体格式错误"))
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

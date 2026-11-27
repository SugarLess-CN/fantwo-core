// QQ 官方机器人 HTTP 处理层（T-40，12 个端点）。
//
// # webhook 端点的三条特殊约束（踩过就知道为什么）
//
//  1. **不套 JWT 中间件**：平台回调不带本站 token，靠 ed25519 签名校验（SDK 的 ParseWebhook）。
//  2. **不套统一响应包装**：平台按 `{"plain_token","signature"}` 裸 JSON 校验回调地址，
//     套上 `{status_code, trace_id...}` 外壳它判定校验失败。所以这里直接 c.JSON / c.Status，
//     绝不走 response.Success。
//  3. **3 秒内返回**：handler 只做「验签 + 落库」，窗口激活与重投由 service 的
//     s.async 异步执行（见 service/qqbot_service.go 的 HandleWebhook 注释）。
//
// # 注解口径
//
// 每个 @Success 都用 `{data=dto.XxxDto}` 嵌套覆盖（docs/18），请求体一律具名；
// `example(...)` 写在描述引号**外面**（写引号内会让 swagger 丢 example，本项目踩过 259 处）。
package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// QqbotHandler QQ 机器人端点。
type QqbotHandler struct {
	Svc *service.QqbotService
}

// qqbotWebhookMaxBody webhook 请求体上限。
//
// 官方事件体远小于此（单聊消息几百字节）；给 1 MiB 是为了拦住超大伪造包，
// 而不是因为真会有这么大的事件。
const qqbotWebhookMaxBody = 1 << 20

// qqbotErr 统一错误响应（本域共用）。
func qqbotErr(c *gin.Context, e *errcode.Err) {
	response.Fail(c, e)
}

// Webhook POST /qqbot/webhook（平台回调，NoAuth）。
//
// 三种回法（对应 service 的三态返回）：
//
//   - op=13 地址验证 → 原样回写 {"plain_token","signature"}（裸 JSON）
//
//   - op=0  业务事件 → 200 + 空对象 `{}`（平台只认 2xx，不解析响应体）
//
//   - 验签/解析失败 → 401/400/503，让平台重推而不是静默丢事件
//
//     @Summary	QQ 机器人回调（平台调用，NoAuth）
//     @Tags		QQ机器人
//     @Accept		json
//     @Produce	json
//     @Param		body	body	object	true	"平台事件体（op=13 地址验证 / op=0 业务事件）"
//     @Success	200		{object}	dto.QqbotWebhookValidationDto	"op=13 时返回 plain_token+signature；op=0 时返回空对象"
//     @Failure	400		{object}	dto.ResponseBody	"事件体结构非法"
//     @Failure	401		{object}	dto.ResponseBody	"签名校验失败"
//     @Failure	503		{object}	dto.ResponseBody	"未配置 bot_secret 或总开关关闭"
//     @ID			QqbotController_webhook
//     @Router		/qqbot/webhook [post]
func (h *QqbotHandler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, qqbotWebhookMaxBody))
	if err != nil || len(body) == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	validation, e := h.Svc.HandleWebhook(c.Request.Context(), body, c.Request.Header)
	if e != nil {
		// 裸错误：不套业务响应体（套了平台会当成「验证失败」，还得靠日志排查）。
		c.Status(e.HTTP)
		return
	}
	if validation != nil {
		c.JSON(http.StatusOK, validation)
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}

// ListApplications GET /qqbot/applications（申请名单）。
//
//	@Summary	邀请码申请名单
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int		false	"页码" example(1)
//	@Param		limit	query	int		false	"每页条数" example(10)
//	@Param		status	query	string	false	"状态：PENDING/APPROVED/REJECTED/CANCELLED" example(PENDING)
//	@Param		keyword	query	string	false	"关键字：open_id/昵称/内容/邀请码" example(邀请码)
//	@Success	200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.QqbotApplicationDto}}
//	@ID			QqbotController_listApplications
//	@Router		/qqbot/applications [get]
func (h *QqbotHandler) ListApplications(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, e := h.Svc.ListApplications(c.Request.Context(),
		c.Query("status"), c.Query("keyword"), page, limit)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// GetApplication GET /qqbot/applications/{id}（申请详情 + 该用户消息）。
//
//	@Summary	申请详情（含该用户聊天流水与相关池任务）
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	string	true	"申请单 ID" example(64b59ed4-1a2b-4c3d-8e9f-0123456789ab)
//	@Success	200	{object}	dto.ResponseBody{data=dto.QqbotApplicationDetailDto}
//	@Failure	404	{object}	dto.ResponseBody	"申请单不存在"
//	@ID			QqbotController_getApplication
//	@Router		/qqbot/applications/{id} [get]
func (h *QqbotHandler) GetApplication(c *gin.Context) {
	detail, e := h.Svc.GetApplication(c.Request.Context(), c.Param("id"))
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, detail)
}

// ApproveApplication POST /qqbot/applications/{id}/approve（单独同意）。
//
//	@Summary	同意申请（分配邀请码并进入发件池）
//	@Tags		QQ机器人
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	string					true	"申请单 ID" example(64b59ed4-1a2b-4c3d-8e9f-0123456789ab)
//	@Param		body	body	dto.QqbotApproveRequest	false	"审批备注（可空）"
//	@Success	200		{object}	dto.ResponseBody{data=dto.QqbotApproveResultDto}
//	@Failure	404		{object}	dto.ResponseBody	"申请单不存在"
//	@Failure	409		{object}	dto.ResponseBody	"该申请已审批过"
//	@ID			QqbotController_approveApplication
//	@Router		/qqbot/applications/{id}/approve [post]
func (h *QqbotHandler) ApproveApplication(c *gin.Context) {
	var in dto.QqbotApproveRequest
	// 备注可空：body 为空也放行（管理员直接点「同意」不该被迫填备注）
	if err := c.ShouldBindJSON(&in); err != nil && err != io.EOF {
		qqbotErr(c, errcode.ErrInvalidParam)
		return
	}
	res, e := h.Svc.ApproveApplication(c.Request.Context(), c.Param("id"),
		middleware.CurrentUserID(c), in.Note)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, res)
}

// BatchApprove POST /qqbot/applications/batch-approve（批量同意）。
//
//	@Summary	批量同意（部分失败也返回 200，逐条看 results）
//	@Tags		QQ机器人
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body	dto.QqbotBatchApproveRequest	true	"申请单 ID 列表（1~100 条）"
//	@Success	200		{object}	dto.ResponseBody{data=dto.QqbotBatchResultDto}
//	@ID			QqbotController_batchApprove
//	@Router		/qqbot/applications/batch-approve [post]
func (h *QqbotHandler) BatchApprove(c *gin.Context) {
	var in dto.QqbotBatchApproveRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		qqbotErr(c, errcode.ErrInvalidParam)
		return
	}
	res, e := h.Svc.BatchApprove(c.Request.Context(), in.IDs, middleware.CurrentUserID(c), in.Note)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, res)
}

// RejectApplication POST /qqbot/applications/{id}/reject（拒绝）。
//
//	@Summary	拒绝申请（给用户发拒绝通知）
//	@Tags		QQ机器人
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	string					true	"申请单 ID" example(64b59ed4-1a2b-4c3d-8e9f-0123456789ab)
//	@Param		body	body	dto.QqbotRejectRequest	false	"拒绝原因（会拼进给用户的通知）"
//	@Success	200		{object}	dto.ResponseBody{data=dto.QqbotApproveResultDto}
//	@Failure	404		{object}	dto.ResponseBody	"申请单不存在"
//	@Failure	409		{object}	dto.ResponseBody	"该申请已审批过"
//	@ID			QqbotController_rejectApplication
//	@Router		/qqbot/applications/{id}/reject [post]
func (h *QqbotHandler) RejectApplication(c *gin.Context) {
	var in dto.QqbotRejectRequest
	if err := c.ShouldBindJSON(&in); err != nil && err != io.EOF {
		qqbotErr(c, errcode.ErrInvalidParam)
		return
	}
	res, e := h.Svc.RejectApplication(c.Request.Context(), c.Param("id"),
		middleware.CurrentUserID(c), in.Note)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, res)
}

// ListConversations GET /qqbot/conversations（会话列表）。
//
//	@Summary	会话列表（按 openid 聚合：最后消息 + 未读 + 待发）
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int	false	"页码" example(1)
//	@Param		limit	query	int	false	"每页条数" example(10)
//	@Success	200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.QqbotConversationDto}}
//	@ID			QqbotController_listConversations
//	@Router		/qqbot/conversations [get]
func (h *QqbotHandler) ListConversations(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, e := h.Svc.ListConversations(c.Request.Context(), page, limit)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// ListMessages GET /qqbot/conversations/{openid}/messages（聊天记录）。
//
//	@Summary	某用户的聊天记录
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		openid	path	string	true	"用户 openid（机器人维度唯一）" example(A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4)
//	@Param		page	query	int		false	"页码" example(1)
//	@Param		limit	query	int		false	"每页条数" example(20)
//	@Success	200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.QqbotMessageDto}}
//	@ID			QqbotController_listMessages
//	@Router		/qqbot/conversations/{openid}/messages [get]
func (h *QqbotHandler) ListMessages(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, e := h.Svc.ListMessages(c.Request.Context(), c.Param("openid"), page, limit)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// SendMessage POST /qqbot/messages/send（后台发消息）。
//
// 走发件池：窗口过期时自动留池等用户开口，不会「点了发送但用户永远收不到」。
//
//	@Summary	后台给某用户发消息（走发件池）
//	@Tags		QQ机器人
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body	dto.QqbotSendMessageRequest	true	"收件 openid + 内容"
//	@Success	200		{object}	dto.ResponseBody{data=dto.QqbotOutboxDto}
//	@ID			QqbotController_sendMessage
//	@Router		/qqbot/messages/send [post]
func (h *QqbotHandler) SendMessage(c *gin.Context) {
	var in dto.QqbotSendMessageRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		qqbotErr(c, errcode.ErrInvalidParam)
		return
	}
	task, e := h.Svc.SendChat(c.Request.Context(), in.OpenID, middleware.CurrentUserID(c), in.Content)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, task)
}

// ListOutbox GET /qqbot/outbox（发件池，含死信）。
//
//	@Summary	发件池列表（含 DEFERRED 积压与 FAILED 死信）
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page	query	int		false	"页码" example(1)
//	@Param		limit	query	int		false	"每页条数" example(10)
//	@Param		status	query	string	false	"状态：PENDING/SENDING/DEFERRED/SENT/FAILED" example(DEFERRED)
//	@Param		kind	query	string	false	"类型：INVITE_CODE/REJECT/CHAT/AUTO_REPLY" example(INVITE_CODE)
//	@Param		open_id	query	string	false	"按收件人过滤" example(A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4)
//	@Success	200		{object}	dto.ResponsePageBody{data=dto.PageBody{list=[]dto.QqbotOutboxDto}}
//	@ID			QqbotController_listOutbox
//	@Router		/qqbot/outbox [get]
func (h *QqbotHandler) ListOutbox(c *gin.Context) {
	page, limit := pageTx(c)
	list, total, e := h.Svc.ListOutbox(c.Request.Context(),
		c.Query("status"), c.Query("kind"), c.Query("open_id"), page, limit)
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// RetryOutbox POST /qqbot/outbox/{id}/retry（人工重试）。
//
//	@Summary	人工重试一条池任务（FAILED/DEFERRED → PENDING 并立即投递）
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	string	true	"池任务 ID" example(2b3c4d5e-0000-4f60-9bac-000000000002)
//	@Success	200	{object}	dto.ResponseBody{data=dto.QqbotOutboxDto}
//	@Failure	404	{object}	dto.ResponseBody	"池任务不存在"
//	@ID			QqbotController_retryOutbox
//	@Router		/qqbot/outbox/{id}/retry [post]
func (h *QqbotHandler) RetryOutbox(c *gin.Context) {
	task, e := h.Svc.RetryOutbox(c.Request.Context(), c.Param("id"))
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, task)
}

// Status GET /qqbot/status（健康检查）。
//
//	@Summary	机器人健康检查（配置齐否 + token 可用否 + 池子积压）
//	@Tags		QQ机器人
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	dto.ResponseBody{data=dto.QqbotStatusDto}
//	@ID			QqbotController_status
//	@Router		/qqbot/status [get]
func (h *QqbotHandler) Status(c *gin.Context) {
	st, e := h.Svc.Status(c.Request.Context())
	if e != nil {
		qqbotErr(c, e)
		return
	}
	response.Success(c, st)
}

// 让 dto 包成为本文件的真实 import（swag 只按注解所在文件的 import 列表解析跨包类型，
// docs/18 §三之一 约束 B）。不参与运行逻辑。
var _ = dto.ResponseBody{}

// 让 strings 保持被引用（本文件目前未直接用，保留以便后续加 query 归一化）。
var _ = strings.TrimSpace

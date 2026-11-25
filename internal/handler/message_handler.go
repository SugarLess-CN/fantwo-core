// 消息域 HTTP 处理层（对齐原 message.controller.ts，19 操作 + 私信/拉黑）。
//
// 路径前缀统一 /api/v1（骨架约定），即：
//
//	POST   /api/v1/message                                   创建消息（全员/部分/个人）
//	GET    /api/v1/message                                   我的消息（含全员通知）
//	GET    /api/v1/message/search                            高级查询
//	POST   /api/v1/message/read-all                          全部已读
//	POST   /api/v1/message/batch                             批量操作（read/delete）
//	GET    /api/v1/message/unread/count                      未读计数（七字段）
//	POST   /api/v1/message/private/read-batch                批量私信已读
//	GET    /api/v1/message/private/conversations             私信会话列表（游标）
//	GET    /api/v1/message/private/conversations/:userId/messages   与某人的私信历史
//	POST   /api/v1/message/private/conversations/:userId/read       整会话已读
//	POST   /api/v1/message/private/recall/:id                撤回私信
//	POST   /api/v1/message/private/block/:userId             拉黑
//	DELETE /api/v1/message/private/block/:userId             取消拉黑
//	GET    /api/v1/message/private/blocks                    拉黑列表
//	POST   /api/v1/message/private/:userId                   发送私信
//	GET    /api/v1/message/:id                               消息详情
//	PATCH  /api/v1/message/:id                               更新消息（message:manage）
//	DELETE /api/v1/message/:id                               删除消息（message:delete）
//	POST   /api/v1/message/:id/read                          单条已读
//
// 权限对齐源端 @Permissions：创建/更新需 message:manage，删除需 message:delete；
// 其余登录即可（登录态由路由组中间件保证）。
package handler

import (
	"github.com/SugarLess-CN/fantwo-core/internal/middleware"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/gin-gonic/gin"
)

// MessageHandler 消息域端点。
type MessageHandler struct {
	Svc      *service.MessageService
	Priv     *service.PrivateMessageService
	Presence *service.MessagePresenceService
}

// msgFail 消息域统一错误响应（带域后缀，避免与其它域 helper 同名）。
func msgFail(c *gin.Context, err error) {
	if err == nil {
		response.Fail(c, errcode.ErrInternal)
		return
	}
	if e, ok := err.(*errcode.Err); ok {
		response.Fail(c, e)
		return
	}
	response.Fail(c, errcode.ErrInternal.With(err.Error()))
}

// msgHasPerm 当前登录用户是否持有任一权限点（claims 来自 JWT，不查库）。
func msgHasPerm(c *gin.Context, perms ...string) bool {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		return false
	}
	for _, req := range perms {
		for _, p := range claims.Permissions {
			if p == req {
				return true
			}
		}
	}
	return false
}

// currentUID 当前登录用户 ID（路由组已保证登录，空值兜底 401）。
func currentUID(c *gin.Context) (string, bool) {
	uid := middleware.CurrentUserID(c)
	if uid == "" {
		response.Fail(c, errcode.ErrNeedAuth)
		return "", false
	}
	return uid, true
}

// ---------- 站内消息 ----------

// Create POST /message：创建消息（全员 / 部分 / 个人）。
//
//	@Summary		创建站内消息
//	@Description	创建全员/部分/个人通知；管理员（message:manage）可指定接收范围
//	@Tags			消息
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		service.CreateMessageInput	true	"CreateMessageInput"
//	@Success		200		{object}	ResponseBody
//	@Failure		403		{object}	ResponseBody
//	@ID				MessageController_create
//	@Router			/message [post]
func (h *MessageHandler) Create(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in service.CreateMessageInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.ErrBadRequest)
		return
	}
	out, err := h.Svc.Create(uid, msgHasPerm(c, "message:manage"), in)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// List GET /message：我的消息（含全员通知）。
//
//	@Summary		我的消息列表
//	@Tags			消息
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page	query		int		false	"页码"
//	@Param			limit	query		int		false	"每页条数"
//	@Success		200		{object}	ResponsePageBody
//	@ID				MessageController_findAll
//	@Router			/message [get]
func (h *MessageHandler) List(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	page, limit := pageOf(c)
	list, total, err := h.Svc.ListByUser(uid, page, limit)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// Search GET /message/search：高级查询（type/isRead/isBroadcast/keyword/senderId/receiverId）。
//
//	@Summary	消息高级查询
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page query int false "页码"
//	@Param		limit query int false "每页条数"
//	@Success	200	{object}	ResponsePageBody
//	@ID				MessageController_search
//	@Router		/message/search [get]
func (h *MessageHandler) Search(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	page, limit := pageOf(c)
	var isRead, isBroadcast *bool
	if v, exists := c.GetQuery("isRead"); exists {
		b := parseBool(v)
		isRead = &b
	}
	if v, exists := c.GetQuery("isBroadcast"); exists {
		b := parseBool(v)
		isBroadcast = &b
	}
	list, total, err := h.Svc.Search(
		uid,
		c.Query("keyword"),
		c.Query("type"),
		c.Query("senderId"),
		c.Query("receiverId"),
		isRead, isBroadcast,
		page, limit,
	)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.SuccessPage(c, list, total, page, limit)
}

// Detail GET /message/:id：单条详情。
//
//	@Summary	消息详情
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_findOne
//	@Router		/message/{id} [get]
func (h *MessageHandler) Detail(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Svc.FindOne(uid, c.Param("id"))
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// Update PATCH /message/:id：更新消息（发送者或 message:manage）。
//
//	@Summary	更新消息
//	@Tags		消息
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Param		body body service.UpdateMessageInput true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_update
//	@Router		/message/{id} [patch]
func (h *MessageHandler) Update(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in service.UpdateMessageInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.ErrBadRequest)
		return
	}
	out, err := h.Svc.Update(uid, c.Param("id"), msgHasPerm(c, "message:manage"), in)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// Remove DELETE /message/:id：删除消息（发送者或 message:manage）。
//
//	@Summary	删除消息
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_remove
//	@Router		/message/{id} [delete]
func (h *MessageHandler) Remove(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	if err := h.Svc.Remove(uid, c.Param("id"), msgHasPerm(c, "message:manage")); err != nil {
		msgFail(c, err)
		return
	}
	response.SuccessMsg(c, i18n.MessageDeleted, gin.H{"success": true})
}

// MarkRead POST /message/:id/read：单条已读。
//
//	@Summary	单条已读
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_markAsRead
//	@Router		/message/{id}/read [post]
func (h *MessageHandler) MarkRead(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	if err := h.Svc.MarkAsRead(uid, c.Param("id")); err != nil {
		msgFail(c, err)
		return
	}
	response.SuccessMsg(c, i18n.MessageMarkedRead, gin.H{"success": true})
}

// MarkAllRead POST /message/read-all：全部已读。
//
//	@Summary	全部已读
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_markAllAsRead
//	@Router		/message/read-all [post]
func (h *MessageHandler) MarkAllRead(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in service.MarkAllReadInput
	_ = c.ShouldBindJSON(&in) // body 可空
	out, err := h.Svc.MarkAllAsRead(uid, in)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// Batch POST /message/batch：批量操作（read / delete）。
//
//	@Summary	批量操作（已读/删除）
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body service.BatchMessageInput true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_batchOperation
//	@Router		/message/batch [post]
func (h *MessageHandler) Batch(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in service.BatchMessageInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.ErrBadRequest)
		return
	}
	out, err := h.Svc.BatchOperation(uid, msgHasPerm(c, "message:manage"), in)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// UnreadCount GET /message/unread/count：未读计数。
//
//	@Summary	未读计数
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_getUnreadCount
//	@Router		/message/unread/count [get]
func (h *MessageHandler) UnreadCount(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Svc.UnreadCount(uid)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// ---------- 私信 ----------

// SendPrivate POST /message/private/:userId：发送私信。
//
//	@Summary		发送私信
//	@Description	向指定用户发私信；被拉黑时返回 403。发送成功后经 WS 推送 privateMessage
//	@Tags			消息
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			userId	path		string	true	"接收者用户 ID"
//	@Param			body	body		service.SendPrivateInput	true	"SendPrivateInput{content,type}"
//	@Success		200		{object}	ResponseBody
//	@Failure		403		{object}	ResponseBody
//	@ID				MessageController_sendPrivateMessage
//	@Router			/message/private/{userId} [post]
func (h *MessageHandler) SendPrivate(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in service.SendPrivateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.ErrBadRequest)
		return
	}
	out, err := h.Priv.Send(uid, c.Param("userId"), in)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// PrivateConversations GET /message/private/conversations：会话列表（游标分页）。
//
//	@Summary		私信会话列表
//	@Tags			消息
//	@Produce		json
//	@Security		BearerAuth
//	@Param			cursor	query		string	false	"游标（上一页最后一条的 cursor）"
//	@Param			limit	query		int		false	"每页条数，默认 20"
//	@Success		200		{object}	ResponseBody
//	@ID				MessageController_getPrivateConversations
//	@Router			/message/private/conversations [get]
func (h *MessageHandler) PrivateConversations(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Priv.Conversations(uid, c.Query("cursor"), queryInt(c, "limit", 20))
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// PrivateHistory GET /message/private/conversations/:userId/messages：私信历史（读后自动已读）。
//
//	@Summary	私信历史
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_getPrivateConversation
//	@Router		/message/private/conversations/{userId}/messages [get]
func (h *MessageHandler) PrivateHistory(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Priv.ConversationMessages(uid, c.Param("userId"), c.Query("cursor"), queryInt(c, "limit", 20))
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// PrivateConversationRead POST /message/private/conversations/:userId/read：整会话已读。
//
//	@Summary	整会话已读
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_markPrivateConversationRead
//	@Router		/message/private/conversations/{userId}/read [post]
func (h *MessageHandler) PrivateConversationRead(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Priv.MarkConversationRead(uid, c.Param("userId"))
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// PrivateReadBatch POST /message/private/read-batch：批量私信已读。
//
//	@Summary	批量私信已读
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body body PrivateReadBatchRequest true "请求体"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_markPrivateMessagesRead
//	@Router		/message/private/read-batch [post]
func (h *MessageHandler) PrivateReadBatch(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in PrivateReadBatchRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.ErrBadRequest)
		return
	}
	out, err := h.Priv.MarkMessagesRead(uid, in.MessageIDs)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// RecallPrivate POST /message/private/recall/:id：撤回私信。
//
//	@Summary	撤回私信
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id path string true "id"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_recallPrivateMessage
//	@Router		/message/private/recall/{id} [post]
func (h *MessageHandler) RecallPrivate(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in) // body 可空
	out, err := h.Priv.Recall(uid, c.Param("id"), in.Reason)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// BlockUser POST /message/private/block/:userId：拉黑。
//
//	@Summary	拉黑用户
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_blockPrivateUser
//	@Router		/message/private/block/{userId} [post]
func (h *MessageHandler) BlockUser(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in)
	var reason *string
	if in.Reason != "" {
		reason = &in.Reason
	}
	out, err := h.Priv.Block(uid, c.Param("userId"), reason)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// UnblockUser DELETE /message/private/block/:userId：取消拉黑。
//
//	@Summary	取消拉黑
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Param		userId path string true "userId"
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_unblockPrivateUser
//	@Router		/message/private/block/{userId} [delete]
func (h *MessageHandler) UnblockUser(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Priv.Unblock(uid, c.Param("userId"))
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// Blocks GET /message/private/blocks：拉黑列表。
//
//	@Summary	我的拉黑列表
//	@Tags		消息
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	ResponseBody
//	@ID				MessageController_getBlockedUsers
//	@Router		/message/private/blocks [get]
func (h *MessageHandler) Blocks(c *gin.Context) {
	uid, ok := currentUID(c)
	if !ok {
		return
	}
	out, err := h.Priv.Blocks(uid)
	if err != nil {
		msgFail(c, err)
		return
	}
	response.Success(c, out)
}

// ---------- 小工具 ----------

// parseBool query 布尔解析（true/1/yes/on 为真）。
func parseBool(v string) bool {
	switch v {
	case "true", "1", "yes", "on", "TRUE", "True":
		return true
	default:
		return false
	}
}

// queryInt 取整型 query（缺省返回 def）。
func queryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
		if n > 1_000_000 {
			return def
		}
	}
	if n <= 0 {
		return def
	}
	return n
}

// ---- 请求体 DTO（注解引用，swag 按包内类型解析）----

// PrivateReadBatchRequest 私信批量已读请求体。
type PrivateReadBatchRequest struct {
	MessageIDs []string `json:"messageIds"`
}

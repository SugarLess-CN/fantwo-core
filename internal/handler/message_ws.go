// 消息域 WebSocket 业务分发与推送适配器。
//
// 分层位置：ws 包只负责连接与帧路由（不 import service），本文件把帧接到
// service 层，并把 service 的 MessagePublisher 接口适配到 ws.MsgGateway 的推送原语。
// 依赖方向：handler → {ws, service}；ws ↛ service；service ↛ ws 的实现（只依赖其常量）。
//
// 事件 → 应答帧对照（逐条对齐源端 message.gateway.ts）：
//
//	客户端事件                 服务端应答帧
//	-------------------------  --------------------------------------------
//	sendMessage                newMessage / privateMessage / privateConversationUpdated / unreadCount
//	getHistory                 history
//	getPrivateConversations    privateConversations
//	getPrivateHistory          privateHistory / privateConversationRead
//	readPrivateMessages        privateMessagesRead / privateMessagesReadReceipt
//	readPrivateConversation    privateConversationRead / privateMessagesReadReceipt
//	recallPrivateMessage       privateMessageRecalled / privateConversationUpdated
//	getUnreadCount             unreadCount
//	markAllAsRead              allMarkedAsRead / unreadCount
//	batchOperation             batchOperationResult / unreadCount
//	readMessage                read / unreadCount
package handler

import (
	"encoding/json"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/jwtx"
	"github.com/SugarLess-CN/fantwo-core/internal/service"
	"github.com/SugarLess-CN/fantwo-core/internal/ws"
)

// MsgPublisher 把 service.MessagePublisher 适配到 ws 网关。
//
// service 层只声明「按用户/全体推送」两个原语，具体落到 gorilla hub。
type MsgPublisher struct {
	Gateway *ws.MsgGateway
}

// SendToUser 定向推送。
func (p *MsgPublisher) SendToUser(userID, event string, data interface{}) {
	if p == nil || p.Gateway == nil {
		return
	}
	p.Gateway.SendToUser(userID, event, data)
}

// Broadcast 全体推送。
func (p *MsgPublisher) Broadcast(event string, data interface{}) {
	if p == nil || p.Gateway == nil {
		return
	}
	p.Gateway.Broadcast(event, data)
}

// MsgWSAuth 由 jwtx 签发的 token 还原连接态用户（WS 鉴权，对齐源端 handleConnection）。
//
// 源端只校验 token 与用户存在，不校验黑名单；这里复用同一口径：
// 验签成功即视为已登录，权限点从 claims 取（供广播类事件鉴权）。
func MsgWSAuth(signer *jwtx.Signer, users *service.MessageService) ws.MsgAuthFunc {
	return func(token string) (ws.MsgUser, bool) {
		if signer == nil {
			return ws.MsgUser{}, false
		}
		claims, err := signer.Parse(token)
		if err != nil || claims == nil || claims.UserID == "" {
			return ws.MsgUser{}, false
		}
		u := ws.MsgUser{ID: claims.UserID, Permissions: claims.Permissions}
		// 取昵称/头像（源端 connected 帧与 getProfile 都要用）；查不到用户视为认证失败
		if users != nil {
			profile, err := users.Profile(claims.UserID)
			if err != nil {
				return ws.MsgUser{}, false
			}
			u.Username = profile.Username
			u.Nickname = profile.Nickname
			u.Avatar = profile.Avatar
		}
		return u, true
	}
}

// MsgDispatch 事件分发实现（ws 包回调，返回错误码时网关推 error 帧）。
func MsgDispatch(svc *service.MessageService, priv *service.PrivateMessageService) ws.MsgDispatchFunc {
	return func(ctx *ws.MsgContext, event string, data json.RawMessage) (interface{}, string) {
		switch event {
		case ws.EventSendMessage:
			return dispatchSendMessage(ctx, svc, priv, data)
		case ws.EventGetHistory:
			return dispatchGetHistory(ctx, svc, data)
		case ws.EventGetPrivateConversations:
			return dispatchPrivateConversations(ctx, priv, data)
		case ws.EventGetPrivateHistory:
			return dispatchPrivateHistory(ctx, priv, data)
		case ws.EventReadPrivateMessages:
			return dispatchReadPrivateMessages(ctx, priv, data)
		case ws.EventReadPrivateConversation:
			return dispatchReadPrivateConversation(ctx, priv, data)
		case ws.EventRecallPrivateMessage:
			return dispatchRecallPrivate(ctx, priv, data)
		case ws.EventGetUnreadCount:
			return dispatchUnreadCount(ctx, svc)
		case ws.EventMarkAllAsRead:
			return dispatchMarkAllAsRead(ctx, svc, data)
		case ws.EventBatchOperation:
			return dispatchBatch(ctx, svc, data)
		case ws.EventReadMessage:
			return dispatchReadMessage(ctx, svc, data)
		default:
			ctx.EmitError(ws.ErrCodeUserNotFound, "不支持的事件: "+event)
			return nil, ""
		}
	}
}

// dispatchSendMessage 发送消息（私信 / 广播 / 群发），对齐源端 handleSendMessage。
func dispatchSendMessage(ctx *ws.MsgContext, svc *service.MessageService, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.SendMessageInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, ws.ErrCodeMessageSendFail
	}
	// 广播权限（源端 message:create）
	if in.IsBroadcast && !ctx.User.HasPerm("message:create", "message:manage") {
		return nil, ws.ErrCodeNoPermission
	}

	// 私信分支：type 缺省 private 且有明确接收者
	if (in.Type == "" || in.Type == service.MsgTypePrivate) && in.ToUserID != "" {
		out, err := priv.Send(ctx.User.ID, in.ToUserID, service.SendPrivateInput{
			Content:     in.Content,
			MessageKind: in.MessageKind,
			Payload:     in.Payload,
		})
		if err != nil {
			return nil, ws.ErrCodeMessageSendFail
		}
		// priv.Send 内部已推 newMessage/privateMessage/privateConversationUpdated
		ctx.Emit(ws.EventPrivateMessage, out)
		return out, ""
	}

	// 站内消息分支（广播 / 群发 / 自测）
	out, err := svc.Create(ctx.User.ID, ctx.User.HasPerm("message:manage"), service.CreateMessageInput{
		ReceiverID:  in.ToUserID,
		ReceiverIDs: in.ReceiverIDs,
		Content:     in.Content,
		Type:        in.Type,
		IsBroadcast: in.IsBroadcast,
	})
	if err != nil {
		return nil, ws.ErrCodeMessageSendFail
	}
	// 广播/群发由依赖方注入了推送回调，这里补一条发起者回显（源端 client.emit("newMessage")）
	if in.ReceiverIDs != nil && len(in.ReceiverIDs) > 0 {
		ctx.Emit(ws.EventNewMessage, out.Data)
	}
	return out, ""
}

// dispatchGetHistory 站内消息历史（对齐 handleGetHistory → 回 history 帧）。
func dispatchGetHistory(ctx *ws.MsgContext, svc *service.MessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.GetHistoryInput
	_ = json.Unmarshal(raw, &in)
	list, total, err := svc.ListByUser(ctx.User.ID, in.Page, in.Limit)
	if err != nil {
		return nil, ws.ErrCodeHistoryFail
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	payload := map[string]interface{}{
		"list": list,
		"meta": map[string]interface{}{
			"total": total, "page": page, "limit": limit,
			"totalPages": (total + int64(limit) - 1) / int64(limit),
		},
	}
	ctx.Emit(ws.EventHistory, payload)
	return payload, ""
}

// dispatchPrivateConversations 私信会话列表（对齐 handleGetPrivateConversations）。
func dispatchPrivateConversations(ctx *ws.MsgContext, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.CursorInput
	_ = json.Unmarshal(raw, &in)
	out, err := priv.Conversations(ctx.User.ID, in.Cursor, in.Limit)
	if err != nil {
		return nil, ws.ErrCodeConversationsFail
	}
	ctx.Emit(ws.EventPrivateConversations, out)
	return out, ""
}

// dispatchPrivateHistory 私信历史（对齐 handleGetPrivateHistory）。
func dispatchPrivateHistory(ctx *ws.MsgContext, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.GetPrivateHistoryInput
	if err := json.Unmarshal(raw, &in); err != nil || in.UserID == "" {
		return nil, ws.ErrCodePrivateHistoryFail
	}
	out, err := priv.ConversationMessages(ctx.User.ID, in.UserID, in.Cursor, in.Limit)
	if err != nil {
		return nil, ws.ErrCodePrivateHistoryFail
	}
	ctx.Emit(ws.EventPrivateHistory, out)
	return out, ""
}

// dispatchReadPrivateMessages 批量私信已读（对齐 handleReadPrivateMessages）。
func dispatchReadPrivateMessages(ctx *ws.MsgContext, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.ReadPrivateMessagesInput
	_ = json.Unmarshal(raw, &in)
	out, err := priv.MarkMessagesRead(ctx.User.ID, in.MessageIDs)
	if err != nil {
		return nil, ws.ErrCodePrivateReadFail
	}
	ctx.Emit(ws.EventPrivateMessagesRead, out)
	return out, ""
}

// dispatchReadPrivateConversation 整会话已读（对齐 handleReadPrivateConversation）。
func dispatchReadPrivateConversation(ctx *ws.MsgContext, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.ReadPrivateConversationInput
	if err := json.Unmarshal(raw, &in); err != nil || in.UserID == "" {
		return nil, ws.ErrCodeConversationReadFail
	}
	out, err := priv.MarkConversationRead(ctx.User.ID, in.UserID)
	if err != nil {
		return nil, ws.ErrCodeConversationReadFail
	}
	// 源端该分支只 return，不额外 emit（回执由 service 推送）
	return out, ""
}

// dispatchRecallPrivate 撤回私信（对齐 handleRecallPrivateMessage）。
func dispatchRecallPrivate(ctx *ws.MsgContext, priv *service.PrivateMessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.RecallPrivateMessageInput
	if err := json.Unmarshal(raw, &in); err != nil || in.MessageID == "" {
		return nil, ws.ErrCodeRecallFail
	}
	out, err := priv.Recall(ctx.User.ID, in.MessageID, in.Reason)
	if err != nil {
		return nil, ws.ErrCodeRecallFail
	}
	// service 已向双方推 privateMessageRecalled，这里补发起者回显
	ctx.Emit(ws.EventPrivateMessageRecalled, out)
	return out, ""
}

// dispatchUnreadCount 未读计数（对齐 handleGetUnreadCount）。
func dispatchUnreadCount(ctx *ws.MsgContext, svc *service.MessageService) (interface{}, string) {
	out, err := svc.UnreadCount(ctx.User.ID)
	if err != nil {
		return nil, ws.ErrCodeUnreadFail
	}
	ctx.Emit(ws.EventUnreadCount, out)
	return out, ""
}

// dispatchMarkAllAsRead 全部已读（对齐 handleMarkAllAsRead：回 allMarkedAsRead + unreadCount）。
func dispatchMarkAllAsRead(ctx *ws.MsgContext, svc *service.MessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.MarkAllAsReadInput
	_ = json.Unmarshal(raw, &in)
	out, err := svc.MarkAllAsRead(ctx.User.ID, service.MarkAllReadInput{
		Type: in.Type, IsBroadcast: in.IsBroadcast,
	})
	if err != nil {
		return nil, ws.ErrCodeMarkAllReadFail
	}
	ctx.Emit(ws.EventAllMarkedAsRead, out)
	if count, err := svc.UnreadCount(ctx.User.ID); err == nil {
		ctx.Emit(ws.EventUnreadCount, count)
	}
	return out, ""
}

// dispatchBatch 批量操作（对齐 handleBatchOperation：回 batchOperationResult）。
func dispatchBatch(ctx *ws.MsgContext, svc *service.MessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.BatchOperationInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, ws.ErrCodeBatchFail
	}
	out, err := svc.BatchOperation(ctx.User.ID, ctx.User.HasPerm("message:manage"), service.BatchMessageInput{
		MessageIDs: in.MessageIDs, Action: in.Action,
	})
	if err != nil {
		return nil, ws.ErrCodeBatchFail
	}
	ctx.Emit(ws.EventBatchOperationResult, out)
	return out, ""
}

// dispatchReadMessage 单条已读（对齐 handleReadMessage：回 read 帧）。
func dispatchReadMessage(ctx *ws.MsgContext, svc *service.MessageService, raw json.RawMessage) (interface{}, string) {
	var in ws.ReadMessageInput
	if err := json.Unmarshal(raw, &in); err != nil || in.MessageID == "" {
		return nil, ws.ErrCodeMarkReadFail
	}
	if err := svc.MarkAsRead(ctx.User.ID, in.MessageID); err != nil {
		return nil, ws.ErrCodeMarkReadFail
	}
	ctx.Emit(ws.EventRead, map[string]interface{}{"messageId": in.MessageID})
	if count, err := svc.UnreadCount(ctx.User.ID); err == nil {
		ctx.Emit(ws.EventUnreadCount, count)
	}
	return map[string]interface{}{"messageId": in.MessageID}, ""
}

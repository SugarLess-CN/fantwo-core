package qqbot

// 发消息：SendC2C / SendGroup。
//
// # 三种发送模式互斥（官方语义，docs/29 §3.2）
//
//	① 被动回复   MsgID 非空（或 EventID 非空） —— 60 分钟内，同一 msg_id 最多 4 次（msg_seq 1→4）
//	② 互动召回   IsWakeup = true              —— 用户对话后 30 天内，4 个周期各 1 条
//	③ 普通主动   两者都为空                    —— 单关系 20/qpm、每用户 1000 条/天
//
// 同时指定多个是**调用方 bug**，必须在本地拦下（ErrInvalidArgument）：
// 发给平台只会得到一个语义模糊的报错，而且白白消耗频控配额。

import (
	"context"
)

// SendC2C 发送单聊消息给指定用户（user_openid 来自事件的 author.user_openid）。
func (c *client) SendC2C(userOpenID string, req SendRequest) (*SendResponse, error) {
	return c.send(opSendC2C, "users", userOpenID, req)
}

// SendGroup 发送群消息（group_openid 来自事件的 d.group_openid）。
func (c *client) SendGroup(groupOpenID string, req SendRequest) (*SendResponse, error) {
	return c.send(opSendGroup, "groups", groupOpenID, req)
}

// send 两个渠道共用的发送实现（端点只差 users / groups 一段路径）。
func (c *client) send(op, kind, openID string, req SendRequest) (*SendResponse, error) {
	if e := c.ready(op); e != nil {
		return nil, e
	}
	if e := validateSendRequest(op, openID, req); e != nil {
		return nil, e
	}

	var out SendResponse
	// 接口签名不带 ctx（types.go 已定），这里用 Background，
	// 超时由 http.Client.Timeout 兜住（默认 10s）。
	if err := c.postJSON(context.Background(), op, c.endpoint(kind, openID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// validateSendRequest 发送前的本地校验。
//
// 为什么放在 SDK 里而不是调用方：这些是**平台协议的硬约束**，散落在业务代码里必然走样；
// 且拦在本地能省掉一次无效上游调用（主动消息有 20/qpm 的频控额度）。
func validateSendRequest(op, openID string, req SendRequest) *Error {
	if openID == "" {
		return invalidArg(op, "openid is required")
	}

	// ---- 三种发送模式互斥 ----
	passive := req.MsgID != "" || req.EventID != ""
	if req.MsgID != "" && req.EventID != "" {
		// 官方是「与 msg_id 二选一」，两个都给属于语义冲突。
		return invalidArg(op, "msg_id and event_id are mutually exclusive, pick one")
	}
	if req.IsWakeup && passive {
		// 互动召回与被动回复互斥：给了 msg_id 就该走被动回复，别同时声明召回。
		return invalidArg(op, "is_wakeup is mutually exclusive with msg_id/event_id")
	}
	if req.MsgSeq > 0 && !passive {
		// msg_seq 只与 msg_id / event_id 联用；单独给没有意义，
		// 说明调用方以为自己在被动回复却没带 ID —— 静默忽略会比报错更难查。
		return invalidArg(op, "msg_seq requires msg_id or event_id")
	}
	if req.MsgSeq < 0 {
		return invalidArg(op, "msg_seq must not be negative")
	}

	// ---- 内容与 msg_type 必须自洽 ----
	switch req.MsgType {
	case MsgTypeMarkdown:
		if req.Markdown == nil || req.Markdown.Content == "" {
			return invalidArg(op, "markdown is required when msg_type is %d", MsgTypeMarkdown)
		}
		if req.Content != "" {
			// 官方：填了 markdown 则 content 必须为空。
			return invalidArg(op, "content must be empty when msg_type is markdown")
		}
	case MsgTypeText:
		if req.Markdown != nil {
			// 传了 markdown 结构却声明 msg_type=0：平台会忽略 markdown，静默丢内容。
			return invalidArg(op, "markdown requires msg_type=%d, got %d", MsgTypeMarkdown, req.MsgType)
		}
		if req.Content == "" {
			return invalidArg(op, "content is required when msg_type is text")
		}
	case MsgTypeInput, MsgTypeMedia, MsgTypeCard:
		// 输入中状态 / 富媒体 / 图文卡片：内容在其它字段（resp.media / ark），
		// 本 SDK 只透传，不校验具体内容。
	default:
		return invalidArg(op, "unsupported msg_type %d", req.MsgType)
	}

	return nil
}

// SendMode 返回请求所属的发送模式（"passive_reply" / "wakeup" / "proactive"）。
//
// 只用于日志与排查，不参与校验决策；暴露出来是为了让调用方不必各写一份判断
// （发件池要按模式分流时正好用它）。
func SendMode(req SendRequest) string {
	switch {
	case req.MsgID != "" || req.EventID != "":
		return "passive_reply"
	case req.IsWakeup:
		return "wakeup"
	default:
		return "proactive"
	}
}

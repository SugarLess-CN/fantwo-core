// Package qqbot QQ 官方机器人（BOT API v2）独立 SDK。
//
// 只做两件事：**发消息** 与 **收事件（webhook）**。不依赖任何项目内部包，
// 可被任意 Go 项目直接引用（与 pkg/oauthkit 同一定位）。
//
// # 为什么自实现
//
// 生态里 `tencent-connect/botgo` 面向**频道（Guild）**为主、v1/v2 混排、依赖重
// （含 WebSocket 全家桶）；其余 fork 多为玩具级，没有错误码分类、也没有 token 并发刷新保护。
// 本项目只需要 HTTP 发消息 + webhook 校验。而**官方错误码分类表才是这个 SDK 的核心产出** ——
// 第三方库都没有做（见 errors.go 的 ErrorKind）。
//
// # 快速开始
//
//	c, err := qqbot.New(qqbot.Config{
//		AppID:     "1020xxxx",          // qqbot_app_id
//		AppSecret: "your-app-secret",   // qqbot_app_secret（取 token 用）
//		BotSecret: "your-bot-secret",   // qqbot_bot_secret（**webhook 验签用，与 AppSecret 不是一回事**）
//	})
//	if err != nil {
//		return err // 未配置齐全 → ErrNotConfigured
//	}
//
//	// ① 发消息。三种模式互斥，SDK 在本地拦截非法组合（KindInvalidArgument，不打上游）。
//	_, err = c.SendC2C("USER_OPENID", qqbot.SendRequest{
//		MsgType: qqbot.MsgTypeText,
//		Content: "你的邀请码是 ABC123",
//		MsgID:   msgID, // 被动回复：来自事件的 **d.id**（不是信封的 id）
//		MsgSeq:  1,
//	})
//
//	// ② 收事件。三态返回：op=13 回写 validation；op=0 处理业务事件；验签失败报错。
//	payload, validation, err := c.ParseWebhook(body, r.Header)
//	switch {
//	case err != nil:
//		w.WriteHeader(http.StatusForbidden) // fail-closed，不放过伪造请求
//	case validation != nil:
//		json.NewEncoder(w).Encode(validation) // 地址验证：原样回写
//	case payload.Type == qqbot.EventC2CMessageCreate:
//		var msg qqbot.C2CMessage
//		if err := payload.DecodeData(&msg); err != nil { ... }
//		// msg.ID 才是被动回复要用的 msg_id
//	}
//
// # 三种发送模式（官方互斥语义，docs/29 §3.2）
//
//	① 被动回复  MsgID / EventID 非空  —— 收到消息后 60 分钟内，同一 msg_id 最多 4 次（msg_seq 1→4）
//	② 互动召回  IsWakeup = true       —— 用户对话后 30 天内，4 个周期各 1 条
//	③ 普通主动  两者都为空             —— 单关系 20/qpm、每用户 1000 条/天，用户可关闭
//
// 用户可在客户端关掉「允许主动发送」，此后 ②③ 一律失败（40054013）。
//
// # 错误处理：只看 Kind，不认错误码
//
// 所有失败都是 *Error，调用方**不需要认识任何官方错误码**：
//
//	_, err := c.SendC2C(openid, req)
//	var qe *qqbot.Error
//	if errors.As(err, &qe) {
//		switch qe.Kind {
//		case qqbot.KindRetryable:     // 频控/去重/上游抖动 → 退避重试
//		case qqbot.KindWindowExpired: // 被动窗口过期 → 进池等待，用户下次发言再投
//		case qqbot.KindPermanent:     // 用户拒收/违规/无好友 —— 重试无用，进死信
//		case qqbot.KindConfig:        // 凭据/机器人状态问题 —— 改配置
//		case qqbot.KindInvalidArgument: // 我传错了 —— 改代码
//		}
//	}
//
// 注意 KindWindowExpired **不算可重试**（Retryable() 返回 false）：它不是「等一会儿再试」，
// 而是「等用户开口」，盲目重试只会反复撞 40034128 并消耗频控额度。
//
// # access_token
//
// 内存缓存 + 并发安全：多人同时发消息只刷一次上游（互斥 + 进锁双检）。
// 缓存有效期 = `expires_in - 60s`（官方语义：临近过期 60 秒内才返回新值）。
// 收到 401 会丢缓存，下一次自动取新 token。Ping() 刻意不吃缓存 —— 健康检查要的是
// 「现在能不能拿到 token」，缓存命中的 200 会把失效凭据一直报成健康。
//
// # 不引入第三方依赖
//
// 只用标准库（net/http / encoding/json / crypto/ed25519）。不依赖 go.mod 变更。
package qqbot

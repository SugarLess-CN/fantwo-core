package qqbot

import (
	"errors"
	"fmt"
)

// ErrorKind 错误归类 —— 本 SDK 最核心的抽象。
//
// 调用方（比如发件池）**不需要认识任何官方错误码**，只看这三类决定怎么做：
//
//	KindRetryable     退避重试（留在池子 PENDING，推后 next_retry_at）
//	KindWindowExpired 被动窗口过期/次数用尽 → 消息**留在池子 DEFERRED**，
//	                  等用户下次发消息激活窗口再发（用户明确要求的语义）
//	KindPermanent     永久失败，重试无意义 → 出池进死信，需人工处理
//
// 为什么必须由 SDK 分类：官方错误码有 40+ 个且会变，散落在调用方必然走样。
// 分类表见 KindOf。
type ErrorKind int

const (
	// KindUnknown 未知错误（保守按 Retryable 处理，避免误判成永久失败丢消息）。
	KindUnknown ErrorKind = iota
	// KindRetryable 可重试：频控、去重冲突、上游临时异常。
	KindRetryable
	// KindWindowExpired 被动回复窗口过期或次数用尽 —— 进池等待窗口重新激活。
	KindWindowExpired
	// KindPermanent 永久失败：用户拒收、无好友关系、内容违规/超长、权限不足。
	KindPermanent
	// KindConfig 配置/凭据问题（appid/secret 错、机器人不存在或被封禁）。
	KindConfig
	// KindInvalidArgument 调用方参数错误（三种发送模式互斥被破坏等），重试无用。
	KindInvalidArgument
	// KindWakeupExhausted 互动召回额度已用尽（40034122）。
	//
	// 调用方动作：**去掉 IsWakeup 用普通主动消息重发一次**（而不是重试召回）。
	// 与 KindRetryable 的区别：Retryable 是「同样的请求稍后再发」，
	// 本类是「换一种发送方式」，请求体要变 —— 混在一起会让调用方无限重试召回
	// （召回额度已用尽，重试必然一直撞到 4 个周期结束）。
	KindWakeupExhausted
)

// String 便于日志。
func (k ErrorKind) String() string {
	switch k {
	case KindRetryable:
		return "retryable"
	case KindWindowExpired:
		return "window_expired"
	case KindPermanent:
		return "permanent"
	case KindConfig:
		return "config"
	case KindInvalidArgument:
		return "invalid_argument"
	case KindWakeupExhausted:
		return "wakeup_exhausted"
	default:
		return "unknown"
	}
}

// Retryable 是否值得重试（仅 KindRetryable / KindUnknown）。
//
// 注意两个「不算可重试」的类，它们的正确动作都不是重试：
//   - KindWindowExpired：不是「等一会儿再试」，而是「等用户开口」——
//     盲目重试只会反复撞 40034128 并浪费频控额度；
//   - KindWakeupExhausted：不是重试，而是「换一种发送方式」（去掉 is_wakeup 走普通主动）。
func (k ErrorKind) Retryable() bool {
	return k == KindRetryable || k == KindUnknown
}

// Error SDK 统一错误类型。
type Error struct {
	// Kind 归类（调用方主要看这个）。
	Kind ErrorKind
	// Code 官方业务错误码（0 表示非业务错误，如网络/解析）。
	Code int
	// Message 官方 message（仅人工排查，官方明说内容可能随时调整，禁止用于分支判断）。
	Message string
	// Op 出错的操作（send_c2c / send_group / get_token / parse_webhook）。
	Op string
	// Err 底层错误（网络/JSON 等）。
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != 0 {
		return fmt.Sprintf("qqbot %s: code=%d kind=%s msg=%s", e.Op, e.Code, e.Kind, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("qqbot %s: kind=%s err=%v", e.Op, e.Kind, e.Err)
	}
	return fmt.Sprintf("qqbot %s: kind=%s", e.Op, e.Kind)
}

// Unwrap 支持 errors.Is/As 穿透到底层错误。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// 哨兵错误（与 oauthkit 同口径：调用方用 errors.Is 判断）。
var (
	// ErrNotConfigured 未配置齐全（appid/secret/bot_secret 缺失）。
	ErrNotConfigured = errors.New("qqbot: not configured")
	// ErrInvalidArgument 调用方参数错误（如三种发送模式同时指定、空 openid）。
	ErrInvalidArgument = errors.New("qqbot: invalid argument supplied by caller")
	// ErrSignatureInvalid webhook 签名校验失败。
	ErrSignatureInvalid = errors.New("qqbot: webhook signature invalid")
	// ErrAccessToken 获取 access_token 失败。
	ErrAccessToken = errors.New("qqbot: failed to obtain access token")
)

// KindOf 把官方业务错误码映射成 ErrorKind。
//
// 表来源：QQ 机器人官方「发送单聊消息」错误码表（docs/29 §4.2 有完整对照）。
// **新增错误码只改这一处** —— 调用方永远不用动。
func KindOf(code int) ErrorKind {
	switch code {
	// ---- 窗口过期 / 次数用尽 → 进池等待 ----
	case 40034005, // 回复消息 msg_id 已过期
		304103,   // 消息 ID 已过期，不能回复
		40034128, // 被动回复时间或次数超限
		40034026: // 请求参数 event_id 已过期
		return KindWindowExpired

	// ---- 可重试 ----
	case 40034100, // 主动消息发送超过频控限制
		40054005, // 消息被去重（msg_seq 冲突，递增后可重试）
		50055002, // 消息发送异常，请稍后重试
		100001,   // Too many requests（token 接口）
		40034004: // 富媒体信息转存失败
		return KindRetryable

	// ---- 永久失败 ----
	case 40054013, // 用户拒收消息（关闭了「允许主动发送」）
		40054004, // 无好友关系
		40054006, // 验证好友关系失败
		40054007, // 消息长度超限
		40054018, // 消息过长或异常
		40034006, // 消息内容违规
		40034105, // 主动消息无权限
		40034024, // msg_id 无效或越权（拿错了 ID，重试没用）
		40034025, // event_id 无效
		40034027, // 该事件不支持回复消息
		40034008, // markdown 参数有空值
		40034009, // markdown 参数有换行符
		40034010, // 模版参数含 markdown 语法
		40034011, // 无效的 markdown 内容
		40034124, // markdown 消息参数错误
		40034127, // 无 markdown 模板权限
		304004,   // 无权限使用该 ARK 模板
		304061,   // 消息内容无效
		304062,   // 订阅按钮数量达到上限
		304064,   // 订阅消息未授权
		304080,   // 文件信息无效
		22006,    // 消息类型与内容不匹配
		50059,    // 输入类型错误
		40034106, // 消息不支持该指令类型
		40034108, // 指令参数长度超限
		40034109, // 指令参数解析失败
		40054016, // 机器人已下线
		340067:   // 获取机器人信息失败
		return KindPermanent

	// ---- 配置 / 凭据 ----
	case 100007, // appid invalid（含机器人被封禁或已删除）
		100016, // invalid appid or secret
		10004:  // 机器人不存在
		return KindConfig

	// ---- 召回额度用尽：换发送方式，不是重试 ----
	case 40034122: // 召回消息已达区间上限 → 调用方去掉 is_wakeup 走普通主动重发一次
		return KindWakeupExhausted

	default:
		return KindUnknown
	}
}

// newErr 构造 *Error（SDK 内部用）。
func newErr(op string, code int, message string) *Error {
	return &Error{Kind: KindOf(code), Code: code, Message: message, Op: op}
}

// wrapErr 包装底层错误（网络/JSON）。
func wrapErr(op string, kind ErrorKind, err error) *Error {
	return &Error{Kind: kind, Op: op, Err: err}
}

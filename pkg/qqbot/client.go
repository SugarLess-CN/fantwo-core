package qqbot

// Client 实现：构造 + HTTP 封装 + Ping。
//
// 三个文件的分工：
//
//	client.go   构造、HTTP 往返、响应错误归一化、Ping
//	token.go    access_token 缓存与并发刷新
//	message.go  SendC2C / SendGroup + 参数互斥校验
//	webhook.go  入站：验签 + 解析 + 地址验证
//
// 本文件不含任何业务语义 —— 谁该收消息、发什么内容，属于调用方。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 操作标签（进 *Error.Op，日志与排查用）。
const (
	opSendC2C      = "send_c2c"
	opSendGroup    = "send_group"
	opParseWebhook = "parse_webhook"
	opPing         = "ping"
)

// client Client 接口实现。
type client struct {
	cfg        Config
	httpClient *http.Client
	tokens     *tokenSource
}

var _ Client = (*client)(nil)

// New 构造客户端。
//
// 只校验 appid / secret（webhook 验签需要的 BotSecret 单独在 ParseWebhook 里判）——
// 这样「只发消息、不收事件」的用法不必配 BotSecret。
//
// 未配置齐全时返回 ErrNotConfigured，而不是返回一个会在用到时才炸的 client：
// 配置缺失属于启动期错误，越早暴露越好（调用方据此把功能整体降级，而不是每条消息都失败）。
func New(cfg Config) (Client, error) {
	return newClient(cfg, nil)
}

// NewWithHTTPClient 同 New，但可注入自定义 *http.Client（自定义 Transport / 代理 / 测试）。
//
// hc 为 nil 时退回默认（超时取 Config.HTTPTimeout，默认 10s）。
func NewWithHTTPClient(cfg Config, hc *http.Client) (Client, error) {
	return newClient(cfg, hc)
}

// newClient 内部构造（返回具体类型，便于同包单测拿 tokens）。
func newClient(cfg Config, hc *http.Client) (*client, error) {
	cfg = cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if hc == nil {
		timeout := defaultHTTPTimeout
		if cfg.HTTPTimeout > 0 {
			timeout = time.Duration(cfg.HTTPTimeout) * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}

	c := &client{cfg: cfg, httpClient: hc}
	c.tokens = &tokenSource{
		httpClient: hc,
		tokenURL:   cfg.TokenURL,
		appID:      cfg.AppID,
		appSecret:  cfg.AppSecret,
		now:        time.Now,
	}
	return c, nil
}

// Ping 健康检查：强制拉一次 access_token，验证 appid/secret 可用。
//
// 刻意不吃缓存 —— 健康检查的价值是「现在能不能拿到 token」，
// 缓存命中的 200 会把失效的凭据一直报成健康。
func (c *client) Ping() error {
	if e := c.ready(opPing); e != nil {
		return e
	}
	if _, err := c.tokens.force(context.Background()); err != nil {
		return err
	}
	return nil
}

// ready 检查客户端可用（nil 接收者 / 配置缺失都归一成 KindConfig）。
//
// 返回 *Error 而不是 error，让调用方统一 errors.As 取 Kind。
func (c *client) ready(op string) *Error {
	if c == nil || c.tokens == nil || c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return &Error{Kind: KindConfig, Op: op, Err: ErrNotConfigured}
	}
	return nil
}

// invalidArg 构造「调用方参数错误」（KindInvalidArgument，重试无用）。
//
// 必须包住 ErrInvalidArgument 哨兵，调用方才能 errors.Is 判断；
// 同时是 *Error，便于 errors.As 取 Kind —— 两条路都给。
func invalidArg(op, format string, args ...any) *Error {
	return &Error{
		Kind: KindInvalidArgument,
		Op:   op,
		Err:  fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), ErrInvalidArgument),
	}
}

// apiResponse 响应体里错误字段的探针（成功响应不含 code）。
type apiResponse struct {
	Code    flexInt64 `json:"code"`
	ErrCode flexInt64 `json:"err_code"`
	Message string    `json:"message"`
	Msg     string    `json:"msg"`
}

// code 返回业务错误码（0 = 无错）。
func (r *apiResponse) codeValue() int {
	if c := int(r.Code); c != 0 {
		return c
	}
	return int(r.ErrCode)
}

// message 返回错误信息。
func (r *apiResponse) messageValue() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}

// postJSON 带鉴权发一次 POST，并把响应归一化成 (业务码 / 哨兵) 两类结果。
//
// 三层处理顺序（顺序很重要）：
//  1. **业务码优先**：官方业务错误走响应体 code，HTTP 可能仍是 200；
//     先判业务码，才能把 40034005「窗口过期」这类可分类错误交给 KindOf。
//  2. HTTP 非 2xx 且无业务码 → 上游/网关异常（KindUnknown，按可重试处理）。
//  3. 2xx 且无业务码 → 解析 out。
//
// withAuth=false 只用于 token 接口（它不带 Authorization）。
func (c *client) postJSON(ctx context.Context, op, endpoint string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return wrapErr(op, KindInvalidArgument, err)
	}

	token, err := c.tokens.get(ctx)
	if err != nil {
		return err
	}

	status, respBody, err := c.doPost(ctx, endpoint, token, body)
	if err != nil {
		return wrapErr(op, KindRetryable, err)
	}

	// 401 通常是 token 失效（被平台提前回收 / 换密钥）：丢缓存并给出可重试错误，
	// 让调用方下一次自动拿到新 token。不在这里递归重试，避免放大上游压力。
	if status == http.StatusUnauthorized {
		c.tokens.invalidate()
		var probe apiResponse
		if json.Unmarshal(respBody, &probe) == nil {
			if code := probe.codeValue(); code != 0 {
				return newErr(op, code, probe.messageValue())
			}
		}
		return wrapErr(op, KindRetryable, fmt.Errorf("qqbot: http 401 (access token refreshed for next attempt)"))
	}

	var probe apiResponse
	probeOK := json.Unmarshal(respBody, &probe) == nil
	if probeOK {
		if code := probe.codeValue(); code != 0 {
			return newErr(op, code, probe.messageValue())
		}
	}

	if status < 200 || status > 299 {
		// 只带状态码，不带响应体：响应体可能含敏感内容（同 docs/14 §7.1.2 禁令 2 口径）。
		return wrapErr(op, KindUnknown, fmt.Errorf("qqbot: http %d", status))
	}
	if !probeOK {
		return wrapErr(op, KindUnknown, fmt.Errorf("qqbot: response is not json"))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return wrapErr(op, KindUnknown, fmt.Errorf("qqbot: decode response: %v", err))
		}
	}
	return nil
}

// doPost 执行请求并读回 (状态码, body)。
func (c *client) doPost(ctx context.Context, endpoint, token string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// 鉴权方案是 `QQBot {token}`（不是 Bearer）。
	req.Header.Set("Authorization", "QQBot "+token)
	// X-Union-Appid 官方要求带上，缺了可能被拒。
	req.Header.Set("X-Union-Appid", c.cfg.AppID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

// endpoint 拼发送地址：{APIBase}/v2/users/{openid}/messages。
//
// openid 必须 PathEscape —— 它由客户端上报，直接拼进路径会被 ../ 之类污染。
func (c *client) endpoint(kind, openID string) string {
	base := strings.TrimRight(c.cfg.APIBase, "/")
	switch kind {
	case "users":
		return base + "/v2/users/" + url.PathEscape(openID) + "/messages"
	default:
		return base + "/v2/groups/" + url.PathEscape(openID) + "/messages"
	}
}

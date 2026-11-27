// Package service 短信发送 provider（通用 HTTP 网关）。
//
// 「可配置即生效、未配置即降级」：
//   - 渠道配置存 config 表的 `sms_channels`（JSON 数组），或单个网关键 `sms_gateway_*`；
//   - 未配置任何启用渠道时 Configured()=false，调用方只写 Redis 并把验证码打日志（开发模式）；
//   - 已配置时按模板真实发起 HTTP 请求，并按 `successContains` / HTTP 状态码判定成功。
//
// 为什么用 config 表而不是独立渠道表：Go 侧没有 `admin/sms/channels` 表
// （原 NestJS 仓库也没有短信模块，全仓库 grep 无实现），
// 用 config 表的 JSON 值即可满足「渠道可配置」，且不新增表/不侵入基础设施域模型。
//
// 渠道 JSON 结构：
//
//	[{"name":"dev-gateway","enabled":true,"url":"https://sms.example.com/send",
//	  "method":"POST","contentType":"application/json",
//	  "headers":{"X-App-Key":"APPKEY"},
//	  "bodyTemplate":"{\"phone\":\"{{phone}}\",\"code\":\"{{code}}\",\"scene\":\"{{scene}}\"}",
//	  "successContains":["\"ok\":true"],"timeoutMs":5000}]
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 短信渠道配置键。
const cfgSMSChannels = "sms_channels"

// SMSChannel 一个短信渠道配置。
type SMSChannel struct {
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	URL             string            `json:"url"`
	Method          string            `json:"method"`       // GET / POST（默认 POST）
	ContentType     string            `json:"content_type"` // 默认 application/json
	Headers         map[string]string `json:"headers"`
	BodyTemplate    string            `json:"body_template"`
	SuccessContains []string          `json:"success_contains"`
	TimeoutMs       int               `json:"timeout_ms"`
}

// SMSSender 短信发送器。
type SMSSender struct {
	d      *Deps
	client *http.Client
}

// NewSMSSender 构造（HTTP 客户端复用，超时按渠道配置覆盖）。
func NewSMSSender(d *Deps) *SMSSender {
	return &SMSSender{d: d, client: &http.Client{Timeout: 10 * time.Second}}
}

// channels 解析启用中的渠道列表（JSON 非法时视为未配置）。
func (s *SMSSender) channels() []SMSChannel {
	if s == nil || s.d == nil || s.d.Cfg == nil {
		return nil
	}
	raw := strings.TrimSpace(s.d.Cfg.GetString(cfgSMSChannels, ""))
	if raw == "" {
		return nil
	}
	var list []SMSChannel
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		s.d.Logger.Warn("sms_channels 配置不是合法 JSON 数组，短信降级为开发模式")
		return nil
	}
	out := make([]SMSChannel, 0, len(list))
	for _, c := range list {
		if c.Enabled && c.URL != "" {
			out = append(out, c)
		}
	}
	return out
}

// Configured 是否存在启用中的渠道。
func (s *SMSSender) Configured() bool { return len(s.channels()) > 0 }

// Send 发送验证码短信：逐个尝试启用渠道，任一成功即返回。
// 全部失败时返回最后一个错误；未配置返回 ErrProviderNotConfigured。
func (s *SMSSender) Send(ctx context.Context, phone, code, scene string) error {
	list := s.channels()
	if len(list) == 0 {
		return ErrProviderNotConfigured
	}
	var lastErr error
	for _, ch := range list {
		if err := s.sendOne(ctx, ch, phone, code, scene); err != nil {
			lastErr = err
			s.d.Logger.Warn("短信渠道发送失败 channel=" + ch.Name + " err=" + err.Error())
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("短信渠道不可用")
	}
	return lastErr
}

// sendOne 单渠道发送。
func (s *SMSSender) sendOne(ctx context.Context, ch SMSChannel, phone, code, scene string) error {
	vars := map[string]string{"phone": phone, "code": code, "scene": scene}
	body := renderTemplate(ch.BodyTemplate, vars)
	method := strings.ToUpper(strings.TrimSpace(ch.Method))
	if method == "" {
		method = http.MethodPost
	}
	contentType := ch.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	var (
		req *http.Request
		err error
	)
	if method == http.MethodGet {
		target := ch.URL
		if body != "" && !strings.Contains(target, "?") {
			target = target + "?" + url.Values{"payload": {body}}.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, ch.URL, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", contentType)
		}
	}
	if err != nil {
		return err
	}
	for k, v := range ch.Headers {
		req.Header.Set(k, renderTemplate(v, vars))
	}
	timeout := time.Duration(ch.TimeoutMs) * time.Millisecond
	if ch.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s", strconv.Itoa(resp.StatusCode))
	}
	text := string(raw)
	for _, want := range ch.SuccessContains {
		if want != "" && !strings.Contains(text, want) {
			return fmt.Errorf("响应未包含预期内容 %q", want)
		}
	}
	return nil
}

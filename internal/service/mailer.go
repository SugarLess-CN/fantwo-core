// Package service 邮件发送 provider（SMTP）。
//
// 设计：**可配置即生效、未配置即降级**。
//   - 配置键从 config 表读（smtp_host / smtp_port / smtp_user / smtp_password /
//     smtp_from / smtp_from_name / smtp_implicit_tls）；
//   - 未配置（host 或 from 为空）时 Configured() 返回 false，调用方只写 Redis 并打 log；
//   - 已配置时用标准库 net/smtp 发送（支持 465 隐式 TLS 与 587 STARTTLS），零新增依赖。
package service

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTP 配置键（config 表 key，未配置即降级）。
const (
	cfgSMTPHost        = "smtp_host"
	cfgSMTPPort        = "smtp_port"
	cfgSMTPUser        = "smtp_user"
	cfgSMTPPassword    = "smtp_password"
	cfgSMTPFrom        = "smtp_from"
	cfgSMTPFromName    = "smtp_from_name"
	cfgSMTPImplicitTLS = "smtp_implicit_tls"
)

// Mailer 邮件发送器。
type Mailer struct {
	d *Deps
}

// NewMailer 构造。
func NewMailer(d *Deps) *Mailer { return &Mailer{d: d} }

// Configured 是否已配置 SMTP（host + from 均非空）。
func (m *Mailer) Configured() bool {
	if m == nil || m.d == nil || m.d.Cfg == nil {
		return false
	}
	return m.d.Cfg.GetString(cfgSMTPHost, "") != "" && m.d.Cfg.GetString(cfgSMTPFrom, "") != ""
}

// Send 发送纯文本/HTML 邮件。未配置时返回 ErrProviderNotConfigured。
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Configured() {
		return ErrProviderNotConfigured
	}
	host := m.d.Cfg.GetString(cfgSMTPHost, "")
	port := m.d.Cfg.GetInt(cfgSMTPPort, 587)
	user := m.d.Cfg.GetString(cfgSMTPUser, "")
	pass := m.d.Cfg.GetString(cfgSMTPPassword, "")
	from := m.d.Cfg.GetString(cfgSMTPFrom, "")
	fromName := m.d.Cfg.GetString(cfgSMTPFromName, "")
	implicit := m.d.Cfg.GetBool(cfgSMTPImplicitTLS, port == 465)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	msg := buildMailMessage(from, fromName, to, subject, body)

	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	if implicit {
		return sendImplicitTLS(addr, host, auth, from, to, msg)
	}
	// 非隐式：smtp.SendMail 在服务端支持时自动 STARTTLS。
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	return deliver(client, from, to, msg)
}

// sendImplicitTLS 465 端口隐式 TLS 发送。
func sendImplicitTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr,
		&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = client.Close() }()
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	return deliver(client, from, to, msg)
}

// deliver 投递（MAIL FROM / RCPT TO / DATA）。
func deliver(c *smtp.Client, from, to string, msg []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMailMessage 组装 MIME 邮件体（UTF-8 + HTML）。
func buildMailMessage(from, fromName, to, subject, body string) []byte {
	var b strings.Builder
	if fromName != "" {
		b.WriteString(fmt.Sprintf("From: %s <%s>\r\n", encodeHeader(fromName), from))
	} else {
		b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	}
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", encodeHeader(subject)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64Lines([]byte(body)))
	return []byte(b.String())
}

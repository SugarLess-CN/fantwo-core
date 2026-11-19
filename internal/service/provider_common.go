// Package service provider 公共辅助：未配置错误、MIME 头编码、模板渲染。
package service

import (
	"encoding/base64"
	"errors"
	"strings"
)

// ErrProviderNotConfigured provider 未配置（调用方据此降级为「只写 Redis + 打 log」）。
var ErrProviderNotConfigured = errors.New("provider 未配置")

// encodeHeader RFC 2047 头编码（中文主题/发件人名）。
func encodeHeader(s string) string {
	needEncode := false
	for _, r := range s {
		if r > 127 {
			needEncode = true
			break
		}
	}
	if !needEncode {
		return s
	}
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// base64Lines 标准 base64 并按 76 列折行（MIME 要求）。
func base64Lines(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\r\n")
	return b.String()
}

// renderTemplate 模板替换：占位符形如 {{phone}} / {{code}} / {{scene}}。
func renderTemplate(tpl string, vars map[string]string) string {
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

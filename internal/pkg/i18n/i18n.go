// Package i18n 国际化文案。
//
// 设计原则：
//   - **所有面向客户端的文案必须是英文**，通过 key 查表得到，不写死在业务代码里；
//   - key 命名规范：`<domain>.<subject>.<result>`，全小写、点分、无空格，
//     例：`user.already_exists` / `upload.file_too_large` / `common.ok`；
//   - 业务代码只持有 Key 与参数，不持有文案；换语言只换文案表；
//   - 当前仅提供 en（英文）。新增语言时在 catalogs 里加一份即可，
//     未覆盖的 key 自动回退 en。
//
// 加新文案的流程（不要绕过）：
//  1. 在 keys.go 加一个 Key 常量；
//  2. 在 en.go 的文案表里加对应英文；
//  3. 业务代码引用该常量。
//
// 严禁：在 handler/service 里直接写中文字符串作为响应 message。
package i18n

import (
	"fmt"
	"strings"
)

// Key 文案键。用自定义类型防止与普通 string 混用（例如误把中文文案当 key 传）。
type Key string

// Locale 语言标识。
type Locale string

// 支持的语言。默认英文。
const (
	LocaleEN Locale = "en"
)

// DefaultLocale 默认语言：英文（用户要求所有响应均为英文）。
const DefaultLocale = LocaleEN

// catalogs 文案表：locale → (key → 文案)。
//
// 只放 en。新增语言时在此追加，未命中的 key 回退 en。
var catalogs = map[Locale]map[Key]string{
	LocaleEN: enCatalog,
}

// T 按默认语言取文案。key 不存在时返回 key 本身（便于暴露漏配）。
func T(k Key) string {
	return TIn(DefaultLocale, k)
}

// TIn 按指定语言取文案；该语言缺该 key 时回退英文。
func TIn(loc Locale, k Key) string {
	if cat, ok := catalogs[loc]; ok {
		if v, ok := cat[k]; ok && v != "" {
			return v
		}
	}
	if v, ok := enCatalog[k]; ok && v != "" {
		return v
	}
	// 漏配：返回 key 本身，方便日志/测试一眼看出
	return string(k)
}

// Tf 取文案并格式化。文案里的占位符用 fmt 风格（%s / %d / %.1f）。
func Tf(k Key, args ...interface{}) string {
	return sprintfSafe(T(k), args)
}

// TfIn 按指定语言取文案并格式化。
func TfIn(loc Locale, k Key, args ...interface{}) string {
	return sprintfSafe(TIn(loc, k), args)
}

// sprintfSafe 无参数时原样返回，避免文案里本就有 % 时被 Sprintf 误解析
// （例如 "100%% 完成" 这类内容）。
func sprintfSafe(format string, args []interface{}) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// ParseLocale 把 Accept-Language 头或语言标识归一成支持的语言。
// 目前只支持 en，其他一律回退默认。
func ParseLocale(raw string) Locale {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return DefaultLocale
	}
	// 取逗号前的第一个语言标签，忽略 q 权重
	if i := strings.IndexAny(raw, ",;"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	if strings.HasPrefix(raw, "en") {
		return LocaleEN
	}
	return DefaultLocale
}

// Keys 返回英文文案表里全部 key（供测试校验完整性与排查漏配）。
func Keys() []Key {
	out := make([]Key, 0, len(enCatalog))
	for k := range enCatalog {
		out = append(out, k)
	}
	return out
}

// 基础设施域本地错误辅助。
//
// 国际化改造后：错误文案一律走 i18n key，本文件不再接受裸中文/英文字符串
// 作为 message（原 svcUnavailable(msg string) 的用法已废弃）。
package infra

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/i18n"
)

// svcUnavailable 服务依赖不可用（DB / Redis / S3 / ES 未初始化或降级）。
//
// 默认文案 "Dependency service unavailable, please try again later"；
// 需要更具体的提示时用 svcUnavailableKey。
func svcUnavailable() *errcode.Err {
	return &errcode.Err{
		Code: http.StatusServiceUnavailable,
		HTTP: http.StatusServiceUnavailable,
		Key:  i18n.CommonDependencyUnavailable,
	}
}

// svcUnavailableKey 带具体 i18n key 的依赖不可用错误。
func svcUnavailableKey(k i18n.Key, args ...interface{}) *errcode.Err {
	return &errcode.Err{
		Code: http.StatusServiceUnavailable,
		HTTP: http.StatusServiceUnavailable,
		Key:  k,
		Args: args,
	}
}

// svcInternal 包装内部错误（真实错误信息进日志，对外统一英文提示）。
func svcInternal(format string, args ...interface{}) *errcode.Err {
	return errcode.ErrInternal.Withf(format, args...)
}

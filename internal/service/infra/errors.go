// 基础设施域本地错误辅助：统一 503「服务未配置」与内部错误构造，
// 避免改动共享的 errcode 包（骨架为全团队共用，禁止平行成员增删码表）。
package infra

import (
	"net/http"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/errcode"
)

// svcUnavailable 服务依赖不可用（DB/Redis/S3/ES 未初始化或降级）。
func svcUnavailable(msg string) *errcode.Err {
	return &errcode.Err{Code: http.StatusServiceUnavailable, HTTP: http.StatusServiceUnavailable, Msg: msg}
}

// svcInternal 包装内部错误。
func svcInternal(format string, args ...interface{}) *errcode.Err {
	return errcode.ErrInternal.Withf(format, args...)
}

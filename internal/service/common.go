// 服务层公共小工具：分页收敛、百分比格式化。
//
// 各业务域 service 共用；只放纯函数，不含业务判断。
package service

import (
	"fmt"

	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
)

// 分页默认值与上限（对齐原 PaginationDto：page 默认 1、limit 默认 10）。
const (
	DefaultPageSize = 10
	MaxPageSize     = 100
)

// normPage 分页参数收敛：page ≥ 1，limit ∈ [1, 100]。
func normPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return page, limit
}

// pageData 组装统一分页响应体（对齐原 PaginatedResponseDto）。
func pageData(list interface{}, total int64, page, limit int) response.PageData {
	tp := total / int64(limit)
	if total%int64(limit) > 0 {
		tp++
	}
	return response.PageData{
		List: list,
		Meta: response.Meta{Total: total, Page: page, Limit: limit, TotalPages: tp},
	}
}

// formatFloat2 保留两位小数的字符串（对齐原 `((a/b)*100).toFixed(2)`）。
func formatFloat2(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

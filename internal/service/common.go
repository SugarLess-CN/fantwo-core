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

// strOrEmpty 指针字符串 → 值；nil 时返回 ""。
//
// 用途：把 model 层的 *string（数据库需要区分 NULL 与空串）转成 dto 层的 string。
// 对外 JSON 规范要求字符串字段空时是 ""，不是 null（见 docs/21-JSON序列化规范.md）。
func strOrEmpty[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// sliceOrEmpty 保证返回非 nil 切片 —— json.Marshal 对 nil 切片输出 null，对空切片输出 []。
//
// 用途：所有对外数组字段（roles / list / items …）赋值时必须走这里，
// 否则空数据会序列化成 null 而不是 []。
func sliceOrEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

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

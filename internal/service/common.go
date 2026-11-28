// 服务层公共小工具：分页收敛、百分比格式化。
//
// 各业务域 service 共用；只放纯函数，不含业务判断。
package service

import (
	"fmt"

	"github.com/SugarLess-CN/fantwo-core/internal/model"
	"github.com/SugarLess-CN/fantwo-core/internal/pkg/response"
	"strconv"
	"strings"
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

// nickIDPair 把 bigint 形态的 nick_id 拆成「数字 + 字符串」两份对外字段。
//
// 为什么必须拆：nick_id 是对外展示的号码（类似 QQ 号），量级可能超过 JS 的
// Number.MAX_SAFE_INTEGER（2^53-1）。JSON 里只给 number 的话，前端 JSON.parse
// 那一刻精度就已经丢了（例如 ...9007199254740993 会变成 ...9007199254740992），
// 之后再怎么处理都救不回来。
//
// 所以对外约定：
//
//	· nick_id      number —— 方便前端直接比较/排序（可能丢精度，仅用于非精确场景）
//	· nick_id_str  string —— **精确值**，展示与回传一律用它
//
// 空值（老用户没有 nick_id）时输出 0 / ""，绝不输出 null（docs/21 字符串规则）。
func nickIDPair(n *int64) (int64, string) {
	if n == nil {
		return 0, ""
	}
	return *n, strconv.FormatInt(*n, 10)
}

// parseNickID 把上游传来的字符串解析成 bigint 形态。
//
// 入参为什么用字符串：前端 JS 把 9007199254740993 写进 JSON 就已经丢精度了，
// 所以修改接口必须收字符串（`{"nick_id": "9007199254740993"}`），这里再转 int64。
// 空串表示「不修改该字段」（指针为 nil 时调用方根本不会进来）。
func parseNickID(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ---- 上传产物 URL 规范化（出参统一）----
//
// 背景（2026-11-28 用户要求兼容旧代码）：
// 历史上图片地址经历过四代格式，库里同时存在：
//
//	① http://testend.cerylab.com/api/v1/uploads/2026/11/28/x.png   绝对 + 中间路径（最老）
//	② https://api.cerylab.com/uploads/2026/10/17/x.jpg            绝对（旧 JS 域名）
//	③ /api/v1/uploads/2026/11/28/x.png                            根相对 + 中间路径
//	④ 2026/11/28/x.png                                            纯相对（新规范）
//
// 出参时全部归一化成「upload_url + /uploads/ + 纯相对路径」，
// 这样换域名 / 去掉 /api/v1 都不用刷库。
//
// ⚠️ 与 infra.stripToRelPath 逻辑一致，但两边不能共用一个函数：
// user 域拿的是 *model.User（DB 列直接存 URL），infra 域处理的是 uploads 表，
// 二者分属不同包的内部细节。宁可重复 15 行，也不为此把 util 提到更上层。

// stripToRelPath 从任意格式的存量 URL 提取纯相对路径。
func stripToRelPath(s string) string {
	if s == "" {
		return ""
	}
	// 绝对 URL：取 path 部分
	if i := strings.Index(s, "://"); i >= 0 {
		if j := strings.Index(s[i+3:], "/"); j >= 0 {
			s = s[i+3+j:]
		} else {
			return ""
		}
	}
	// 去掉查询串与锚点（S3 预签名会带 ?X-Amz-...）
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "/")
	// 剥掉可能嵌套的中间路径前缀
	for {
		trimmed := false
		for _, p := range []string{"api/v1/uploads/", "uploads/"} {
			if strings.HasPrefix(s, p) {
				s = strings.TrimPrefix(s, p)
				trimmed = true
			}
		}
		if !trimmed {
			break
		}
	}
	return s
}

// normalizeUploadURL 把存量图片地址规范成「配置域名 + /uploads/ + 相对路径」。
//
// 空串原样返回空串（docs/21 字符串规则）。
// cfg 为 nil 时退化为根相对路径（`/uploads/...`），本地开发可直接用。
func normalizeUploadURL(cfg ConfigReader, raw string) string {
	rel := stripToRelPath(raw)
	if rel == "" {
		return ""
	}
	base := ""
	if cfg != nil {
		base = strings.TrimSpace(cfg.GetString(model.ConfigKeyUploadURL, ""))
		base = strings.TrimRight(base, "/")
		if strings.HasSuffix(base, "/api/v1") {
			base = strings.TrimSuffix(base, "/api/v1")
		}
	}
	if base == "" {
		return "/uploads/" + rel
	}
	return base + "/uploads/" + rel
}

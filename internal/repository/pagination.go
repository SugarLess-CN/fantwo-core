package repository

// 分页参数规范化。
//
// 为什么需要它：GORM 的 `Limit(0)` 会生成 `LIMIT 0`，**返回空结果集**。
// 若 repository 直接把调用方传来的 limit 透传给 Limit()，那么「不传分页参数」
// 就变成「查不到任何数据」——接口看起来正常、total 也有值、list 却是空的，
// 是极难排查的一类 bug（2026-11-21 集成测试在分类列表上实测到）。
//
// 约定：所有分页 List 方法在拼 SQL 前先调用 normPageArgs，统一兜底。
const (
	// DefaultPageSize 未指定 limit 时的默认每页条数（对齐各域 handler 的默认值）。
	DefaultPageSize = 10
	// MaxPageSize 单页上限，防止调用方传超大值拖垮数据库。
	MaxPageSize = 200
)

// normPageArgs 规范化 (page, limit)：
//   - page < 1 → 1
//   - limit <= 0 → DefaultPageSize
//   - limit > MaxPageSize → MaxPageSize
func normPageArgs(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return page, limit
}

// pageClause 返回可直接用于 GORM 的 (offset, limit)。
func pageClause(page, limit int) (int, int) {
	page, limit = normPageArgs(page, limit)
	return (page - 1) * limit, limit
}

// limitOf 只规范化 limit（便于链式调用）。
func limitOf(limit int) int {
	_, l := normPageArgs(1, limit)
	return l
}

// offsetOf 只计算 offset（便于链式调用）。
func offsetOf(page, limit int) int {
	p, l := normPageArgs(page, limit)
	return (p - 1) * l
}

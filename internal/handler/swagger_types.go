// Swagger 展示用类型（仅用于 @Success/@Failure 的 {object} 引用，不参与运行逻辑）。
//
// 为什么需要它：swaggo 解析注解时要求 {object} 指向**当前包内可解析**的类型，
// 跨包引用 internal/pkg/response.Body 会报 "cannot find type definition"
// （article/comment 等文件并未 import response，且跨包解析依赖 parseDependency 也不稳定）。
// 因此在 handler 包内声明一个与统一响应结构同形的类型，注解统一用它。
//
// 运行时响应仍是 response.Body（{code,message,data}），二者字段完全一致。
package handler

// ResponseBody 统一响应包装（{code,message,data}）的文档展示类型。
type ResponseBody struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"操作成功"`
	Data    interface{} `json:"data"`
}

// ResponsePageBody 分页响应的文档展示类型（data = {list, meta}）。
type ResponsePageBody struct {
	Code    int               `json:"code" example:"200"`
	Message string            `json:"message" example:"操作成功"`
	Data    ResponsePageInner `json:"data"`
}

// ResponsePageInner 分页载荷（对齐 response.PageData）。
type ResponsePageInner struct {
	List interface{}   `json:"list"`
	Meta ResponsePageMeta `json:"meta"`
}

// ResponsePageMeta 分页元信息（对齐 response.Meta）。
type ResponsePageMeta struct {
	Total      int64 `json:"total" example:"100"`
	Page       int   `json:"page" example:"1"`
	Limit      int   `json:"limit" example:"10"`
	TotalPages int64 `json:"totalPages" example:"10"`
}

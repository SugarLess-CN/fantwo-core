// Package dto 对外 HTTP 契约的数据模型（Swagger schema 来源）。
//
// 为什么独立成包：swag 生成的 schema 名 = 包名末段 + 类型名。
// 放在 internal/handler 里会变成 internal_handler.Xxx（一长串、Apifox 里难认），
// 放在 internal/dto 里是 dto.Xxx。swag 不提供自定义前缀参数，这是唯一改善命名的办法。
//
// 命名规范（docs/18 §二，强制）：
//
//	响应实体   XxxDto      例：UserDto、WalletTransactionDto
//	请求体     XxxRequest  例：LoginRequest、UpdateProfileRequest
//	分页容器   复用 PageBody，不新建
//	嵌套子结构 语义命名     例：RoleBriefDto、PageMeta
//
// 这些类型**只用于 swag 解析注解**，不参与运行逻辑；运行时响应结构以
// internal/pkg/response.Body / PageData 为准，两者字段必须保持一致。
//
// # 两条 swag 使用硬约束（2026-11-25 T-40 实测，不看会踩坑）
//
//  1. **用了 dto.Xxx 注解的 handler 文件，必须自己 import 本包。**
//     swag 解析 `{object} dto.LoginRequest` 时，只在**该注解所在文件的 import 列表**里
//     找包路径；只在别的文件（如 swagger_types.go）import 无效，会报
//     "cannot find type definition: dto.LoginRequest"。每个文件末尾加一行
//     `var _ = dto.ResponseBody{}` 即可（不参与运行逻辑）。
//
//  2. **swag 命令的 --exclude 必须带上 internal/dto：`--exclude tmp,internal/dto`。**
//     少了它，本包的类型会被 go list（完整模块路径）和目录扫描（相对路径）各登记一次，
//     swag 判定 "NotUnique" 后改用全路径前缀，definitions 里的名字会变成
//     `github_com_SugarLess-CN_fantwo-core_internal_dto.UserDto` 这种一长串，
//     docs/18 §二「schema 名 = dto.」的命名目标当场落空。加 exclude 后回归 `dto.UserDto`。
//     （本包没有 handler 注解，exclude 掉不会漏扫任何接口；go list 仍会把它作为
//     依赖包解析，字段完整。）
package dto

// ResponseBody 所有非分页接口的统一响应外壳。
//
// 字段与 internal/pkg/response.Body 一一对应（契约来源：AGENTS「响应契约与 i18n」）。
// data / message 为空时运行时整个字段省略（Body.MarshalJSON），文档里保留字段说明。
type ResponseBody struct {
	Data       interface{} `json:"data,omitempty"`
	Message    string      `json:"message,omitempty" example:"Success"`
	StatusCode int         `json:"status_code" example:"0"`
	TraceID    string      `json:"trace_id" example:"4c3abb48-ddf2-4943-b685-ec4ec35449ed"`
	Timestamp  int64       `json:"timestamp" example:"1795540372656"`
}

// ResponsePageBody 所有分页接口的统一响应外壳。
type ResponsePageBody struct {
	Data       PageBody `json:"data"`
	Message    string   `json:"message,omitempty" example:"Success"`
	StatusCode int      `json:"status_code" example:"0"`
	TraceID    string   `json:"trace_id" example:"4c3abb48-ddf2-4943-b685-ec4ec35449ed"`
	Timestamp  int64    `json:"timestamp" example:"1795540372656"`
}

// PageBody 分页数据体，字段与 internal/pkg/response.PageData 一致。
//
// 注意：不是 {list,total,page,size}。运行时分页载荷是 {list, meta:{...}}，
// 元信息在 meta 子对象里（response.PageData 是唯一事实来源）。
// 注解里用嵌套覆盖指明 list 的元素类型：
//
//	@Success 200 {object} dto.ResponsePageBody{data=dto.PageBody{list=[]dto.UserDto}}
type PageBody struct {
	List interface{} `json:"list"`
	Meta PageMeta    `json:"meta"`
}

// PageMeta 分页元信息，字段与 internal/pkg/response.Meta 一致。
type PageMeta struct {
	Total      int64 `json:"total" example:"128"`
	Page       int   `json:"page" example:"1"`
	Limit      int   `json:"limit" example:"20"`
	TotalPages int64 `json:"totalPages" example:"7"`
}

// SuccessDto 只回 {"success":true} 的动作类接口 data（登出、解绑、删除等）。
type SuccessDto struct {
	Success bool `json:"success" example:"true"`
}

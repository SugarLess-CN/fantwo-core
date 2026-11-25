package dto

// 本文件覆盖 profile 域（当前登录用户的个人化只读视图）的对外响应结构。
//
// 目前只有一条端点：GET /profile/menus —— 当前用户可见的后台菜单树。
//
// 为什么单独一个文件而不是塞进 role.go 的域：/profile/menus 虽然由
// RoleHandler.MyMenus 提供（菜单键来自角色），但它返回的是**当前用户的**
// 个人化视图，语义属 profile 域；DTO 按语义归属而不是按 handler 所在文件归属。

// MenuNodeDto 菜单树节点。
//
// 契约现状（读 internal/service/role_service.go:MyMenus 得到的事实）：
//
//	{"menus":[{"key":"dashboard","title":"仪表盘"}, ...], "total": 23}
//
// 即 **平铺列表**，不嵌套 —— MyMenus 只 append 叶子节点，从不填 Children。
// 因此 Children 用 omitempty 镜像实际行为（不填就不出现在响应里），
// 而不是无 omitempty 声明一个运行时永远不存在的 `children: []`。
//
// 若将来真的改成分组嵌套树，正确做法是**在唯一组装点初始化**（对齐
// internal/service/category.go:302/322 的 toCategoryView：Children 无 omitempty +
// make([]T, 0, n)），那时本字段才该去掉 omitempty。当前不具备该前提，
// 且 MenuNode 在 internal/service/role_service.go（RBAC 域写范围）里，
// 本次不越界修改。
type MenuNodeDto struct {
	Key      string        `json:"key" example:"dashboard"`
	Title    string        `json:"title" example:"仪表盘"`
	Children []MenuNodeDto `json:"children,omitempty"`
}

// MyMenusDto 我的菜单树（GET /profile/menus 的 data）。
//
// Menus 无 omitempty：空时必须 `[]` 而不是 null —— 组装点
// （role_service.go:543 `make([]*MenuNode, 0, len(allowed))`）已保证非 nil。
type MyMenusDto struct {
	Menus []MenuNodeDto `json:"menus"`
	Total int           `json:"total" example:"23"`
}

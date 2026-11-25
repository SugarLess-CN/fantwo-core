// RBAC 域的 model → dto 转换层（角色 / 权限）。
//
// 为什么单独一个文件：role_service.go 已经 700+ 行，转换是纯函数、无依赖，
// 独立出来后 service 主流程只留业务判断，且便于单测（见 role_dto_test.go）。
//
// 转换存在的唯一理由（docs/21-JSON序列化规范.md）：
// model.Role / model.Permission 的 displayName / description 是 *string
// —— 数据库要区分 SQL NULL 与空串，但对外 JSON 只能是 ""
// （指针直出就是 "displayName": null，前端要多写一层判空）。
// 其它字段是值类型，原样透传即可，不必为了「转换」而转换。
package service

import (
	"github.com/SugarLess-CN/fantwo-core/internal/dto"
	"github.com/SugarLess-CN/fantwo-core/internal/model"
)

// toPermissionDto 权限实体 → 对外 DTO。
//
// Description 走 strOrEmpty：nil → ""（不是 null）。
func toPermissionDto(p *model.Permission) dto.PermissionDto {
	if p == nil {
		return dto.PermissionDto{}
	}
	return dto.PermissionDto{
		ID:          p.ID,
		Name:        p.Name,
		Description: strOrEmpty(p.Description),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// toPermissionDtos 批量转换；nil 入参返回空切片（不是 nil，否则 JSON 出 null）。
func toPermissionDtos(list []model.Permission) []dto.PermissionDto {
	out := make([]dto.PermissionDto, 0, len(list))
	for i := range list {
		out = append(out, toPermissionDto(&list[i]))
	}
	return out
}

// toRoleDto 角色实体 → 对外 DTO。
//
// DisplayName / Description 走 strOrEmpty：nil → ""（不是 null）。
// Permissions 无 omitempty（dto 侧已去掉），空时必须是 [] 而不是整个字段消失 ——
// 所以这里显式 make，不能直接传 nil 切片。
func toRoleDto(r *model.Role) dto.RoleDto {
	if r == nil {
		return dto.RoleDto{Permissions: []dto.PermissionDto{}}
	}
	return dto.RoleDto{
		ID:          r.ID,
		Name:        r.Name,
		DisplayName: strOrEmpty(r.DisplayName),
		Description: strOrEmpty(r.Description),
		IsActive:    r.IsActive,
		IsSystem:    r.IsSystem,
		Permissions: toPermissionDtos(r.Permissions),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// toRoleDtos 批量转换；nil 入参返回空切片（不是 nil，否则 JSON 出 null）。
func toRoleDtos(list []model.Role) []dto.RoleDto {
	out := make([]dto.RoleDto, 0, len(list))
	for i := range list {
		out = append(out, toRoleDto(&list[i]))
	}
	return out
}

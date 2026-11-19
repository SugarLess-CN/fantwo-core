// Package repository 数据访问层：封装 GORM 查询。
//
// 职责边界：
//   - 只做数据读写（CRUD、分页、预加载、事务边界由 service 显式传入 *gorm.DB）；
//   - 不含业务判断；
//   - 表名/列名 snake_case，与原 TypeORM 存量库零迁移对齐。
//
// 后续按业务域拆文件：user.go、article.go 等（对应原 NestJS 各 repository/实体查询）。
package repository

// Package handler HTTP 处理层：解析请求（binding 校验）→ 调 service → 统一响应。
//
// 职责边界：
//   - 只做参数解析/校验与响应包装，不写业务逻辑；
//   - 业务逻辑全部下沉 internal/service；
//   - 错误统一用 internal/pkg/errcode 返回（response.Fail）。
//
// 后续按业务域拆文件：user.go、article.go、comment.go 等（对应原 NestJS 各 controller）。
package handler

// Package service 业务逻辑层：编排 repository、事务、事件、队列投递。
//
// 职责边界：
//   - 业务规则与状态机在这里（如订单 PENDING→PAID、审核状态回写）；
//   - 不直接接触 gin.Context / HTTP 语义；
//   - 可调用 internal/queue 投递异步任务、internal/ws 推送事件。
//
// 后续按业务域拆文件：user.go、order.go 等（对应原 NestJS 各 service）。
package service

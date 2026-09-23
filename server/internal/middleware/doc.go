// Package middleware 提供 Gin 中间件。
//
// 已实现：panic recovery（500 信封，不向前端泄漏堆栈）、请求上下文（ip/ua 审计注入）、
// 请求日志（方法/路径/状态/耗时/ip/ua）。
// 后续任务将在此实现 JWT 认证与 RBAC；
// 权限判断必须以后端为最终边界，不接受前端传入的角色声明。
package middleware

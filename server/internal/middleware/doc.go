// Package middleware 提供 Gin 中间件。
//
// 已实现：panic recovery（500 信封，不向前端泄漏堆栈）。
// 后续任务将在此实现请求日志、JWT 认证与 RBAC；
// 权限判断必须以后端为最终边界，不接受前端传入的角色声明。
package middleware

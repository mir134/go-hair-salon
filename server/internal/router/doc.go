// Package router 负责路由注册与中间件装配。
//
// 已注册 GET /health（免认证）；所有 API 统一前缀 /api/v1（02-AGENTS.md:64-66）；
// 静态文件托管与 SPA 回退由后续任务实现。
package router

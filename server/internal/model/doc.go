// Package model 定义持久化模型（GORM Model）。
//
// 硬规则：金额字段一律使用 int64 整数分，禁止使用浮点数；时间统一存 UTC。
// 模型与 API DTO 分离，禁止把持久化字段直接暴露为接口响应。
package model

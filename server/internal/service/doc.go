// Package service 承载业务规则与事务边界。
//
// 涉及资金、余额、积分、订单的操作必须在本层以数据库事务保证一致性，
// 任一步失败整体回滚（AGENTS.md 第 5 节）。
package service

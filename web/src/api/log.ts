import { get } from './http'
import type { PageData } from './http'

/**
 * 操作日志 API（04-API.md:237-243、plan todo 47/48）。
 *
 * 仅 admin 可访问（06-BUSINESS-RULES.md §7「staff 禁操作日志」）：
 * 前端隐藏入口只是导航简化，后端 RBAC 是最终边界。
 * 审计记录只增不改不删（AGENTS.md 第 5 节），本模块只有查询。
 */

/** 操作日志 DTO（server/internal/controller/operationlog.go OperationLogView） */
export interface OperationLog {
  id: number
  /** 操作人用户 id；null 表示无登录上下文的系统动作 */
  operator_id: number | null
  /** 操作人用户名（后端联表 users.username 冗余，停用后历史日志仍可读） */
  operator_name: string
  action: string
  target_type: string
  target_id: number
  content: string
  ip: string
  user_agent: string
  created_at: string
}

/** GET /operation-logs 查询参数（04-API.md:237-243）：筛选 + 分页，时间倒序 */
export interface OperationLogListQuery {
  operator_id?: number
  /** 精确匹配动作名（如 customer_create / order_pay） */
  action?: string
  start_date?: string
  end_date?: string
  page?: number
  page_size?: number
}

/** GET /operation-logs（仅 admin）：操作人/动作/日期筛选 + 分页（时间倒序，最新在前） */
export function listOperationLogs(
  query: OperationLogListQuery = {},
): Promise<PageData<OperationLog>> {
  return get<PageData<OperationLog>>('/operation-logs', { ...query })
}

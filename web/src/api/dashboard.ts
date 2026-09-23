import { get } from './http'
import type { Order } from './order'
import type { Recharge } from './recharge'

/**
 * Dashboard API（04-API.md:217-236、plan todo 44-46）。
 *
 * 口径与 06-BUSINESS-RULES.md §8 一致：
 * - /dashboard/summary：无日期参数，「今日」按服务器本地时区（D3 决议），
 *   待结账单数为当前实时 pending 总数（即使传入日期参数也不漂移）；附最近消费/最近充值各 ≤10 条；
 * - /dashboard/revenue、/dashboard/customers、/dashboard/employee-performance：
 *   按 start_date/end_date（YYYY-MM-DD，含当天，本地时区日界）返回区间序列/汇总。
 *
 * 字段名对齐 server/internal/controller 的 DTO（dashboard_test.go 镜像：
 * completed_order_count / consuming_customer_count / recent_orders / recent_recharges）。
 * 金额一律整数分；前端仅展示（utils/format.ts），不参与计算提交。
 */

/** GET /dashboard/summary 响应（今日 + 实时口径） */
export interface DashboardSummary {
  /** 统计日（服务器本地时区，YYYY-MM-DD） */
  date: string
  /** 今日营业额（分）= 当日 completed 实付合计 − 当日退款冲减 */
  revenue_cents: number
  /** 今日消费单数 = 当日 completed 订单数 */
  completed_order_count: number
  /** 今日消费人数 = 当日有 completed 订单的去重客户数 */
  consuming_customer_count: number
  /** 今日新增客户数 */
  new_customer_count: number
  /** 今日充值金额（分）= 当日 actual 合计 − 当日冲正（不含赠送） */
  recharge_cents: number
  /** 待结账单数：当前 pending 挂单总数，实时，不受日期范围影响 */
  pending_order_count: number
  /** 最近消费（≤10 条，时间倒序，completed 口径） */
  recent_orders: Order[]
  /** 最近充值（≤10 条，时间倒序，含已冲正记录） */
  recent_recharges: Recharge[]
}

/** 区间查询参数（04-API.md:228-233）：两端均含当天，YYYY-MM-DD */
export interface DashboardRangeQuery {
  start_date: string
  end_date: string
}

/** 每日营收点（/dashboard/revenue） */
export interface DashboardRevenuePoint {
  date: string
  /** 当日营业额（分）：completed 实付合计 − 当日退款冲减 */
  revenue_cents: number
}

/** GET /dashboard/revenue 响应：日期范围内逐日营收序列（无图表库，表格呈现） */
export interface DashboardRevenue {
  start_date: string
  end_date: string
  items: DashboardRevenuePoint[]
}

/** 每日客户点（/dashboard/customers） */
export interface DashboardCustomerPoint {
  date: string
  /** 当日新增客户数 */
  new_customer_count: number
  /** 当日消费人数（去重） */
  consuming_customer_count: number
}

/** GET /dashboard/customers 响应：日期范围内逐日客户序列 */
export interface DashboardCustomers {
  start_date: string
  end_date: string
  items: DashboardCustomerPoint[]
}

/** 员工业绩行（/dashboard/employee-performance，plan todo 45） */
export interface EmployeePerformanceRow {
  /** 员工 id：null = 未分配（明细与订单均无员工），聚合行仍返回 */
  employee_id: number | null
  /** 员工姓名；未分配行为空字符串 */
  employee_name: string
  /** 业绩金额（分）：completed 明细成交金额（discount 后）汇总，退款按退款发生日冲减 */
  amount_cents: number
}

/** GET /dashboard/employee-performance 响应：按员工汇总（日期范围过滤，金额降序） */
export interface EmployeePerformance {
  start_date: string
  end_date: string
  items: EmployeePerformanceRow[]
}

/**
 * GET /dashboard/summary（both）：今日 KPI + 待结账单数（实时）+ 最近消费/最近充值。
 * 不传日期参数；页面切换日期范围时不重复调用（06-BUSINESS-RULES.md:98-103）。
 */
export function getSummary(): Promise<DashboardSummary> {
  return get<DashboardSummary>('/dashboard/summary')
}

/** GET /dashboard/revenue（both）：日期范围逐日营收序列 */
export function getRevenue(query: DashboardRangeQuery): Promise<DashboardRevenue> {
  return get<DashboardRevenue>('/dashboard/revenue', { ...query })
}

/** GET /dashboard/customers（both）：日期范围逐日客户序列（新增/消费人数） */
export function getCustomers(query: DashboardRangeQuery): Promise<DashboardCustomers> {
  return get<DashboardCustomers>('/dashboard/customers', { ...query })
}

/** GET /dashboard/employee-performance（both）：日期范围员工业绩汇总 */
export function getEmployeePerformance(query: DashboardRangeQuery): Promise<EmployeePerformance> {
  return get<EmployeePerformance>('/dashboard/employee-performance', { ...query })
}

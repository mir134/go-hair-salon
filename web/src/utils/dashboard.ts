/**
 * Dashboard 页展示辅助（plan todo 46、07-UI.md:76-83）。
 *
 * 日期一律使用本地时区 YYYY-MM-DD：后端按服务器本地时区聚合「日」边界
 * （06-BUSINESS-RULES.md:113-118），前端只透传与展示，不做时区换算。
 */

import type { DashboardCustomerPoint, DashboardRevenuePoint } from '@/api'
import { formatDay } from '@/utils/format'

/** 日期范围 [开始, 结束]，两端均为 YYYY-MM-DD（含当天） */
export type DateRange = [string, string]

/** 今日范围（本地时区）：Dashboard 默认统计区间 */
export function todayRange(): DateRange {
  const today = formatDay(new Date())
  return [today, today]
}

/**
 * 归一化日期范围（malformed_input 防护）：
 * - 缺失/清空 → 回退今日；
 * - 起止颠倒 → 交换为正确顺序（YYYY-MM-DD 可直接字典序比较）。
 */
export function normalizeRange(range: readonly string[] | null | undefined): DateRange {
  const start = range?.[0] ?? ''
  const end = range?.[1] ?? ''
  if (start === '' || end === '') {
    return todayRange()
  }
  return start <= end ? [start, end] : [end, start]
}

/** 范围展示文案：2026-09-23 至 2026-09-23（07-UI.md:80 必须显示统计日期范围） */
export function formatRangeLabel(range: DateRange): string {
  return `${range[0]} 至 ${range[1]}`
}

/** 区间每日明细行：营收序列与客户序列按日期合并（无图表库，表格呈现） */
export interface DashboardDailyRow {
  date: string
  revenue_cents: number
  new_customer_count: number
  /** 当日消费人数（去重值） */
  consuming_customer_count: number
}

/**
 * 合并 /dashboard/revenue 与 /dashboard/customers 的每日序列：
 * 取日期并集并按日期升序；某一侧缺失的日期以 0 补齐（空数据日显示 0）。
 */
export function mergeDailyRows(
  revenue: readonly DashboardRevenuePoint[],
  customers: readonly DashboardCustomerPoint[],
): DashboardDailyRow[] {
  const byDate = new Map<string, DashboardDailyRow>()

  function ensure(date: string): DashboardDailyRow {
    const existing = byDate.get(date)
    if (existing !== undefined) {
      return existing
    }
    const row: DashboardDailyRow = {
      date,
      revenue_cents: 0,
      new_customer_count: 0,
      consuming_customer_count: 0,
    }
    byDate.set(date, row)
    return row
  }

  for (const point of revenue) {
    ensure(point.date).revenue_cents = point.revenue_cents
  }
  for (const point of customers) {
    const row = ensure(point.date)
    row.new_customer_count = point.new_customer_count
    row.consuming_customer_count = point.consuming_customer_count
  }

  return [...byDate.values()].sort((left, right) =>
    left.date < right.date ? -1 : left.date > right.date ? 1 : 0,
  )
}

/** 区间合计：营业额/新增客户可直接求和；消费人数是每日去重值，求和即「人次」 */
export interface DashboardRangeTotals {
  revenue_cents: number
  new_customer_count: number
  customer_visits: number
}

/** 汇总每日明细 → 区间合计（用于区间统计卡片） */
export function sumDailyRows(rows: readonly DashboardDailyRow[]): DashboardRangeTotals {
  return rows.reduce<DashboardRangeTotals>(
    (totals, row) => ({
      revenue_cents: totals.revenue_cents + row.revenue_cents,
      new_customer_count: totals.new_customer_count + row.new_customer_count,
      customer_visits: totals.customer_visits + row.consuming_customer_count,
    }),
    { revenue_cents: 0, new_customer_count: 0, customer_visits: 0 },
  )
}

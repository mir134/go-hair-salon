import { listCustomerBalanceTransactions } from './customer'
import { get, post } from './http'
import type { PageData } from './http'
import type { OrderPaymentMethod } from './order'

/**
 * 充值 / 余额调整 API（04-API.md:157-193、plan todo 33）。
 *
 * 字段与后端 controller 的充值 DTO 对齐（server/internal/controller/recharge_test.go
 * 的 rechargeView 镜像）：金额一律整数分；request_id 为客户端幂等键，重复提交
 * 返回原记录且不重复入账（04-API.md:277-299）。
 */

/** 充值支付方式（model/const.go:26-32）：与订单共用枚举；UI 仅提供现金/微信/支付宝 */
export type RechargePaymentMethod = OrderPaymentMethod

/** 充值记录 DTO（03-DATABASE.md:164-186）：本金 + 赠送 = 余额增加；actual=客户实际支付 */
export interface Recharge {
  id: number
  customer_id: number
  customer_name: string
  request_id: string
  /** 充值本金（分），计入余额 */
  recharge_amount_cents: number
  /** 赠送金额（分），计入余额 */
  gift_amount_cents: number
  /** 客户实际支付（分）：MVP 等于本金；充值优惠时可小于本金（03-DATABASE.md:170） */
  actual_amount_cents: number
  payment_method: string
  /** active=有效 / refunded=已冲正（model/const.go:50-54） */
  status: string
  operator_id: number | null
  remark: string
  created_at: string
}

/** GET /recharges 查询参数（04-API.md:162）：客户 / 日期筛选 + 分页，时间倒序 */
export interface RechargeListQuery {
  customer_id?: number
  start_date?: string
  end_date?: string
  page?: number
  page_size?: number
}

/**
 * POST /recharges 请求体（04-API.md:167-174、03-DATABASE.md:164-172）。
 *
 * 页面「实付金额」在 MVP 同时是充值本金：余额增加 = 实付 + 赠送（07-UI.md:70-74）。
 * actual_amount_cents 不提交时由后端缺省为本金；充值优惠场景由后端显式传入。
 */
export interface RechargeCreatePayload {
  /** 客户端幂等键（UUID）：同一次提交尝试复用同一值（04-API.md:283-286） */
  request_id: string
  customer_id: number
  /** 充值本金（分）：> 0，等于客户实付金额 */
  recharge_amount_cents: number
  /** 赠送金额（分）：>= 0，不赠送时省略（后端缺省 0） */
  gift_amount_cents?: number
  payment_method: RechargePaymentMethod
  remark?: string
}

/** POST /customers/:id/balance-adjustments 请求体（04-API.md:176-192）：reason 必填，金额可正负 */
export interface BalanceAdjustmentPayload {
  /** 调整金额（分）：正=增加、负=扣减；不得导致余额为负（后端 422） */
  amount_cents: number
  /** 调整原因：必填（06-BUSINESS-RULES.md:62） */
  reason: string
}

/** 余额调整响应 DTO（controller/balanceadjust.go:33-41）：balance_after_cents 为服务端真值 */
export interface BalanceAdjustmentResult {
  transaction_id: number
  customer_id: number
  amount_cents: number
  balance_before_cents: number
  balance_after_cents: number
  reason: string
  created_at: string
}

/** POST /recharges（both）：201=新建 / 200=幂等命中原记录；400/404 等失败零写入 */
export function createRecharge(payload: RechargeCreatePayload): Promise<Recharge> {
  return post<Recharge>('/recharges', payload)
}

/** GET /recharges（both）：分页 + 客户/日期筛选（04-API.md:162） */
export function listRecharges(query: RechargeListQuery = {}): Promise<PageData<Recharge>> {
  return get<PageData<Recharge>>('/recharges', { ...query })
}

/**
 * POST /customers/:id/balance-adjustments（仅 admin，04-API.md:178-182）：
 * 写 balance_transactions（type=adjustment）与 operation_logs；不产生订单、不计营业额。
 * 201=新建 / 200=成功；调整后余额为负时后端返回 422，调用方需就地展示后端文案。
 * 响应 balance_after_cents 即调整后的最新余额（服务端真值）。
 */
export function createBalanceAdjustment(
  customerId: number,
  payload: BalanceAdjustmentPayload,
): Promise<BalanceAdjustmentResult> {
  return post<BalanceAdjustmentResult>(`/customers/${customerId}/balance-adjustments`, payload)
}

/**
 * GET /customers/:id/balance-transactions（04-API.md:81）：余额流水（含 before/after）。
 * 复用 customer.ts 既有实现，避免同一端点重复实现（AGENTS.md §7）。
 */
export const listBalanceTransactions = listCustomerBalanceTransactions

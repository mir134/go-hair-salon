import { get, post } from './http'
import type { PageData } from './http'

/**
 * 支付方式（server/internal/model/const.go:27-32、06-BUSINESS-RULES.md:24）。
 * 消费至少支持现金/微信/支付宝/余额四种。
 */
export type OrderPaymentMethod = 'cash' | 'wechat' | 'alipay' | 'balance'

/**
 * 订单明细 DTO，对齐 server/internal/model/order.go 的 OrderItem（03-DATABASE.md:147-162）。
 * service_name_snapshot 冻结下单时的服务名，防止改名后历史订单显示错误；金额一律整数分。
 */
export interface OrderItem {
  id: number
  order_id: number
  service_id: number
  service_name_snapshot: string
  quantity: number
  unit_price_cents: number
  discount_amount_cents: number
  amount_cents: number
  employee_id: number | null
  created_at: string
}

/**
 * 订单 DTO，对齐 orders 表（03-DATABASE.md:113-145）与 04-API.md:127-135。
 * customer_name/employee_name 为列表 DTO 的联表冗余字段；items 仅详情接口填充。
 */
export interface Order {
  id: number
  order_no: string
  request_id?: string
  customer_id: number
  customer_name?: string
  employee_id: number | null
  employee_name?: string
  original_amount_cents: number
  discount_amount_cents: number
  paid_amount_cents: number
  payment_method: string
  status: string
  remark: string
  items?: OrderItem[]
  created_at: string
  updated_at: string
}

/** POST /orders 明细行：unit_price_cents 为成交单价（仅 admin 可≠标准价，06 §3.1） */
export interface OrderItemPayload {
  service_id: number
  quantity: number
  /** 成交单价（分）：默认=服务标准价；改价时为改后价，差值计入 discount_amount_cents */
  unit_price_cents: number
}

/**
 * POST /orders 请求体（04-API.md:132-134、06-BUSINESS-RULES.md:20-30）。
 * request_id 为客户端幂等键（04-API.md:283），同一值重复提交后端返回原订单、不重复入账。
 * 本页只做「直接完成」（status=completed）；挂单（pending）由 plan todo 29 提供。
 */
export interface OrderCreatePayload {
  request_id: string
  customer_id: number
  employee_id: number | null
  payment_method: OrderPaymentMethod
  /** 直接完成：确认收款后立即完成订单（07-UI.md:54） */
  status: 'completed'
  /** 改价原因：任一明细成交价≠标准价时必填并写入 operation_logs（06 §3.1） */
  discount_reason?: string
  items: OrderItemPayload[]
}

/** GET /orders 查询参数（04-API.md:131-134、plan todo 22） */
export interface OrderListQuery {
  status?: string
  customer_id?: number
  employee_id?: number
  start_date?: string
  end_date?: string
  /** 排序口径：recent=按创建时间倒序 */
  sort?: string
  page?: number
  page_size?: number
}

/**
 * POST /orders（both）：直接完成订单，金额/余额/流水由后端事务统一处理（04-API.md:143）。
 * 同 request_id 重复提交 → 200 + 原订单（幂等，04-API.md:283-286）。
 * 后端可能返回 400/422（余额不足等业务校验失败），调用方需就地展示且保留表单。
 */
export function createOrder(payload: OrderCreatePayload): Promise<Order> {
  return post<Order>('/orders', payload)
}

/** GET /orders（both）：分页 + 状态/客户/员工/日期筛选（DTO 含 customer/employee 名） */
export function listOrders(query: OrderListQuery = {}): Promise<PageData<Order>> {
  return get<PageData<Order>>('/orders', { ...query })
}

/** GET /orders/:id（both）：详情含 items 快照（service_name_snapshot） */
export function getOrder(id: number): Promise<Order> {
  return get<Order>(`/orders/${id}`)
}

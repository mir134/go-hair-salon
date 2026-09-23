import { del, get, post, put } from './http'
import type { PageData } from './http'

/**
 * 支付方式（server/internal/model/const.go:27-32、06-BUSINESS-RULES.md:24）。
 * 消费至少支持现金/微信/支付宝/余额四种。
 */
export type OrderPaymentMethod = 'cash' | 'wechat' | 'alipay' | 'balance'

/**
 * 订单提交模式（07-UI.md:52-55）：
 * - completed 直接完成：确认收款后立即完成订单（同一事务扣余额/写流水）；
 * - pending 挂单：先开单不收款，订单进入「待结账」，之后结账（06-BUSINESS-RULES.md:30）。
 */
export type OrderSubmitStatus = 'completed' | 'pending'

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
 * status=completed 直接完成（payment_method 必填）；status=pending 挂单（不收款，省略 payment_method）。
 */
export interface OrderCreatePayload {
  request_id: string
  customer_id: number
  employee_id: number | null
  /** 直接完成时必填；挂单不收款，省略（06-BUSINESS-RULES.md:30） */
  payment_method?: OrderPaymentMethod
  status: OrderSubmitStatus
  /** 改价原因：任一明细成交价≠标准价时必填并写入 operation_logs（06 §3.1） */
  discount_reason?: string
  items: OrderItemPayload[]
}

/** POST /orders/:id/pay 请求体（04-API.md:150）：结账时记录实际收款方式 */
export interface OrderPayPayload {
  payment_method: OrderPaymentMethod
}

/** POST /orders/:id/items 请求体（04-API.md:136）：仅 pending 可追加，单价默认标准价 */
export interface OrderItemAddPayload {
  service_id: number
  quantity: number
  unit_price_cents: number
  /** 改价原因：成交单价≠标准价时必填（06 §3.1）；追加默认标准价，可省略 */
  discount_reason?: string
}

/** PUT /orders/:id/items/:item_id 请求体（04-API.md:137）：仅 pending 可改；改单价仅 admin+原因 */
export interface OrderItemUpdatePayload {
  quantity: number
  unit_price_cents: number
  /** 成交单价变化时必填（06 §3.1），后端据此写 operation_logs */
  discount_reason?: string
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
 * POST /orders（both）：创建订单，金额/余额/流水由后端事务统一处理（04-API.md:143）。
 * status=completed 直接完成；status=pending 挂单（不产生资金/余额/积分变动）。
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

/**
 * POST /orders/:id/pay（both）：挂单结账 → completed（04-API.md:150）。
 * 仅 pending 可结账，重复结账 → 409；余额支付在同一事务内扣减并写余额/积分流水。
 * 返回结账后的订单详情（金额与状态以服务端为准）。
 */
export function payOrder(id: number, payload: OrderPayPayload): Promise<Order> {
  return post<Order>(`/orders/${id}/pay`, payload)
}

/**
 * POST /orders/:id/cancel（admin）：取消挂单（04-API.md:152、06-BUSINESS-RULES.md:37）。
 * 仅 pending 可取消（否则 409/422）；已结账订单只能退款，不可取消。
 */
export function cancelOrder(id: number): Promise<Order> {
  return post<Order>(`/orders/${id}/cancel`)
}

/**
 * POST /orders/:id/refund（admin）：全额退款（04-API.md:139,155、plan todo 35）。
 *
 * 仅 completed 可退款（重复退款 → 409）；余额支付退回余额并写反向流水，积分按原获得
 * 扣回（不足时 422 且零写入），累计消费与营业额同步冲减。MVP 仅全额退款。
 * 返回退款后的订单详情（status=refunded，服务端真值）。
 */
export function refundOrder(id: number): Promise<Order> {
  return post<Order>(`/orders/${id}/refund`)
}

/**
 * POST /orders/:id/items（both）：挂单追加服务项目（04-API.md:136-138）。
 * 仅 pending 可编辑（否则 409）；订单金额随明细由后端重算，编辑不产生资金变动。
 */
export function addOrderItem(id: number, payload: OrderItemAddPayload): Promise<Order> {
  return post<Order>(`/orders/${id}/items`, payload)
}

/**
 * PUT /orders/:id/items/:item_id（both）：挂单修改明细数量/成交单价（04-API.md:137）。
 * 仅 pending 可编辑（否则 409）；改单价仅 admin 且必填 discount_reason（06 §3.1）。
 */
export function updateOrderItem(
  id: number,
  itemId: number,
  payload: OrderItemUpdatePayload,
): Promise<Order> {
  return put<Order>(`/orders/${id}/items/${itemId}`, payload)
}

/** DELETE /orders/:id/items/:item_id（both）：挂单删除明细（04-API.md:138）；仅 pending 可编辑 */
export function deleteOrderItem(id: number, itemId: number): Promise<Order> {
  return del<Order>(`/orders/${id}/items/${itemId}`)
}

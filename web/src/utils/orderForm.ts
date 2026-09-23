import type { OrderItemPayload, Service } from '@/api'

import { formatCents } from './format'
import { parsePriceToCents } from './serviceForm'

/**
 * 快速消费的页面态明细行（plan todo 23）。
 *
 * 金额只在整数分上运算：服务标准价来自 DTO（price_cents），成交单价以「元文本」
 * 承载输入（仅 admin 可改），需要计算时经 parsePriceToCents 解析回整数分，
 * 全程不引入浮点。
 */
export interface ConsumeItem {
  service: Service
  quantity: number
  /** 成交单价（元文本）；staff 恒为标准价文本，admin 可改（06-BUSINESS-RULES.md §3.1） */
  unitPriceText: string
}

/** 提交前本地校验输入（后端仍是最终安全边界） */
export interface ConsumeFormInput {
  customerSelected: boolean
  items: readonly ConsumeItem[]
  isAdmin: boolean
  reason: string
}

export interface OrderTotals {
  originalCents: number
  paidCents: number
  discountCents: number
}

/** 选中服务时建行：自动带出标准价（07-UI.md:50）、数量默认 1 */
export function consumeItemFromService(service: Service): ConsumeItem {
  return { service, quantity: 1, unitPriceText: formatCents(service.price_cents) }
}

/**
 * 成交单价（分）。文本非法时回退标准价，保证预览/合计永不产生 NaN 或负数；
 * admin 输入的非法文本会在提交前被 validateConsumeForm 拦截。
 */
export function itemUnitPriceCents(item: ConsumeItem): number {
  return parsePriceToCents(item.unitPriceText) ?? item.service.price_cents
}

/** 该行是否改价（成交单价 ≠ 标准价） */
export function isPriceOverridden(item: ConsumeItem): boolean {
  return itemUnitPriceCents(item) !== item.service.price_cents
}

/** 是否存在改价（决定「改价原因」是否必填） */
export function hasPriceOverride(items: readonly ConsumeItem[]): boolean {
  return items.some(isPriceOverridden)
}

/** 合计（整数分）：原价=Σ标准价×数量；成交价=Σ成交单价×数量；优惠=差额（06 §3.1） */
export function orderTotals(items: readonly ConsumeItem[]): OrderTotals {
  let originalCents = 0
  let paidCents = 0
  for (const item of items) {
    originalCents += item.service.price_cents * item.quantity
    paidCents += itemUnitPriceCents(item) * item.quantity
  }
  return { originalCents, paidCents, discountCents: originalCents - paidCents }
}

/**
 * 提交前本地校验：返回错误文案，空串=通过。
 * staff 的价格文本不可编辑且来自 formatCents，因此「改价必填原因」天然只约束 admin。
 */
export function validateConsumeForm(input: ConsumeFormInput): string {
  if (!input.customerSelected) {
    return '请先选择客户'
  }
  if (input.items.length === 0) {
    return '请至少选择一个服务项目'
  }
  for (const item of input.items) {
    if (!Number.isInteger(item.quantity) || item.quantity < 1) {
      return `服务「${item.service.name}」的数量必须大于 0`
    }
    if (input.isAdmin && parsePriceToCents(item.unitPriceText) === null) {
      return `服务「${item.service.name}」的成交单价格式不正确（最多两位小数的元金额）`
    }
  }
  if (hasPriceOverride(input.items) && input.reason.trim() === '') {
    return '已修改成交价：必须填写改价原因（06-BUSINESS-RULES.md §3.1）'
  }
  return ''
}

/** 明细 → POST /orders 请求体（成交单价一律显式提交） */
export function buildOrderItemPayloads(items: readonly ConsumeItem[]): OrderItemPayload[] {
  return items.map((item) => ({
    service_id: item.service.id,
    quantity: item.quantity,
    unit_price_cents: itemUnitPriceCents(item),
  }))
}

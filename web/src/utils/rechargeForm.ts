import type { OrderPaymentMethod } from '@/api'

import { parsePriceToCents } from './serviceForm'

/**
 * 充值 / 余额调整表单的金额解析、校验与幂等签名（plan todo 33）。
 *
 * 金额全程整数分：输入以「元文本」承载，解析按字符串拆分（不做浮点乘法），
 * 仅在预览与提交时换算为整数分（AGENTS.md 硬规则：禁止 float64 保存金额）。
 */

/** 充值表单模型：实付 / 赠送以文本持有，提交前解析为整数分 */
export interface RechargeFormModel {
  rechargeText: string
  giftText: string
  paymentMethod: OrderPaymentMethod
  remark: string
}

/** 新增充值默认值：现金支付、不赠送 */
export function emptyRechargeForm(): RechargeFormModel {
  return { rechargeText: '', giftText: '', paymentMethod: 'cash', remark: '' }
}

/** 实付金额（分）：必须是 > 0 的元金额；非法/0 返回 null（本金 > 0，04-API.md:169） */
export function parseRechargeCents(value: unknown): number | null {
  const cents = parsePriceToCents(value)
  return cents !== null && cents > 0 ? cents : null
}

/** 赠送金额（分）：空文本视为 0（不赠送）；否则必须 >= 0，非法返回 null（04-API.md:169） */
export function parseGiftCents(value: unknown): number | null {
  const text = typeof value === 'string' ? value.trim() : ''
  if (text === '') {
    return 0
  }
  return parsePriceToCents(text)
}

/** 有符号元金额（分）：余额调整可正可负（04-API.md:187）；非法文本返回 null */
export function parseSignedCents(value: unknown): number | null {
  const text = typeof value === 'string' ? value.trim() : ''
  const match = /^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(text)
  if (match === null) {
    return null
  }
  const yuan = Number(match[2] ?? '0')
  const fen = Number((match[3] ?? '').padEnd(2, '0'))
  const cents = yuan * 100 + fen
  if (!Number.isSafeInteger(cents)) {
    return null
  }
  return match[1] === '-' ? -cents : cents
}

/** 增加余额（分）= 实付 + 赠送（06-BUSINESS-RULES.md:50、03-DATABASE.md:172） */
export function rechargeBalanceIncrease(rechargeCents: number, giftCents: number): number {
  return rechargeCents + giftCents
}

/** 充值列表筛选（充值记录页）：客户 + 日期范围；空值表示不筛选 */
export interface RechargeListFilters {
  customerId: number | null
  startDate: string
  endDate: string
}

/** 充值提交前本地校验：返回错误文案，空串=通过（后端仍是最终边界） */
export function validateRechargeForm(input: {
  customerSelected: boolean
  rechargeText: string
  giftText: string
}): string {
  if (!input.customerSelected) {
    return '请先选择客户'
  }
  if (parseRechargeCents(input.rechargeText) === null) {
    return '实付金额必须是大于 0 的元金额（最多两位小数）'
  }
  if (parseGiftCents(input.giftText) === null) {
    return '赠送金额格式不正确（不能为负，最多两位小数）'
  }
  return ''
}

/** 充值表单内容签名：变化即视为新的提交，作废旧幂等键（04-API.md:283-286） */
export function rechargeFormSignature(input: {
  customerId: number | null
  rechargeText: string
  giftText: string
  paymentMethod: OrderPaymentMethod
  remark: string
}): string {
  return JSON.stringify({
    customer_id: input.customerId,
    recharge_amount_cents: parseRechargeCents(input.rechargeText),
    gift_amount_cents: parseGiftCents(input.giftText),
    payment_method: input.paymentMethod,
    remark: input.remark.trim(),
  })
}

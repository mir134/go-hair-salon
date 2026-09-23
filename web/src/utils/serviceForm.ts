import type { Service } from '@/api'

import { formatCents } from './format'

/**
 * 服务新增/编辑表单模型：价格与时长以文本持有，提交前解析为整数（禁止浮点运算）。
 * 与 utils/customerProfile.ts 同层，供 ServiceFormDialog 与 ServiceFieldsForm 共用。
 */
export interface ServiceFormModel {
  name: string
  categoryId: number | null
  priceText: string
  durationText: string
  status: number
  remark: string
}

/** 新增默认值：启用、30 分钟 */
export function emptyServiceForm(): ServiceFormModel {
  return { name: '', categoryId: null, priceText: '', durationText: '30', status: 1, remark: '' }
}

/** 编辑回填：金额分 → 元文本（两位小数），其余字段原样 */
export function serviceFormFromService(service: Service): ServiceFormModel {
  return {
    name: service.name,
    categoryId: service.category_id,
    priceText: formatCents(service.price_cents),
    durationText: String(service.duration_minutes),
    status: service.status,
    remark: service.remark,
  }
}

/** 元文本 → 整数分；空/负/超过两位小数/超安全整数返回 null（字符串拆分，不做浮点乘法） */
export function parsePriceToCents(value: unknown): number | null {
  const text = typeof value === 'string' ? value.trim() : ''
  if (!/^\d+(\.\d{1,2})?$/.test(text)) {
    return null
  }
  const [yuanText = '0', fenText = ''] = text.split('.')
  const cents = Number(yuanText) * 100 + Number((fenText + '00').slice(0, 2))
  return Number.isSafeInteger(cents) ? cents : null
}

/** 时长文本 → 0-1440 整数分钟；非法（空/负/小数/超范围）返回 null */
export function parseDuration(value: unknown): number | null {
  const text = typeof value === 'string' ? value.trim() : ''
  if (!/^\d{1,4}$/.test(text)) {
    return null
  }
  const parsed = Number.parseInt(text, 10)
  return parsed <= 1440 ? parsed : null
}

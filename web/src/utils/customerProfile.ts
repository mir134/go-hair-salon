import { attachCustomerTag, detachCustomerTag } from '@/api'
import type { Customer, CustomerPayload, Tag } from '@/api'

/**
 * 客户表单模型与差集同步工具（客户列表弹窗与客户详情页共用）。
 * 金额无关；标签不在 POST/PUT /customers 请求体内，单独经挂/摘接口维护。
 */

/** 新增/编辑客户表单模型：可写字段 + 标签多选 */
export interface CustomerProfileForm {
  name: string
  phone: string
  gender: string
  birthday: string | null
  wechat: string
  source: string
  remark: string
  tagIds: number[]
}

/** 空表单（新增客户） */
export function emptyCustomerProfileForm(): CustomerProfileForm {
  return {
    name: '',
    phone: '',
    gender: '',
    birthday: null,
    wechat: '',
    source: '',
    remark: '',
    tagIds: [],
  }
}

/** 客户详情 → 表单模型（标签仅取未删除项） */
export function customerProfileFormFromDetail(detail: Customer): CustomerProfileForm {
  return {
    name: detail.name,
    phone: detail.phone,
    gender: detail.gender,
    birthday: detail.birthday,
    wechat: detail.wechat,
    source: detail.source,
    remark: detail.remark,
    tagIds: editableTagIds(detail.tags),
  }
}

/** 未删除标签的 id 集合：历史软删除标签不参与编辑差集，保持原样 */
export function editableTagIds(tags: Tag[]): number[] {
  return tags.filter((tag) => !tag.deleted).map((tag) => tag.id)
}

/** 表单模型 → POST/PUT /customers 请求体（字符串字段去首尾空白） */
export function toCustomerPayload(form: CustomerProfileForm): CustomerPayload {
  return {
    name: form.name.trim(),
    phone: form.phone.trim(),
    gender: form.gender,
    birthday: form.birthday,
    wechat: form.wechat.trim(),
    source: form.source.trim(),
    remark: form.remark,
  }
}

/** 标签差集同步：新增挂载、取消摘除（调用方保证 before/after 均为未删除标签 id） */
export async function syncCustomerTags(
  customerId: number,
  before: number[],
  after: number[],
): Promise<void> {
  for (const tagId of after.filter((id) => !before.includes(id))) {
    await attachCustomerTag(customerId, tagId)
  }
  for (const tagId of before.filter((id) => !after.includes(id))) {
    await detachCustomerTag(customerId, tagId)
  }
}

import { del, get, post, put } from './http'
import type { PageData } from './http'
import type { Tag } from './tag'

/**
 * 客户 DTO，对齐 server/internal/controller/customer.go 的 CustomerView。
 * 金额一律整数分，时间一律 UTC（ISO 字符串），生日按 YYYY-MM-DD 输出。
 */
export interface Customer {
  id: number
  name: string
  phone: string
  gender: string
  birthday: string | null
  avatar: string
  wechat: string
  source: string
  first_visit_at: string | null
  last_visit_at: string | null
  total_spent_cents: number
  balance_cents: number
  points: number
  remark: string
  /** 仅客户详情接口填充；列表接口恒为 []（不逐行查标签） */
  tags: Tag[]
  created_at: string
  updated_at: string
}

/** 新增/编辑客户的可写字段（POST/PUT /customers 请求体，后端白名单） */
export interface CustomerPayload {
  name: string
  phone: string
  gender: string
  birthday: string | null
  wechat: string
  source: string
  remark: string
}

/** GET /customers 查询参数（04-API.md:85-95）；keyword 支持姓名/手机号/微信号 */
export interface CustomerListQuery {
  keyword?: string
  phone?: string
  tag_id?: number
  sort?: string
  page?: number
  page_size?: number
}

/** GET /customers：分页 + 筛选（查询/新增/编辑 both，删除仅 admin，04-API.md:72） */
export function listCustomers(query: CustomerListQuery = {}): Promise<PageData<Customer>> {
  return get<PageData<Customer>>('/customers', { ...query })
}

/** POST /customers：非空手机号重复时后端返回 409（06-BUSINESS-RULES.md 第 1 节） */
export function createCustomer(payload: CustomerPayload): Promise<Customer> {
  return post<Customer>('/customers', payload)
}

/** GET /customers/:id：详情包含标签（含已软删除的历史标签） */
export function getCustomer(id: number): Promise<Customer> {
  return get<Customer>(`/customers/${id}`)
}

/** PUT /customers/:id：按 id 更新原行（改手机号不新建客户） */
export function updateCustomer(id: number, payload: CustomerPayload): Promise<Customer> {
  return put<Customer>(`/customers/${id}`, payload)
}

/**
 * DELETE /customers/:id：软删除，历史消费/充值/流水保留（06-BUSINESS-RULES.md:11）。
 * 路由层 RBAC 仅 admin 可调用；前端按角色隐藏按钮只是 UI 简化（07-UI.md:30）。
 */
export function deleteCustomer(id: number): Promise<Record<string, never>> {
  return del<Record<string, never>>(`/customers/${id}`)
}

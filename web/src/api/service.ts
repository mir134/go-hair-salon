import { del, get, post, put } from './http'
import type { PageData } from './http'

/**
 * 服务分类 DTO，对齐 server/internal/model/service.go 的 ServiceCategory
 * （03-DATABASE.md:87-96）：sort 控制展示顺序，status 1=启用 0=停用。
 */
export interface ServiceCategory {
  id: number
  name: string
  sort: number
  status: number
  created_at: string
  updated_at: string
}

/** 新增/编辑分类请求体（POST/PUT /service-categories，后端白名单） */
export interface ServiceCategoryPayload {
  name: string
  sort: number
  status: number
}

/**
 * 服务项目 DTO，对齐 server/internal/model/service.go 的 Service（03-DATABASE.md:98-111）。
 * price_cents 一律整数分（禁止 float64）；category_name 为后端可选的联表冗余字段，
 * 缺失时页面用已加载的分类列表按 category_id 解析（见 ServiceTable.vue）。
 */
export interface Service {
  id: number
  category_id: number
  name: string
  price_cents: number
  duration_minutes: number
  status: number
  remark: string
  category_name?: string
  created_at: string
  updated_at: string
}

/** 新增/编辑服务请求体（POST/PUT /services，后端白名单） */
export interface ServicePayload {
  category_id: number
  name: string
  price_cents: number
  duration_minutes: number
  status: number
  remark: string
}

/**
 * 列表边界归一化：GET /service-categories（后端已确认）与 GET /services 按数组返回，
 * 但 04-API.md 只列端点、未定列表形态；同时接受分页形态，保证两种契约下页面都能工作。
 */
function toList<T>(data: T[] | PageData<T>): T[] {
  return Array.isArray(data) ? data : data.items
}

/** GET /service-categories（both）：全部分类，按 sort 升序（页面再兜底排序） */
export async function listServiceCategories(): Promise<ServiceCategory[]> {
  return toList(await get<ServiceCategory[] | PageData<ServiceCategory>>('/service-categories'))
}

/** POST /service-categories（admin） */
export function createServiceCategory(payload: ServiceCategoryPayload): Promise<ServiceCategory> {
  return post<ServiceCategory>('/service-categories', payload)
}

/** PUT /service-categories/:id（admin） */
export function updateServiceCategory(
  id: number,
  payload: ServiceCategoryPayload,
): Promise<ServiceCategory> {
  return put<ServiceCategory>(`/service-categories/${id}`, payload)
}

/**
 * DELETE /service-categories/:id（admin）。
 * 分类下仍有服务时后端返回 422（plan todo 18），调用方需就地展示并引导先处理服务。
 */
export function deleteServiceCategory(id: number): Promise<Record<string, never>> {
  return del<Record<string, never>>(`/service-categories/${id}`)
}

/** GET /services（both）：全部服务（含分类信息），页面按分类/状态/关键字在前端筛选 */
export async function listServices(): Promise<Service[]> {
  return toList(await get<Service[] | PageData<Service>>('/services'))
}

/** POST /services（admin）：price_cents 必须 > 0，否则后端 400（plan todo 19） */
export function createService(payload: ServicePayload): Promise<Service> {
  return post<Service>('/services', payload)
}

/** GET /services/:id（admin）：编辑弹窗可直接用列表行，此接口供详情/兜底使用 */
export function getService(id: number): Promise<Service> {
  return get<Service>(`/services/${id}`)
}

/** PUT /services/:id（admin）：改价不影响历史订单（订单保存快照，06-BUSINESS-RULES.md:17） */
export function updateService(id: number, payload: ServicePayload): Promise<Service> {
  return put<Service>(`/services/${id}`, payload)
}

/**
 * DELETE /services/:id（admin）：软删除，仅允许无订单的服务。
 * 已产生订单的服务后端返回 422（06-BUSINESS-RULES.md:16），调用方需引导改为停用。
 */
export function deleteService(id: number): Promise<Record<string, never>> {
  return del<Record<string, never>>(`/services/${id}`)
}

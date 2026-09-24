import { del, get, post, put } from './http'
import type { PageData } from './http'

/**
 * 员工状态（server/internal/model/const.go：1=启用 0=停用）。
 * 停用是 DELETE /employees/:id 的语义（行保留，永不物理删除，06 §11:120-125）。
 */
export const EMPLOYEE_STATUS_ENABLED = 1
export const EMPLOYEE_STATUS_DISABLED = 0

/**
 * 员工 DTO，对齐 server/internal/controller/employee.go 的 EmployeeView
 * （03-DATABASE.md:31-42）：joined_at 为 RFC3339 或 null；
 * avatar 为 POST /uploads 返回的相对路径（/uploads/<hash>.<ext>），空串表示无头像。
 */
export interface Employee {
  id: number
  name: string
  phone: string
  avatar: string
  position: string
  status: number
  joined_at: string | null
  remark: string
}

/**
 * 新增/编辑员工请求体（POST/PUT /employees，后端白名单）。
 * status 不提供时后端默认启用/保持原值，但前端始终显式提交当前值。
 * joined_at 接受 RFC3339 或 YYYY-MM-DD；null 表示清空。
 */
export interface EmployeePayload {
  name: string
  phone: string
  avatar: string
  position: string
  status: number
  joined_at: string | null
  remark: string
}

/**
 * 列表边界归一化：GET /employees（后端已确认）按数组返回；
 * 同时接受分页形态，保证两种契约下页面都能工作（与 service.ts 一致）。
 */
function toList<T>(data: T[] | PageData<T>): T[] {
  return Array.isArray(data) ? data : data.items
}

/**
 * GET /employees（both）：默认含已停用员工；status=1|0 可选过滤
 * （非法值后端宽松回退为不过滤，不得 500）。
 * 查询开放给 staff：快速消费/挂单需选择服务员工（07-UI.md:50）；
 * 员工建档/修改/停用仍仅 admin。
 */
export async function listEmployees(status?: number): Promise<Employee[]> {
  const params = status === undefined ? undefined : { status }
  return toList(await get<Employee[] | PageData<Employee>>('/employees', params))
}

/** GET /employees/:id（admin）：含已停用员工，记录始终可读 */
export function getEmployee(id: number): Promise<Employee> {
  return get<Employee>(`/employees/${id}`)
}

/** POST /employees（admin）：姓名必填；joined_at 格式非法 → 400 */
export function createEmployee(payload: EmployeePayload): Promise<Employee> {
  return post<Employee>('/employees', payload)
}

/** PUT /employees/:id（admin）：全量更新档案字段 */
export function updateEmployee(id: number, payload: EmployeePayload): Promise<Employee> {
  return put<Employee>(`/employees/${id}`, payload)
}

/**
 * DELETE /employees/:id（admin）：语义是停用（status=0，行保留）——
 * 不是物理删除；停用员工不会出现在新订单的选择池中，但历史订单/账号关联保留。
 */
export function disableEmployee(id: number): Promise<Record<string, never>> {
  return del<Record<string, never>>(`/employees/${id}`)
}

import { get, post, put } from './http'
import type { AuthUser, UserRole } from './auth'
import type { PageData } from './http'

/**
 * 用户状态（server/internal/model/const.go：1=启用 0=停用）。
 * 停用只改 status、保留账号记录（06 §11:120-125）；后端没有用户删除接口。
 */
export const USER_STATUS_ENABLED = 1
export const USER_STATUS_DISABLED = 0

/**
 * 用户管理 DTO：与 GET /auth/me 的 AuthUser 同构
 * （server/internal/controller/auth.go 的 UserView，字段白名单，绝不含密码哈希）。
 * 复用同一类型，避免用户管理页与登录态出现两份漂移的结构定义。
 */
export type User = AuthUser

/**
 * POST /users 请求体（admin）：username/role（仅 admin|staff）/password 必填；
 * employee_id 可选（关联员工，null = 不关联）。
 */
export interface UserCreatePayload {
  username: string
  role: UserRole
  password: string
  employee_id: number | null
}

/**
 * PUT /users/:id 请求体（admin）：status 停用/启用与/或 password 重置密码，
 * 至少提供一项；本类型按需构造，不提供字段即不下发。
 */
export interface UserUpdatePayload {
  status?: number
  password?: string
}

/**
 * 列表边界归一化：GET /users（后端已确认）按数组返回；
 * 同时接受分页形态，保证两种契约下页面都能工作（与 service.ts 一致）。
 */
function toList<T>(data: T[] | PageData<T>): T[] {
  return Array.isArray(data) ? data : data.items
}

/**
 * GET /users（admin）：默认含已停用用户；status=1|0 可选过滤
 * （非法值后端宽松回退为不过滤，不得 500）。
 */
export async function listUsers(status?: number): Promise<User[]> {
  const params = status === undefined ? undefined : { status }
  return toList(await get<User[] | PageData<User>>('/users', params))
}

/** POST /users（admin）：重复 username → 409；角色非法 → 400；关联员工不存在 → 400 */
export function createUser(payload: UserCreatePayload): Promise<User> {
  return post<User>('/users', payload)
}

/**
 * PUT /users/:id（admin）：status 停用/启用 + 可选 password 重置密码。
 * 停用后该用户旧 token 立即失效（后端 JWT 中间件每次请求实时查库校验 status）。
 */
export function updateUser(id: number, payload: UserUpdatePayload): Promise<User> {
  return put<User>(`/users/${id}`, payload)
}

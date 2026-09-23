import { get, post } from './http'

/** 角色（04-API.md:60-68 权限矩阵）：admin 全量；staff 仅日常操作 */
export type UserRole = 'admin' | 'staff'

/** 用户 DTO，对齐 server/internal/controller/auth.go 的 UserView（绝不含密码哈希） */
export interface AuthUser {
  id: number
  username: string
  role: UserRole
  employee_id: number | null
  status: number
}

export interface LoginPayload {
  username: string
  password: string
}

/** POST /auth/login 的 data：24h JWT + 脱敏用户信息 */
export interface LoginResult {
  token: string
  expires_at: string
  user: AuthUser
}

/** POST /auth/login（04-API.md:46-58） */
export function login(payload: LoginPayload): Promise<LoginResult> {
  return post<LoginResult>('/auth/login', payload)
}

/** GET /auth/me：返回当前登录用户（后端 JWT 中间件实时查库校验 status） */
export function fetchMe(): Promise<AuthUser> {
  return get<AuthUser>('/auth/me')
}

/** POST /auth/logout：MVP 无黑名单，服务端仅写审计日志，前端负责丢弃 token */
export function logout(): Promise<Record<string, never>> {
  return post<Record<string, never>>('/auth/logout')
}

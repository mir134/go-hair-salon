import axios, { AxiosError, type AxiosRequestConfig, type AxiosResponse } from 'axios'
import { ElMessage } from 'element-plus'

/** 后端统一响应包络（04-API.md）：成功 code=0，失败 code 非 0 */
export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

/** 业务/网络错误：拦截器已弹出提示，调用方可按需就地处理 */
export class ApiError extends Error {
  readonly code: number

  constructor(code: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

/**
 * 登录 token 的 localStorage 键。
 * 本层（src/api）只读取与清除；写入由 auth store（todo 12）负责。
 */
export const TOKEN_STORAGE_KEY = 'hair-salon-token'

/** 与 server/internal/service/errors.go 的 CodeUnauthorized 对齐 */
const CODE_UNAUTHORIZED = 40100

/** 登录页路径（todo 12 提供该页面） */
const LOGIN_PATH = '/login'

/** 全站唯一的 axios 实例：统一前缀、超时与拦截器 */
export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

function isApiEnvelope(value: unknown): value is ApiEnvelope<unknown> {
  return typeof value === 'object' && value !== null && 'code' in value && 'message' in value
}

/** 未登录/凭证失效：清除本地 token 并整页跳转登录页（不依赖 router，避免 api -> router 反向依赖） */
function handleUnauthorized(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
  ElMessage.error('登录已失效，请重新登录')
  if (window.location.pathname !== LOGIN_PATH) {
    window.location.assign(LOGIN_PATH)
  }
}

function toApiError(error: unknown): ApiError {
  if (error instanceof AxiosError) {
    const payload: unknown = error.response?.data
    if (isApiEnvelope(payload)) {
      return new ApiError(payload.code, payload.message)
    }
    if (error.response) {
      return new ApiError(error.response.status, `请求失败（HTTP ${error.response.status}）`)
    }
    return new ApiError(-1, '网络连接失败，请检查网络后重试')
  }
  return new ApiError(-1, '请求失败，请稍后重试')
}

function isUnauthorizedCode(code: number): boolean {
  return code === CODE_UNAUTHORIZED || code === 401
}

// 请求拦截：自动携带 Authorization: Bearer <token>（04-API.md 认证）
http.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  if (token !== null && token !== '') {
    config.headers.set('Authorization', `Bearer ${token}`)
  }
  return config
})

// 响应拦截：统一处理包络错误与 HTTP 错误
http.interceptors.response.use(
  (response: AxiosResponse) => {
    const payload: unknown = response.data
    if (!isApiEnvelope(payload) || payload.code === 0) {
      return response
    }
    const message = payload.message !== '' ? payload.message : '请求失败'
    if (isUnauthorizedCode(payload.code)) {
      handleUnauthorized()
    } else {
      ElMessage.error(message)
    }
    return Promise.reject(new ApiError(payload.code, message))
  },
  (error: unknown) => {
    const apiError = toApiError(error)
    if (isUnauthorizedCode(apiError.code)) {
      handleUnauthorized()
    } else {
      ElMessage.error(apiError.message)
    }
    return Promise.reject(apiError)
  },
)

/** 发起请求并解包包络中的 data；失败时拦截器已提示并 reject ApiError */
export async function request<T>(config: AxiosRequestConfig): Promise<T> {
  const response = await http.request<ApiEnvelope<T>>(config)
  return response.data.data
}

/** GET /api/v1<url> */
export function get<T>(url: string, params?: Record<string, unknown>): Promise<T> {
  return request<T>({ url, method: 'GET', params })
}

/** POST /api/v1<url> */
export function post<T>(url: string, data?: unknown): Promise<T> {
  return request<T>({ url, method: 'POST', data })
}

/** PUT /api/v1<url> */
export function put<T>(url: string, data?: unknown): Promise<T> {
  return request<T>({ url, method: 'PUT', data })
}

/** DELETE /api/v1<url> */
export function del<T>(url: string, params?: Record<string, unknown>): Promise<T> {
  return request<T>({ url, method: 'DELETE', params })
}

// src/api 是前端唯一允许直连 axios 的目录（见 eslint.config.js 的 no-restricted-imports）。
// 其他层只从这里 import：import { get, post } from '@/api'
export { ApiError, TOKEN_STORAGE_KEY, del, get, http, post, put, request } from './http'
export type { ApiEnvelope } from './http'
export { fetchMe, login, logout } from './auth'
export type { AuthUser, LoginPayload, LoginResult, UserRole } from './auth'

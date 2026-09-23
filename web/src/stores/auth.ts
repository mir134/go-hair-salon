import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import {
  TOKEN_STORAGE_KEY,
  fetchMe as fetchMeApi,
  login as loginApi,
  logout as logoutApi,
} from '@/api'
import type { AuthUser, LoginPayload, UserRole } from '@/api'

/**
 * 登录态 store（todo 12）。
 *
 * token 持久化在 localStorage，键复用 `@/api` 导出的 TOKEN_STORAGE_KEY（`hair-salon-token`）——
 * 请求拦截器与 401 处理都依赖同一个键，禁止另建键名。
 */
export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_STORAGE_KEY) ?? '')
  const user = ref<AuthUser | null>(null)

  /** 是否持有 token；路由守卫只认它（真实权限由后端 RBAC 兜底） */
  const isAuthenticated = computed(() => token.value !== '')
  const role = computed<UserRole | null>(() => user.value?.role ?? null)
  const username = computed(() => user.value?.username ?? '')

  function setToken(value: string): void {
    token.value = value
    if (value === '') {
      localStorage.removeItem(TOKEN_STORAGE_KEY)
    } else {
      localStorage.setItem(TOKEN_STORAGE_KEY, value)
    }
  }

  /** 登录成功后写入 token 与用户信息 */
  async function login(payload: LoginPayload): Promise<void> {
    const result = await loginApi(payload)
    setToken(result.token)
    user.value = result.user
  }

  /**
   * 恢复登录态（刷新页面 / 应用启动）。
   * 401 由 axios 拦截器清 token 并整页跳登录页；其他错误（如网络）保留 token 供重试。
   */
  async function fetchMe(): Promise<void> {
    if (!isAuthenticated.value) {
      return
    }
    try {
      user.value = await fetchMeApi()
    } catch {
      // 拦截器已处理 401（清除 + 跳转）；此处只保证不残留过期用户信息
      if (!isAuthenticated.value) {
        user.value = null
      }
    }
  }

  /** 退出登录：先尽力写审计日志，无论成败都清空本地登录态 */
  async function logout(): Promise<void> {
    try {
      await logoutApi()
    } catch {
      // 服务端登出失败不阻塞本地退出（token 无黑名单，丢弃即失效）
    }
    setToken('')
    user.value = null
  }

  return { token, user, role, username, isAuthenticated, login, logout, fetchMe }
})

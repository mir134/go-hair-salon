import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import type { UserRole } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { pinia } from '@/stores'
import PlaceholderView from '@/views/PlaceholderView.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** 需要登录：父路由声明，子路由通过 to.matched 继承 */
    requiresAuth?: boolean
    /** 页面/菜单标题 */
    title?: string
    /** 可见该菜单的角色（仅导航简化，权限边界在后端 RBAC，07-UI.md:25-30） */
    roles?: readonly UserRole[]
    /** 子页面（如客户详情）高亮的左侧导航路径；不设时用当前路径 */
    navPath?: string
  }
}

export interface NavItem {
  path: string
  title: string
  roles: readonly UserRole[]
}

/** 左侧主导航（07-UI.md:19-30）：staff 仅 4 项，admin 额外 4 项 */
export const NAV_ITEMS: readonly NavItem[] = [
  { path: '/', title: 'Dashboard', roles: ['admin', 'staff'] },
  { path: '/customers', title: '客户', roles: ['admin', 'staff'] },
  { path: '/orders', title: '消费', roles: ['admin', 'staff'] },
  { path: '/recharges', title: '充值', roles: ['admin', 'staff'] },
  { path: '/services', title: '服务', roles: ['admin'] },
  { path: '/employees', title: '员工', roles: ['admin'] },
  { path: '/logs', title: '日志', roles: ['admin'] },
  { path: '/system', title: '系统', roles: ['admin'] },
]

/**
 * 已落地的导航页面（后续 todo 逐个替换 PlaceholderView）。
 * 键为 NAV_ITEMS 的 path，值为真实页面组件（懒加载）。
 */
const NAV_COMPONENTS: Readonly<Record<string, RouteRecordRaw['component']>> = {
  '/customers': () => import('@/views/customer/CustomerListView.vue'),
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { title: '登录' },
  },
  {
    path: '/',
    component: () => import('@/layouts/AppShell.vue'),
    meta: { requiresAuth: true },
    children: [
      // 导航项与左侧菜单（NAV_ITEMS）一一对应；未落地页面仍用占位页
      ...NAV_ITEMS.map(
        (item): RouteRecordRaw => ({
          path: item.path === '/' ? '' : item.path.slice(1),
          name: item.path === '/' ? 'dashboard' : item.path.slice(1),
          component: NAV_COMPONENTS[item.path] ?? PlaceholderView,
          meta: { title: item.title, roles: item.roles },
        }),
      ),
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

// history 模式；刷新/直达子路径由后端 SPA 兜底（todo 8）
export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

/**
 * 路由守卫（todo 12）：
 * - 未登录访问受保护路由 → /login（带 redirect 回跳）；
 * - 已登录访问 /login → /（避免重复登录）。
 * 每次导航（含浏览器后退）都会重新判定，logout 清除 token 后后退同样被拦截。
 */
router.beforeEach((to) => {
  const auth = useAuthStore(pinia)
  const requiresAuth = to.matched.some((record) => record.meta.requiresAuth === true)

  if (requiresAuth && !auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && auth.isAuthenticated) {
    return { path: '/' }
  }
  return true
})

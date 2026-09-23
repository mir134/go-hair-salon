import { createRouter, createWebHistory } from 'vue-router'

// history 模式；刷新/直达子路径由后端 SPA 兜底（todo 8）
export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
  ],
})

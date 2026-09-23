import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'

import App from './App.vue'
import { router } from './router'
import { pinia } from './stores'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
app.use(pinia).use(router).use(ElementPlus)

// 应用启动：本地有 token 则恢复登录态；401 由 axios 拦截器清除并跳转登录页（todo 12）
const auth = useAuthStore(pinia)
if (auth.isAuthenticated) {
  void auth.fetchMe()
}

app.mount('#app')

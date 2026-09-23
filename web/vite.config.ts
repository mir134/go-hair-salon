import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    // 开发期把 /api 与 /uploads 交给本地 Go 服务（todo 8：8080 同时提供 /api/v1 与 SPA 兜底；
    // /uploads 为头像上传接口的静态托管路径，前端 <img src> 直接引用）
    proxy: {
      '/api': 'http://localhost:8080',
      '/uploads': 'http://localhost:8080',
    },
  },
})

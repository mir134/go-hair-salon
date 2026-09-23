<template>
  <main class="login">
    <el-card class="login__card">
      <h1 class="login__title">
        {{ DEFAULT_SHOP_NAME }}
      </h1>
      <p class="login__subtitle">
        客户管理系统
      </p>

      <el-form
        class="login__form"
        @submit.prevent="handleSubmit"
      >
        <el-form-item>
          <el-input
            v-model="username"
            placeholder="用户名"
            autocomplete="username"
            size="large"
            clearable
            autofocus
          />
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="password"
            type="password"
            placeholder="密码"
            autocomplete="current-password"
            size="large"
            show-password
            @keyup.enter="handleSubmit"
          />
        </el-form-item>

        <p
          v-if="errorMessage !== ''"
          class="login__error"
        >
          {{ errorMessage }}
        </p>

        <el-button
          type="primary"
          native-type="submit"
          size="large"
          class="login__submit"
          :loading="loading"
        >
          登录
        </el-button>
      </el-form>
    </el-card>
  </main>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { useAuthStore } from '@/stores/auth'
import { DEFAULT_SHOP_NAME } from '@/constants'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMessage = ref('')

/** 仅接受站内路径，防止开放重定向（//evil.com 之类） */
function safeRedirect(value: unknown): string {
  if (typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')) {
    return value
  }
  return '/'
}

async function handleSubmit(): Promise<void> {
  // 防止 Enter 触发原生 submit + keyup 双发
  if (loading.value) {
    return
  }
  if (username.value.trim() === '' || password.value === '') {
    errorMessage.value = '请输入用户名和密码'
    return
  }

  loading.value = true
  errorMessage.value = ''
  try {
    await auth.login({ username: username.value.trim(), password: password.value })
    ElMessage.success('登录成功')
    await router.replace(safeRedirect(route.query.redirect))
  } catch (error) {
    // 拦截器已弹出后端 message；这里就地再展示一次（07-UI.md:88 表单错误就地提示）
    errorMessage.value = error instanceof Error ? error.message : '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: #f5f7fa;
  font-family: system-ui, -apple-system, 'Segoe UI', 'Microsoft YaHei', sans-serif;
}

.login__card {
  width: 360px;
}

.login__title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  text-align: center;
}

.login__subtitle {
  margin: 8px 0 24px;
  color: #909399;
  font-size: 14px;
  text-align: center;
}

.login__error {
  margin: 0 0 12px;
  color: var(--el-color-danger);
  font-size: 13px;
}

.login__submit {
  width: 100%;
}
</style>

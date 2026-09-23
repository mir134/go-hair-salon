<template>
  <el-container class="shell">
    <el-aside
      width="200px"
      class="shell__aside"
    >
      <div class="shell__brand">
        {{ settings.shopName }}
      </div>
      <el-menu
        :default-active="activeMenu"
        class="shell__menu"
        router
      >
        <el-menu-item
          v-for="item in visibleMenu"
          :key="item.path"
          :index="item.path"
        >
          {{ item.title }}
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="shell__header">
        <span class="shell__shop">{{ settings.shopName }}</span>
        <div class="shell__account">
          <span class="shell__username">{{ auth.username }}</span>
          <el-tag
            v-if="roleLabel !== ''"
            size="small"
            type="info"
          >
            {{ roleLabel }}
          </el-tag>
          <el-button
            link
            type="primary"
            @click="handleLogout"
          >
            退出登录
          </el-button>
        </div>
      </el-header>

      <el-main class="shell__main">
        <RouterView />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { NAV_ITEMS } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'

const ROLE_LABELS = { admin: '管理员', staff: '店员' } as const

const auth = useAuthStore()
const settings = useSettingsStore()
const route = useRoute()
const router = useRouter()

// 门店名称来自 settings API（GET /settings，both 可读）；设置页保存后同一 store 即时更新
onMounted(() => {
  void settings.ensureLoaded()
})

const roleLabel = computed(() => (auth.role === null ? '' : ROLE_LABELS[auth.role]))

/** 详情页等子页面用 meta.navPath 高亮所属导航（如 /customers/:id → 客户） */
const activeMenu = computed(() => route.meta.navPath ?? route.path)

// 角色过滤仅导航简化；真实权限由后端 RBAC 中间件兜底（07-UI.md:30）
const visibleMenu = computed(() => {
  const role = auth.role
  if (role === null) {
    return []
  }
  return NAV_ITEMS.filter((item) => item.roles.includes(role))
})

async function handleLogout(): Promise<void> {
  await auth.logout()
  await router.replace('/login')
}
</script>

<style scoped>
.shell {
  min-height: 100vh;
  font-family: system-ui, -apple-system, 'Segoe UI', 'Microsoft YaHei', sans-serif;
}

.shell__aside {
  border-right: 1px solid var(--el-border-color-light);
}

.shell__brand {
  display: flex;
  align-items: center;
  height: 60px;
  padding: 0 16px;
  font-size: 16px;
  font-weight: 600;
}

.shell__menu {
  border-right: none;
}

.shell__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--el-border-color-light);
}

.shell__shop {
  font-size: 15px;
  font-weight: 500;
}

.shell__account {
  display: flex;
  align-items: center;
  gap: 12px;
}

.shell__username {
  font-size: 14px;
  color: var(--el-text-color-primary);
}

.shell__main {
  background: #f5f7fa;
}
</style>

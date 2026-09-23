<template>
  <section
    class="quick-entries"
    aria-label="快捷入口"
  >
    <h2 class="quick-entries__title">
      快捷入口
    </h2>
    <div class="quick-entries__grid">
      <RouterLink
        v-for="entry in ENTRIES"
        :key="entry.key"
        :to="entry.to"
        class="quick-entries__item"
      >
        <span class="quick-entries__item-title">{{ entry.title }}</span>
        <span class="quick-entries__item-desc">{{ entry.description }}</span>
      </RouterLink>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

// 手机首页快捷入口（plan todo 53、07-UI.md:42）：
// 搜索客户 / 最近客户 / 快速消费 / 快速充值；卡片式大按钮，触控目标 ≥44px。
interface QuickEntry {
  key: string
  title: string
  description: string
  to: RouteLocationRaw
}

const ENTRIES: readonly QuickEntry[] = [
  {
    key: 'search',
    title: '搜索客户',
    description: '手机号 / 姓名',
    to: { path: '/customers', query: { focus: 'search' } },
  },
  {
    key: 'recent',
    title: '最近客户',
    description: '按最近到访排序',
    to: { path: '/customers', query: { sort: 'recent' } },
  },
  { key: 'consume', title: '快速消费', description: '选服务 → 收款', to: { path: '/orders/new' } },
  { key: 'recharge', title: '快速充值', description: '实付 + 赠送', to: { path: '/recharges/new' } },
]
</script>

<style scoped>
.quick-entries {
  margin-bottom: 16px;
}

.quick-entries__title {
  margin: 0 0 8px;
  font-size: 14px;
  font-weight: 600;
}

.quick-entries__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.quick-entries__item {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  min-height: 88px;
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  text-decoration: none;
  -webkit-tap-highlight-color: transparent;
}

.quick-entries__item-title {
  color: var(--el-text-color-primary);
  font-size: 17px;
  font-weight: 600;
}

.quick-entries__item-desc {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

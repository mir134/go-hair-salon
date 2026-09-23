<template>
  <nav
    class="mobile-tabbar"
    aria-label="底部导航"
  >
    <RouterLink
      v-for="item in items"
      :key="item.path"
      :to="item.path"
      class="mobile-tabbar__item"
      :class="{ 'mobile-tabbar__item--active': item.path === activePath }"
    >
      {{ item.title }}
    </RouterLink>
  </nav>
</template>

<script setup lang="ts">
// 手机端底部 Tab（plan todo 53、07-UI.md:31-43、02-AGENTS.md:92）：
// 4 个核心入口（首页/客户/消费/充值，均 both 角色），触控目标 ≥44px；
// 高亮项由 activePath 决定（详情页经 route.meta.navPath 高亮所属 Tab）。
// 管理员专属页面（服务/员工/日志等）不在 Tab 中，由 AppShell 顶栏「管理」菜单进入。
defineProps<{
  items: readonly { path: string; title: string }[]
  activePath: string
}>()
</script>

<style scoped>
.mobile-tabbar {
  position: fixed;
  right: 0;
  bottom: 0;
  left: 0;
  z-index: 100;
  display: flex;
  height: calc(56px + env(safe-area-inset-bottom));
  padding-bottom: env(safe-area-inset-bottom);
  background: #fff;
  border-top: 1px solid var(--el-border-color-light);
}

.mobile-tabbar__item {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  min-height: 56px;
  color: var(--el-text-color-regular);
  font-size: 14px;
  text-decoration: none;
  -webkit-tap-highlight-color: transparent;
}

.mobile-tabbar__item--active {
  color: var(--el-color-primary);
  font-weight: 600;
  box-shadow: inset 0 2px 0 var(--el-color-primary);
}
</style>

<template>
  <el-container
    class="shell"
    :class="{ 'shell--mobile': isMobile }"
    direction="vertical"
  >
    <el-container class="shell__wrap">
      <!-- PC 侧边栏：手机端隐藏（底部 Tab 承接主导航，02-AGENTS.md:88-92） -->
      <el-aside
        v-if="!isMobile"
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
            <template v-if="!isMobile">
              <span class="shell__username">{{ auth.username }}</span>
              <el-tag
                v-if="roleLabel !== ''"
                size="small"
                type="info"
              >
                {{ roleLabel }}
              </el-tag>
            </template>
            <!-- 手机端：底部 Tab 只放 4 个核心入口，管理员专属页面收进「管理」菜单 -->
            <el-dropdown
              v-if="isMobile && adminMenu.length > 0"
              trigger="click"
              @command="handleAdminCommand"
            >
              <el-button class="shell__admin">
                管理
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    v-for="item in adminMenu"
                    :key="item.path"
                    :command="item.path"
                  >
                    {{ item.title }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button
              link
              type="primary"
              class="shell__logout"
              @click="handleLogout"
            >
              退出登录
            </el-button>
          </div>
        </el-header>

        <el-main
          class="shell__main"
          :class="{ 'shell__main--mobile': isMobile }"
        >
          <RouterView />
        </el-main>
      </el-container>
    </el-container>

    <MobileTabBar
      v-if="isMobile"
      :items="mobileTabs"
      :active-path="activeMenu"
    />
  </el-container>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { useIsMobile } from '@/composables/useIsMobile'
import { NAV_ITEMS } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { setPageTitle } from '@/utils/title'

import MobileTabBar from './MobileTabBar.vue'

const ROLE_LABELS = { admin: '管理员', staff: '店员' } as const

/** 手机底部 Tab 的 4 个核心入口（07-UI.md:31-43）：均为 both 角色可见 */
const MOBILE_TAB_PATHS: readonly string[] = ['/', '/customers', '/orders', '/recharges']

const auth = useAuthStore()
const settings = useSettingsStore()
const route = useRoute()
const router = useRouter()
const isMobile = useIsMobile()

// 门店名称来自 settings API（GET /settings，both 可读）；设置页保存后同一 store 即时更新
onMounted(() => {
  void settings.ensureLoaded()
})

// 网页标题跟随店名：首次加载完成、管理员保存店名后都会触发（immediate 保证先设置兜底）
watch(
  () => settings.shopName,
  (name) => {
    setPageTitle(name)
  },
  { immediate: true },
)

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

/** 手机底部 Tab：与核心路径一一对应（顺序同主导航） */
const mobileTabs = computed(() =>
  NAV_ITEMS.filter((item) => MOBILE_TAB_PATHS.includes(item.path)),
)

/** 手机「管理」菜单：Tab 之外的导航项（仅 admin 有额外页面） */
const adminMenu = computed(() =>
  visibleMenu.value.filter((item) => !MOBILE_TAB_PATHS.includes(item.path)),
)

async function handleLogout(): Promise<void> {
  await auth.logout()
  await router.replace('/login')
}

/** el-dropdown 的 command 是宽类型；只接受导航路径字符串 */
function handleAdminCommand(command: string | number | object): void {
  if (typeof command === 'string') {
    void router.push(command)
  }
}
</script>

<style scoped>
.shell {
  min-height: 100vh;
  font-family: system-ui, -apple-system, 'Segoe UI', 'Microsoft YaHei', sans-serif;
}

.shell__wrap {
  flex: 1;
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

/* 顶部导航固定：滚动时吸附视口顶部，不随内容滚走（07-UI.md:23 顶部区域）。
   需要不透明底色，否则滚动内容会透到栏下；z-index 低于 Element Plus 弹层（2000+）。 */
.shell__header {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
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

/* 手机布局：内容区收窄内边距，并为固定底部 Tab 预留空间（含 iOS 安全区） */
.shell__main--mobile {
  padding: 12px;
  padding-bottom: calc(68px + env(safe-area-inset-bottom));
}

/* 手机端触控目标 ≥44px（07-UI.md:87、plan todo 53） */
.shell--mobile .shell__logout,
.shell--mobile .shell__admin {
  min-height: 44px;
}
</style>

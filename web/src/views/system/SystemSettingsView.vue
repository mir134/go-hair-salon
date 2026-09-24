<template>
  <section class="system-settings">
    <el-card
      v-loading="settings.loading"
      shadow="never"
    >
      <template #header>
        <div
          class="system-settings__header"
          :class="{ 'system-settings__header--mobile': isMobile }"
        >
          <span class="system-settings__title">系统设置</span>
          <span class="system-settings__subtitle">仅管理员可修改；修改需二次确认并写入操作日志</span>
        </div>
      </template>

      <el-form
        :label-width="isMobile ? 'auto' : '110px'"
        :label-position="isMobile ? 'top' : 'right'"
        @submit.prevent
      >
        <el-form-item label="门店名称">
          <div
            class="system-settings__row"
            :class="{ 'system-settings__row--mobile': isMobile }"
          >
            <el-input
              v-model="shopNameDraft"
              class="system-settings__input"
              maxlength="255"
              placeholder="请输入门店名称"
            />
            <el-button
              type="primary"
              :loading="savingKey === SETTING_KEY_SHOP_NAME"
              :disabled="!canSaveShopName"
              @click="handleSaveShopName"
            >
              保存
            </el-button>
          </div>
          <p class="system-settings__tip">
            显示在顶栏与侧栏；当前：{{ settings.shopName || '—' }}
          </p>
        </el-form-item>

        <el-form-item label="积分比例">
          <div
            class="system-settings__row"
            :class="{ 'system-settings__row--mobile': isMobile }"
          >
            <el-input
              v-model="ratioDraft"
              class="system-settings__ratio"
              placeholder="正整数"
            />
            <span class="system-settings__unit">积分 / 每消费 1 元</span>
            <el-button
              type="primary"
              :loading="savingKey === SETTING_KEY_POINTS_PER_YUAN"
              :disabled="!canSaveRatio"
              @click="handleSaveRatio"
            >
              保存
            </el-button>
          </div>
          <p class="system-settings__tip">
            当前每消费 1 元获得 {{ settings.pointsPerYuan ?? '—' }} 积分；新比例仅影响之后的消费，不追溯历史订单
          </p>
        </el-form-item>
      </el-form>
    </el-card>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { SETTING_KEY_POINTS_PER_YUAN, SETTING_KEY_SHOP_NAME } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { useSettingsStore } from '@/stores/settings'

// 系统设置页（07-UI.md:96-110、plan todo 42）：仅 admin 路由可达（router meta.roles）。
// 两项值一律来自 GET /settings（store），页面不写死默认值；
// 保存成功后 store 即时更新本地状态 → 顶栏门店名称无需刷新即变化（07-UI.md:104）。
// 手机端（<768px）：标签置顶（label-position=top）+ 控件纵向铺满整行，
// 避免固定 label-width="110px" 的横排布局在 375px 视口横向溢出（plan todo 53/55、07-UI.md:87）。
const settings = useSettingsStore()
const isMobile = useIsMobile()

const shopNameDraft = ref('')
const ratioDraft = ref('')
const savingKey = ref<string | null>(null)

watch(
  () => settings.shopName,
  (value) => {
    shopNameDraft.value = value
  },
  { immediate: true },
)
watch(
  () => settings.pointsPerYuan,
  (value) => {
    ratioDraft.value = value === null ? '' : String(value)
  },
  { immediate: true },
)

const canSaveShopName = computed(() => {
  const value = shopNameDraft.value.trim()
  return savingKey.value === null && value !== '' && value !== settings.shopName
})

/** 比例草稿 → 正整数；空/非数字/0 返回 null（前端校验拦截，后端仍兜底校验） */
const parsedRatio = computed(() => {
  const text = ratioDraft.value.trim()
  if (!/^\d+$/.test(text)) {
    return null
  }
  const value = Number.parseInt(text, 10)
  return value > 0 ? value : null
})

const canSaveRatio = computed(
  () => savingKey.value === null && parsedRatio.value !== null && parsedRatio.value !== settings.pointsPerYuan,
)

onMounted(() => {
  void settings.load()
})

/** 二次确认（07-UI.md:105）：取消返回 false，不发起请求 */
async function confirmChange(message: string): Promise<boolean> {
  try {
    await ElMessageBox.confirm(message, '修改二次确认', {
      type: 'warning',
      confirmButtonText: '确认修改',
      cancelButtonText: '取消',
    })
    return true
  } catch {
    return false
  }
}

async function handleSaveShopName(): Promise<void> {
  const value = shopNameDraft.value.trim()
  if (value === '') {
    ElMessage.warning('门店名称不能为空')
    return
  }
  if (value === settings.shopName) {
    return
  }
  const confirmed = await confirmChange(
    `确认把门店名称改为「${value}」？保存后顶栏立即更新，并写入操作日志。`,
  )
  if (!confirmed) {
    return
  }
  await save(SETTING_KEY_SHOP_NAME, value, '门店名称已保存')
}

async function handleSaveRatio(): Promise<void> {
  const ratio = parsedRatio.value
  if (ratio === null) {
    ElMessage.error('积分比例必须是正整数（如 1 表示每消费 1 元获得 1 积分）')
    return
  }
  if (ratio === settings.pointsPerYuan) {
    return
  }
  const confirmed = await confirmChange(
    `确认把积分比例改为 ${ratio}（每消费 1 元获得 ${ratio} 积分）？` +
      '新比例仅影响之后的消费，不追溯历史订单；修改将写入操作日志。',
  )
  if (!confirmed) {
    return
  }
  await save(SETTING_KEY_POINTS_PER_YUAN, String(ratio), '积分比例已保存，仅影响之后的消费')
}

/** 保存单项设置：成功后 store 立即回写（顶栏即时生效）；失败文案由 axios 拦截器提示 */
async function save(key: string, value: string, successText: string): Promise<void> {
  savingKey.value = key
  try {
    await settings.save(key, value)
    ElMessage.success(successText)
  } catch {
    // 400/403/404 等错误已由拦截器提示，草稿保留供修改
  } finally {
    savingKey.value = null
  }
}
</script>

<style scoped>
.system-settings__header {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

/* 手机端：标题与说明纵向排列，长说明不再与标题抢宽度（plan todo 55） */
.system-settings__header--mobile {
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.system-settings__title {
  font-size: 16px;
  font-weight: 600;
}

.system-settings__subtitle {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.system-settings__row {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* 手机端：输入框 + 单位说明 + 按钮纵向铺满整行，触控目标 ≥44px（plan todo 53/55、07-UI.md:87） */
.system-settings__row--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.system-settings__row--mobile .el-button {
  min-height: 44px;
}

.system-settings__row--mobile .system-settings__input {
  max-width: none;
  width: 100%;
}

.system-settings__row--mobile .system-settings__ratio {
  width: 100%;
}

.system-settings__input {
  max-width: 320px;
}

.system-settings__ratio {
  width: 120px;
}

.system-settings__unit {
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.system-settings__tip {
  width: 100%;
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>

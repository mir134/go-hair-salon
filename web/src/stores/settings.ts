import { ref } from 'vue'
import { defineStore } from 'pinia'

import {
  SETTING_KEY_POINTS_PER_YUAN,
  SETTING_KEY_SHOP_NAME,
  listSettings,
  updateSetting as updateSettingApi,
} from '@/api'
import type { Setting } from '@/api'

/**
 * 系统设置 store（todo 42、07-UI.md:96-110）。
 *
 * 顶栏门店名称与系统设置页共用同一状态：保存成功后立即更新本地值，
 * 顶栏无需刷新即变化（07-UI.md:104「顶部门店名称即时更新」）。
 * 初始值一律来自 GET /settings，不在前端写死默认（03-DATABASE.md:263）。
 */
export const useSettingsStore = defineStore('settings', () => {
  const shopName = ref('')
  const pointsPerYuan = ref<number | null>(null)
  const loaded = ref(false)
  const loading = ref(false)

  /** 把服务端设置项写入本地状态（加载与保存共用）。 */
  function apply(setting: Setting): void {
    if (setting.key === SETTING_KEY_SHOP_NAME) {
      shopName.value = setting.value
    }
    if (setting.key === SETTING_KEY_POINTS_PER_YUAN) {
      pointsPerYuan.value = parseRatio(setting.value)
    }
  }

  /** GET /settings：覆盖本地状态；失败由 axios 拦截器提示，页面显示空值。 */
  async function load(): Promise<void> {
    if (loading.value) {
      return
    }
    loading.value = true
    try {
      const settings = await listSettings()
      for (const setting of settings) {
        apply(setting)
      }
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  /** 进入 AppShell 时加载一次；已加载则不重复请求（刷新页面后会重新加载）。 */
  async function ensureLoaded(): Promise<void> {
    if (loaded.value || loading.value) {
      return
    }
    await load()
  }

  /** PUT /settings/:key（admin）：成功后立即更新本地状态，顶栏即时生效。 */
  async function save(key: string, value: string): Promise<Setting> {
    const setting = await updateSettingApi(key, value)
    apply(setting)
    return setting
  }

  return { shopName, pointsPerYuan, loaded, loading, ensureLoaded, load, save }
})

/** 解析积分比例：后端保证十进制正整数；异常值回退 null（页面显示占位符）。 */
function parseRatio(value: string): number | null {
  const ratio = Number.parseInt(value, 10)
  return Number.isInteger(ratio) && ratio > 0 ? ratio : null
}

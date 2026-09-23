import { ref, type Ref } from 'vue'

import { getCustomer, listServices } from '@/api'
import type { Customer, Service } from '@/api'

/** useConsumeCatalog 返回句柄：服务目录 + 入口预选客户（plan todo 23/29） */
export interface ConsumeCatalog {
  services: Ref<Service[]>
  loadingServices: Ref<boolean>
  /** 加载服务项目（含停用项，页面自行过滤）；失败时拦截器已提示，列表降级为空 */
  loadServices: () => Promise<void>
  /** 客户详情「快速消费」带 ?customer_id= 进入时预选客户；非法/不存在返回 null */
  presetCustomer: (rawId: unknown) => Promise<Customer | null>
}

/**
 * 快速消费页的目录数据（07-UI.md:50）：服务项目列表与入口预选客户。
 * 与提交编排（useConsumeSubmit）分离，页面只保留表单状态与提交流程。
 */
export function useConsumeCatalog(): ConsumeCatalog {
  const services = ref<Service[]>([])
  const loadingServices = ref(false)

  async function loadServices(): Promise<void> {
    loadingServices.value = true
    try {
      services.value = await listServices()
    } catch {
      // 拦截器已提示；服务选择降级为空，可刷新重试
    } finally {
      loadingServices.value = false
    }
  }

  async function presetCustomer(rawId: unknown): Promise<Customer | null> {
    const id = Number(Array.isArray(rawId) ? rawId[0] : rawId)
    if (!Number.isSafeInteger(id) || id <= 0) {
      return null
    }
    try {
      return await getCustomer(id)
    } catch {
      // 拦截器已提示（客户不存在/已删除）；保持未选择状态，可手动搜索
      return null
    }
  }

  return { services, loadingServices, loadServices, presetCustomer }
}

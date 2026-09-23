import { ref, type Ref } from 'vue'

import type { PageData } from '@/api'

/** usePagedList 的返回句柄：分页状态与加载动作（ref 均为顶层，可直接在模板中使用） */
export interface PagedList<T> {
  items: Ref<T[]>
  total: Ref<number>
  page: Ref<number>
  pageSize: Ref<number>
  loading: Ref<boolean>
  /** 按当前 page/pageSize 拉取一页；失败时拦截器已提示且保留原列表 */
  load: () => Promise<void>
}

/**
 * 通用分页列表状态（客户列表与客户详情各流水 tab 共用）。
 *
 * fetchPage 由调用方闭包提供，天然携带筛选条件；后端会回显归一化后的
 * page/page_size（非法值在服务端回退），因此加载成功后回写状态。
 */
export function usePagedList<T>(
  fetchPage: (page: number, pageSize: number) => Promise<PageData<T>>,
  initialPageSize = 20,
): PagedList<T> {
  const items = ref([]) as Ref<T[]>
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(initialPageSize)
  const loading = ref(false)

  async function load(): Promise<void> {
    loading.value = true
    try {
      const data = await fetchPage(page.value, pageSize.value)
      items.value = data.items
      total.value = data.total
      page.value = data.page
      pageSize.value = data.page_size
    } catch {
      // 请求失败：拦截器已弹提示；保留当前列表，避免把页面清空
    } finally {
      loading.value = false
    }
  }

  return { items, total, page, pageSize, loading, load }
}

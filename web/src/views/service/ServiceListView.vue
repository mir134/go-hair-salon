<template>
  <section class="service-list">
    <ServiceCategoryPanel
      :categories="categories"
      :loading="loadingCategories"
      @changed="handleCategoriesChanged"
    />

    <ServiceTable
      :services="services"
      :categories="categories"
      :loading="loadingServices"
      @changed="handleServicesChanged"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listServiceCategories, listServices } from '@/api'
import type { Service, ServiceCategory } from '@/api'

import ServiceCategoryPanel from './components/ServiceCategoryPanel.vue'
import ServiceTable from './components/ServiceTable.vue'

// 服务项目页（07-UI.md:12、plan todo 20）：分类列表 + 服务表格。
// 仅 admin 可访问（路由 meta.roles + 导航过滤），后端 RBAC 仍是最终边界。
const categories = ref<ServiceCategory[]>([])
const services = ref<Service[]>([])
const loadingCategories = ref(false)
const loadingServices = ref(false)

onMounted(() => {
  void loadCategories()
  void loadServices()
})

async function loadCategories(): Promise<void> {
  loadingCategories.value = true
  try {
    categories.value = await listServiceCategories()
  } catch {
    // 拦截器已提示；保留原列表
  } finally {
    loadingCategories.value = false
  }
}

async function loadServices(): Promise<void> {
  loadingServices.value = true
  try {
    services.value = await listServices()
  } catch {
    // 拦截器已提示；保留原列表
  } finally {
    loadingServices.value = false
  }
}

/** 分类增删改后：分类列表与服务行的分类名都可能变化，两者都刷新 */
function handleCategoriesChanged(): void {
  void loadCategories()
  void loadServices()
}

/** 服务增删改/启停后刷新服务列表 */
function handleServicesChanged(): void {
  void loadServices()
}
</script>

<style scoped>
.service-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>

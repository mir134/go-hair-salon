<template>
  <section class="employee-list">
    <EmployeeTable
      :employees="employees"
      :loading="loading"
      @changed="load"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listEmployees } from '@/api'
import type { Employee } from '@/api'

import EmployeeTable from './components/EmployeeTable.vue'

// 员工管理页（07-UI.md:13、plan todo 40）：仅 admin 可访问
// （路由 meta.roles + 导航过滤），后端 RBAC 仍是最终边界。
// 列表包含已停用员工（后端默认返回全部），页面按状态在前端筛选。
const employees = ref<Employee[]>([])
const loading = ref(false)

onMounted(() => {
  void load()
})

async function load(): Promise<void> {
  loading.value = true
  try {
    employees.value = await listEmployees()
  } catch {
    // 拦截器已提示；保留原列表
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.employee-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>

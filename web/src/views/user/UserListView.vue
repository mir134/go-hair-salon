<template>
  <section class="user-list">
    <UserTable
      :users="users"
      :employees="employees"
      :loading="loadingUsers"
      @changed="loadUsers"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listEmployees, listUsers } from '@/api'
import type { Employee, User } from '@/api'

import UserTable from './components/UserTable.vue'

// 用户管理页（plan todo 40）：仅 admin 可访问（路由 meta.roles + 导航过滤）。
// 用户列表与员工列表分开加载：用户变更只需刷新用户；员工列表用于展示关联员工名
// 以及「新增用户」弹窗的关联下拉（弹窗内只提供启用中的员工）。
const users = ref<User[]>([])
const employees = ref<Employee[]>([])
const loadingUsers = ref(false)

onMounted(() => {
  void loadUsers()
  void loadEmployees()
})

async function loadUsers(): Promise<void> {
  loadingUsers.value = true
  try {
    users.value = await listUsers()
  } catch {
    // 拦截器已提示；保留原列表
  } finally {
    loadingUsers.value = false
  }
}

async function loadEmployees(): Promise<void> {
  try {
    employees.value = await listEmployees()
  } catch {
    // 拦截器已提示；关联员工名与下拉选项退化为空
  }
}
</script>

<style scoped>
.user-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>

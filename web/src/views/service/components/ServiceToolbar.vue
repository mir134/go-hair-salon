<template>
  <div class="service-toolbar">
    <el-input
      v-model="keyword"
      class="service-toolbar__search"
      placeholder="搜索服务名称"
      clearable
    />
    <el-select
      v-model="categoryFilter"
      class="service-toolbar__filter"
      placeholder="全部分类"
      clearable
    >
      <el-option
        v-for="category in categories"
        :key="category.id"
        :label="category.name"
        :value="category.id"
      />
    </el-select>
    <el-select
      v-model="statusFilter"
      class="service-toolbar__status"
      placeholder="全部状态"
      clearable
    >
      <el-option
        label="启用"
        :value="1"
      />
      <el-option
        label="停用"
        :value="0"
      />
    </el-select>
    <span class="service-toolbar__spacer" />
    <el-button
      type="primary"
      @click="emit('create')"
    >
      新增服务
    </el-button>
  </div>
</template>

<script setup lang="ts">
import type { ServiceCategory } from '@/api'

// 服务表格工具栏：筛选状态由父级持有（用于过滤），本组件只做 v-model 透传与新增入口。
const keyword = defineModel<string>('keyword', { required: true })
const categoryFilter = defineModel<number | null>('categoryFilter', { required: true })
const statusFilter = defineModel<number | null>('statusFilter', { required: true })

defineProps<{ categories: ServiceCategory[] }>()

const emit = defineEmits<{
  create: []
}>()
</script>

<style scoped>
.service-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.service-toolbar__search {
  width: 220px;
}

.service-toolbar__filter {
  width: 160px;
}

.service-toolbar__status {
  width: 130px;
}

.service-toolbar__spacer {
  flex: 1;
}
</style>

<template>
  <div class="customer-toolbar">
    <el-input
      v-model="keyword"
      class="customer-toolbar__search"
      placeholder="搜索手机号 / 姓名 / 微信号"
      clearable
      @keyup.enter="emit('search')"
    />
    <el-select
      v-model="tagFilter"
      class="customer-toolbar__filter"
      placeholder="全部标签"
      clearable
      @change="emit('search')"
    >
      <el-option
        v-for="tag in tags"
        :key="tag.id"
        :label="tag.name"
        :value="tag.id"
      />
    </el-select>
    <el-button
      type="primary"
      @click="emit('search')"
    >
      搜索
    </el-button>
    <el-button @click="emit('reset')">
      重置
    </el-button>
    <span class="customer-toolbar__spacer" />
    <el-button
      type="primary"
      @click="emit('create')"
    >
      新增客户
    </el-button>
  </div>
</template>

<script setup lang="ts">
import type { Tag } from '@/api'

// 客户列表工具栏：关键字（优先手机号）/标签筛选 + 新增入口。
// 筛选状态由父级持有，本组件只负责交互与 v-model 透传。
const keyword = defineModel<string>('keyword', { required: true })
const tagFilter = defineModel<number | null>('tagFilter', { required: true })

defineProps<{ tags: Tag[] }>()

const emit = defineEmits<{
  /** 关键字/标签变化或点击搜索 */
  search: []
  reset: []
  create: []
}>()
</script>

<style scoped>
.customer-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.customer-toolbar__search {
  width: 260px;
}

.customer-toolbar__filter {
  width: 180px;
}

.customer-toolbar__spacer {
  flex: 1;
}
</style>

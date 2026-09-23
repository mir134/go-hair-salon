<template>
  <div class="list-pagination">
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="size"
      :total="total"
      :page-sizes="pageSizes"
      layout="total, sizes, prev, pager, next"
      background
      @change="emit('change')"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

// 分页条（客户列表与详情各流水 tab 共用）：只同步页码与每页条数，加载由调用方决定。
const props = withDefaults(
  defineProps<{
    total: number
    page: number
    pageSize: number
    pageSizes?: number[]
  }>(),
  { pageSizes: () => [10, 20, 50] },
)

const emit = defineEmits<{
  'update:page': [value: number]
  'update:pageSize': [value: number]
  /** 页码或每页条数变化后触发 */
  change: []
}>()

const currentPage = computed({
  get: () => props.page,
  set: (value: number) => emit('update:page', value),
})

const size = computed({
  get: () => props.pageSize,
  set: (value: number) => emit('update:pageSize', value),
})
</script>

<style scoped>
.list-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>

<template>
  <div class="log-filters">
    <el-select
      v-model="operatorId"
      class="log-filters__operator"
      clearable
      filterable
      placeholder="按操作人筛选"
    >
      <el-option
        v-for="item in operators"
        :key="item.id"
        :label="item.name"
        :value="item.id"
      />
    </el-select>
    <el-select
      v-model="action"
      class="log-filters__action"
      clearable
      filterable
      placeholder="按动作筛选"
    >
      <el-option
        v-for="item in OPERATION_LOG_ACTIONS"
        :key="item"
        :label="operationLogActionLabel(item)"
        :value="item"
      />
    </el-select>
    <el-date-picker
      v-model="range"
      type="daterange"
      unlink-panels
      range-separator="至"
      start-placeholder="开始日期"
      end-placeholder="结束日期"
      value-format="YYYY-MM-DD"
    />
    <el-button @click="reset">
      重置
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

import {
  OPERATION_LOG_ACTIONS,
  emptyOperationLogFilters,
  operationLogActionLabel,
  type OperationLogFilters,
} from '@/utils/operationLog'

// 操作日志筛选（plan todo 48）：
// - 操作人：下拉选项由列表页从已加载日志中累积（用户管理 API 落地前的 MVP 方案，
//   避免为筛选新增未在 04-API 定义的接口）；null=不筛选；
// - 动作：受控词表（utils/operationLog.ts，与后端 WriteLog 实参对齐）；
// - 日期范围：YYYY-MM-DD 透传，后端按 UTC 日期边界解释。
// 任一条件变化即 emit change，由列表页重置到第 1 页后加载。
defineProps<{
  operators: readonly { id: number; name: string }[]
}>()

const emit = defineEmits<{
  change: [filters: OperationLogFilters]
}>()

const operatorId = ref<number | null>(null)
const action = ref('')
const range = ref<[string, string] | null>(null)

watch([operatorId, action, range], () => {
  const [start = '', end = ''] = range.value ?? []
  emit('change', {
    operatorId: operatorId.value,
    action: action.value,
    startDate: start,
    endDate: end,
  })
})

function reset(): void {
  const empty = emptyOperationLogFilters()
  operatorId.value = empty.operatorId
  action.value = empty.action
  range.value = null
}
</script>

<style scoped>
.log-filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.log-filters__operator {
  width: 200px;
}

.log-filters__action {
  width: 180px;
}
</style>

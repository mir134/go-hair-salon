<template>
  <!-- 手机端（<768px）：筛选项整列铺满 + size=large + 按钮全宽，避免 375px 横向溢出（plan todo 53/54、07-UI.md:119） -->
  <div
    v-if="isMobile"
    class="log-filters log-filters--mobile"
  >
    <el-select
      v-model="operatorId"
      class="log-filters__operator"
      size="large"
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
      size="large"
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
    <ResponsiveDatePicker
      v-model="range"
      class="log-filters__range"
      mode="range"
    />
    <el-button
      class="log-filters__reset"
      size="large"
      @click="reset"
    >
      重置
    </el-button>
  </div>

  <!-- PC：保持原有横排筛选栏不变 -->
  <div
    v-else
    class="log-filters"
  >
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
    <ResponsiveDatePicker
      v-model="range"
      mode="range"
    />
    <el-button @click="reset">
      重置
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

import ResponsiveDatePicker from '@/components/ResponsiveDatePicker.vue'
import { useIsMobile } from '@/composables/useIsMobile'
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
// 手机端（<768px，plan todo 53/54）：整列大控件；PC 端保持原横排布局。
defineProps<{
  operators: readonly { id: number; name: string }[]
}>()

const emit = defineEmits<{
  change: [filters: OperationLogFilters]
}>()

const isMobile = useIsMobile()
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

/* 手机端：整列布局，触控目标 ≥44px（plan todo 53/54、07-UI.md:119） */
.log-filters--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.log-filters--mobile .log-filters__operator,
.log-filters--mobile .log-filters__action {
  width: 100%;
}

/* 手机端重置按钮全宽 + 触控目标 ≥44px（plan todo 53/54） */
.log-filters--mobile .log-filters__reset {
  width: 100%;
  min-height: 44px;
  margin-left: 0;
}
</style>

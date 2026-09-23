<template>
  <el-table
    v-loading="loading"
    :data="items"
    :row-key="rowKey"
    class="performance-table"
  >
    <el-table-column
      label="员工"
      min-width="140"
    >
      <template #default="{ row }">
        {{ row.employee_name ? row.employee_name : '未分配' }}
      </template>
    </el-table-column>
    <el-table-column
      label="业绩金额（元）"
      width="160"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.amount_cents) }}
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="所选日期范围内暂无员工业绩" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { EmployeePerformanceRow } from '@/api'
import { formatCents } from '@/utils/format'

// 员工业绩表（plan todo 45/46）：按明细员工汇总成交金额，随页面日期范围刷新；
// 口径为 discount 后的成交金额（06-BUSINESS-RULES.md:86-94）。
// 明细与订单都无员工的行 employee_id=null，显示为「未分配」（金额仍计入）。
defineProps<{
  items: EmployeePerformanceRow[]
  loading: boolean
}>()

/** null 员工行不能作为 row-key，统一映射为稳定字符串 */
function rowKey(row: EmployeePerformanceRow): string {
  return row.employee_id === null ? 'unassigned' : `employee-${row.employee_id}`
}
</script>

<style scoped>
.performance-table {
  width: 100%;
}
</style>

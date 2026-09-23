<template>
  <el-table
    v-loading="loading"
    :data="items"
    row-key="id"
  >
    <el-table-column
      label="时间"
      width="150"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作人"
      width="120"
    >
      <template #default="{ row }">
        {{ operationLogOperatorLabel(row.operator_id, row.operator_name) }}
      </template>
    </el-table-column>
    <el-table-column
      label="动作"
      width="130"
    >
      <template #default="{ row }">
        {{ operationLogActionLabel(row.action) }}
      </template>
    </el-table-column>
    <el-table-column
      label="目标"
      width="140"
    >
      <template #default="{ row }">
        {{ operationLogTargetLabel(row.target_type, row.target_id) }}
      </template>
    </el-table-column>
    <el-table-column
      prop="content"
      label="内容"
      min-width="220"
      show-overflow-tooltip
    />
    <el-table-column
      prop="ip"
      label="IP"
      width="130"
    />
    <el-table-column
      label="操作"
      width="80"
      fixed="right"
    >
      <template #default="{ row }">
        <el-button
          link
          type="primary"
          @click="handleDetail(row)"
        >
          详情
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无操作日志" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { OperationLog } from '@/api'
import { formatDateTime } from '@/utils/format'
import {
  operationLogActionLabel,
  operationLogOperatorLabel,
  operationLogTargetLabel,
} from '@/utils/operationLog'

// 操作日志表格（plan todo 48）：时间倒序的审计流水；详情抽屉展示 content/ip/ua 全文。
// 本页只读——审计日志禁止修改/删除（AGENTS.md 第 5 节）。
defineProps<{
  items: OperationLog[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 查看某条日志详情（父级打开详情抽屉） */
  detail: [log: OperationLog]
}>()

/**
 * el-table 插槽 row 的类型是 Element Plus 的 DefaultRow（含索引签名），
 * 无法直接传给强类型的 emit；此处在库边界收敛为 OperationLog。
 */
function handleDetail(row: unknown): void {
  emit('detail', row as OperationLog)
}
</script>

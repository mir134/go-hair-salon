<template>
  <el-table
    v-loading="loading"
    :data="items"
    row-key="name"
  >
    <el-table-column
      prop="name"
      label="文件名"
      min-width="240"
      show-overflow-tooltip
    />
    <el-table-column
      label="大小"
      width="110"
    >
      <template #default="{ row }">
        {{ formatBytes(row.size_bytes) }}
      </template>
    </el-table-column>
    <el-table-column
      label="创建时间"
      width="170"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="110"
      fixed="right"
    >
      <template #default="{ row }">
        <el-button
          link
          type="danger"
          @click="handleRestore(row)"
        >
          恢复
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无备份，点击「手动备份」立即创建" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { Backup } from '@/api'
import { formatBytes } from '@/utils/backup'
import { formatDateTime } from '@/utils/format'

// 备份列表表格（plan todo 52）：文件名 / 大小 / 创建时间 + 恢复入口。
// 列表不提供删除：保留策略由后端（最近 7 份）统一管理，备份文件是数据安全网。
defineProps<{
  items: Backup[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 请求恢复某个备份（父级打开二次确认弹窗） */
  restore: [backup: Backup]
}>()

/** el-table 插槽 row 为库的宽松类型，在此收敛为 Backup（与 LogTable 一致） */
function handleRestore(row: unknown): void {
  emit('restore', row as Backup)
}
</script>

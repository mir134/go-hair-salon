<template>
  <el-card
    shadow="never"
    class="recent-card"
  >
    <template #header>
      <div class="recent-card__head">
        <span class="recent-card__title">最近消费</span>
        <span class="recent-card__hint">最新 10 条</span>
      </div>
    </template>
    <el-table
      v-loading="loading"
      :data="items"
      row-key="id"
      size="small"
      class="recent-card__table"
    >
      <el-table-column
        label="时间"
        width="140"
      >
        <template #default="{ row }">
          {{ formatDateTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column
        label="客户"
        min-width="100"
      >
        <template #default="{ row }">
          {{ row.customer_name ? row.customer_name : '—' }}
        </template>
      </el-table-column>
      <el-table-column
        label="实付（元）"
        width="110"
        align="right"
      >
        <template #default="{ row }">
          {{ formatCents(row.paid_amount_cents) }}
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="90"
      >
        <template #default="{ row }">
          <el-tag
            :type="ORDER_STATUS_TAG_TYPES[row.status] ?? 'info'"
            size="small"
          >
            {{ ORDER_STATUS_LABELS[row.status] ?? row.status }}
          </el-tag>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无消费记录" />
      </template>
    </el-table>
  </el-card>
</template>

<script setup lang="ts">
import type { Order } from '@/api'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TAG_TYPES } from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 最近消费（07-UI.md:78）：来自 /dashboard/summary 的最近 10 条订单，独立于日期范围。
defineProps<{
  items: Order[]
  loading: boolean
}>()
</script>

<style scoped>
.recent-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.recent-card__title {
  font-size: 16px;
  font-weight: 600;
}

.recent-card__hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.recent-card__table {
  width: 100%;
}
</style>

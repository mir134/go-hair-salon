<template>
  <el-descriptions
    class="pending-totals"
    :column="1"
    border
  >
    <el-descriptions-item label="原价（元）">
      {{ formatCents(order.original_amount_cents) }}
    </el-descriptions-item>
    <el-descriptions-item label="优惠（元）">
      {{ formatCents(order.discount_amount_cents) }}
    </el-descriptions-item>
    <el-descriptions-item label="实付（元）">
      <span class="pending-totals__paid">¥{{ formatCents(order.paid_amount_cents) }}</span>
    </el-descriptions-item>
  </el-descriptions>
</template>

<script setup lang="ts">
import type { Order } from '@/api'
import { formatCents } from '@/utils/format'

// 挂单金额合计（07-UI.md:59）：金额随明细由后端重算，这里只展示服务端返回值（禁止本地推算）。
defineProps<{ order: Order }>()
</script>

<style scoped>
.pending-totals {
  margin-top: 16px;
}

.pending-totals__paid {
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
</style>

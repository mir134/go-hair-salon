<template>
  <el-table
    :data="items"
    row-key="id"
    class="order-items-table"
  >
    <el-table-column
      label="服务"
      min-width="160"
    >
      <template #default="{ row }">
        {{ row.service_name_snapshot }}
      </template>
    </el-table-column>
    <el-table-column
      prop="quantity"
      label="数量"
      width="80"
      align="right"
    />
    <el-table-column
      label="单价（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.unit_price_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="优惠（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.discount_amount_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="金额（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.amount_cents) }}
      </template>
    </el-table-column>
    <template #empty>
      <el-empty
        description="订单已无明细"
        :image-size="60"
      />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { OrderItem } from '@/api'
import { formatCents } from '@/utils/format'

// 订单明细表（03-DATABASE.md:147-162）：服务名快照/数量/成交单价/优惠/金额；金额一律整数分。
defineProps<{ items: OrderItem[] }>()
</script>

<style scoped>
.order-items-table {
  width: 100%;
}
</style>

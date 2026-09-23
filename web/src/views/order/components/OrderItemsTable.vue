<template>
  <div class="order-items">
    <!-- 手机端（<768px）：明细卡片替代表格（plan todo 55、07-UI.md:119） -->
    <div
      v-if="isMobile"
      class="order-items__cards"
    >
      <article
        v-for="row in items"
        :key="row.id"
        class="order-items__card"
      >
        <div class="order-items__card-head">
          <span class="order-items__card-name">{{ row.service_name_snapshot }}</span>
          <span class="order-items__card-amount">¥{{ formatCents(row.amount_cents) }}</span>
        </div>
        <div class="order-items__card-meta">
          <span>{{ row.quantity }} × ¥{{ formatCents(row.unit_price_cents) }}</span>
          <span v-if="row.discount_amount_cents !== 0">
            优惠 ¥{{ formatCents(row.discount_amount_cents) }}
          </span>
        </div>
      </article>
      <el-empty
        v-if="items.length === 0"
        description="订单已无明细"
        :image-size="60"
      />
    </div>

    <el-table
      v-else
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
  </div>
</template>

<script setup lang="ts">
import type { OrderItem } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatCents } from '@/utils/format'

// 订单明细表（03-DATABASE.md:147-162）：服务名快照/数量/成交单价/优惠/金额；金额一律整数分。
// 手机端以卡片列表呈现（plan todo 55）。
defineProps<{ items: OrderItem[] }>()

const isMobile = useIsMobile()
</script>

<style scoped>
.order-items-table {
  width: 100%;
}

/* 手机端明细卡片（plan todo 55） */
.order-items__cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.order-items__card {
  padding: 10px 12px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.order-items__card-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.order-items__card-name {
  font-size: 15px;
  font-weight: 600;
}

.order-items__card-amount {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.order-items__card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

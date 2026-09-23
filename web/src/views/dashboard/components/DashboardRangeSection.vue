<template>
  <el-card
    v-loading="loading"
    shadow="never"
    class="range-section"
  >
    <template #header>
      <div class="range-section__head">
        <span class="range-section__title">区间统计</span>
        <span class="range-section__range">{{ rangeLabel }}</span>
      </div>
    </template>

    <div class="range-section__totals">
      <div class="range-total">
        <span class="range-total__label">区间营业额（元）</span>
        <span class="range-total__value">{{ formatCents(totals.revenue_cents) }}</span>
      </div>
      <div class="range-total">
        <span class="range-total__label">区间新增客户</span>
        <span class="range-total__value">{{ totals.new_customer_count }}</span>
      </div>
      <div class="range-total">
        <span class="range-total__label">区间消费人次</span>
        <span class="range-total__value">{{ totals.customer_visits }}</span>
      </div>
    </div>

    <el-table
      :data="rows"
      row-key="date"
      size="small"
      class="range-section__table"
    >
      <el-table-column
        label="日期"
        prop="date"
        min-width="120"
      />
      <el-table-column
        label="营业额（元）"
        width="140"
        align="right"
      >
        <template #default="{ row }">
          {{ formatCents(row.revenue_cents) }}
        </template>
      </el-table-column>
      <el-table-column
        label="消费人数"
        width="100"
        align="right"
        prop="consuming_customer_count"
      />
      <el-table-column
        label="新增客户"
        width="100"
        align="right"
        prop="new_customer_count"
      />
      <template #empty>
        <el-empty description="所选日期范围内暂无数据" />
      </template>
    </el-table>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import { formatCents } from '@/utils/format'
import { sumDailyRows } from '@/utils/dashboard'
import type { DashboardDailyRow } from '@/utils/dashboard'

// 区间统计（plan todo 46：日期范围驱动 revenue/customers 接口；不用图表库）：
// 合计卡片 + 每日明细表格（revenue 与 customers 序列按日期合并）。
// 「消费人数」是每日去重值，区间合计列作「人次」，避免把去重数直接相加造成误导。
const props = defineProps<{
  rows: DashboardDailyRow[]
  rangeLabel: string
  loading: boolean
}>()

const totals = computed(() => sumDailyRows(props.rows))
</script>

<style scoped>
.range-section__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.range-section__title {
  font-size: 16px;
  font-weight: 600;
}

.range-section__range {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.range-section__totals {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.range-total {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.range-total__label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.range-total__value {
  font-size: 18px;
  font-weight: 600;
}

.range-section__table {
  width: 100%;
}
</style>

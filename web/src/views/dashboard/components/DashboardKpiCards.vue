<template>
  <div
    v-loading="loading"
    class="kpi-grid"
  >
    <el-card
      v-for="card in cards"
      :key="card.key"
      shadow="never"
      class="kpi-card"
      :class="{ 'kpi-card--clickable': card.clickable }"
      @click="handleClick(card)"
    >
      <div class="kpi-card__label">
        <span>{{ card.label }}</span>
        <el-tag
          v-if="card.realtime"
          size="small"
          type="warning"
          effect="plain"
        >
          实时
        </el-tag>
      </div>
      <div class="kpi-card__value">
        {{ card.value }}
      </div>
      <div
        v-if="card.clickable"
        class="kpi-card__hint"
      >
        点击查看待结账订单
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { DashboardSummary } from '@/api'
import { formatCents } from '@/utils/format'

// KPI 卡片（07-UI.md:78、06-BUSINESS-RULES.md:98-103）：
// 今日口径来自 /dashboard/summary（无日期参数，始终是当天）；
// 待结账单数为当前实时 pending 总数，不受页面日期范围影响，点击跳转待结账列表。
const props = defineProps<{
  summary: DashboardSummary | null
  loading: boolean
}>()

const emit = defineEmits<{
  'open-pending': []
}>()

interface KpiCard {
  key: string
  label: string
  value: string
  /** 实时数据（不受日期范围影响），卡片角标展示 */
  realtime: boolean
  clickable: boolean
}

const cards = computed<KpiCard[]>(() => {
  const summary = props.summary
  return [
    {
      key: 'revenue',
      label: '今日营业额',
      value: money(summary?.revenue_cents),
      realtime: false,
      clickable: false,
    },
    {
      key: 'order-count',
      label: '今日消费单数',
      value: count(summary?.completed_order_count),
      realtime: false,
      clickable: false,
    },
    {
      key: 'customer-count',
      label: '今日消费人数',
      value: count(summary?.consuming_customer_count),
      realtime: false,
      clickable: false,
    },
    {
      key: 'new-customers',
      label: '今日新增客户',
      value: count(summary?.new_customer_count),
      realtime: false,
      clickable: false,
    },
    {
      key: 'recharge',
      label: '今日充值金额',
      value: money(summary?.recharge_cents),
      realtime: false,
      clickable: false,
    },
    {
      key: 'pending',
      label: '待结账单数',
      value: count(summary?.pending_order_count),
      realtime: true,
      clickable: true,
    },
  ]
})

/** 未加载完成显示占位，避免把「无数据」误显示为 0 */
function money(cents: number | undefined): string {
  return cents === undefined ? '—' : `¥${formatCents(cents)}`
}

function count(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

function handleClick(card: KpiCard): void {
  if (card.clickable) {
    emit('open-pending')
  }
}
</script>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.kpi-card__label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.kpi-card__value {
  margin-top: 8px;
  font-size: 22px;
  font-weight: 600;
  line-height: 1.2;
}

.kpi-card__hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-color-primary);
}

.kpi-card--clickable {
  cursor: pointer;
}

.kpi-card--clickable:hover {
  border-color: var(--el-color-primary);
}
</style>

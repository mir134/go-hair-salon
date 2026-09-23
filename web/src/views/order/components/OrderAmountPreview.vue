<template>
  <div class="amount-preview">
    <el-descriptions
      :column="1"
      border
    >
      <el-descriptions-item label="服务">
        {{ serviceSummary }}
      </el-descriptions-item>
      <el-descriptions-item label="原价（元）">
        {{ formatCents(totals.originalCents) }}
      </el-descriptions-item>
      <el-descriptions-item label="成交价（元）">
        {{ formatCents(totals.paidCents) }}
      </el-descriptions-item>
      <el-descriptions-item
        v-if="totals.discountCents !== 0"
        label="优惠（元）"
      >
        {{ formatCents(totals.discountCents) }}
      </el-descriptions-item>
      <el-descriptions-item label="操作员工">
        {{ employeeLabel }}
      </el-descriptions-item>
      <el-descriptions-item label="支付方式">
        {{ paymentLabel }}
      </el-descriptions-item>
      <el-descriptions-item label="合计（元）">
        <span class="amount-preview__total">¥{{ formatCents(totals.paidCents) }}</span>
      </el-descriptions-item>
    </el-descriptions>

    <div
      v-if="isAdmin"
      class="amount-preview__reason"
    >
      <el-input
        :model-value="reason"
        :placeholder="needsReason ? '改价原因（必填，写入操作日志）' : '改价原因（本次未改价，可不填）'"
        maxlength="200"
        show-word-limit
        @input="(value) => emit('update:reason', value)"
      />
      <p
        v-if="needsReason && reason.trim() === ''"
        class="amount-preview__error"
      >
        已修改成交价：必须填写原因（06-BUSINESS-RULES.md §3.1）。
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import { formatCents } from '@/utils/format'
import { hasPriceOverride, orderTotals, type ConsumeItem } from '@/utils/orderForm'

// 消费第 5 步「确认前预览」（07-UI.md:61）：服务 / 原价 / 成交价 / 员工 / 支付方式 / 合计。
const props = defineProps<{
  items: ConsumeItem[]
  isAdmin: boolean
  employeeLabel: string
  paymentLabel: string
  reason: string
}>()

const emit = defineEmits<{
  'update:reason': [value: string]
}>()

const totals = computed(() => orderTotals(props.items))

/** 改价时原因必填（06 §3.1）；staff 不可改价，故该标记只对 admin 有意义 */
const needsReason = computed(() => hasPriceOverride(props.items))

const serviceSummary = computed(() => {
  if (props.items.length === 0) {
    return '—'
  }
  return props.items.map((item) => `${item.service.name} ×${item.quantity}`).join('、')
})
</script>

<style scoped>
.amount-preview__total {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.amount-preview__reason {
  margin-top: 12px;
}

.amount-preview__error {
  margin: 6px 0 0;
  color: var(--el-color-danger);
  font-size: 12px;
}
</style>

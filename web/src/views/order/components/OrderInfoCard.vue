<template>
  <div>
    <!-- 手机端：实付金额大字展示（plan todo 55、07-UI.md:117） -->
    <div
      v-if="isMobile"
      class="order-info__hero"
    >
      <span class="order-info__hero-label">实付（元）</span>
      <span class="order-info__hero-value">{{ formatCents(order.paid_amount_cents) }}</span>
    </div>
    <el-descriptions
      :column="isMobile ? 1 : 2"
      border
    >
      <el-descriptions-item label="订单号">
        {{ order.order_no }}
      </el-descriptions-item>
      <el-descriptions-item label="状态">
        <el-tag
          :type="ORDER_STATUS_TAG_TYPES[order.status] ?? 'info'"
          size="small"
        >
          {{ ORDER_STATUS_LABELS[order.status] ?? order.status }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="客户">
        <el-link
          type="primary"
          @click="emit('view-customer', order.customer_id)"
        >
          {{ order.customer_name !== undefined && order.customer_name !== '' ? order.customer_name : '—' }}
        </el-link>
      </el-descriptions-item>
      <el-descriptions-item label="操作员工">
        {{ order.employee_name !== undefined && order.employee_name !== '' ? order.employee_name : '未指定' }}
      </el-descriptions-item>
      <el-descriptions-item label="原价（元）">
        {{ formatCents(order.original_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="优惠（元）">
        {{ formatCents(order.discount_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item
        v-if="!isMobile"
        label="实付（元）"
      >
        <span class="order-info__paid">¥{{ formatCents(order.paid_amount_cents) }}</span>
      </el-descriptions-item>
      <el-descriptions-item label="支付方式">
        {{ paymentLabel }}
      </el-descriptions-item>
      <el-descriptions-item label="创建时间">
        {{ formatDateTime(order.created_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="备注">
        {{ order.remark !== '' ? order.remark : '—' }}
      </el-descriptions-item>
    </el-descriptions>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Order } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TAG_TYPES, PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 订单信息卡（07-UI.md:44-59）：状态/客户/员工/金额（原价/优惠/实付）/支付方式/时间/备注。
// 纯展示；客户点击跳转由父级处理。手机端实付大字 + 单列描述（plan todo 55）。
const props = defineProps<{ order: Order }>()

const emit = defineEmits<{
  'view-customer': [id: number]
}>()

const isMobile = useIsMobile()

const paymentLabel = computed(() => {
  const value = props.order.payment_method
  if (value === '') {
    return '—'
  }
  return PAYMENT_METHOD_LABELS[value] ?? value
})
</script>

<style scoped>
.order-info__paid {
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

/* 手机端实付大字（plan todo 55） */
.order-info__hero {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 12px;
  padding: 10px 12px;
  background: #f5f7fa;
  border-radius: 10px;
}

.order-info__hero-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.order-info__hero-value {
  font-size: 28px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
}
</style>

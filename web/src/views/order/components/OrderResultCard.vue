<template>
  <el-card shadow="never">
    <el-result
      :icon="isPending ? 'info' : 'success'"
      :title="title"
      :sub-title="`订单号：${order.order_no}`"
    >
      <template #extra>
        <el-button
          type="primary"
          @click="emit('restart')"
        >
          再开一单
        </el-button>
        <el-button @click="emit('viewCustomer')">
          查看客户
        </el-button>
        <!-- 挂单结果：父级按 slot 参数 pending 追加「去结账」入口 -->
        <slot
          name="actions"
          :pending="isPending"
        />
      </template>
    </el-result>

    <el-descriptions
      :column="2"
      border
    >
      <el-descriptions-item label="客户">
        {{ order.customer_name ?? customerName }}
      </el-descriptions-item>
      <el-descriptions-item label="状态">
        <el-tag
          :type="ORDER_STATUS_TAG_TYPES[order.status] ?? 'info'"
          size="small"
        >
          {{ ORDER_STATUS_LABELS[order.status] ?? order.status }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="原价（元）">
        {{ formatCents(order.original_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="优惠（元）">
        {{ formatCents(order.discount_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="实付（元）">
        {{ formatCents(order.paid_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item
        v-if="order.payment_method !== ''"
        label="支付方式"
      >
        {{ PAYMENT_METHOD_LABELS[order.payment_method] ?? order.payment_method }}
      </el-descriptions-item>
      <!-- 最新余额仅直接完成时展示（挂单不产生资金变动，无余额可核对） -->
      <el-descriptions-item
        v-if="balanceAfter !== null"
        label="最新余额（元）"
        :span="2"
      >
        {{ formatCents(balanceAfter) }}
      </el-descriptions-item>
    </el-descriptions>

    <p class="result__note">
      {{ note }}
    </p>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Order } from '@/api'
import {
  ORDER_STATUS_LABELS,
  ORDER_STATUS_TAG_TYPES,
  PAYMENT_METHOD_LABELS,
} from '@/constants'
import { formatCents } from '@/utils/format'

// 消费结果（07-UI.md:117）：订单号 + 金额明细 + 客户最新余额（余额支付重点核对）。
// 直接完成与挂单共用：文案按 order.status 推导，余额仅在直接完成后传入。
const props = defineProps<{
  order: Order
  customerName: string
  balanceAfter: number | null
}>()

/** 挂单=待结账（无资金变动），其余=已完成的直接完成单 */
const isPending = computed(() => props.order.status === 'pending')
const title = computed(() => (isPending.value ? '挂单成功（待结账）' : '消费已完成'))
const note = computed(() =>
  isPending.value
    ? '挂单不产生任何资金/余额/积分变动（06-BUSINESS-RULES.md:30）；请到「待结账」入口结账。'
    : '订单、余额流水与积分流水已由后端在同一事务内完成（06-BUSINESS-RULES.md:35）。',
)

const emit = defineEmits<{
  restart: []
  viewCustomer: []
}>()
</script>

<style scoped>
.result__note {
  margin: 12px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

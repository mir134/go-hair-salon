<template>
  <el-card shadow="never">
    <el-result
      icon="success"
      title="消费已完成"
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
        {{ ORDER_STATUS_LABELS[order.status] ?? order.status }}
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
      <el-descriptions-item label="支付方式">
        {{ PAYMENT_METHOD_LABELS[order.payment_method] ?? order.payment_method }}
      </el-descriptions-item>
      <el-descriptions-item
        label="最新余额（元）"
        :span="2"
      >
        {{ balanceAfter === null ? '—' : formatCents(balanceAfter) }}
      </el-descriptions-item>
    </el-descriptions>

    <p class="result__note">
      订单、余额流水与积分流水已由后端在同一事务内完成（06-BUSINESS-RULES.md:35）。
    </p>
  </el-card>
</template>

<script setup lang="ts">
import type { Order } from '@/api'
import { ORDER_STATUS_LABELS, PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents } from '@/utils/format'

// 消费成功结果（07-UI.md:117）：订单号 + 金额明细 + 客户最新余额（余额支付重点核对）。
defineProps<{
  order: Order
  customerName: string
  balanceAfter: number | null
}>()

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

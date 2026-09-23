<template>
  <!-- 手机端（<768px）：卡片列表替代表格（02-AGENTS.md:92、07-UI.md:119） -->
  <OrderCardList
    v-if="isMobile"
    :items="items"
    :loading="loading"
    :is-admin="isAdmin"
    @detail="(id) => emit('detail', id)"
    @checkout="(id) => emit('checkout', id)"
    @edit-items="(id) => emit('edit-items', id)"
    @cancel="(id, orderNo) => emit('cancel', id, orderNo)"
  />
  <el-table
    v-else
    v-loading="loading"
    :data="items"
    row-key="id"
    class="order-table"
  >
    <el-table-column
      label="订单号"
      min-width="180"
    >
      <template #default="{ row }">
        <el-link
          type="primary"
          @click="emit('detail', row.id)"
        >
          {{ row.order_no }}
        </el-link>
      </template>
    </el-table-column>
    <el-table-column
      label="客户"
      min-width="110"
    >
      <template #default="{ row }">
        {{ row.customer_name !== '' ? row.customer_name : '—' }}
      </template>
    </el-table-column>
    <el-table-column
      label="原价（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.original_amount_cents) }}
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
      label="实付（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.paid_amount_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="支付方式"
      width="100"
    >
      <template #default="{ row }">
        {{ row.payment_method === '' ? '—' : (PAYMENT_METHOD_LABELS[row.payment_method] ?? row.payment_method) }}
      </template>
    </el-table-column>
    <el-table-column
      label="状态"
      width="100"
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
    <el-table-column
      label="时间"
      width="150"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="210"
      fixed="right"
    >
      <template #default="{ row }">
        <template v-if="row.status === 'pending'">
          <el-button
            link
            type="primary"
            @click="emit('checkout', row.id)"
          >
            结账
          </el-button>
          <el-button
            link
            type="primary"
            @click="emit('edit-items', row.id)"
          >
            编辑明细
          </el-button>
          <el-button
            v-if="isAdmin"
            link
            type="danger"
            @click="emit('cancel', row.id, row.order_no)"
          >
            取消
          </el-button>
        </template>
        <el-button
          v-else
          link
          type="primary"
          @click="emit('detail', row.id)"
        >
          详情
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无消费记录" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { Order } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { ORDER_STATUS_LABELS, ORDER_STATUS_TAG_TYPES, PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

import OrderCardList from './OrderCardList.vue'

// 消费记录表格（07-UI.md:40、57）：订单号/客户/金额/支付方式/状态标签/时间。
// 行操作：待结账行提供 结账/编辑明细（取消仅 admin）；其余状态只读进详情。
// 手机端由 OrderCardList 以卡片呈现（plan todo 53）。
defineProps<{
  items: Order[]
  loading: boolean
  isAdmin: boolean
}>()

const isMobile = useIsMobile()

const emit = defineEmits<{
  detail: [id: number]
  checkout: [id: number]
  'edit-items': [id: number]
  cancel: [id: number, orderNo: string]
}>()
</script>

<style scoped>
.order-table {
  width: 100%;
}
</style>

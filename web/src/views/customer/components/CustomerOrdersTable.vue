<template>
  <div class="tx-table">
    <!-- 手机端（<768px）：卡片列表替代表格；点击卡片进订单详情（plan todo 54、07-UI.md:119） -->
    <OrderCardList
      v-if="isMobile"
      :items="items"
      :loading="loading"
      :is-admin="false"
      :actions="false"
      @detail="goDetail"
    />
    <el-table
      v-else
      v-loading="loading"
      :data="items"
      row-key="id"
    >
      <el-table-column
        prop="order_no"
        label="订单号"
        min-width="180"
      />
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
          {{ PAYMENT_METHOD_LABELS[row.payment_method] ?? row.payment_method }}
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
      <template #empty>
        <el-empty description="暂无消费记录" />
      </template>
    </el-table>

    <ListPagination
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="total"
      @change="handlePageChange"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'

import { listCustomerOrders } from '@/api'
import type { CustomerOrder } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { usePagedList } from '@/composables/usePagedList'
import {
  ORDER_STATUS_LABELS,
  ORDER_STATUS_TAG_TYPES,
  PAYMENT_METHOD_LABELS,
} from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'
import OrderCardList from '@/views/order/components/OrderCardList.vue'

// 客户详情「消费记录」tab：GET /customers/:id/orders（分页 + 时间倒序）。
// 手机端以卡片列表呈现（plan todo 54），点击卡片进订单详情。
const props = defineProps<{ customerId: number }>()

const router = useRouter()
const isMobile = useIsMobile()

const { items, total, page, pageSize, loading, load } = usePagedList<CustomerOrder>(
  (currentPage, currentPageSize) =>
    listCustomerOrders(props.customerId, { page: currentPage, page_size: currentPageSize }),
)

onMounted(() => {
  void load()
})

function handlePageChange(): void {
  void load()
}

/** 手机端卡片点击 → 订单详情（PC 表格行保持纯展示，不受影响） */
function goDetail(orderId: number): void {
  void router.push(`/orders/${orderId}`)
}
</script>

<template>
  <div class="tx-table">
    <el-table
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

import { listCustomerOrders } from '@/api'
import type { CustomerOrder } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import {
  ORDER_STATUS_LABELS,
  ORDER_STATUS_TAG_TYPES,
  PAYMENT_METHOD_LABELS,
} from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 客户详情「消费记录」tab：GET /customers/:id/orders（分页 + 时间倒序）。
const props = defineProps<{ customerId: number }>()

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
</script>

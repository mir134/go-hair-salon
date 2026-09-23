<template>
  <div class="tx-table">
    <el-table
      v-loading="loading"
      :data="items"
      row-key="id"
    >
      <el-table-column
        label="时间"
        width="150"
      >
        <template #default="{ row }">
          {{ formatDateTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column
        label="类型"
        width="100"
      >
        <template #default="{ row }">
          {{ POINTS_TX_TYPE_LABELS[row.type] ?? row.type }}
        </template>
      </el-table-column>
      <el-table-column
        label="积分变动"
        width="110"
        align="right"
      >
        <template #default="{ row }">
          <span :class="amountClass(row.points)">
            {{ row.points > 0 ? `+${row.points}` : row.points }}
          </span>
        </template>
      </el-table-column>
      <el-table-column
        label="变动前"
        width="100"
        align="right"
      >
        <template #default="{ row }">
          {{ row.balance_before }}
        </template>
      </el-table-column>
      <el-table-column
        label="变动后"
        width="100"
        align="right"
      >
        <template #default="{ row }">
          {{ row.balance_after }}
        </template>
      </el-table-column>
      <el-table-column
        label="关联"
        min-width="120"
      >
        <template #default="{ row }">
          {{ referenceText(row.reference_type, row.reference_id) }}
        </template>
      </el-table-column>
      <el-table-column
        prop="remark"
        label="备注"
        min-width="120"
      />
      <template #empty>
        <el-empty description="暂无积分流水" />
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

import { listCustomerPointsTransactions } from '@/api'
import type { PointsTransaction } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import { POINTS_TX_TYPE_LABELS, REFERENCE_TYPE_LABELS } from '@/constants'
import { formatDateTime } from '@/utils/format'

// 客户详情「积分流水」tab：GET /customers/:id/points-transactions（分页 + 时间倒序）。
const props = defineProps<{ customerId: number }>()

const { items, total, page, pageSize, loading, load } = usePagedList<PointsTransaction>(
  (currentPage, currentPageSize) =>
    listCustomerPointsTransactions(props.customerId, {
      page: currentPage,
      page_size: currentPageSize,
    }),
)

onMounted(() => {
  void load()
})

function handlePageChange(): void {
  void load()
}

/** 积分变动着色：增为正、减为负（只影响展示） */
function amountClass(points: number): string {
  return points < 0 ? 'tx-table__amount--out' : 'tx-table__amount--in'
}

/** 关联对象展示：「订单 #12」；无关联时显示占位符 */
function referenceText(referenceType: string, referenceId: number | null): string {
  if (referenceType === '') {
    return '—'
  }
  const label = REFERENCE_TYPE_LABELS[referenceType] ?? referenceType
  return referenceId === null ? label : `${label} #${referenceId}`
}
</script>

<style scoped>
.tx-table__amount--in {
  color: var(--el-color-success);
}

.tx-table__amount--out {
  color: var(--el-color-danger);
}
</style>

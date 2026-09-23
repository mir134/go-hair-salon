<template>
  <div class="tx-table">
    <!-- 手机端（<768px）：卡片列表替代表格（plan todo 54、07-UI.md:119） -->
    <div
      v-if="isMobile"
      v-loading="loading"
      class="tx-cards"
    >
      <article
        v-for="row in items"
        :key="row.id"
        class="tx-card"
      >
        <div class="tx-card__head">
          <span class="tx-card__type">{{ BALANCE_TX_TYPE_LABELS[row.type] ?? row.type }}</span>
          <span
            class="tx-card__amount"
            :class="amountClass(row.amount_cents)"
          >
            {{ formatCents(row.amount_cents) }}
          </span>
        </div>
        <div class="tx-card__meta">
          <span>{{ formatDateTime(row.created_at) }}</span>
          <span>{{ referenceText(row.reference_type, row.reference_id) }}</span>
        </div>
        <div class="tx-card__meta">
          余额 {{ formatCents(row.balance_before_cents) }} → {{ formatCents(row.balance_after_cents) }}
        </div>
        <p
          v-if="row.remark !== ''"
          class="tx-card__remark"
        >
          {{ row.remark }}
        </p>
      </article>
      <el-empty
        v-if="!loading && items.length === 0"
        description="暂无余额流水"
      />
    </div>

    <el-table
      v-else
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
          {{ BALANCE_TX_TYPE_LABELS[row.type] ?? row.type }}
        </template>
      </el-table-column>
      <el-table-column
        label="变动（元）"
        width="120"
        align="right"
      >
        <template #default="{ row }">
          <span :class="amountClass(row.amount_cents)">
            {{ formatCents(row.amount_cents) }}
          </span>
        </template>
      </el-table-column>
      <el-table-column
        label="变动前（元）"
        width="120"
        align="right"
      >
        <template #default="{ row }">
          {{ formatCents(row.balance_before_cents) }}
        </template>
      </el-table-column>
      <el-table-column
        label="变动后（元）"
        width="120"
        align="right"
      >
        <template #default="{ row }">
          {{ formatCents(row.balance_after_cents) }}
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
        <el-empty description="暂无余额流水" />
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

import { listCustomerBalanceTransactions } from '@/api'
import type { BalanceTransaction } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { usePagedList } from '@/composables/usePagedList'
import { BALANCE_TX_TYPE_LABELS, REFERENCE_TYPE_LABELS } from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 客户详情「余额流水」tab：GET /customers/:id/balance-transactions（分页 + 时间倒序）。
// 手机端以卡片列表呈现（plan todo 54）。
const props = defineProps<{ customerId: number }>()

const isMobile = useIsMobile()

const { items, total, page, pageSize, loading, load } = usePagedList<BalanceTransaction>(
  (currentPage, currentPageSize) =>
    listCustomerBalanceTransactions(props.customerId, {
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

/** 余额变动着色：增为正、减为负（只影响展示，不参与计算） */
function amountClass(amountCents: number): string {
  return amountCents < 0 ? 'tx-table__amount--out' : 'tx-table__amount--in'
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

/* 手机端余额流水卡片（plan todo 54） */
.tx-cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.tx-card {
  padding: 10px 12px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.tx-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.tx-card__type {
  font-size: 15px;
  font-weight: 600;
}

.tx-card__amount {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.tx-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.tx-card__remark {
  margin: 6px 0 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  word-break: break-word;
}
</style>

<template>
  <section class="recharge-list">
    <div class="recharge-list__head">
      <span class="recharge-list__title">充值记录</span>
      <el-button
        type="primary"
        @click="goCreate"
      >
        新增充值
      </el-button>
    </div>

    <el-card shadow="never">
      <RechargeFilterBar @change="handleFilterChange" />
      <RechargeTable
        :items="items"
        :loading="loading"
        show-customer
      />
      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        @change="handlePageChange"
      />
    </el-card>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { listRecharges } from '@/api'
import type { Recharge } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import type { RechargeListFilters } from '@/utils/rechargeForm'

import RechargeFilterBar from './components/RechargeFilterBar.vue'
import RechargeTable from './components/RechargeTable.vue'

// 充值导航页（plan todo 33）：充值记录列表（客户/日期筛选 + 分页），
// 「新增充值」进入 /recharges/new；客户详情「充值」带 ?customer_id= 预选客户。
const router = useRouter()
const filters = ref<RechargeListFilters>({ customerId: null, startDate: '', endDate: '' })

const { items, total, page, pageSize, loading, load } = usePagedList<Recharge>(
  (currentPage, currentPageSize) =>
    listRecharges({
      page: currentPage,
      page_size: currentPageSize,
      ...(filters.value.customerId === null ? {} : { customer_id: filters.value.customerId }),
      ...(filters.value.startDate === '' ? {} : { start_date: filters.value.startDate }),
      ...(filters.value.endDate === '' ? {} : { end_date: filters.value.endDate }),
    }),
)

onMounted(() => {
  void load()
})

/** 筛选变化：回到第 1 页并按新条件加载 */
function handleFilterChange(next: RechargeListFilters): void {
  filters.value = next
  page.value = 1
  void load()
}

function handlePageChange(): void {
  void load()
}

function goCreate(): void {
  void router.push('/recharges/new')
}
</script>

<style scoped>
.recharge-list__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.recharge-list__title {
  font-size: 16px;
  font-weight: 600;
}
</style>

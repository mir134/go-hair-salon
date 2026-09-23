<template>
  <div>
    <RechargeTable
      :items="items"
      :loading="loading"
    />
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

import { listRecharges } from '@/api'
import type { Recharge } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import RechargeTable from '@/views/recharge/components/RechargeTable.vue'

// 客户详情「充值记录」tab（06-BUSINESS-RULES.md:11）：
// GET /recharges?customer_id=（04-API.md:162），分页 + 时间倒序。
const props = defineProps<{ customerId: number }>()

const { items, total, page, pageSize, loading, load } = usePagedList<Recharge>(
  (currentPage, currentPageSize) =>
    listRecharges({
      customer_id: props.customerId,
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
</script>

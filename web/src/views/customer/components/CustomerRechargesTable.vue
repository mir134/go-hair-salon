<template>
  <div>
    <RechargeTable
      :items="items"
      :loading="loading"
      :is-admin="isAdmin"
      :refunding-id="refundingId"
      @refund="handleRefund"
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
import { computed, onMounted } from 'vue'

import { listRecharges } from '@/api'
import type { Recharge } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import { useRechargeRefund } from '@/composables/useRechargeRefund'
import { useAuthStore } from '@/stores/auth'
import RechargeTable from '@/views/recharge/components/RechargeTable.vue'

// 客户详情「充值记录」tab（06-BUSINESS-RULES.md:11）：
// GET /recharges?customer_id=（04-API.md:162），分页 + 时间倒序。
// 冲正入口仅 admin（plan todo 37）：成功后 emit 给客户详情，由父级重载客户余额
// 并重挂载各流水 tab（本组件随父级 :key 重挂载后重新拉取，服务端真值）。
const props = defineProps<{ customerId: number }>()

const emit = defineEmits<{
  /** 冲正成功：父级据此重载客户余额/积分并刷新各流水 tab */
  refunded: []
}>()

const auth = useAuthStore()
const isAdmin = computed(() => auth.role === 'admin')
const { refundingId, refund } = useRechargeRefund()

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

/** 冲正成功：通知父级刷新余额/积分/各流水；409 等失败：仅同步本 tab；用户取消不刷新 */
async function handleRefund(id: number, refundCents: number, customerName: string): Promise<void> {
  const outcome = await refund(id, refundCents, customerName)
  if (outcome === 'cancelled') {
    return
  }
  if (outcome === 'refunded') {
    emit('refunded')
    return
  }
  await load()
}
</script>

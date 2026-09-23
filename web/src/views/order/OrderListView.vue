<template>
  <section class="order-list">
    <el-card shadow="never">
      <div
        class="order-list__toolbar"
        :class="{ 'order-list__toolbar--mobile': isMobile }"
      >
        <el-radio-group
          v-model="statusFilter"
          @change="handleFilterChange"
        >
          <el-radio-button
            v-for="option in STATUS_FILTERS"
            :key="option.value"
            :value="option.value"
          >
            {{ option.label }}
            <span v-if="option.value === 'pending' && pendingCount > 0">（{{ pendingCount }}）</span>
          </el-radio-button>
        </el-radio-group>
        <el-button
          type="primary"
          :size="isMobile ? 'large' : 'default'"
          @click="goCreate"
        >
          开单
        </el-button>
      </div>

      <OrderTable
        :items="items"
        :loading="loading"
        :is-admin="isAdmin"
        @detail="goDetail"
        @checkout="openCheckout"
        @edit-items="openItems"
        @cancel="handleCancel"
      />

      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        @change="handlePageChange"
      />
    </el-card>

    <OrderCheckoutDialog
      v-model="checkoutVisible"
      :order-id="activeOrderId"
      @paid="handlePaid"
    />
    <OrderItemsDrawer
      v-model="itemsVisible"
      :order-id="activeOrderId"
      :is-admin="isAdmin"
      @updated="handleItemsUpdated"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { cancelOrder, listOrders } from '@/api'
import type { Order, OrderListQuery } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { usePagedList } from '@/composables/usePagedList'
import { useAuthStore } from '@/stores/auth'
import { formatCents } from '@/utils/format'

import OrderCheckoutDialog from './components/OrderCheckoutDialog.vue'
import OrderItemsDrawer from './components/OrderItemsDrawer.vue'
import OrderTable from './components/OrderTable.vue'

// 消费记录页（07-UI.md:40、57、plan todo 29）：
// 状态筛选（含「待结账」角标）+ 分页；待结账行提供 结账/编辑明细 入口；
// 取消仅 admin 且需确认（06-BUSINESS-RULES.md:37）。退款属 plan todo 37，本页不提供。
const STATUS_FILTERS = [
  { value: '', label: '全部' },
  { value: 'pending', label: '待结账' },
  { value: 'completed', label: '已完成' },
  { value: 'refunded', label: '已退款' },
  { value: 'cancelled', label: '已取消' },
] as const

type StatusFilter = (typeof STATUS_FILTERS)[number]['value']

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const isMobile = useIsMobile()
/** 取消仅 admin（06-BUSINESS-RULES.md §7）；隐藏按钮是 UI 简化，最终边界在后端 RBAC */
const isAdmin = computed(() => auth.role === 'admin')

/** 初始筛选：支持 /orders?status=pending（挂单成功后「去结账」入口） */
function initialFilter(): StatusFilter {
  const raw = route.query.status
  const value = Array.isArray(raw) ? raw[0] : raw
  const matched = STATUS_FILTERS.find((option) => option.value === value)
  return matched === undefined ? '' : matched.value
}

const statusFilter = ref<StatusFilter>(initialFilter())
/** 待结账订单数（角标）：结账/取消/编辑后随列表一起刷新 */
const pendingCount = ref(0)

const { items, total, page, pageSize, loading, load } = usePagedList<Order>(
  (currentPage, currentPageSize) => listOrders(buildQuery(currentPage, currentPageSize)),
)

const checkoutVisible = ref(false)
const itemsVisible = ref(false)
/** 当前操作订单 id：结账弹窗与明细抽屉共用（打开时按 id 拉取最新订单） */
const activeOrderId = ref<number | null>(null)

onMounted(() => {
  void refresh()
})

function buildQuery(currentPage: number, currentPageSize: number): OrderListQuery {
  const query: OrderListQuery = { page: currentPage, page_size: currentPageSize }
  if (statusFilter.value !== '') {
    query.status = statusFilter.value
  }
  return query
}

/** 列表与待结账角标一起刷新（结账/取消/编辑明细后必须立即同步） */
async function refresh(): Promise<void> {
  await Promise.all([load(), loadPendingCount()])
}

async function loadPendingCount(): Promise<void> {
  try {
    const data = await listOrders({ status: 'pending', page: 1, page_size: 1 })
    pendingCount.value = data.total
  } catch {
    // 拦截器已提示；角标降级为不显示
    pendingCount.value = 0
  }
}

/** 筛选变化：回到第一页重新加载 */
function handleFilterChange(): void {
  page.value = 1
  void load()
}

function handlePageChange(): void {
  void load()
}

function openCheckout(orderId: number): void {
  activeOrderId.value = orderId
  checkoutVisible.value = true
}

function openItems(orderId: number): void {
  activeOrderId.value = orderId
  itemsVisible.value = true
}

async function handlePaid(_order: Order, balanceAfter: number | null): Promise<void> {
  ElMessage.success(
    balanceAfter === null ? '结账成功' : `结账成功，客户最新余额 ¥${formatCents(balanceAfter)}`,
  )
  activeOrderId.value = null
  await refresh()
}

async function handleItemsUpdated(): Promise<void> {
  await refresh()
}

async function handleCancel(orderId: number, orderNo: string): Promise<void> {
  try {
    await ElMessageBox.confirm(
      `取消订单「${orderNo}」？取消后不可恢复，需重新开单。`,
      '取消订单',
      { type: 'warning', confirmButtonText: '确认取消', cancelButtonText: '返回' },
    )
  } catch {
    return // 用户取消
  }
  try {
    await cancelOrder(orderId)
    ElMessage.success('订单已取消')
    // 取消当前页最后一条时回退一页，避免停留在空白页
    if (items.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    await refresh()
  } catch {
    // 拦截器已提示（403/409/422 等）；刷新以同步服务端真实状态
    await refresh()
  }
}

function goDetail(id: number): void {
  void router.push(`/orders/${id}`)
}

/** 开单入口：快速消费页（07-UI.md:36、50-55） */
function goCreate(): void {
  void router.push('/orders/new')
}
</script>

<style scoped>
.order-list__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

/* 手机端（<768px）：筛选按钮换行 + 开单按钮占满整行（plan todo 55：待结账入口可用） */
.order-list__toolbar--mobile {
  flex-direction: column;
  align-items: stretch;
}

.order-list__toolbar--mobile .el-radio-group {
  flex-wrap: wrap;
  gap: 4px;
}

.order-list__toolbar--mobile .el-button {
  min-height: 44px;
}
</style>

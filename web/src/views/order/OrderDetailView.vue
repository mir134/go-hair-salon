<template>
  <section class="order-detail">
    <div class="order-detail__nav">
      <el-button
        link
        type="primary"
        @click="goBack"
      >
        ← 返回消费记录
      </el-button>
      <!-- 待结账订单操作区（07-UI.md:57-59）：结账 / 编辑明细 / 取消（取消仅 admin） -->
      <div
        v-if="order !== null && order.status === 'pending'"
        class="order-detail__actions"
      >
        <el-button
          type="primary"
          :disabled="items.length === 0"
          @click="checkoutVisible = true"
        >
          结账
        </el-button>
        <el-button @click="itemsVisible = true">
          编辑明细
        </el-button>
        <el-button
          v-if="isAdmin"
          type="danger"
          plain
          @click="handleCancel"
        >
          取消订单
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="balanceAfter !== null"
      class="order-detail__alert"
      type="success"
      :closable="false"
      show-icon
      :title="`结账成功，客户最新余额：¥${formatCents(balanceAfter)}`"
    />

    <el-card
      v-loading="loading"
      shadow="never"
    >
      <template v-if="order !== null">
        <OrderInfoCard
          :order="order"
          @view-customer="goCustomer"
        />

        <OrderItemsTable
          :items="items"
          class="order-detail__items"
        />

        <p
          v-if="order.status === 'pending' && items.length === 0"
          class="order-detail__hint"
        >
          订单已无明细：请先「编辑明细」追加服务项目，或「取消订单」。
        </p>
        <!-- 已完成/已退款/已取消订单：只读。退款入口属 plan todo 37（POST /orders/:id/refund），本 todo 不实现。 -->
      </template>
      <el-empty
        v-else-if="!loading"
        description="订单不存在"
      />
    </el-card>

    <OrderCheckoutDialog
      v-model="checkoutVisible"
      :order-id="orderId"
      @paid="handlePaid"
    />
    <OrderItemsDrawer
      v-model="itemsVisible"
      :order-id="orderId"
      :is-admin="isAdmin"
      @updated="handleItemsUpdated"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { cancelOrder, getOrder } from '@/api'
import type { Order } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { formatCents } from '@/utils/format'

import OrderCheckoutDialog from './components/OrderCheckoutDialog.vue'
import OrderInfoCard from './components/OrderInfoCard.vue'
import OrderItemsDrawer from './components/OrderItemsDrawer.vue'
import OrderItemsTable from './components/OrderItemsTable.vue'

// 订单详情页（07-UI.md:44-59、plan todo 29）：
// 明细 + 金额（原价/优惠/实付）+ 状态；待结账可结账/编辑明细，取消仅 admin；
// 已完成只读（退款属 todo 37）。结账成功后显示客户最新余额。
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
/** 取消仅 admin（06-BUSINESS-RULES.md §7）；隐藏按钮是 UI 简化，最终边界在后端 RBAC */
const isAdmin = computed(() => auth.role === 'admin')

const orderId = computed(() => Number(String(route.params.id)))
const order = ref<Order | null>(null)
const loading = ref(false)
/** 结账成功后的客户最新余额（服务端真值，07-UI.md:117） */
const balanceAfter = ref<number | null>(null)
const checkoutVisible = ref(false)
const itemsVisible = ref(false)

const items = computed(() => order.value?.items ?? [])

// 路由参数变化（从列表进入其它订单）时整体重载
watch(
  orderId,
  () => {
    balanceAfter.value = null
    void loadOrder()
  },
  { immediate: true },
)

async function loadOrder(): Promise<void> {
  if (!Number.isSafeInteger(orderId.value) || orderId.value <= 0) {
    order.value = null
    return
  }
  loading.value = true
  try {
    order.value = await getOrder(orderId.value)
  } catch {
    // 拦截器已提示（404 订单不存在等）
    order.value = null
  } finally {
    loading.value = false
  }
}

async function handlePaid(paid: Order, balance: number | null): Promise<void> {
  order.value = paid
  balanceAfter.value = balance
  ElMessage.success('结账成功')
  await loadOrder() // 以服务端为准刷新（状态/积分/明细快照）
}

async function handleItemsUpdated(): Promise<void> {
  await loadOrder()
}

async function handleCancel(): Promise<void> {
  const current = order.value
  if (current === null) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `取消订单「${current.order_no}」？取消后不可恢复，需重新开单。`,
      '取消订单',
      { type: 'warning', confirmButtonText: '确认取消', cancelButtonText: '返回' },
    )
  } catch {
    return // 用户取消
  }
  try {
    await cancelOrder(current.id)
    ElMessage.success('订单已取消')
    await loadOrder()
  } catch {
    // 拦截器已提示（403/409/422 等）；刷新以同步服务端真实状态
    await loadOrder()
  }
}

function goBack(): void {
  void router.push('/orders')
}

function goCustomer(customerId: number): void {
  void router.push(`/customers/${customerId}`)
}
</script>

<style scoped>
.order-detail__nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.order-detail__actions {
  display: flex;
  gap: 8px;
}

.order-detail__alert {
  margin-bottom: 12px;
}

.order-detail__items {
  margin-top: 16px;
  width: 100%;
}

.order-detail__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

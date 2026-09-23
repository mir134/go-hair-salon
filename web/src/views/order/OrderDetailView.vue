<template>
  <section class="order-detail">
    <div
      class="order-detail__nav"
      :class="{ 'order-detail__nav--mobile': isMobile }"
    >
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
          :size="actionSize"
          :disabled="items.length === 0"
          @click="checkoutVisible = true"
        >
          结账
        </el-button>
        <el-button
          :size="actionSize"
          @click="itemsVisible = true"
        >
          编辑明细
        </el-button>
        <el-button
          v-if="isAdmin"
          type="danger"
          plain
          :size="actionSize"
          @click="handleCancel"
        >
          取消订单
        </el-button>
      </div>
      <!-- 已完成订单操作区：全额退款仅 admin（04-API.md:129,139；plan todo 37） -->
      <div
        v-if="isAdmin && order !== null && order.status === 'completed'"
        class="order-detail__actions"
      >
        <el-button
          type="danger"
          plain
          :size="actionSize"
          :loading="refunding"
          @click="handleRefund"
        >
          退款
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="actionNotice !== null"
      class="order-detail__alert"
      type="success"
      :closable="false"
      show-icon
      :title="actionNotice"
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
        <!-- 已完成订单：仅 admin 可全额退款（入口在上方操作区）；已退款/已取消订单只读。 -->
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

import { cancelOrder, getCustomer, getOrder, refundOrder } from '@/api'
import type { Order } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { useAuthStore } from '@/stores/auth'
import { formatCents } from '@/utils/format'

import OrderCheckoutDialog from './components/OrderCheckoutDialog.vue'
import OrderInfoCard from './components/OrderInfoCard.vue'
import OrderItemsDrawer from './components/OrderItemsDrawer.vue'
import OrderItemsTable from './components/OrderItemsTable.vue'

// 订单详情页（07-UI.md:44-59、plan todo 29/37）：
// 明细 + 金额（原价/优惠/实付）+ 状态；待结账可结账/编辑明细，取消仅 admin；
// 已完成仅 admin 可全额退款（二次确认）；账务操作成功后显示客户最新余额/积分。
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
/** 取消/退款仅 admin（06-BUSINESS-RULES.md §7、04-API.md:129）；隐藏按钮是 UI 简化，最终边界在后端 RBAC */
const isAdmin = computed(() => auth.role === 'admin')
const isMobile = useIsMobile()
/** 手机端操作按钮加大到 large（触控目标 ≥44px，plan todo 53/55） */
const actionSize = computed(() => (isMobile.value ? 'large' : 'default'))

const orderId = computed(() => Number(String(route.params.id)))
const order = ref<Order | null>(null)
const loading = ref(false)
/** 结账/退款成功后的服务端真值提示（客户最新余额/积分，07-UI.md:117） */
const actionNotice = ref<string | null>(null)
/** 退款请求进行中：按钮 loading，防重复提交 */
const refunding = ref(false)
const checkoutVisible = ref(false)
const itemsVisible = ref(false)

const items = computed(() => order.value?.items ?? [])

// 路由参数变化（从列表进入其它订单）时整体重载
watch(
  orderId,
  () => {
    actionNotice.value = null
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
  // 余额为结账响应的服务端真值；现金等不涉及余额的支付方式不显示提示条
  actionNotice.value =
    balance === null ? null : `结账成功，客户最新余额：¥${formatCents(balance)}`
  ElMessage.success('结账成功')
  await loadOrder() // 以服务端为准刷新（状态/积分/明细快照）
}

/**
 * 全额退款（仅 admin，04-API.md:129,139,155；plan todo 35/37）：
 * 危险账务操作二次确认（07-UI.md:89），确认文案明确全额退款、不可撤销及对余额/积分的影响。
 * 成功后以服务端为准刷新订单（状态 → 已退款）并回读客户最新余额/积分。
 */
async function handleRefund(): Promise<void> {
  const current = order.value
  if (current === null) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认对订单「${current.order_no}」执行全额退款（¥${formatCents(current.paid_amount_cents)}）？` +
        '退款后订单状态变为「已退款」：余额支付将退回客户余额并扣回本次获得积分，' +
        '累计消费与营业额同步冲减；本操作不可撤销。',
      '退款二次确认',
      { type: 'warning', confirmButtonText: '确认退款', cancelButtonText: '返回' },
    )
  } catch {
    return // 用户取消
  }

  refunding.value = true
  try {
    await refundOrder(current.id)
    await loadOrder() // 服务端真值：状态 → 已退款
    await loadCustomerSnapshot(current.customer_id)
    ElMessage.success('退款成功')
  } catch {
    // 拦截器已提示后端文案（403/404/409 重复退款/422 积分不足等）；刷新同步服务端状态
    await loadOrder()
  } finally {
    refunding.value = false
  }
}

/** 退款后回读客户最新余额/积分（07-UI.md:117：账务操作成功后明确显示结果与最新余额） */
async function loadCustomerSnapshot(customerId: number): Promise<void> {
  try {
    const customer = await getCustomer(customerId)
    actionNotice.value =
      `退款成功，客户最新余额：¥${formatCents(customer.balance_cents)}，` +
      `积分：${customer.points}`
  } catch {
    // 客户已软删除等回读失败场景：不否定退款结果本身
    actionNotice.value = '退款成功'
  }
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

/* 手机端（<768px）：返回行 + 全宽大操作按钮（plan todo 55：待结账入口可用） */
.order-detail__nav--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.order-detail__nav--mobile .order-detail__actions {
  width: 100%;
}

.order-detail__nav--mobile .order-detail__actions .el-button {
  flex: 1;
  min-height: 44px;
  margin-left: 0;
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

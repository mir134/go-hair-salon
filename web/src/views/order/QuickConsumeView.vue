<template>
  <section class="quick-consume">
    <div class="quick-consume__nav">
      <el-button
        link
        type="primary"
        @click="goBack"
      >
        ← 返回
      </el-button>
      <span class="quick-consume__title">快速消费</span>
    </div>

    <!-- 成功结果：订单号 + 金额明细 + 客户最新余额（余额支付重点核对，07-UI.md:117） -->
    <OrderResultCard
      v-if="result !== null"
      :order="result"
      :customer-name="customerName"
      :balance-after="balanceAfter"
      @restart="startNew"
      @view-customer="goCustomer"
    />

    <template v-else>
      <OrderCustomerCard v-model:customer="customer" />
      <OrderItemsEditor
        v-model:items="items"
        :services="enabledServices"
        :is-admin="isAdmin"
        :loading="loadingServices"
      />
      <OrderEmployeeCard />
      <OrderPaymentCard
        v-model:payment-method="paymentMethod"
        :customer="customer"
        :paid-cents="totals.paidCents"
      />
      <OrderConfirmCard
        v-model:reason="reason"
        :items="items"
        :is-admin="isAdmin"
        :employee-label="EMPLOYEE_UNASSIGNED"
        :payment-label="paymentLabel"
        :error-message="errorMessage"
        :loading="submitting"
        @submit="handleSubmit"
      />
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { ApiError, createOrder, getCustomer, listServices } from '@/api'
import type { Customer, Order, OrderCreatePayload, OrderPaymentMethod, Service } from '@/api'
import { PAYMENT_METHOD_LABELS } from '@/constants'
import { useAttemptRequestId } from '@/composables/useAttemptRequestId'
import { useAuthStore } from '@/stores/auth'
import {
  buildOrderItemPayloads,
  itemUnitPriceCents,
  orderTotals,
  validateConsumeForm,
  type ConsumeItem,
} from '@/utils/orderForm'

import OrderConfirmCard from './components/OrderConfirmCard.vue'
import OrderCustomerCard from './components/OrderCustomerCard.vue'
import OrderEmployeeCard from './components/OrderEmployeeCard.vue'
import OrderItemsEditor from './components/OrderItemsEditor.vue'
import OrderPaymentCard from './components/OrderPaymentCard.vue'
import OrderResultCard from './components/OrderResultCard.vue'

// 快速消费页（07-UI.md:36、48-67，plan todo 23）：
// 客户 → 服务（多选+数量，自动带标准价）→ 员工（可选）→ 支付方式 → 金额预览 → 确认消费。
// 只做「直接完成」（status=completed）；挂单/结账/取消属于 plan todo 29，本页不提供。
const EMPLOYEE_UNASSIGNED = '未指定'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

/** 改价仅 admin（06-BUSINESS-RULES.md §3.1）；前端限制只是 UI 层，后端 RBAC 是最终边界 */
const isAdmin = computed(() => auth.role === 'admin')

const customer = ref<Customer | null>(null)
const items = ref<ConsumeItem[]>([])
const paymentMethod = ref<OrderPaymentMethod>('cash')
const reason = ref('')
const services = ref<Service[]>([])
const loadingServices = ref(false)

const submitting = ref(false)
const errorMessage = ref('')
const result = ref<Order | null>(null)
/** 消费后的客户最新余额（余额支付需明确显示，07-UI.md:117） */
const balanceAfter = ref<number | null>(null)

const enabledServices = computed(() => services.value.filter((service) => service.status === 1))
const totals = computed(() => orderTotals(items.value))
const paymentLabel = computed(() => PAYMENT_METHOD_LABELS[paymentMethod.value] ?? paymentMethod.value)
const customerName = computed(() => customer.value?.name ?? '')

/** 表单内容签名：变化即视为新的提交，作废旧幂等键（04-API.md:283-286） */
function formSignature(): string {
  return JSON.stringify({
    customer_id: customer.value?.id ?? null,
    payment_method: paymentMethod.value,
    reason: reason.value.trim(),
    items: items.value.map((item) => ({
      service_id: item.service.id,
      quantity: item.quantity,
      unit_price_cents: itemUnitPriceCents(item),
    })),
  })
}

const requestId = useAttemptRequestId(formSignature)

onMounted(() => {
  void loadServices()
  void applyPresetCustomer()
})

async function loadServices(): Promise<void> {
  loadingServices.value = true
  try {
    services.value = await listServices()
  } catch {
    // 拦截器已提示；服务选择降级为空，可刷新重试
  } finally {
    loadingServices.value = false
  }
}

/** 客户详情「快速消费」带 ?customer_id= 进入时预选客户 */
async function applyPresetCustomer(): Promise<void> {
  const raw = route.query.customer_id
  const id = Number(Array.isArray(raw) ? raw[0] : raw)
  if (!Number.isSafeInteger(id) || id <= 0) {
    return
  }
  try {
    customer.value = await getCustomer(id)
  } catch {
    // 拦截器已提示（客户不存在/已删除）；保持未选择状态，可手动搜索
  }
}

async function handleSubmit(): Promise<void> {
  const invalid = validateConsumeForm({
    customerSelected: customer.value !== null,
    items: items.value,
    isAdmin: isAdmin.value,
    reason: reason.value,
  })
  if (invalid !== '') {
    errorMessage.value = invalid
    return
  }
  const selectedCustomer = customer.value
  if (selectedCustomer === null) {
    return
  }

  const payload: OrderCreatePayload = {
    request_id: requestId.ensure(),
    customer_id: selectedCustomer.id,
    employee_id: null,
    payment_method: paymentMethod.value,
    status: 'completed',
    items: buildOrderItemPayloads(items.value),
  }
  if (reason.value.trim() !== '') {
    payload.discount_reason = reason.value.trim()
  }

  submitting.value = true
  errorMessage.value = ''
  try {
    const order = await createOrder(payload)
    result.value = order
    requestId.clear()
    ElMessage.success('消费已完成')
    await refreshBalanceAfter(selectedCustomer.id)
  } catch (error) {
    // 失败不丢表单、不重置幂等键：后端 422（如余额不足）文案就地展示，重试仍复用同一 request_id
    errorMessage.value = error instanceof ApiError ? error.message : '请求失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}

async function refreshBalanceAfter(customerId: number): Promise<void> {
  try {
    const fresh = await getCustomer(customerId)
    balanceAfter.value = fresh.balance_cents
  } catch {
    // 拦截器已提示；余额显示 "—"，订单结果仍然有效
    balanceAfter.value = null
  }
}

/** 再开一单：保留当前客户（同一客户常连续开单），清空明细与金额状态 */
function startNew(): void {
  result.value = null
  balanceAfter.value = null
  errorMessage.value = ''
  items.value = []
  reason.value = ''
  paymentMethod.value = 'cash'
  requestId.clear()
}

function goCustomer(): void {
  if (customer.value !== null) {
    void router.push(`/customers/${customer.value.id}`)
    return
  }
  void router.push('/customers')
}

function goBack(): void {
  if (window.history.length > 1) {
    router.back()
    return
  }
  void router.push('/customers')
}
</script>

<style scoped>
.quick-consume {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 880px;
}

.quick-consume__nav {
  display: flex;
  align-items: center;
  gap: 12px;
}

.quick-consume__title {
  font-size: 16px;
  font-weight: 600;
}
</style>

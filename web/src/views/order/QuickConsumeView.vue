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
    >
      <!-- 挂单结果：直达「待结账」入口结账（07-UI.md:40、57） -->
      <template #actions="{ pending }">
        <el-button
          v-if="pending"
          @click="goPendingOrders"
        >
          去结账
        </el-button>
      </template>
    </OrderResultCard>

    <template v-else>
      <OrderCustomerCard v-model:customer="customer" />
      <OrderItemsEditor
        v-model:items="items"
        :services="enabledServices"
        :is-admin="isAdmin"
        :loading="loadingServices"
      />
      <OrderEmployeeCard
        v-model="employeeId"
        :employees="employees"
        :loading="loadingEmployees"
        :load-error="employeesError"
        @retry="loadEmployees"
      />
      <!-- 挂单不收款：隐藏支付方式（07-UI.md:52-55、06-BUSINESS-RULES.md:30） -->
      <OrderPaymentCard
        v-if="mode === 'completed'"
        v-model:payment-method="paymentMethod"
        :customer="customer"
        :paid-cents="totals.paidCents"
      />
      <OrderConfirmCard
        v-model:reason="reason"
        v-model:mode="mode"
        :items="items"
        :is-admin="isAdmin"
        :employee-label="employeeLabel"
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

import type { Customer, Employee, OrderCreatePayload, OrderPaymentMethod, OrderSubmitStatus } from '@/api'
import { EMPLOYEE_STATUS_ENABLED, listEmployees } from '@/api'
import { PAYMENT_METHOD_LABELS } from '@/constants'
import { useAttemptRequestId } from '@/composables/useAttemptRequestId'
import { useConsumeCatalog } from '@/composables/useConsumeCatalog'
import { useConsumeSubmit } from '@/composables/useConsumeSubmit'
import { useAuthStore } from '@/stores/auth'
import {
  buildOrderItemPayloads,
  consumeFormSignature,
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

// 快速消费页（07-UI.md:36、48-67，plan todo 23/29）：
// 客户 → 服务（多选+数量，自动带标准价）→ 员工（可选）→ 支付方式 → 金额预览 → 确认。
// 两种完成方式：直接完成（status=completed）或挂单（status=pending，开单不收款）。
const EMPLOYEE_UNASSIGNED = '未指定'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

/** 改价仅 admin（06-BUSINESS-RULES.md §3.1）；前端限制只是 UI 层，后端 RBAC 是最终边界 */
const isAdmin = computed(() => auth.role === 'admin')

const customer = ref<Customer | null>(null)
const items = ref<ConsumeItem[]>([])
const paymentMethod = ref<OrderPaymentMethod>('cash')
/** 完成方式：直接完成 / 挂单（07-UI.md:52-55） */
const mode = ref<OrderSubmitStatus>('completed')
const reason = ref('')

/** 选中的员工 id；null = 不指定（员工可选，07-UI.md:50） */
const employeeId = ref<number | null>(null)
/** 员工选择池：仅启用中的员工（plan todo 40；停用员工不得被新订单选择） */
const employees = ref<Employee[]>([])
const loadingEmployees = ref(false)
const employeesError = ref(false)

const { services, loadingServices, loadServices, presetCustomer } = useConsumeCatalog()
const { submitting, errorMessage, result, balanceAfter, submit, reset } = useConsumeSubmit()

const enabledServices = computed(() => services.value.filter((service) => service.status === 1))
const totals = computed(() => orderTotals(items.value))
const paymentLabel = computed(() =>
  mode.value === 'pending'
    ? '—（挂单不收款）'
    : (PAYMENT_METHOD_LABELS[paymentMethod.value] ?? paymentMethod.value),
)
const customerName = computed(() => customer.value?.name ?? '')
const employeeLabel = computed(() => {
  const selected = employees.value.find((employee) => employee.id === employeeId.value)
  return selected === undefined ? EMPLOYEE_UNASSIGNED : selected.name
})

const requestId = useAttemptRequestId(() =>
  consumeFormSignature({
    customerId: customer.value?.id ?? null,
    employeeId: employeeId.value,
    status: mode.value,
    paymentMethod: paymentMethod.value,
    reason: reason.value,
    items: items.value,
  }),
)

onMounted(() => {
  void loadServices()
  void loadEmployees()
  void applyPresetCustomer()
})

/** 员工选择池：只加载启用中的员工；失败时可留空提交并重试 */
async function loadEmployees(): Promise<void> {
  loadingEmployees.value = true
  employeesError.value = false
  try {
    employees.value = await listEmployees(EMPLOYEE_STATUS_ENABLED)
  } catch {
    // 拦截器已提示；卡片内展示「加载失败 + 重试」，不阻塞消费提交
    employeesError.value = true
  } finally {
    loadingEmployees.value = false
  }
}

/** 客户详情「快速消费」带 ?customer_id= 进入时预选客户 */
async function applyPresetCustomer(): Promise<void> {
  const preset = await presetCustomer(route.query.customer_id)
  if (preset !== null) {
    customer.value = preset
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
    employee_id: employeeId.value,
    status: mode.value,
    items: buildOrderItemPayloads(items.value),
  }
  // 挂单不收款：不带支付方式（06-BUSINESS-RULES.md:30）
  if (mode.value === 'completed') {
    payload.payment_method = paymentMethod.value
  }
  if (reason.value.trim() !== '') {
    payload.discount_reason = reason.value.trim()
  }

  const ok = await submit(payload, mode.value)
  if (!ok) {
    return // 失败不丢表单、不重置幂等键，错误文案已就地展示
  }
  requestId.clear()
  ElMessage.success(mode.value === 'pending' ? '已挂单，订单待结账' : '消费已完成')
}

/** 再开一单：保留当前客户（同一客户常连续开单），清空明细与金额状态 */
function startNew(): void {
  reset()
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

/** 挂单成功后直达「待结账」筛选（07-UI.md:40） */
function goPendingOrders(): void {
  void router.push({ path: '/orders', query: { status: 'pending' } })
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

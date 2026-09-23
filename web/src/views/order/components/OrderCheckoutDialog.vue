<template>
  <el-dialog
    v-model="visible"
    title="结账"
    width="460px"
    :close-on-click-modal="false"
  >
    <div v-loading="loading">
      <template v-if="order !== null">
        <el-descriptions
          :column="1"
          border
        >
          <el-descriptions-item label="订单号">
            {{ order.order_no }}
          </el-descriptions-item>
          <el-descriptions-item label="客户">
            {{ order.customer_name !== undefined && order.customer_name !== '' ? order.customer_name : '—' }}
          </el-descriptions-item>
          <el-descriptions-item label="应收（元）">
            <span class="checkout__amount">¥{{ formatCents(order.paid_amount_cents) }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <el-radio-group
          v-model="paymentMethod"
          class="checkout__methods"
        >
          <el-radio
            v-for="method in PAYMENT_METHODS"
            :key="method"
            :value="method"
          >
            {{ PAYMENT_METHOD_LABELS[method] ?? method }}
          </el-radio>
        </el-radio-group>
        <p
          v-if="balanceHint !== ''"
          class="checkout__hint"
          :class="{ 'checkout__hint--warn': shortfallCents > 0 }"
        >
          {{ balanceHint }}
        </p>
        <!-- 状态已被并发变更（已结账/已取消）：禁止再次提交，关闭后可刷新列表 -->
        <el-alert
          v-if="notPending"
          class="checkout__error"
          type="warning"
          :closable="false"
          show-icon
          :title="`该订单已不是待结账状态（当前：${statusLabel}），无法结账。`"
        />
      </template>
      <el-empty
        v-else-if="!loading"
        description="订单不存在或已不可结账"
        :image-size="60"
      />

      <!-- 后端业务错误（422 余额不足 / 409 订单已结账等）就地展示，弹窗不关闭可改支付方式重试 -->
      <el-alert
        v-if="errorMessage !== ''"
        class="checkout__error"
        type="error"
        :closable="false"
        show-icon
        :title="errorMessage"
      />
    </div>

    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        :disabled="order === null || loading || notPending"
        @click="handleConfirm"
      >
        确认收款
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { ApiError, getCustomer, getOrder, payOrder } from '@/api'
import type { Customer, Order, OrderPaymentMethod } from '@/api'
import { ORDER_STATUS_LABELS, PAYMENT_METHOD_LABELS, PAYMENT_METHODS } from '@/constants'
import { formatCents } from '@/utils/format'

// 结账弹窗（04-API.md:150、07-UI.md:57）：选择支付方式 → 确认收款 → pending 转 completed。
// 打开时按 orderId 拉取订单与客户余额（列表行可能已过期，结账金额以服务端为准）。
// 余额是否足够由后端事务判定（06-BUSINESS-RULES.md:34-36）；这里只提示差额，不阻止提交。
const props = defineProps<{
  modelValue: boolean
  orderId: number | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 结账成功：返回服务端订单与客户最新余额（余额显示以服务端真值为准，不做本地加减） */
  paid: [order: Order, balanceAfter: number | null]
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const order = ref<Order | null>(null)
const customer = ref<Customer | null>(null)
const paymentMethod = ref<OrderPaymentMethod>('cash')
const loading = ref(false)
const submitting = ref(false)
const errorMessage = ref('')

/** 并发变更后的非待结账状态：禁止重复结账（06-BUSINESS-RULES.md:34-37） */
const notPending = computed(() => order.value !== null && order.value.status !== 'pending')
const statusLabel = computed(() => {
  const value = order.value?.status ?? ''
  return ORDER_STATUS_LABELS[value] ?? value
})

const shortfallCents = computed(() => {
  const current = order.value
  if (current === null || customer.value === null || paymentMethod.value !== 'balance') {
    return 0
  }
  return current.paid_amount_cents - customer.value.balance_cents
})

const balanceHint = computed(() => {
  const current = order.value
  const customerValue = customer.value
  if (current === null || customerValue === null || paymentMethod.value !== 'balance') {
    return ''
  }
  if (shortfallCents.value > 0) {
    return `余额不足（还差 ¥${formatCents(shortfallCents.value)}）：确认后将被后端拒绝（422），请先充值或改用其他支付方式。`
  }
  return `余额支付后余额：¥${formatCents(customerValue.balance_cents - current.paid_amount_cents)}（当前 ¥${formatCents(customerValue.balance_cents)}）。`
})

// 每次打开重置状态并拉取订单/客户最新余额（提示用，非事务依据）
watch(visible, (open) => {
  if (open) {
    void prepare()
  }
})

async function prepare(): Promise<void> {
  order.value = null
  customer.value = null
  paymentMethod.value = 'cash'
  errorMessage.value = ''
  const id = props.orderId
  if (id === null) {
    return
  }
  loading.value = true
  try {
    const detail = await getOrder(id)
    order.value = detail
    try {
      customer.value = await getCustomer(detail.customer_id)
    } catch {
      // 拦截器已提示；余额提示降级为不显示，不阻塞结账
    }
  } catch {
    // 拦截器已提示（404/网络）；保持空态
  } finally {
    loading.value = false
  }
}

async function handleConfirm(): Promise<void> {
  const current = order.value
  if (current === null) {
    return
  }
  submitting.value = true
  errorMessage.value = ''
  try {
    const paid = await payOrder(current.id, { payment_method: paymentMethod.value })
    const balanceAfter = await refreshBalance(current.customer_id)
    emit('paid', paid, balanceAfter)
    visible.value = false
  } catch (error) {
    // 409（已结账/已取消）等业务错误：就地展示后端文案，并重载订单同步真实状态
    errorMessage.value = error instanceof ApiError ? error.message : '结账失败，请稍后重试'
    await reloadOrder()
  } finally {
    submitting.value = false
  }
}

/** 失败后同步订单真实状态（可能已被其他会话结账/取消） */
async function reloadOrder(): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  try {
    order.value = await getOrder(id)
  } catch {
    // 拦截器已提示；保留原显示
  }
}

async function refreshBalance(customerId: number): Promise<number | null> {
  try {
    return (await getCustomer(customerId)).balance_cents
  } catch {
    // 拦截器已提示；余额显示降级为 null，结账结果仍然有效
    return null
  }
}
</script>

<style scoped>
.checkout__amount {
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.checkout__methods {
  margin-top: 16px;
}

.checkout__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.checkout__hint--warn {
  color: var(--el-color-warning);
}

.checkout__error {
  margin-top: 12px;
}
</style>

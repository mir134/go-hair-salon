<template>
  <el-card shadow="never">
    <template #header>
      <span>4. 支付方式</span>
    </template>
    <el-radio-group
      :model-value="paymentMethod"
      @change="handleChange"
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
      v-if="hint !== ''"
      class="payment__hint"
      :class="{ 'payment__hint--warn': shortfallCents > 0 }"
    >
      {{ hint }}
    </p>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Customer, OrderPaymentMethod } from '@/api'
import { PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents } from '@/utils/format'

// 消费第 4 步：支付方式（07-UI.md:24 现金/微信/支付宝/余额）+ 余额支付差额提示。
// 余额是否足够由后端事务判定（06-BUSINESS-RULES.md:27）；这里只提示，不阻止提交，
// 以便用户能看到后端 422 文案。
const PAYMENT_METHODS: readonly OrderPaymentMethod[] = ['cash', 'wechat', 'alipay', 'balance']

const props = defineProps<{
  customer: Customer | null
  paidCents: number
}>()

const paymentMethod = defineModel<OrderPaymentMethod>('paymentMethod', { required: true })

const shortfallCents = computed(() => {
  if (props.customer === null || paymentMethod.value !== 'balance') {
    return 0
  }
  return props.paidCents - props.customer.balance_cents
})

const hint = computed(() => {
  const customer = props.customer
  if (customer === null || paymentMethod.value !== 'balance') {
    return ''
  }
  if (shortfallCents.value > 0) {
    return `余额不足（还差 ¥${formatCents(shortfallCents.value)}）：提交后将被后端拒绝（422），请先充值或改用其他支付方式。`
  }
  return `余额支付后余额：¥${formatCents(customer.balance_cents - props.paidCents)}（当前 ¥${formatCents(customer.balance_cents)}）。`
})

/** el-radio-group 的 change 值为宽类型；只接受四种支付方式，其余忽略 */
function handleChange(value: unknown): void {
  const matched = PAYMENT_METHODS.find((method) => method === value)
  if (matched !== undefined) {
    paymentMethod.value = matched
  }
}
</script>

<style scoped>
.payment__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.payment__hint--warn {
  color: var(--el-color-warning);
}
</style>

<template>
  <section class="recharge-create">
    <div class="recharge-create__nav">
      <el-button
        link
        type="primary"
        @click="goBack"
      >
        ← 返回充值记录
      </el-button>
      <span class="recharge-create__title">新增充值</span>
    </div>

    <!-- 成功结果：实付/赠送/增加余额 + 客户最新余额（服务端回读真值） -->
    <RechargeResultCard
      v-if="result !== null"
      :recharge="result"
      :customer-name="customerName"
      :balance-after="balanceAfter"
      @restart="startNew"
      @view-customer="goCustomer"
    />

    <template v-else>
      <OrderCustomerCard v-model:customer="customer" />
      <RechargeFormCard v-model:form="form" />
      <RechargeConfirmCard
        :form="form"
        :customer-label="customerLabel"
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

import { getCustomer } from '@/api'
import type { Customer, RechargeCreatePayload } from '@/api'
import { useAttemptRequestId } from '@/composables/useAttemptRequestId'
import { useRechargeSubmit } from '@/composables/useRechargeSubmit'
import { formatCents } from '@/utils/format'
import {
  emptyRechargeForm,
  parseGiftCents,
  parseRechargeCents,
  rechargeFormSignature,
  validateRechargeForm,
  type RechargeFormModel,
} from '@/utils/rechargeForm'
import OrderCustomerCard from '@/views/order/components/OrderCustomerCard.vue'

import RechargeConfirmCard from './components/RechargeConfirmCard.vue'
import RechargeFormCard from './components/RechargeFormCard.vue'
import RechargeResultCard from './components/RechargeResultCard.vue'

// 新增充值页（07-UI.md:68-74、plan todo 33）：
// 客户 → 实付/赠送/支付方式/备注 → 确认前显示「实付 + 赠送 = 增加余额」→ 提交。
// request_id 由 useAttemptRequestId 管理：失败重试复用同一值，后端据此防止重复入账。
const route = useRoute()
const router = useRouter()

const customer = ref<Customer | null>(null)
const form = ref<RechargeFormModel>(emptyRechargeForm())
const { submitting, errorMessage, result, balanceAfter, submit, reset } = useRechargeSubmit()

const customerName = computed(() => customer.value?.name ?? '')
const customerLabel = computed(() => {
  const selected = customer.value
  if (selected === null) {
    return '未选择'
  }
  return `${selected.name}（当前余额 ¥${formatCents(selected.balance_cents)}）`
})

const requestId = useAttemptRequestId(() =>
  rechargeFormSignature({
    customerId: customer.value?.id ?? null,
    rechargeText: form.value.rechargeText,
    giftText: form.value.giftText,
    paymentMethod: form.value.paymentMethod,
    remark: form.value.remark,
  }),
)

onMounted(() => {
  void applyPresetCustomer()
})

/** 客户详情「充值」带 ?customer_id= 进入时预选客户（余额以服务端最新值为准） */
async function applyPresetCustomer(): Promise<void> {
  const id = Number(String(route.query.customer_id ?? ''))
  if (!Number.isSafeInteger(id) || id <= 0) {
    return
  }
  try {
    customer.value = await getCustomer(id)
  } catch {
    // 拦截器已提示；保持未选择状态，可手动搜索客户
  }
}

async function handleSubmit(): Promise<void> {
  const invalid = validateRechargeForm({
    customerSelected: customer.value !== null,
    rechargeText: form.value.rechargeText,
    giftText: form.value.giftText,
  })
  if (invalid !== '') {
    errorMessage.value = invalid
    return
  }
  const selected = customer.value
  const rechargeCents = parseRechargeCents(form.value.rechargeText)
  const giftCents = parseGiftCents(form.value.giftText)
  if (selected === null || rechargeCents === null || giftCents === null) {
    return
  }

  const remark = form.value.remark.trim()
  const payload: RechargeCreatePayload = {
    request_id: requestId.ensure(),
    customer_id: selected.id,
    recharge_amount_cents: rechargeCents,
    payment_method: form.value.paymentMethod,
  }
  if (giftCents > 0) {
    payload.gift_amount_cents = giftCents
  }
  if (remark !== '') {
    payload.remark = remark
  }

  const ok = await submit(payload)
  if (!ok) {
    return // 失败不丢表单、不重置幂等键，错误文案已就地展示
  }
  requestId.clear()
  ElMessage.success('充值成功')
}

/** 再充一笔：清空金额与结果，回读客户最新余额供下一次核对 */
function startNew(): void {
  reset()
  form.value = emptyRechargeForm()
  requestId.clear()
  void refreshCustomer()
}

async function refreshCustomer(): Promise<void> {
  const current = customer.value
  if (current === null) {
    return
  }
  try {
    customer.value = await getCustomer(current.id)
  } catch {
    // 拦截器已提示；保留原显示
  }
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
  void router.push('/recharges')
}
</script>

<style scoped>
.recharge-create {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 880px;
}

.recharge-create__nav {
  display: flex;
  align-items: center;
  gap: 12px;
}

.recharge-create__title {
  font-size: 16px;
  font-weight: 600;
}

/* 手机端（<768px）：卡片紧凑排布（plan todo 55：≤5 步完成充值） */
@media (max-width: 767px) {
  .recharge-create {
    gap: 12px;
    max-width: none;
  }
}
</style>

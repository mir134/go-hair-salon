<template>
  <el-card shadow="never">
    <template #header>
      <span>3. 确认充值</span>
    </template>
    <el-descriptions
      :column="1"
      border
    >
      <el-descriptions-item label="客户">
        {{ customerLabel }}
      </el-descriptions-item>
      <el-descriptions-item label="实付金额（元）">
        {{ amountText(rechargeCents) }}
        <span class="recharge-confirm__tag">客户实际支付</span>
      </el-descriptions-item>
      <el-descriptions-item label="赠送金额（元）">
        {{ amountText(giftCents) }}
      </el-descriptions-item>
      <el-descriptions-item label="增加余额（元）">
        <span class="recharge-confirm__total">{{ amountText(increaseCents) }}</span>
      </el-descriptions-item>
      <el-descriptions-item label="支付方式">
        {{ paymentLabel }}
      </el-descriptions-item>
      <el-descriptions-item label="备注">
        {{ form.remark.trim() !== '' ? form.remark : '—' }}
      </el-descriptions-item>
    </el-descriptions>

    <p class="recharge-confirm__formula">
      实付 + 赠送 = 增加余额：{{ amountText(rechargeCents) }} + {{ amountText(giftCents) }} =
      {{ amountText(increaseCents) }}（06-BUSINESS-RULES.md:50）；赠送金额不是客户实际支付的金额。
    </p>

    <!-- 后端业务错误（400 金额非法 / 404 客户不存在等）就地展示：不关闭、不清空表单，便于修正后重试 -->
    <el-alert
      v-if="errorMessage !== ''"
      class="recharge-confirm__error"
      type="error"
      :closable="false"
      show-icon
      :title="errorMessage"
    />

    <el-button
      class="recharge-confirm__submit"
      type="primary"
      size="large"
      :loading="loading"
      @click="emit('submit')"
    >
      确认充值
    </el-button>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import { PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents } from '@/utils/format'
import {
  parseGiftCents,
  parseRechargeCents,
  rechargeBalanceIncrease,
  type RechargeFormModel,
} from '@/utils/rechargeForm'

// 充值第 3 步「确认前预览」（07-UI.md:72-74）：
// 必须显示「实付 + 赠送 = 增加余额」，且不得把赠送金额显示为客户实际支付金额。
const props = defineProps<{
  form: RechargeFormModel
  customerLabel: string
  errorMessage: string
  loading: boolean
}>()

const emit = defineEmits<{
  submit: []
}>()

const rechargeCents = computed(() => parseRechargeCents(props.form.rechargeText))
const giftCents = computed(() => parseGiftCents(props.form.giftText))
const increaseCents = computed(() => {
  const recharge = rechargeCents.value
  const gift = giftCents.value
  if (recharge === null || gift === null) {
    return null
  }
  return rechargeBalanceIncrease(recharge, gift)
})

const paymentLabel = computed(
  () => PAYMENT_METHOD_LABELS[props.form.paymentMethod] ?? props.form.paymentMethod,
)

/** 预览金额：非法输入显示占位符，绝不把未解析的文本当金额展示 */
function amountText(cents: number | null): string {
  return cents === null ? '—' : `¥${formatCents(cents)}`
}
</script>

<style scoped>
.recharge-confirm__tag {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.recharge-confirm__total {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.recharge-confirm__formula {
  margin: 12px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.recharge-confirm__error {
  margin-top: 12px;
}

.recharge-confirm__submit {
  display: block;
  width: 100%;
  max-width: 320px;
  margin-top: 16px;
}
</style>

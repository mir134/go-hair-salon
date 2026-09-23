<template>
  <el-card shadow="never">
    <template #header>
      <span>5. 确认消费</span>
    </template>
    <!-- 完成方式（07-UI.md:52-55）：直接完成（确认收款）或挂单（开单不收款） -->
    <el-radio-group
      class="confirm-card__mode"
      :model-value="mode"
      @change="handleModeChange"
    >
      <el-radio value="completed">
        直接完成
      </el-radio>
      <el-radio value="pending">
        挂单（开单不收款）
      </el-radio>
    </el-radio-group>
    <p class="confirm-card__hint">
      {{ modeHint }}
    </p>
    <OrderAmountPreview
      v-model:reason="reason"
      :items="items"
      :is-admin="isAdmin"
      :employee-label="employeeLabel"
      :payment-label="paymentLabel"
    />
    <!-- 后端业务错误（如 422 余额不足、409 订单已结账）就地展示：不关闭、不清空表单，便于修正后重试 -->
    <el-alert
      v-if="errorMessage !== ''"
      class="confirm-card__error"
      type="error"
      :closable="false"
      show-icon
      :title="errorMessage"
    />
    <el-button
      class="confirm-card__submit"
      type="primary"
      size="large"
      :loading="loading"
      @click="emit('submit')"
    >
      {{ submitLabel }}
    </el-button>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { OrderSubmitStatus } from '@/api'
import type { ConsumeItem } from '@/utils/orderForm'

import OrderAmountPreview from './OrderAmountPreview.vue'

// 消费第 5 步：确认前预览（07-UI.md:61）+ 完成方式 + 错误就地提示 + 提交（校验与请求由父级执行）。
const props = defineProps<{
  items: ConsumeItem[]
  isAdmin: boolean
  employeeLabel: string
  paymentLabel: string
  errorMessage: string
  loading: boolean
  mode: OrderSubmitStatus
}>()

const emit = defineEmits<{
  submit: []
  'update:mode': [value: OrderSubmitStatus]
}>()

const reason = defineModel<string>('reason', { required: true })

const submitLabel = computed(() => (props.mode === 'pending' ? '挂单（开单不收款）' : '确认消费'))
const modeHint = computed(() =>
  props.mode === 'pending'
    ? '挂单后订单为「待结账」，不产生任何资金/余额/积分变动；可在消费记录页结账或编辑明细（06-BUSINESS-RULES.md:30-33）。'
    : '确认收款后订单立即完成，余额扣减、余额流水与积分流水在同一事务内入账（06-BUSINESS-RULES.md:35）。',
)

/** el-radio-group 的 change 值为宽类型；只接受两种提交模式，其余忽略 */
function handleModeChange(value: unknown): void {
  if (value === 'completed' || value === 'pending') {
    emit('update:mode', value)
  }
}
</script>

<style scoped>
.confirm-card__mode {
  margin-bottom: 4px;
}

.confirm-card__hint {
  margin: 0 0 12px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.confirm-card__error {
  margin-top: 12px;
}

.confirm-card__submit {
  display: block;
  width: 100%;
  max-width: 320px;
  margin-top: 16px;
}
</style>

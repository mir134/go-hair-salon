<template>
  <el-card shadow="never">
    <template #header>
      <span>5. 确认消费</span>
    </template>
    <OrderAmountPreview
      v-model:reason="reason"
      :items="items"
      :is-admin="isAdmin"
      :employee-label="employeeLabel"
      :payment-label="paymentLabel"
    />
    <!-- 后端业务错误（如 422 余额不足）就地展示：不关闭、不清空表单，便于修正后重试 -->
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
      确认消费
    </el-button>
  </el-card>
</template>

<script setup lang="ts">
import type { ConsumeItem } from '@/utils/orderForm'

import OrderAmountPreview from './OrderAmountPreview.vue'

// 消费第 5 步：确认前预览（07-UI.md:61）+ 错误就地提示 + 提交（校验与请求由父级执行）。
defineProps<{
  items: ConsumeItem[]
  isAdmin: boolean
  employeeLabel: string
  paymentLabel: string
  errorMessage: string
  loading: boolean
}>()

const emit = defineEmits<{
  submit: []
}>()

const reason = defineModel<string>('reason', { required: true })
</script>

<style scoped>
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

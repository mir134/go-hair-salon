<template>
  <el-card shadow="never">
    <template #header>
      <span>2. 充值信息</span>
    </template>
    <el-form
      :label-width="isMobile ? 'auto' : '120px'"
      :label-position="isMobile ? 'top' : 'right'"
      @submit.prevent
    >
      <el-form-item
        label="实付金额（元）"
        required
      >
        <el-input
          :model-value="form.rechargeText"
          inputmode="decimal"
          placeholder="客户实际支付金额，如 100"
          @input="patch({ rechargeText: $event })"
        />
        <p class="recharge-form__hint">
          实付金额作为充值本金；余额增加 = 实付 + 赠送（07-UI.md:70-74）。
        </p>
      </el-form-item>
      <el-form-item label="赠送金额（元）">
        <el-input
          :model-value="form.giftText"
          inputmode="decimal"
          placeholder="不赠送留空，如 20"
          @input="patch({ giftText: $event })"
        />
      </el-form-item>
      <el-form-item
        label="支付方式"
        required
      >
        <el-radio-group
          :model-value="form.paymentMethod"
          @change="handlePaymentChange"
        >
          <el-radio
            v-for="method in RECHARGE_PAYMENT_METHODS"
            :key="method"
            :value="method"
          >
            {{ PAYMENT_METHOD_LABELS[method] ?? method }}
          </el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="备注">
        <el-input
          :model-value="form.remark"
          maxlength="200"
          show-word-limit
          placeholder="选填"
          @input="patch({ remark: $event })"
        />
      </el-form-item>
    </el-form>
  </el-card>
</template>

<script setup lang="ts">
import { useIsMobile } from '@/composables/useIsMobile'
import { PAYMENT_METHOD_LABELS, RECHARGE_PAYMENT_METHODS } from '@/constants'
import type { RechargeFormModel } from '@/utils/rechargeForm'

// 充值第 2 步（07-UI.md:68-70）：实付金额、赠送金额、支付方式、备注。
// 表单模型整体经 defineModel 上抛，父级负责解析、校验、预览与提交。
// 手机端标签置顶（label-position=top），避免 375px 下输入框被挤压（plan todo 55）。
const form = defineModel<RechargeFormModel>('form', { required: true })

const isMobile = useIsMobile()

function patch(partial: Partial<RechargeFormModel>): void {
  form.value = { ...form.value, ...partial }
}

/** el-radio-group 的 change 值为宽类型；只接受充值可用的三种支付方式（余额不可用于充值） */
function handlePaymentChange(value: unknown): void {
  if (value === 'cash' || value === 'wechat' || value === 'alipay') {
    patch({ paymentMethod: value })
  }
}
</script>

<style scoped>
.recharge-form__hint {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
}
</style>

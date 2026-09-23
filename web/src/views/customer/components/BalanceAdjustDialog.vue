<template>
  <el-dialog
    v-model="visible"
    title="余额调整"
    width="480px"
    :close-on-click-modal="false"
  >
    <el-alert
      class="adjust__notice"
      type="warning"
      :closable="false"
      show-icon
      title="仅管理员可操作：调整金额可正可负，必须填写原因，并写入余额流水（type=adjustment）与操作日志（04-API.md:176-192）。"
    />

    <el-form
      label-width="120px"
      @submit.prevent
    >
      <el-form-item label="客户">
        <span>{{ customerName }}（当前余额 ¥{{ formatCents(balanceCents) }}）</span>
      </el-form-item>
      <el-form-item
        label="调整金额（元）"
        required
      >
        <el-input
          v-model="amountText"
          inputmode="decimal"
          placeholder="正数增加、负数扣减，如 50 或 -10"
        />
      </el-form-item>
      <el-form-item
        label="调整原因"
        required
      >
        <el-input
          v-model="reason"
          type="textarea"
          :rows="2"
          maxlength="200"
          show-word-limit
          placeholder="必填，写入操作日志（06-BUSINESS-RULES.md:62）"
        />
      </el-form-item>
    </el-form>

    <el-descriptions
      :column="1"
      border
    >
      <el-descriptions-item label="调整后余额（元）">
        <span :class="{ 'adjust__after--negative': afterCents !== null && afterCents < 0 }">
          {{ afterCents === null ? '—' : formatCents(afterCents) }}
        </span>
      </el-descriptions-item>
    </el-descriptions>
    <p class="adjust__hint">
      调整不产生订单、不计入营业额（06-BUSINESS-RULES.md:63）；若调整后余额为负，后端将拒绝（422）。
    </p>

    <!-- 后端业务错误（422 余额将变为负数 / 400 缺原因 / 403 无权限）就地展示，弹窗保持可修正重试 -->
    <el-alert
      v-if="errorMessage !== ''"
      class="adjust__error"
      type="error"
      :closable="false"
      show-icon
      :title="errorMessage"
    />

    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        @click="handleSubmit"
      >
        确认调整
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { ApiError, createBalanceAdjustment } from '@/api'
import { formatCents } from '@/utils/format'
import { parseSignedCents } from '@/utils/rechargeForm'

// 余额调整弹窗（04-API.md:176-192、06-BUSINESS-RULES.md:62-65）：
// 仅 admin（入口按钮由父级按角色渲染，后端 RBAC 才是最终边界）；
// 金额可正负、原因必填、危险操作二次确认；422（将致负余额）展示后端文案。
const props = defineProps<{
  modelValue: boolean
  customerId: number
  customerName: string
  balanceCents: number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 调整成功：父级据此重载客户与余额流水（余额以服务端为准） */
  adjusted: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const amountText = ref('')
const reason = ref('')
const submitting = ref(false)
const errorMessage = ref('')

const amountCents = computed(() => parseSignedCents(amountText.value))
const afterCents = computed(() => {
  const amount = amountCents.value
  return amount === null ? null : props.balanceCents + amount
})

// 每次打开重置输入与错误（金额/原因不跨次复用）
watch(visible, (open) => {
  if (open) {
    amountText.value = ''
    reason.value = ''
    errorMessage.value = ''
  }
})

async function handleSubmit(): Promise<void> {
  const amount = amountCents.value
  if (amount === null) {
    errorMessage.value = '调整金额格式不正确（可正可负，最多两位小数）'
    return
  }
  if (amount === 0) {
    errorMessage.value = '调整金额不能为 0'
    return
  }
  const trimmed = reason.value.trim()
  if (trimmed === '') {
    errorMessage.value = '必须填写调整原因（06-BUSINESS-RULES.md:62）'
    return
  }

  // 危险操作二次确认（07-UI.md:89）：金额、原因、调整后余额一次性核对
  const sign = amount > 0 ? '+' : '-'
  const after = props.balanceCents + amount
  try {
    await ElMessageBox.confirm(
      `确认将「${props.customerName}」余额调整 ${sign}¥${formatCents(Math.abs(amount))}？原因：${trimmed}；调整后余额：¥${formatCents(after)}。`,
      '余额调整二次确认',
      { type: 'warning', confirmButtonText: '确认调整', cancelButtonText: '返回' },
    )
  } catch {
    return // 用户取消
  }

  submitting.value = true
  errorMessage.value = ''
  try {
    const result = await createBalanceAdjustment(props.customerId, {
      amount_cents: amount,
      reason: trimmed,
    })
    emit('adjusted')
    // balance_after_cents 是本次调整后的服务端余额真值（不做本地加减）
    ElMessage.success(`余额调整成功，最新余额 ¥${formatCents(result.balance_after_cents)}`)
    visible.value = false
  } catch (error) {
    // 422（调整后余额为负）等业务错误：展示后端文案，保留输入便于修正重试
    errorMessage.value = error instanceof ApiError ? error.message : '调整失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.adjust__notice {
  margin-bottom: 16px;
}

.adjust__after--negative {
  color: var(--el-color-danger);
  font-weight: 600;
}

.adjust__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.adjust__error {
  margin-top: 12px;
}
</style>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="480px"
    :close-on-click-modal="false"
    @closed="handleClosed"
  >
    <el-alert
      v-if="submitError !== ''"
      :title="submitError"
      type="error"
      :closable="false"
      show-icon
      class="user-password__alert"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="96px"
      @submit.prevent
    >
      <el-form-item
        label="新密码"
        prop="password"
      >
        <el-input
          v-model="form.password"
          type="password"
          show-password
          maxlength="72"
          placeholder="不超过 72 字节"
        />
      </el-form-item>
      <el-form-item
        label="确认新密码"
        prop="confirmPassword"
      >
        <el-input
          v-model="form.confirmPassword"
          type="password"
          show-password
          maxlength="72"
          placeholder="再次输入新密码"
        />
      </el-form-item>
    </el-form>
    <p class="user-password__hint">
      重置后请通知用户尽快自行修改密码（操作会写入操作日志，不含密码明文）。
    </p>
    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        @click="handleSubmit"
      >
        重置密码
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'

import { ApiError, updateUser } from '@/api'
import type { User } from '@/api'

// 重置密码弹窗（plan todo 40）：弹窗 + 提交前二次确认（危险操作，07-UI.md:89）。
// PUT /users/:id 只支持 status/password；重置成功后用户用新密码登录。
const props = defineProps<{
  modelValue: boolean
  user: User | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

interface PasswordForm {
  password: string
  confirmPassword: string
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() =>
  props.user === null ? '重置密码' : `重置「${props.user.username}」的密码`,
)

const formRef = ref<FormInstance>()
const form = ref<PasswordForm>({ password: '', confirmPassword: '' })
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<PasswordForm> = {
  password: [{ required: true, message: '请输入新密码', trigger: 'blur' }],
  confirmPassword: [
    {
      validator: (_rule, value, callback) => {
        if (value !== form.value.password) {
          callback(new Error('两次输入的密码不一致'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
}

watch(visible, (open) => {
  if (open) {
    submitError.value = ''
    form.value = { password: '', confirmPassword: '' }
  }
})

function handleClosed(): void {
  formRef.value?.clearValidate()
  submitError.value = ''
}

async function handleSubmit(): Promise<void> {
  submitError.value = ''
  const user = props.user
  if (user === null || formRef.value === undefined) {
    return
  }
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认重置用户「${user.username}」的密码？重置后旧密码立即失效，请通知用户使用新密码登录。`,
      '重置密码',
      { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  submitting.value = true
  try {
    await updateUser(user.id, { password: form.value.password })
    ElMessage.success('密码已重置')
    emit('saved')
    visible.value = false
  } catch (error) {
    // 400（密码空/超长）/404 业务错误：就地展示后端文案
    submitError.value = error instanceof ApiError ? error.message : '重置失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.user-password__alert {
  margin-bottom: 16px;
}

.user-password__hint {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

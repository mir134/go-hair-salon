<template>
  <el-dialog
    v-model="visible"
    title="新增用户"
    width="520px"
    :close-on-click-modal="false"
    @closed="handleClosed"
  >
    <el-alert
      v-if="submitError !== ''"
      :title="submitError"
      type="error"
      :closable="false"
      show-icon
      class="user-create__alert"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="88px"
      @submit.prevent
    >
      <el-form-item
        label="用户名"
        prop="username"
      >
        <el-input
          v-model="form.username"
          maxlength="64"
          placeholder="登录名"
        />
      </el-form-item>
      <el-form-item
        label="角色"
        prop="role"
      >
        <el-radio-group v-model="form.role">
          <el-radio value="staff">
            店员
          </el-radio>
          <el-radio value="admin">
            管理员
          </el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item
        label="密码"
        prop="password"
      >
        <el-input
          v-model="form.password"
          type="password"
          show-password
          maxlength="72"
          placeholder="初始密码（不超过 72 字节）"
        />
      </el-form-item>
      <el-form-item
        label="关联员工"
        prop="employeeId"
      >
        <el-select
          v-model="form.employeeId"
          class="user-create__select"
          placeholder="选填；仅可选择启用中的员工"
          clearable
          filterable
        >
          <el-option
            v-for="employee in enabledEmployees"
            :key="employee.id"
            :label="employeeLabel(employee)"
            :value="employee.id"
          />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        @click="handleSubmit"
      >
        创建
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'

import { ApiError, EMPLOYEE_STATUS_ENABLED, createUser } from '@/api'
import type { Employee, UserCreatePayload, UserRole } from '@/api'

// 新增用户弹窗（plan todo 40，MVP 流程 A：新增员工 → 创建用户 → 关联）：
// 员工关联为可选，只提供启用中的员工（停用员工不得被关联，与订单选择一致）。
const props = defineProps<{
  modelValue: boolean
  employees: Employee[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

interface UserCreateForm {
  username: string
  role: UserRole
  password: string
  employeeId: number | null
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const formRef = ref<FormInstance>()
const form = ref<UserCreateForm>(emptyForm())
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<UserCreateForm> = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入初始密码', trigger: 'blur' }],
}

const enabledEmployees = computed(() =>
  props.employees.filter((employee) => employee.status === EMPLOYEE_STATUS_ENABLED),
)

watch(visible, (open) => {
  if (open) {
    submitError.value = ''
    form.value = emptyForm()
  }
})

function emptyForm(): UserCreateForm {
  return { username: '', role: 'staff', password: '', employeeId: null }
}

/** 下拉展示：姓名（职位），职位为空时只显示姓名 */
function employeeLabel(employee: Employee): string {
  return employee.position === '' ? employee.name : `${employee.name}（${employee.position}）`
}

function handleClosed(): void {
  formRef.value?.clearValidate()
  submitError.value = ''
}

async function handleSubmit(): Promise<void> {
  submitError.value = ''
  if (formRef.value === undefined) {
    return
  }
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) {
    return
  }
  const payload: UserCreatePayload = {
    username: form.value.username.trim(),
    role: form.value.role,
    password: form.value.password,
    employee_id: form.value.employeeId,
  }
  submitting.value = true
  try {
    await createUser(payload)
    ElMessage.success('用户已创建')
    emit('saved')
    visible.value = false
  } catch (error) {
    // 409（用户名已存在）/400（角色不合法等）业务错误：就地展示后端文案
    submitError.value = error instanceof ApiError ? error.message : '创建失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.user-create__alert {
  margin-bottom: 16px;
}

.user-create__select {
  width: 100%;
}
</style>

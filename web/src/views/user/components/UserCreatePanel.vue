<template>
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
    :label-width="isMobile ? 'auto' : '88px'"
    :label-position="isMobile ? 'top' : 'right'"
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
  <div
    class="user-create__actions"
    :class="{ 'user-create__actions--mobile': isMobile }"
  >
    <el-button
      :disabled="submitting"
      @click="emit('close')"
    >
      取消
    </el-button>
    <el-button
      type="primary"
      :loading="submitting"
      @click="handleSubmit"
    >
      创建
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'

import { ApiError, EMPLOYEE_STATUS_ENABLED, createUser } from '@/api'
import type { Employee, UserCreatePayload, UserRole } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

// 新增用户表单主体（plan todo 40/54，MVP 流程 A：新增员工 → 创建用户 → 关联）：
// 弹窗（PC）与底部抽屉（手机）两种外壳共用本组件，表单与提交逻辑只实现一份。
// 员工关联为可选，只提供启用中的员工（停用员工不得被关联，与订单选择一致）。
const props = defineProps<{
  employees: Employee[]
}>()

const emit = defineEmits<{
  /** 用户创建成功：父组件据此刷新列表并关闭外壳 */
  saved: []
  /** 用户取消 / 创建成功后的关闭请求 */
  close: []
}>()

interface UserCreateForm {
  username: string
  role: UserRole
  password: string
  employeeId: number | null
}

const isMobile = useIsMobile()
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

onMounted(() => {
  // 外壳以 v-if 控制挂载，每次打开都是全新实例：清空错误与表单即可
  submitError.value = ''
  form.value = emptyForm()
})

function emptyForm(): UserCreateForm {
  return { username: '', role: 'staff', password: '', employeeId: null }
}

/** 下拉展示：姓名（职位），职位为空时只显示姓名 */
function employeeLabel(employee: Employee): string {
  return employee.position === '' ? employee.name : `${employee.name}（${employee.position}）`
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
    emit('close')
  } catch (error) {
    // 409（用户名已存在）/400（角色不合法等）业务错误：就地展示后端文案（07-UI.md:88）
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

.user-create__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 手机端（表单位于底部抽屉）：按钮加大到 ≥44px（plan todo 53/54） */
.user-create__actions--mobile {
  position: sticky;
  bottom: 0;
  padding: 8px 0;
  background: #fff;
}

.user-create__actions--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

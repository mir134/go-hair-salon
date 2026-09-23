<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
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
      class="employee-form__alert"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="88px"
      @submit.prevent
    >
      <el-form-item
        label="姓名"
        prop="name"
      >
        <el-input
          v-model="form.name"
          maxlength="64"
          show-word-limit
          placeholder="员工姓名"
        />
      </el-form-item>
      <el-form-item
        label="手机号"
        prop="phone"
      >
        <el-input
          v-model="form.phone"
          maxlength="32"
          placeholder="选填"
        />
      </el-form-item>
      <el-form-item
        label="头像"
        prop="avatar"
      >
        <AvatarUpload v-model="form.avatar" />
      </el-form-item>
      <el-form-item
        label="职位"
        prop="position"
      >
        <el-input
          v-model="form.position"
          maxlength="64"
          placeholder="如：发型师"
        />
      </el-form-item>
      <el-form-item
        label="入职日期"
        prop="joinedAt"
      >
        <el-date-picker
          v-model="form.joinedAt"
          type="date"
          value-format="YYYY-MM-DD"
          placeholder="选填"
          clearable
          class="employee-form__date"
        />
      </el-form-item>
      <el-form-item
        label="备注"
        prop="remark"
      >
        <el-input
          v-model="form.remark"
          type="textarea"
          :rows="2"
          maxlength="255"
          placeholder="选填"
        />
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
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'

import { ApiError, EMPLOYEE_STATUS_ENABLED, createEmployee, updateEmployee } from '@/api'
import type { Employee, EmployeePayload } from '@/api'
import AvatarUpload from '@/components/AvatarUpload.vue'

// 员工新增/编辑弹窗：employee 为 null = 新增（默认启用）。
// 停用/启用由列表页的状态操作负责（二次确认），本弹窗只维护档案字段；
// 头像经 POST /uploads 上传后存相对路径（avatar 字段），保存时原样回传。
const props = defineProps<{
  modelValue: boolean
  employee: Employee | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

interface EmployeeForm {
  name: string
  phone: string
  avatar: string
  position: string
  joinedAt: string | null
  remark: string
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.employee === null ? '新增员工' : '编辑员工'))

const formRef = ref<FormInstance>()
const form = ref<EmployeeForm>(emptyForm())
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<EmployeeForm> = {
  name: [{ required: true, message: '请输入员工姓名', trigger: 'blur' }],
  phone: [{ max: 32, message: '手机号不能超过 32 个字符', trigger: 'blur' }],
  position: [{ max: 64, message: '职位不能超过 64 个字符', trigger: 'blur' }],
}

watch(visible, (open) => {
  if (open) {
    prepare()
  }
})

function emptyForm(): EmployeeForm {
  return { name: '', phone: '', avatar: '', position: '', joinedAt: null, remark: '' }
}

/** 打开弹窗时回填：编辑取原值，新增用空表单 */
function prepare(): void {
  submitError.value = ''
  const employee = props.employee
  form.value =
    employee === null
      ? emptyForm()
      : {
          name: employee.name,
          phone: employee.phone,
          avatar: employee.avatar,
          position: employee.position,
          joinedAt: employee.joined_at,
          remark: employee.remark,
        }
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
  const employee = props.employee
  const payload: EmployeePayload = {
    name: form.value.name.trim(),
    phone: form.value.phone.trim(),
    avatar: form.value.avatar.trim(),
    position: form.value.position.trim(),
    status: employee?.status ?? EMPLOYEE_STATUS_ENABLED,
    joined_at: form.value.joinedAt,
    remark: form.value.remark.trim(),
  }
  submitting.value = true
  try {
    if (employee === null) {
      await createEmployee(payload)
    } else {
      await updateEmployee(employee.id, payload)
    }
    ElMessage.success(employee === null ? '员工已创建' : '员工已保存')
    emit('saved')
    visible.value = false
  } catch (error) {
    // 400（姓名空等）业务错误：就地展示后端文案
    submitError.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.employee-form__alert {
  margin-bottom: 16px;
}

.employee-form__date {
  width: 100%;
}
</style>

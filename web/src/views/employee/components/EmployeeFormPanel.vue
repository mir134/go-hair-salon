<template>
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
    :label-width="isMobile ? 'auto' : '88px'"
    :label-position="isMobile ? 'top' : 'right'"
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
      <ResponsiveDatePicker
        v-model="form.joinedAt"
        class="employee-form__date"
        mode="date"
        placeholder="选填"
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
  <div
    class="employee-form__actions"
    :class="{ 'employee-form__actions--mobile': isMobile }"
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
      保存
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'

import { ApiError, EMPLOYEE_STATUS_ENABLED, createEmployee, updateEmployee } from '@/api'
import type { Employee, EmployeePayload } from '@/api'
import AvatarUpload from '@/components/AvatarUpload.vue'
import ResponsiveDatePicker from '@/components/ResponsiveDatePicker.vue'
import { useIsMobile } from '@/composables/useIsMobile'

// 员工表单主体（新增/编辑员工，plan todo 54）：
// 弹窗（PC）与底部抽屉（手机）两种外壳共用本组件，表单与提交逻辑只实现一份。
// employee 为 null = 新增（默认启用）。停用/启用由列表页的状态操作负责（二次确认），
// 本组件只维护档案字段；头像经 POST /uploads 上传后存相对路径（avatar 字段），保存时原样回传。
const props = defineProps<{
  employee: Employee | null
}>()

const emit = defineEmits<{
  /** 保存成功：父组件据此刷新列表并关闭外壳 */
  saved: []
  /** 用户取消 / 保存成功后的关闭请求 */
  close: []
}>()

interface EmployeeForm {
  name: string
  phone: string
  avatar: string
  position: string
  joinedAt: string | null
  remark: string
}

const isMobile = useIsMobile()
const formRef = ref<FormInstance>()
const form = ref<EmployeeForm>(emptyForm())
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<EmployeeForm> = {
  name: [{ required: true, message: '请输入员工姓名', trigger: 'blur' }],
  phone: [{ max: 32, message: '手机号不能超过 32 个字符', trigger: 'blur' }],
  position: [{ max: 64, message: '职位不能超过 64 个字符', trigger: 'blur' }],
}

onMounted(() => {
  prepare()
})

function emptyForm(): EmployeeForm {
  return { name: '', phone: '', avatar: '', position: '', joinedAt: null, remark: '' }
}

/** 面板挂载时回填：编辑取原值，新增用空表单（外壳以 v-if 控制挂载，每次打开都是全新实例） */
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
    emit('close')
  } catch (error) {
    // 400（姓名空等）业务错误：就地展示后端文案（07-UI.md:88）
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

.employee-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 手机端（表单位于底部抽屉）：按钮加大到 ≥44px（plan todo 53/54） */
.employee-form__actions--mobile {
  position: sticky;
  bottom: 0;
  padding: 8px 0;
  background: #fff;
}

.employee-form__actions--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

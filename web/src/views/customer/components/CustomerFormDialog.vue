<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="560px"
    :close-on-click-modal="false"
    @closed="handleClosed"
  >
    <el-alert
      v-if="submitError !== ''"
      :title="submitError"
      type="error"
      :closable="false"
      show-icon
      class="customer-form__alert"
    />
    <el-form
      ref="formRef"
      v-loading="loadingDetail"
      :model="form"
      :rules="rules"
      label-width="80px"
      @submit.prevent
    >
      <CustomerFieldsForm
        v-model="form"
        :tags="tags"
      />
    </el-form>
    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        :disabled="loadFailed"
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

import { ApiError, createCustomer, getCustomer, updateCustomer } from '@/api'
import type { Tag } from '@/api'
import {
  customerProfileFormFromDetail,
  emptyCustomerProfileForm,
  syncCustomerTags,
  toCustomerPayload,
} from '@/utils/customerProfile'
import type { CustomerProfileForm } from '@/utils/customerProfile'

import CustomerFieldsForm from './CustomerFieldsForm.vue'

// 新增 = customerId 为 null；编辑 = 传入 id（打开时拉详情回填，含标签）。
// 标签不在 POST/PUT 请求体内，保存后经挂/摘接口做差集同步。
const props = defineProps<{
  modelValue: boolean
  customerId: number | null
  tags: Tag[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 资料保存成功（标签可能部分失败）：父组件据此刷新列表 */
  saved: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.customerId === null ? '新增客户' : '编辑客户'))

const formRef = ref<FormInstance>()
const form = ref<CustomerProfileForm>(emptyCustomerProfileForm())
const submitting = ref(false)
const loadingDetail = ref(false)
/** 编辑时详情加载失败：禁止保存，避免把空表单覆盖到已有客户 */
const loadFailed = ref(false)
const submitError = ref('')
/** 编辑前的标签集合（仅未删除标签；历史软删除标签保持原样） */
const originalTagIds = ref<number[]>([])

const rules: FormRules<CustomerProfileForm> = {
  name: [{ required: true, message: '请输入客户姓名', trigger: 'blur' }],
}

watch(visible, (open) => {
  if (open) {
    void prepare()
  }
})

/** 打开弹窗时初始化：新增清空表单；编辑拉取详情（含标签）回填 */
async function prepare(): Promise<void> {
  submitError.value = ''
  loadFailed.value = false
  originalTagIds.value = []
  Object.assign(form.value, emptyCustomerProfileForm())
  const customerId = props.customerId
  if (customerId === null) {
    return
  }
  loadingDetail.value = true
  try {
    const model = customerProfileFormFromDetail(await getCustomer(customerId))
    originalTagIds.value = [...model.tagIds]
    Object.assign(form.value, model)
  } catch (error) {
    // 拦截器已提示；同时禁止保存，避免空表单覆盖原数据
    loadFailed.value = true
    submitError.value = errorMessage(error)
  } finally {
    loadingDetail.value = false
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
  const payload = toCustomerPayload(form.value)
  submitting.value = true
  try {
    let customerId: number
    if (props.customerId === null) {
      customerId = (await createCustomer(payload)).id
    } else {
      customerId = props.customerId
      await updateCustomer(customerId, payload)
    }
    emit('saved')
    visible.value = false
    try {
      await syncCustomerTags(customerId, [...originalTagIds.value], [...form.value.tagIds])
    } catch (error) {
      ElMessage.warning(`客户资料已保存，但标签未全部保存：${errorMessage(error)}`)
    }
  } catch (error) {
    // 重复手机号（409）等业务错误：就地展示后端文案（07-UI.md:88）
    submitError.value = errorMessage(error)
  } finally {
    submitting.value = false
  }
}

function errorMessage(error: unknown): string {
  return error instanceof ApiError ? error.message : '保存失败，请稍后重试'
}
</script>

<style scoped>
.customer-form__alert {
  margin-bottom: 16px;
}
</style>

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
      class="service-form__alert"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="96px"
      @submit.prevent
    >
      <ServiceFieldsForm
        v-model="form"
        :categories="categories"
      />
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

import { ApiError, createService, updateService } from '@/api'
import type { Service, ServiceCategory, ServicePayload } from '@/api'
import {
  emptyServiceForm,
  parseDuration,
  parsePriceToCents,
  serviceFormFromService,
} from '@/utils/serviceForm'
import type { ServiceFormModel } from '@/utils/serviceForm'

import ServiceFieldsForm from './ServiceFieldsForm.vue'

// 服务新增/编辑弹窗：service 为 null = 新增。金额与时长在 utils/serviceForm.ts 中
// 以文本解析为整数（价格 → 整数分），非法输入在提交前拦截。
const props = defineProps<{
  modelValue: boolean
  service: Service | null
  categories: ServiceCategory[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.service === null ? '新增服务' : '编辑服务'))

const formRef = ref<FormInstance>()
const form = ref<ServiceFormModel>(emptyServiceForm())
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<ServiceFormModel> = {
  name: [{ required: true, message: '请输入服务名称', trigger: 'blur' }],
  categoryId: [{ required: true, message: '请选择所属分类', trigger: 'change' }],
  priceText: [
    {
      validator: (_rule, value, callback) => {
        const cents = parsePriceToCents(value)
        if (cents === null) {
          callback(new Error('请输入正确的金额（最多两位小数）'))
          return
        }
        if (cents <= 0) {
          callback(new Error('价格必须大于 0 元'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  durationText: [
    {
      validator: (_rule, value, callback) => {
        if (parseDuration(value) === null) {
          callback(new Error('时长需为 0-1440 的整数（分钟）'))
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
    prepare()
  }
})

/** 打开弹窗时回填：编辑取原值（分 → 元文本），新增用默认值 */
function prepare(): void {
  submitError.value = ''
  form.value = props.service === null ? emptyServiceForm() : serviceFormFromService(props.service)
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
  const categoryId = form.value.categoryId
  const priceCents = parsePriceToCents(form.value.priceText)
  const duration = parseDuration(form.value.durationText)
  if (categoryId === null || priceCents === null || priceCents <= 0 || duration === null) {
    return
  }
  const payload: ServicePayload = {
    category_id: categoryId,
    name: form.value.name.trim(),
    price_cents: priceCents,
    duration_minutes: duration,
    status: form.value.status,
    remark: form.value.remark.trim(),
  }
  submitting.value = true
  try {
    if (props.service === null) {
      await createService(payload)
    } else {
      await updateService(props.service.id, payload)
    }
    ElMessage.success(props.service === null ? '服务已创建' : '服务已保存')
    emit('saved')
    visible.value = false
  } catch (error) {
    // 400（价格 0/负等）/409 业务错误：就地展示后端文案
    submitError.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.service-form__alert {
  margin-bottom: 16px;
}
</style>

<template>
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
    :label-width="isMobile ? 'auto' : '96px'"
    :label-position="isMobile ? 'top' : 'right'"
    @submit.prevent
  >
    <ServiceFieldsForm
      v-model="form"
      :categories="categories"
    />
  </el-form>
  <div
    class="service-form__actions"
    :class="{ 'service-form__actions--mobile': isMobile }"
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

import { ApiError, createService, updateService } from '@/api'
import type { Service, ServiceCategory, ServicePayload } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import {
  emptyServiceForm,
  parseDuration,
  parsePriceToCents,
  serviceFormFromService,
} from '@/utils/serviceForm'
import type { ServiceFormModel } from '@/utils/serviceForm'

import ServiceFieldsForm from './ServiceFieldsForm.vue'

// 服务表单主体（新增/编辑服务，plan todo 53/54）：
// 弹窗（PC）与底部抽屉（手机）两种外壳共用本组件，表单与提交逻辑只实现一份。
// service 为 null = 新增；挂载时回填（编辑取原值，新增用默认值）。
// 手机端表单标签改为顶部对齐（label-position="top"），避免窄屏标签挤压输入框（07-UI.md:87,119）。
const props = defineProps<{
  service: Service | null
  categories: ServiceCategory[]
}>()

const emit = defineEmits<{
  /** 保存成功：父组件据此刷新列表 */
  saved: []
  /** 用户取消 / 保存成功后的关闭请求 */
  close: []
}>()

const isMobile = useIsMobile()
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

onMounted(() => {
  prepare()
})

/** 挂载时回填：编辑取原值（分 → 元文本），新增用默认值 */
function prepare(): void {
  submitError.value = ''
  form.value = props.service === null ? emptyServiceForm() : serviceFormFromService(props.service)
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
    emit('close')
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

.service-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 手机端（表单位于底部抽屉）：按钮加大到 ≥44px 并吸底（plan todo 53/54） */
.service-form__actions--mobile {
  position: sticky;
  bottom: 0;
  padding: 8px 0;
  background: #fff;
}

.service-form__actions--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="420px"
    :close-on-click-modal="false"
    @closed="handleClosed"
  >
    <el-alert
      v-if="submitError !== ''"
      :title="submitError"
      type="error"
      :closable="false"
      show-icon
      class="category-form__alert"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="72px"
      @submit.prevent
    >
      <el-form-item
        label="名称"
        prop="name"
      >
        <el-input
          v-model="form.name"
          maxlength="64"
          show-word-limit
          placeholder="如：剪发"
        />
      </el-form-item>
      <el-form-item
        label="排序"
        prop="sortText"
      >
        <el-input
          v-model="form.sortText"
          class="category-form__sort"
          placeholder="数字越小越靠前"
        />
      </el-form-item>
      <el-form-item label="状态">
        <el-radio-group v-model="form.status">
          <el-radio :value="1">
            启用
          </el-radio>
          <el-radio :value="0">
            停用
          </el-radio>
        </el-radio-group>
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

import { ApiError, createServiceCategory, updateServiceCategory } from '@/api'
import type { ServiceCategory, ServiceCategoryPayload } from '@/api'

// 分类新增/编辑弹窗：category 为 null = 新增；sort 用文本输入再解析为整数，
// 避免 el-input-number 的 number | undefined 双向绑定在 strict 下的类型退化。
const props = defineProps<{
  modelValue: boolean
  category: ServiceCategory | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

interface CategoryForm {
  name: string
  sortText: string
  status: number
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.category === null ? '新增分类' : '编辑分类'))

const formRef = ref<FormInstance>()
const form = ref<CategoryForm>(emptyForm())
const submitting = ref(false)
const submitError = ref('')

const rules: FormRules<CategoryForm> = {
  name: [{ required: true, message: '请输入分类名称', trigger: 'blur' }],
  sortText: [
    {
      validator: (_rule, value, callback) => {
        if (parseSort(value) === null) {
          callback(new Error('排序需为 0-9999 的整数'))
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

function emptyForm(): CategoryForm {
  return { name: '', sortText: '0', status: 1 }
}

/** 打开弹窗时回填：编辑取原值，新增用默认（sort=0、启用） */
function prepare(): void {
  submitError.value = ''
  const category = props.category
  form.value =
    category === null
      ? emptyForm()
      : { name: category.name, sortText: String(category.sort), status: category.status }
}

/** 排序文本 → 非负整数；非法（空/负/小数/超范围）返回 null */
function parseSort(value: unknown): number | null {
  const text = typeof value === 'string' ? value.trim() : ''
  if (!/^\d{1,4}$/.test(text)) {
    return null
  }
  const parsed = Number.parseInt(text, 10)
  return parsed >= 0 && parsed <= 9999 ? parsed : null
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
  const sort = parseSort(form.value.sortText)
  if (sort === null) {
    return
  }
  const payload: ServiceCategoryPayload = {
    name: form.value.name.trim(),
    sort,
    status: form.value.status,
  }
  submitting.value = true
  try {
    if (props.category === null) {
      await createServiceCategory(payload)
    } else {
      await updateServiceCategory(props.category.id, payload)
    }
    ElMessage.success(props.category === null ? '分类已创建' : '分类已保存')
    emit('saved')
    visible.value = false
  } catch (error) {
    // 400/409 等业务错误：就地展示后端文案
    submitError.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.category-form__alert {
  margin-bottom: 16px;
}

.category-form__sort {
  width: 160px;
}
</style>

<template>
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
    :label-width="isMobile ? 'auto' : '72px'"
    :label-position="isMobile ? 'top' : 'right'"
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
  <div
    class="category-form__actions"
    :class="{ 'category-form__actions--mobile': isMobile }"
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

import { ApiError, createServiceCategory, updateServiceCategory } from '@/api'
import type { ServiceCategory, ServiceCategoryPayload } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

// 分类表单主体（新增/编辑分类，plan todo 54）：
// 弹窗（PC）与底部抽屉（手机）两种外壳共用本组件，表单与提交逻辑只实现一份。
// category 为 null = 新增；sort 用文本输入再解析为整数，
// 避免 el-input-number 的 number | undefined 双向绑定在 strict 下的类型退化。
// 手机端表单标签改为顶部对齐（label-position="top"），避免窄屏标签挤压输入框（07-UI.md:87,119）。
const props = defineProps<{
  category: ServiceCategory | null
}>()

const emit = defineEmits<{
  /** 保存成功：父组件据此刷新列表 */
  saved: []
  /** 用户取消 / 保存成功后的关闭请求 */
  close: []
}>()

interface CategoryForm {
  name: string
  sortText: string
  status: number
}

const isMobile = useIsMobile()
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

onMounted(() => {
  prepare()
})

function emptyForm(): CategoryForm {
  return { name: '', sortText: '0', status: 1 }
}

/** 挂载时回填：编辑取原值，新增用默认（sort=0、启用） */
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
    emit('close')
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

.category-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 手机端（表单位于底部抽屉）：按钮加大到 ≥44px 并吸底（plan todo 53/54） */
.category-form__actions--mobile {
  position: sticky;
  bottom: 0;
  padding: 8px 0;
  background: #fff;
}

.category-form__actions--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

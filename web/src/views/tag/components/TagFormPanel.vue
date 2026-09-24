<template>
  <el-alert
    v-if="submitError !== ''"
    :title="submitError"
    type="error"
    :closable="false"
    show-icon
    class="tag-form__alert"
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
        placeholder="如：老客户"
      />
    </el-form-item>
  </el-form>
  <div
    class="tag-form__actions"
    :class="{ 'tag-form__actions--mobile': isMobile }"
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

import { ApiError, createTag, updateTag } from '@/api'
import type { Tag, TagPayload } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

// 标签表单主体（新增/编辑标签，plan todo 54）：
// 弹窗（PC）与底部抽屉（手机）两种外壳共用本组件，表单与提交逻辑只实现一份。
// tag 为 null = 新增。表单不暴露颜色：前端标签一律按 el-tag type 展示，不渲染 color；
// 编辑时原样回传已有 color、新增时留空，避免暴露无实际效果的字段。
const props = defineProps<{
  tag: Tag | null
}>()

const emit = defineEmits<{
  /** 保存成功：父组件据此刷新列表并关闭外壳 */
  saved: []
  /** 用户取消 / 保存成功后的关闭请求 */
  close: []
}>()

const isMobile = useIsMobile()
const formRef = ref<FormInstance>()
const form = ref<TagForm>({ name: '' })
const submitting = ref(false)
const submitError = ref('')

interface TagForm {
  name: string
}

const rules: FormRules<TagForm> = {
  name: [{ required: true, message: '请输入标签名称', trigger: 'blur' }],
}

onMounted(() => {
  // 外壳用 v-if 保证每次打开重新挂载：挂载即回填（编辑取原值，新增清空），
  // 关闭时随组件卸载自动丢弃校验状态，无需再手动 clearValidate。
  form.value = { name: props.tag?.name ?? '' }
})

async function handleSubmit(): Promise<void> {
  submitError.value = ''
  if (formRef.value === undefined) {
    return
  }
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) {
    return
  }
  const payload: TagPayload = {
    name: form.value.name.trim(),
    color: props.tag?.color ?? '',
  }
  submitting.value = true
  try {
    if (props.tag === null) {
      await createTag(payload)
    } else {
      await updateTag(props.tag.id, payload)
    }
    ElMessage.success(props.tag === null ? '标签已创建' : '标签已保存')
    emit('saved')
    emit('close')
  } catch (error) {
    // 400/409 等业务错误：就地展示后端文案（07-UI.md:88）
    submitError.value = error instanceof ApiError ? error.message : '保存失败，请稍后重试'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.tag-form__alert {
  margin-bottom: 16px;
}

.tag-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}

/* 手机端（表单位于底部抽屉）：按钮加大到 ≥44px（plan todo 53/54、07-UI.md:87） */
.tag-form__actions--mobile {
  position: sticky;
  bottom: 0;
  padding: 8px 0;
  background: #fff;
}

.tag-form__actions--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

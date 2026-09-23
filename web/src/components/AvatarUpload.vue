<template>
  <div class="avatar-upload">
    <el-avatar
      v-if="modelValue !== ''"
      :src="modelValue"
      :size="56"
      shape="square"
      class="avatar-upload__preview"
    />
    <div class="avatar-upload__actions">
      <el-upload
        :show-file-list="false"
        :http-request="handleRequest"
        :before-upload="handleBeforeUpload"
        :disabled="disabled"
        :accept="UPLOAD_ACCEPT"
      >
        <el-button
          size="small"
          :loading="uploading"
          :disabled="disabled"
        >
          {{ modelValue === '' ? '上传头像' : '更换头像' }}
        </el-button>
      </el-upload>
      <el-button
        v-if="modelValue !== ''"
        size="small"
        :disabled="disabled || uploading"
        @click="clear"
      >
        清除
      </el-button>
      <span class="avatar-upload__hint">jpg/jpeg/png/webp/gif，≤2MB</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, type UploadRequestOptions } from 'element-plus'

import { UPLOAD_ACCEPT, UPLOAD_MAX_SIZE_BYTES, uploadImage } from '@/api'

// 头像上传控件（客户表单与员工表单共用）：
// 上传成功后把后端返回的相对路径（/uploads/<hash>.<ext>）写回 v-model 的 avatar 字段。
// 请求一律经 @/api（views/components 不直接使用 axios）。
const modelValue = defineModel<string>({ required: true })

withDefaults(defineProps<{ disabled?: boolean }>(), { disabled: false })

const uploading = ref(false)

/** 客户端预检（后端仍会二次校验 magic bytes 与大小，前端校验只为省一次请求） */
function handleBeforeUpload(file: File): boolean {
  if (!UPLOAD_ACCEPT.split(',').includes(file.type)) {
    ElMessage.error('仅支持 jpg/jpeg/png/webp/gif 图片')
    return false
  }
  if (file.size > UPLOAD_MAX_SIZE_BYTES) {
    ElMessage.error('图片大小不能超过 2MB')
    return false
  }
  return true
}

async function handleRequest(options: UploadRequestOptions): Promise<void> {
  uploading.value = true
  try {
    const result = await uploadImage(options.file)
    modelValue.value = result.url
    ElMessage.success('头像已上传')
  } catch {
    // 拦截器已提示后端文案（400/401/403），此处不重复弹出
  } finally {
    uploading.value = false
  }
}

function clear(): void {
  modelValue.value = ''
}
</script>

<style scoped>
.avatar-upload {
  display: flex;
  align-items: center;
  gap: 12px;
}

.avatar-upload__preview {
  flex: none;
  border: 1px solid var(--el-border-color-lighter);
}

.avatar-upload__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.avatar-upload__hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* 手机端：按钮触控区域 ≥44px（plan todo 53/54） */
@media (max-width: 768px) {
  .avatar-upload__actions :deep(.el-button) {
    min-height: 44px;
  }
}
</style>

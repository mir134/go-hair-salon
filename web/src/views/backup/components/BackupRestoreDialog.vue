<template>
  <el-dialog
    v-model="visible"
    title="恢复备份"
    width="520px"
    :close-on-click-modal="false"
    :close-on-press-escape="!restoring"
    :show-close="!restoring"
  >
    <template v-if="backup !== null">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="恢复会用该备份时点的数据整体替换当前数据库、uploads 与 config.yaml。"
      >
        <p class="restore__warn">
          恢复期间系统进入维护模式：业务写入会被拒绝（503「系统维护中」），读请求不受影响。
          恢复前会自动生成当前数据的安全备份；恢复失败时原数据不受影响。
        </p>
      </el-alert>

      <el-descriptions
        class="restore__target"
        :column="1"
        border
      >
        <el-descriptions-item label="备份文件">
          {{ backup.name }}
        </el-descriptions-item>
        <el-descriptions-item label="大小 / 时间">
          {{ formatBytes(backup.size_bytes) }} · {{ formatDateTime(backup.created_at) }}
        </el-descriptions-item>
      </el-descriptions>

      <template v-if="result === null">
        <el-checkbox
          v-model="agreed"
          class="restore__agree"
          :disabled="restoring"
        >
          我已知晓恢复会覆盖当前数据，并确认要恢复到该备份时点
        </el-checkbox>
        <el-form
          label-width="130px"
          @submit.prevent
        >
          <el-form-item :label="`输入「${RESTORE_CONFIRM_TEXT}」确认`">
            <el-input
              v-model="confirmText"
              placeholder="请输入确认文本"
              maxlength="16"
              :disabled="restoring"
            />
          </el-form-item>
        </el-form>

        <el-alert
          v-if="restoring"
          class="restore__status"
          type="info"
          :closable="false"
          show-icon
          title="正在恢复，请勿关闭页面或刷新浏览器……"
        />
        <el-alert
          v-if="errorMessage !== ''"
          class="restore__status"
          type="error"
          :closable="false"
          show-icon
          :title="errorMessage"
        />
      </template>

      <el-alert
        v-else
        class="restore__status"
        type="success"
        :closable="false"
        show-icon
        title="恢复完成"
      >
        <p class="restore__result">
          已恢复：{{ result.restored }}
        </p>
        <p class="restore__result">
          恢复前安全备份：{{ result.safety_backup }}（{{ result.file_count }} 个文件，uploads
          {{ result.uploads_replaced ? '已替换' : '未包含' }}，config
          {{ result.config_replaced ? '已替换' : '未包含' }}）
        </p>
      </el-alert>
    </template>

    <template #footer>
      <el-button
        :disabled="restoring"
        @click="visible = false"
      >
        {{ result === null ? '取消' : '关闭' }}
      </el-button>
      <el-button
        v-if="result === null"
        type="danger"
        :loading="restoring"
        :disabled="!canConfirm"
        @click="handleConfirm"
      >
        确认恢复
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { ApiError, CODE_SERVICE_UNAVAILABLE, restoreBackup } from '@/api'
import type { Backup, RestoreResult } from '@/api'
import { RESTORE_CONFIRM_TEXT, formatBytes, isRestoreConfirmMatched } from '@/utils/backup'
import { formatDateTime } from '@/utils/format'

// 恢复二次确认弹窗（plan todo 52、05-TASKS.md:166）：
// 勾选确认 + 输入「恢复」双条件才允许提交；恢复期间展示进度提示，
// 503（code=50300）展示明确的「系统维护中」文案。
const props = defineProps<{
  modelValue: boolean
  backup: Backup | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 恢复成功：父级刷新备份列表（安全备份会出现在列表中） */
  restored: [result: RestoreResult]
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const agreed = ref(false)
const confirmText = ref('')
const restoring = ref(false)
const result = ref<RestoreResult | null>(null)
const errorMessage = ref('')

const canConfirm = computed(
  () => agreed.value && isRestoreConfirmMatched(confirmText.value) && !restoring.value,
)

// 每次打开（或更换目标备份）重置二次确认与结果状态
watch(
  () => [props.modelValue, props.backup?.name] as const,
  ([open]) => {
    if (open) {
      agreed.value = false
      confirmText.value = ''
      restoring.value = false
      result.value = null
      errorMessage.value = ''
    }
  },
)

async function handleConfirm(): Promise<void> {
  const target = props.backup
  if (target === null || !canConfirm.value) {
    return
  }
  restoring.value = true
  errorMessage.value = ''
  try {
    result.value = await restoreBackup(target.name, { confirm: true })
    emit('restored', result.value)
  } catch (error) {
    // 503 + 50300：恢复期间（或另一次恢复进行中）业务写入被拒 → 明确的维护提示
    if (error instanceof ApiError && error.code === CODE_SERVICE_UNAVAILABLE) {
      errorMessage.value = '系统维护中：数据恢复可能正在进行，请稍后重试。'
    } else {
      errorMessage.value = error instanceof ApiError ? error.message : '恢复失败，请稍后重试'
    }
  } finally {
    restoring.value = false
  }
}
</script>

<style scoped>
.restore__warn {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.6;
}

.restore__target {
  margin-top: 16px;
}

.restore__agree {
  display: block;
  margin: 16px 0 4px;
}

.restore__status {
  margin-top: 12px;
}

.restore__result {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.6;
  word-break: break-all;
}
</style>

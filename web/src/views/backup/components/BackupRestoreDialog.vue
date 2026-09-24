<template>
  <!-- 手机端（<768px）：底部抽屉承载恢复二次确认（plan todo 53/54、07-UI.md:119） -->
  <el-drawer
    v-if="isMobile"
    v-model="visible"
    title="恢复备份"
    direction="btt"
    size="86%"
    :close-on-click-modal="false"
    :close-on-press-escape="!restoring"
    :show-close="!restoring"
  >
    <BackupRestorePanel
      v-if="visible"
      v-model:agreed="agreed"
      v-model:confirm-text="confirmText"
      :backup="backup"
      :result="result"
      :restoring="restoring"
      :error-message="errorMessage"
    />
    <template #footer>
      <div class="restore__actions restore__actions--mobile">
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
      </div>
    </template>
  </el-drawer>

  <!-- PC：保持原有弹窗形态与 footer 按钮位置不变 -->
  <el-dialog
    v-else
    v-model="visible"
    title="恢复备份"
    width="520px"
    :close-on-click-modal="false"
    :close-on-press-escape="!restoring"
    :show-close="!restoring"
  >
    <BackupRestorePanel
      v-if="visible"
      v-model:agreed="agreed"
      v-model:confirm-text="confirmText"
      :backup="backup"
      :result="result"
      :restoring="restoring"
      :error-message="errorMessage"
    />
    <template #footer>
      <div class="restore__actions">
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
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { ApiError, CODE_SERVICE_UNAVAILABLE, restoreBackup } from '@/api'
import type { Backup, RestoreResult } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { isRestoreConfirmMatched } from '@/utils/backup'

import BackupRestorePanel from './BackupRestorePanel.vue'

// 恢复二次确认外壳（plan todo 52、05-TASKS.md:166）：
// 勾选确认 + 输入「恢复」双条件才允许提交；恢复期间展示进度提示，
// 503（code=50300）展示明确的「系统维护中」文案。
// 手机端（<768px，plan todo 53/54）：底部抽屉 + 大号 footer 按钮；PC 端保持弹窗与 footer 布局。
// 确认状态与提交逻辑全部保留在本组件（主体 UI 由 BackupRestorePanel 复用），行为不变。
const props = defineProps<{
  modelValue: boolean
  backup: Backup | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 恢复成功：父级刷新备份列表（安全备份会出现在列表中） */
  restored: [result: RestoreResult]
}>()

const isMobile = useIsMobile()

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
/* 手机端（底部抽屉 footer）：两枚按钮等宽铺满、触控目标 ≥44px（plan todo 53/54） */
.restore__actions--mobile {
  display: flex;
  gap: 10px;
}

.restore__actions--mobile .el-button {
  flex: 1;
  min-height: 44px;
  margin-left: 0;
}
</style>

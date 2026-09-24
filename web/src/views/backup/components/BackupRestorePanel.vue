<template>
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
        :label-width="isMobile ? 'auto' : '130px'"
        :label-position="isMobile ? 'top' : 'right'"
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
</template>

<script setup lang="ts">
import type { Backup, RestoreResult } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { RESTORE_CONFIRM_TEXT, formatBytes } from '@/utils/backup'
import { formatDateTime } from '@/utils/format'

// 恢复二次确认主体（plan todo 52/53）：PC 弹窗与手机底部抽屉两种外壳共用本组件，
// 「勾选 + 输入确认文本」双条件所需的勾选/输入状态经 v-model 与外壳同步，
// 提交、进度与错误处理仍由外壳（BackupRestoreDialog）统一负责，保证只有一份提交逻辑。
// 手机端（<768px）：确认输入改为标签置顶，避免 375px 下输入框被挤压（plan todo 53/54）。
defineProps<{
  backup: Backup | null
  result: RestoreResult | null
  restoring: boolean
  errorMessage: string
}>()

const agreed = defineModel<boolean>('agreed', { required: true })
const confirmText = defineModel<string>('confirmText', { required: true })

const isMobile = useIsMobile()
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

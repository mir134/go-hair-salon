<template>
  <el-drawer
    v-model="visible"
    title="日志详情"
    :size="isMobile ? '100%' : '520px'"
  >
    <el-descriptions
      v-if="log !== null"
      :column="1"
      border
    >
      <el-descriptions-item label="时间">
        {{ formatDateTime(log.created_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="操作人">
        {{ operationLogOperatorLabel(log.operator_id, log.operator_name) }}
      </el-descriptions-item>
      <el-descriptions-item label="动作">
        {{ operationLogActionLabel(log.action) }}（{{ log.action }}）
      </el-descriptions-item>
      <el-descriptions-item label="目标">
        {{ operationLogTargetLabel(log.target_type, log.target_id) }}
      </el-descriptions-item>
      <el-descriptions-item label="IP">
        {{ log.ip === '' ? '—' : log.ip }}
      </el-descriptions-item>
      <el-descriptions-item label="User-Agent">
        {{ log.user_agent === '' ? '—' : log.user_agent }}
      </el-descriptions-item>
      <el-descriptions-item label="内容">
        <span class="log-detail__content">{{ log.content }}</span>
      </el-descriptions-item>
    </el-descriptions>
    <el-empty
      v-else
      description="未选择日志"
    />
  </el-drawer>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { OperationLog } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatDateTime } from '@/utils/format'
import {
  operationLogActionLabel,
  operationLogOperatorLabel,
  operationLogTargetLabel,
} from '@/utils/operationLog'

// 日志详情抽屉（plan todo 48）：content / ip / user_agent 全文展示；
// 审计记录只读，无任何编辑入口。
// 手机端（<768px）：抽屉占满整屏（plan todo 53/54），避免 IP/User-Agent/内容长文本被挤压；
// PC 端保持 520px 侧边抽屉。
const props = defineProps<{
  modelValue: boolean
  log: OperationLog | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const isMobile = useIsMobile()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})
</script>

<style scoped>
.log-detail__content {
  white-space: pre-wrap;
  word-break: break-all;
}
</style>

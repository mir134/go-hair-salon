<template>
  <!-- 手机端（<768px）：卡片列表替代表格，点击整卡打开详情（plan todo 53/54、07-UI.md:119） -->
  <div
    v-if="isMobile"
    v-loading="loading"
    class="log-cards"
  >
    <article
      v-for="log in items"
      :key="log.id"
      class="log-card"
      @click="emit('detail', log)"
    >
      <header class="log-card__head">
        <span class="log-card__time">{{ formatDateTime(log.created_at) }}</span>
        <el-tag
          type="info"
          size="small"
        >
          {{ operationLogActionLabel(log.action) }}
        </el-tag>
      </header>
      <p class="log-card__operator">
        操作人：{{ operationLogOperatorLabel(log.operator_id, log.operator_name) }}
      </p>
      <p class="log-card__content">
        {{ log.content }}
      </p>
      <footer class="log-card__more">
        查看详情
      </footer>
    </article>

    <el-empty
      v-if="!loading && items.length === 0"
      description="暂无操作日志"
    />
  </div>

  <el-table
    v-else
    v-loading="loading"
    :data="items"
    row-key="id"
  >
    <el-table-column
      label="时间"
      width="150"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作人"
      width="120"
    >
      <template #default="{ row }">
        {{ operationLogOperatorLabel(row.operator_id, row.operator_name) }}
      </template>
    </el-table-column>
    <el-table-column
      label="动作"
      width="130"
    >
      <template #default="{ row }">
        {{ operationLogActionLabel(row.action) }}
      </template>
    </el-table-column>
    <el-table-column
      label="目标"
      width="140"
    >
      <template #default="{ row }">
        {{ operationLogTargetLabel(row.target_type, row.target_id) }}
      </template>
    </el-table-column>
    <el-table-column
      prop="content"
      label="内容"
      min-width="220"
      show-overflow-tooltip
    />
    <el-table-column
      prop="ip"
      label="IP"
      width="130"
    />
    <el-table-column
      label="操作"
      width="80"
      fixed="right"
    >
      <template #default="{ row }">
        <el-button
          link
          type="primary"
          @click="handleDetail(row)"
        >
          详情
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无操作日志" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { OperationLog } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatDateTime } from '@/utils/format'
import {
  operationLogActionLabel,
  operationLogOperatorLabel,
  operationLogTargetLabel,
} from '@/utils/operationLog'

// 操作日志表格（plan todo 48）：时间倒序的审计流水；详情抽屉展示 content/ip/ua 全文。
// 本页只读——审计日志禁止修改/删除（AGENTS.md 第 5 节）。
// 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119「手机端不要堆复杂表格」），
// 卡片是表格列的移动端投影（时间/操作人/动作/内容摘要），点击整卡打开详情抽屉；PC 端表格列不变。
defineProps<{
  items: OperationLog[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 查看某条日志详情（父级打开详情抽屉） */
  detail: [log: OperationLog]
}>()

const isMobile = useIsMobile()

/**
 * el-table 插槽 row 的类型是 Element Plus 的 DefaultRow（含索引签名），
 * 无法直接传给强类型的 emit；此处在库边界收敛为 OperationLog。
 */
function handleDetail(row: unknown): void {
  emit('detail', row as OperationLog)
}
</script>

<style scoped>
/* 手机端日志卡片列表（plan todo 53/54）：整卡可点，触控目标大 */
.log-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.log-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  cursor: pointer;
}

.log-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.log-card__time {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.log-card__operator {
  margin: 8px 0 0;
  font-size: 14px;
  font-weight: 600;
}

/* 内容摘要：手机端最多两行，完整内容点卡片进详情查看 */
.log-card__content {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  margin: 6px 0 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}

.log-card__more {
  margin-top: 8px;
  color: var(--el-color-primary);
  font-size: 13px;
  text-align: right;
}
</style>

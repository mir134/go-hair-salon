<template>
  <section class="log-list">
    <div
      class="log-list__head"
      :class="{ 'log-list__head--mobile': isMobile }"
    >
      <span class="log-list__title">操作日志</span>
      <span class="log-list__hint">审计记录只读，按时间倒序（最新在前）</span>
    </div>

    <el-card shadow="never">
      <LogFilterBar
        :operators="operators"
        @change="handleFilterChange"
      />
      <LogTable
        :items="items"
        :loading="loading"
        @detail="openDetail"
      />
      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        @change="handlePageChange"
      />
    </el-card>

    <LogDetailDrawer
      v-model="drawerVisible"
      :log="selected"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { listOperationLogs } from '@/api'
import type { OperationLog } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { usePagedList } from '@/composables/usePagedList'
import { emptyOperationLogFilters } from '@/utils/operationLog'
import type { OperationLogFilters } from '@/utils/operationLog'

import LogDetailDrawer from './components/LogDetailDrawer.vue'
import LogFilterBar from './components/LogFilterBar.vue'
import LogTable from './components/LogTable.vue'

// 操作日志页（plan todo 48、05-TASKS.md:150-155）：仅 admin 菜单/路由（路由守卫）；
// 列表 + 操作人/动作/日期筛选 + 详情抽屉（content/ip/ua）；后端 RBAC 是最终边界。
// 手机端（<768px，plan todo 53/54）：标题/说明上下排布，列表与筛选由子组件切换为卡片+大控件。
const isMobile = useIsMobile()
const filters = ref<OperationLogFilters>(emptyOperationLogFilters())
const operators = ref<{ id: number; name: string }[]>([])
const selected = ref<OperationLog | null>(null)
const drawerVisible = ref(false)

const { items, total, page, pageSize, loading, load } = usePagedList<OperationLog>(
  (currentPage, currentPageSize) =>
    listOperationLogs({
      page: currentPage,
      page_size: currentPageSize,
      ...(filters.value.operatorId === null ? {} : { operator_id: filters.value.operatorId }),
      ...(filters.value.action === '' ? {} : { action: filters.value.action }),
      ...(filters.value.startDate === '' ? {} : { start_date: filters.value.startDate }),
      ...(filters.value.endDate === '' ? {} : { end_date: filters.value.endDate }),
    }),
)

onMounted(() => {
  void loadLogs()
})

/**
 * 加载一页日志，并把出现的操作人累积为筛选下拉选项。
 * MVP 阶段用户管理 API（todo 39）尚未落地，为避免新增未在 04-API 定义的接口，
 * 操作人选项取自日志本身；后续如有 /users 可改为直读。
 */
async function loadLogs(): Promise<void> {
  await load()
  rememberOperators(items.value)
}

function rememberOperators(rows: OperationLog[]): void {
  for (const row of rows) {
    const operatorId = row.operator_id
    if (operatorId === null || row.operator_name === '') {
      continue
    }
    if (operators.value.some((item) => item.id === operatorId)) {
      continue
    }
    operators.value.push({ id: operatorId, name: row.operator_name })
  }
}

/** 筛选变化：回到第 1 页并按新条件加载 */
function handleFilterChange(next: OperationLogFilters): void {
  filters.value = next
  page.value = 1
  void loadLogs()
}

function handlePageChange(): void {
  void loadLogs()
}

function openDetail(log: OperationLog): void {
  selected.value = log
  drawerVisible.value = true
}
</script>

<style scoped>
.log-list__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 12px;
}

.log-list__title {
  font-size: 16px;
  font-weight: 600;
}

.log-list__hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

/* 手机端（<768px）：标题与说明上下排布，避免 375px 视口挤压（plan todo 53/54） */
.log-list__head--mobile {
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}
</style>

<template>
  <section class="dashboard">
    <!-- 手机首页：搜索客户 / 最近客户 / 快速消费 / 快速充值（07-UI.md:42、plan todo 53） -->
    <MobileQuickEntries v-if="isMobile" />

    <div class="dashboard__head">
      <span class="dashboard__title">Dashboard</span>
      <div class="dashboard__toolbar">
        <el-date-picker
          v-model="range"
          type="daterange"
          unlink-panels
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          @change="handleRangeChange"
        />
        <el-button @click="handleRefresh">
          刷新
        </el-button>
      </div>
    </div>

    <p class="dashboard__range-note">
      统计日期范围：{{ rangeLabel }}（今日概览与待结账单数为实时数据，不受范围影响）
    </p>

    <div class="dashboard__section-head">
      <span class="dashboard__section-title">今日概览</span>
      <span class="dashboard__section-note">{{ todayNote }}</span>
    </div>

    <DashboardKpiCards
      :summary="summary"
      :loading="summaryLoading"
      @open-pending="goPendingOrders"
    />

    <DashboardRangeSection
      :rows="dailyRows"
      :range-label="rangeLabel"
      :loading="rangeLoading"
    />

    <el-card
      v-loading="rangeLoading"
      shadow="never"
      class="dashboard__performance"
    >
      <template #header>
        <div class="dashboard__performance-head">
          <span class="dashboard__performance-title">员工业绩</span>
          <span class="dashboard__performance-range">{{ rangeLabel }}</span>
        </div>
      </template>
      <EmployeePerformanceTable
        :items="performance"
        :loading="rangeLoading"
      />
    </el-card>

    <div class="dashboard__recent">
      <RecentOrdersCard
        :items="recentOrders"
        :loading="summaryLoading"
      />
      <RecentRechargesCard
        :items="recentRecharges"
        :loading="summaryLoading"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { getCustomers, getEmployeePerformance, getRevenue, getSummary } from '@/api'
import type {
  DashboardCustomerPoint,
  DashboardRevenuePoint,
  DashboardSummary,
  EmployeePerformanceRow,
} from '@/api'
import {
  formatRangeLabel,
  mergeDailyRows,
  normalizeRange,
  todayRange,
} from '@/utils/dashboard'
import type { DateRange } from '@/utils/dashboard'
import { useIsMobile } from '@/composables/useIsMobile'

import DashboardKpiCards from './components/DashboardKpiCards.vue'
import DashboardRangeSection from './components/DashboardRangeSection.vue'
import EmployeePerformanceTable from './components/EmployeePerformanceTable.vue'
import MobileQuickEntries from './components/MobileQuickEntries.vue'
import RecentOrdersCard from './components/RecentOrdersCard.vue'
import RecentRechargesCard from './components/RecentRechargesCard.vue'

// Dashboard 页（plan todo 46、07-UI.md:76-83）：
// - 今日 KPI 与最近消费/最近充值来自 /dashboard/summary（无日期参数，始终当天/实时）；
// - 日期范围选择器驱动 /dashboard/revenue、/dashboard/customers、/dashboard/employee-performance；
// - 待结账单数来自 summary 的实时 pending 总数，切换范围不重新拉取、数值不受范围影响；
// - 不用图表库：区间序列以合计卡片 + 每日明细表格呈现。
const router = useRouter()
/** 手机布局（<768px）：首页顶部插入 4 个快捷入口；PC 布局保持不变 */
const isMobile = useIsMobile()

const range = ref<DateRange | null>(null)
const summary = ref<DashboardSummary | null>(null)
const revenuePoints = ref<DashboardRevenuePoint[]>([])
const customerPoints = ref<DashboardCustomerPoint[]>([])
const performance = ref<EmployeePerformanceRow[]>([])

const summaryLoading = ref(false)
const rangeLoading = ref(false)

const rangeLabel = computed(() => formatRangeLabel(normalizeRange(range.value)))
const dailyRows = computed(() => mergeDailyRows(revenuePoints.value, customerPoints.value))
const recentOrders = computed(() => summary.value?.recent_orders ?? [])
const recentRecharges = computed(() => summary.value?.recent_recharges ?? [])

/** 今日概览的统计日（服务器本地时区）：加载前只说明口径，避免显示错误日期 */
const todayNote = computed(() => {
  const date = summary.value?.date
  return date === undefined || date === '' ? '不受日期范围影响' : `${date} · 不受日期范围影响`
})

// 快速切换日期范围时，旧请求的响应可能后到；用序号丢弃过期响应（stale_state 防护）
let rangeRequestSeq = 0

onMounted(() => {
  range.value = todayRange()
  void loadAll()
})

/** 首次进入 / 手动刷新：今日概览 + 当前范围数据 */
async function loadAll(): Promise<void> {
  await Promise.all([loadSummary(), loadRange()])
}

async function loadSummary(): Promise<void> {
  summaryLoading.value = true
  try {
    summary.value = await getSummary()
  } catch {
    // 拦截器已提示；保留上一次数据，可用「刷新」重试
  } finally {
    summaryLoading.value = false
  }
}

async function loadRange(): Promise<void> {
  const current = normalizeRange(range.value)
  const seq = ++rangeRequestSeq
  rangeLoading.value = true
  try {
    const [revenue, customers, employeePerformance] = await Promise.all([
      getRevenue({ start_date: current[0], end_date: current[1] }),
      getCustomers({ start_date: current[0], end_date: current[1] }),
      getEmployeePerformance({ start_date: current[0], end_date: current[1] }),
    ])
    if (seq !== rangeRequestSeq) {
      return
    }
    revenuePoints.value = revenue.items
    customerPoints.value = customers.items
    performance.value = employeePerformance.items
  } catch {
    // 拦截器已提示；保留上一次数据，可用「刷新」重试
  } finally {
    if (seq === rangeRequestSeq) {
      rangeLoading.value = false
    }
  }
}

/** 日期范围变化：归一化（清空回退今日、颠倒自动交换）后只重拉范围数据，不重拉今日概览 */
function handleRangeChange(): void {
  const next = normalizeRange(range.value)
  const current = range.value
  if (current === null || current[0] !== next[0] || current[1] !== next[1]) {
    range.value = next
  }
  void loadRange()
}

function handleRefresh(): void {
  void loadAll()
}

/** 待结账单数卡片 → 消费记录页待结账 tab（OrderListView 读取 ?status=pending） */
function goPendingOrders(): void {
  void router.push({ path: '/orders', query: { status: 'pending' } })
}
</script>

<style scoped>
.dashboard__head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.dashboard__title {
  font-size: 16px;
  font-weight: 600;
}

.dashboard__toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
}

.dashboard__range-note {
  margin: 0 0 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.dashboard__section-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 8px;
}

.dashboard__section-title {
  font-size: 14px;
  font-weight: 600;
}

.dashboard__section-note {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.dashboard__performance {
  margin-top: 16px;
}

.dashboard__performance-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.dashboard__performance-title {
  font-size: 16px;
  font-weight: 600;
}

.dashboard__performance-range {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.dashboard__recent {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(420px, 1fr));
  gap: 16px;
  margin-top: 16px;
}
</style>

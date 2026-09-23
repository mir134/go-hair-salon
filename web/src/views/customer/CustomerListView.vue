<template>
  <section class="customer-list">
    <el-card shadow="never">
      <CustomerToolbar
        v-model:keyword="keyword"
        v-model:tag-filter="tagFilter"
        :tags="tags"
        :mobile="isMobile"
        :recent-sort="recentSort"
        :auto-focus="focusSearch"
        @search="handleSearch"
        @reset="handleReset"
        @create="openCreate"
        @clear-sort="handleClearSort"
      />

      <!-- 手机端（<768px）：卡片列表替代表格，点击卡片进详情（plan todo 54、07-UI.md:119） -->
      <div
        v-if="isMobile"
        v-loading="loading"
        class="customer-cards"
      >
        <article
          v-for="row in items"
          :key="row.id"
          class="customer-card"
          @click="goDetail(row.id)"
        >
          <div class="customer-card__head">
            <span class="customer-card__name">{{ row.name }}</span>
            <span class="customer-card__phone">{{ row.phone !== '' ? row.phone : '无手机号' }}</span>
          </div>
          <dl class="customer-card__stats">
            <div class="customer-card__cell">
              <dt>余额（元）</dt>
              <dd class="customer-card__value">
                ¥{{ formatCents(row.balance_cents) }}
              </dd>
            </div>
            <div class="customer-card__cell">
              <dt>积分</dt>
              <dd class="customer-card__value">
                {{ row.points }}
              </dd>
            </div>
            <div class="customer-card__cell">
              <dt>最近到店</dt>
              <dd class="customer-card__value customer-card__value--small">
                {{ formatDateTime(row.last_visit_at) }}
              </dd>
            </div>
          </dl>
          <footer
            class="customer-card__actions"
            @click.stop
          >
            <el-button @click="openEdit(row.id)">
              编辑
            </el-button>
            <el-button
              v-if="isAdmin"
              type="danger"
              plain
              @click="handleDelete(row.id, row.name)"
            >
              删除
            </el-button>
          </footer>
        </article>

        <!-- 无结果空状态：仍可直接新增客户（plan todo 54 QA：可继续新增） -->
        <el-empty
          v-if="!loading && items.length === 0"
          description="未找到客户"
        >
          <el-button
            type="primary"
            size="large"
            @click="openCreate"
          >
            新增客户
          </el-button>
        </el-empty>
      </div>

      <el-table
        v-else
        v-loading="loading"
        :data="items"
        row-key="id"
        class="customer-list__table"
      >
        <el-table-column
          label="姓名"
          min-width="110"
        >
          <template #default="{ row }">
            <el-link
              type="primary"
              @click="goDetail(row.id)"
            >
              {{ row.name }}
            </el-link>
          </template>
        </el-table-column>
        <el-table-column
          label="手机号"
          min-width="130"
        >
          <template #default="{ row }">
            {{ row.phone !== '' ? row.phone : '—' }}
          </template>
        </el-table-column>
        <el-table-column
          label="性别"
          width="80"
        >
          <template #default="{ row }">
            {{ GENDER_LABELS[row.gender] ?? '—' }}
          </template>
        </el-table-column>
        <el-table-column
          label="余额（元）"
          width="110"
          align="right"
        >
          <template #default="{ row }">
            {{ formatCents(row.balance_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="points"
          label="积分"
          width="90"
          align="right"
        />
        <el-table-column
          label="累计消费（元）"
          width="130"
          align="right"
        >
          <template #default="{ row }">
            {{ formatCents(row.total_spent_cents) }}
          </template>
        </el-table-column>
        <el-table-column
          label="最近到店"
          width="150"
        >
          <template #default="{ row }">
            {{ formatDateTime(row.last_visit_at) }}
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="130"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="openEdit(row.id)"
            >
              编辑
            </el-button>
            <el-button
              v-if="isAdmin"
              link
              type="danger"
              @click="handleDelete(row.id, row.name)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无客户" />
        </template>
      </el-table>

      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        @change="handlePageChange"
      />
    </el-card>

    <CustomerFormDialog
      v-model="dialogVisible"
      :customer-id="editingCustomerId"
      :tags="tags"
      @saved="handleSaved"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'

import { deleteCustomer, listCustomers, listTags } from '@/api'
import type { Customer, CustomerListQuery, Tag } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { useIsMobile } from '@/composables/useIsMobile'
import { usePagedList } from '@/composables/usePagedList'
import { GENDER_LABELS } from '@/constants'
import { useAuthStore } from '@/stores/auth'
import { formatCents, formatDateTime } from '@/utils/format'

import CustomerFormDialog from './components/CustomerFormDialog.vue'
import CustomerToolbar from './components/CustomerToolbar.vue'

// 客户列表（07-UI.md:5-18、84-94、plan todo 16/54）：搜索优先手机号、标签筛选、分页、新增/编辑。
// 手机端（<768px）：大搜索框 + 结果卡片列表（点击进详情）+ 底部抽屉表单；
// 入口支持首页快捷入口：?focus=search（自动聚焦）、?sort=recent（按最近到访排序）。
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const isMobile = useIsMobile()
/** 删除仅 admin（06-BUSINESS-RULES.md §7）；隐藏按钮是 UI 简化，最终边界在后端 RBAC */
const isAdmin = computed(() => auth.role === 'admin')

/** 首页「搜索客户」快捷入口：进入即聚焦搜索框（仅手机端生效，见 CustomerToolbar） */
const focusSearch = route.query.focus === 'search'
/** 首页「最近客户」快捷入口：sort=recent → 后端按 last_visit_at desc 排序 */
const recentSort = ref(route.query.sort === 'recent')

const keyword = ref('')
const tagFilter = ref<number | null>(null)
const tags = ref<Tag[]>([])

const { items, total, page, pageSize, loading, load } = usePagedList<Customer>(
  (currentPage, currentPageSize) => listCustomers(buildQuery(currentPage, currentPageSize)),
)

const dialogVisible = ref(false)
/** null = 新增；数字 = 编辑对应客户（表单挂载后拉取详情补全标签） */
const editingCustomerId = ref<number | null>(null)

onMounted(() => {
  void loadTags()
  void load()
})

function buildQuery(currentPage: number, currentPageSize: number): CustomerListQuery {
  const query: CustomerListQuery = { page: currentPage, page_size: currentPageSize }
  const trimmedKeyword = keyword.value.trim()
  if (trimmedKeyword !== '') {
    query.keyword = trimmedKeyword
  }
  if (tagFilter.value !== null) {
    query.tag_id = tagFilter.value
  }
  if (recentSort.value) {
    query.sort = 'recent'
  }
  return query
}

async function loadTags(): Promise<void> {
  try {
    tags.value = await listTags()
  } catch {
    // 拦截器已提示；标签筛选与多选降级为空列表
    tags.value = []
  }
}

/** 搜索/筛选变化：回到第一页重新加载 */
function handleSearch(): void {
  page.value = 1
  void load()
}

function handlePageChange(): void {
  void load()
}

function handleReset(): void {
  keyword.value = ''
  tagFilter.value = null
  recentSort.value = false
  handleSearch()
}

/** 关闭「按最近到访排序」标记：恢复默认排序并重新加载 */
function handleClearSort(): void {
  recentSort.value = false
  page.value = 1
  void load()
}

function openCreate(): void {
  editingCustomerId.value = null
  dialogVisible.value = true
}

function openEdit(customerId: number): void {
  editingCustomerId.value = customerId
  dialogVisible.value = true
}

/** 进入客户详情（07-UI.md:44-46） */
function goDetail(customerId: number): void {
  void router.push(`/customers/${customerId}`)
}

/** 新增/编辑成功后刷新（列表与标签都可能变化） */
function handleSaved(): void {
  void loadTags()
  void load()
}

async function handleDelete(customerId: number, customerName: string): Promise<void> {
  try {
    await ElMessageBox.confirm(
      `删除客户「${customerName}」？历史消费、充值、流水仍会保留。`,
      '删除客户',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  try {
    await deleteCustomer(customerId)
    ElMessage.success('客户已删除')
    // 删除当前页最后一条时回退一页，避免停留在空白页
    if (items.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    await load()
  } catch {
    // 拦截器已提示（403/404 等）
  }
}
</script>

<style scoped>
.customer-list__table {
  width: 100%;
}

/* 手机端卡片列表（plan todo 54） */
.customer-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.customer-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.customer-card__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.customer-card__name {
  font-size: 17px;
  font-weight: 600;
}

.customer-card__phone {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.customer-card__stats {
  display: grid;
  grid-template-columns: 1fr 1fr 1.4fr;
  gap: 8px;
  margin: 10px 0 0;
}

.customer-card__cell dt {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.customer-card__cell dd {
  margin: 2px 0 0;
}

.customer-card__value {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.customer-card__value--small {
  font-size: 13px;
  font-weight: 400;
  color: var(--el-text-color-regular);
}

.customer-card__actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53/54） */
.customer-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

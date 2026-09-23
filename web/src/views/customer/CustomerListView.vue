<template>
  <section class="customer-list">
    <el-card shadow="never">
      <div class="customer-list__toolbar">
        <el-input
          v-model="keyword"
          class="customer-list__search"
          placeholder="搜索手机号 / 姓名 / 微信号"
          clearable
          @keyup.enter="handleSearch"
        />
        <el-select
          v-model="tagFilter"
          class="customer-list__filter"
          placeholder="全部标签"
          clearable
          @change="handleSearch"
        >
          <el-option
            v-for="tag in tags"
            :key="tag.id"
            :label="tag.name"
            :value="tag.id"
          />
        </el-select>
        <el-button
          type="primary"
          @click="handleSearch"
        >
          搜索
        </el-button>
        <el-button @click="handleReset">
          重置
        </el-button>
        <span class="customer-list__spacer" />
        <el-button
          type="primary"
          @click="openCreate"
        >
          新增客户
        </el-button>
      </div>

      <el-table
        v-loading="loading"
        :data="items"
        row-key="id"
        class="customer-list__table"
      >
        <el-table-column
          prop="name"
          label="姓名"
          min-width="110"
        />
        <el-table-column
          prop="phone"
          label="手机号"
          min-width="130"
        />
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
import { ElMessage, ElMessageBox } from 'element-plus'

import { deleteCustomer, listCustomers, listTags } from '@/api'
import type { Customer, CustomerListQuery, Tag } from '@/api'
import ListPagination from '@/components/ListPagination.vue'
import { usePagedList } from '@/composables/usePagedList'
import { GENDER_LABELS } from '@/constants'
import { useAuthStore } from '@/stores/auth'
import { formatCents, formatDateTime } from '@/utils/format'

import CustomerFormDialog from './components/CustomerFormDialog.vue'

// 客户列表（07-UI.md:5-18、84-94）：搜索优先手机号、标签筛选、分页、新增/编辑弹窗。
const auth = useAuthStore()
/** 删除仅 admin（06-BUSINESS-RULES.md §7）；隐藏按钮是 UI 简化，最终边界在后端 RBAC */
const isAdmin = computed(() => auth.role === 'admin')

const keyword = ref('')
const tagFilter = ref<number | null>(null)
const tags = ref<Tag[]>([])

const { items, total, page, pageSize, loading, load } = usePagedList<Customer>(
  (currentPage, currentPageSize) => listCustomers(buildQuery(currentPage, currentPageSize)),
)

const dialogVisible = ref(false)
/** null = 新增；数字 = 编辑对应客户（弹窗打开后拉取详情补全标签） */
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
  handleSearch()
}

function openCreate(): void {
  editingCustomerId.value = null
  dialogVisible.value = true
}

function openEdit(customerId: number): void {
  editingCustomerId.value = customerId
  dialogVisible.value = true
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
.customer-list__toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.customer-list__search {
  width: 260px;
}

.customer-list__filter {
  width: 180px;
}

.customer-list__spacer {
  flex: 1;
}

.customer-list__table {
  width: 100%;
}
</style>

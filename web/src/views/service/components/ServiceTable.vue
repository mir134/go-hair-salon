<template>
  <el-card
    shadow="never"
    class="service-table"
  >
    <ServiceToolbar
      v-model:keyword="keyword"
      v-model:category-filter="categoryFilter"
      v-model:status-filter="statusFilter"
      :categories="categories"
      :mobile="isMobile"
      @create="openCreate"
    />

    <!-- 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119） -->
    <div
      v-if="isMobile"
      v-loading="loading"
      class="service-cards"
    >
      <article
        v-for="service in filteredServices"
        :key="service.id"
        class="service-card"
      >
        <header class="service-card__head">
          <span class="service-card__name">{{ service.name }}</span>
          <span class="service-card__status">
            <span class="service-card__status-label">状态</span>
            <!-- 与表格同一份启停逻辑：切换期间禁用全部开关，失败时开关保持原状态 -->
            <el-switch
              :model-value="service.status === 1"
              :loading="togglingId === service.id"
              :disabled="togglingId !== null"
              @change="(value) => handleStatusChange(service.id, value)"
            />
          </span>
        </header>

        <dl class="service-card__stats">
          <div class="service-card__cell">
            <dt>分类</dt>
            <dd>{{ categoryName(service.category_id, service.category_name) }}</dd>
          </div>
          <div class="service-card__cell">
            <dt>价格（元）</dt>
            <dd class="service-card__value">
              ¥{{ formatCents(service.price_cents) }}
            </dd>
          </div>
          <div class="service-card__cell">
            <dt>时长（分钟）</dt>
            <dd class="service-card__value">
              {{ service.duration_minutes }}
            </dd>
          </div>
        </dl>

        <footer class="service-card__actions">
          <el-button @click="openEdit(service.id)">
            编辑
          </el-button>
          <el-button
            type="danger"
            plain
            @click="handleDelete(service.id)"
          >
            删除
          </el-button>
        </footer>
      </article>

      <el-empty
        v-if="!loading && filteredServices.length === 0"
        description="暂无服务"
      />
    </div>

    <!-- PC：保持原有表格形态与列不变 -->
    <el-table
      v-else
      v-loading="loading"
      :data="filteredServices"
      row-key="id"
    >
      <el-table-column
        prop="name"
        label="名称"
        min-width="140"
      />
      <el-table-column
        label="分类"
        width="140"
      >
        <template #default="{ row }">
          {{ categoryName(row.category_id, row.category_name) }}
        </template>
      </el-table-column>
      <el-table-column
        label="价格（元）"
        width="110"
        align="right"
      >
        <template #default="{ row }">
          {{ formatCents(row.price_cents) }}
        </template>
      </el-table-column>
      <el-table-column
        label="时长（分钟）"
        width="110"
        align="right"
      >
        <template #default="{ row }">
          {{ row.duration_minutes }}
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="110"
      >
        <template #default="{ row }">
          <el-switch
            :model-value="row.status === 1"
            :loading="togglingId === row.id"
            :disabled="togglingId !== null"
            @change="(value) => handleStatusChange(row.id, value)"
          />
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="140"
        align="right"
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
            link
            type="danger"
            @click="handleDelete(row.id)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无服务" />
      </template>
    </el-table>
  </el-card>

  <ServiceFormDialog
    v-model="dialogVisible"
    :service="editing"
    :categories="categories"
    @saved="emit('changed')"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { ApiError, CODE_VALIDATION_FAILED, deleteService, updateService } from '@/api'
import type { Service, ServiceCategory, ServicePayload } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatCents } from '@/utils/format'

import ServiceFormDialog from './ServiceFormDialog.vue'
import ServiceToolbar from './ServiceToolbar.vue'

// 服务表格：前端筛选（服务数量为单店量级）+ 启停开关 + 编辑/删除。
// 数据由父级加载，任何变更后 emit('changed') 触发刷新，避免页面残留旧数据。
// 手机端（<768px）：同一份数据/逻辑渲染卡片列表，PC 保持表格（plan todo 53/54）。
const props = defineProps<{
  services: Service[]
  categories: ServiceCategory[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 服务新增/编辑/启停/删除成功，父级需刷新列表 */
  changed: []
}>()

const isMobile = useIsMobile()
const keyword = ref('')
const categoryFilter = ref<number | null>(null)
const statusFilter = ref<number | null>(null)
const dialogVisible = ref(false)
const editing = ref<Service | null>(null)
/** 正在切换状态的服务 id：期间禁用其他开关，防止并发重复提交 */
const togglingId = ref<number | null>(null)

const filteredServices = computed(() => {
  const text = keyword.value.trim().toLowerCase()
  return props.services.filter((service) => {
    if (categoryFilter.value !== null && service.category_id !== categoryFilter.value) {
      return false
    }
    if (statusFilter.value !== null && service.status !== statusFilter.value) {
      return false
    }
    return text === '' || service.name.toLowerCase().includes(text)
  })
})

const categoryNames = computed(
  () => new Map(props.categories.map((category) => [category.id, category.name])),
)

/** 分类名优先用后端冗余字段，缺失时按 category_id 在已加载分类中解析 */
function categoryName(categoryId: number, categoryNameValue?: string): string {
  return categoryNameValue ?? categoryNames.value.get(categoryId) ?? '—'
}

/** el-table 插槽的 row 是 DefaultRow（无法参数化），因此只传 id、在 props 中解析实体 */
function findService(id: number): Service | null {
  return props.services.find((service) => service.id === id) ?? null
}

function openCreate(): void {
  editing.value = null
  dialogVisible.value = true
}

function openEdit(id: number): void {
  editing.value = findService(id)
  dialogVisible.value = true
}

function toPayload(service: Service, status: number): ServicePayload {
  return {
    category_id: service.category_id,
    name: service.name,
    price_cents: service.price_cents,
    duration_minutes: service.duration_minutes,
    status,
    remark: service.remark,
  }
}

/** 启停开关：成功后刷新列表；失败时开关保持原状态（:model-value 未被改动） */
async function handleStatusChange(id: number, value: string | number | boolean): Promise<void> {
  const service = findService(id)
  if (service === null) {
    return
  }
  const enabled = value === true
  togglingId.value = service.id
  try {
    await updateService(service.id, toPayload(service, enabled ? 1 : 0))
    ElMessage.success(enabled ? '服务已启用' : '服务已停用')
    emit('changed')
  } catch {
    // 拦截器已提示（403 等）
  } finally {
    togglingId.value = null
  }
}

async function handleDelete(id: number): Promise<void> {
  const service = findService(id)
  if (service === null) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `删除服务「${service.name}」？历史订单中的服务快照不受影响。`,
      '删除服务',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  try {
    await deleteService(service.id)
    ElMessage.success('服务已删除')
    emit('changed')
  } catch (error) {
    if (error instanceof ApiError && error.code === CODE_VALIDATION_FAILED) {
      await handleDeleteConflict(service, error)
    }
    // 其余错误（403/404/网络）拦截器已提示
  }
}

/** 422 = 服务已产生订单禁止删除（06-BUSINESS-RULES.md:16）：展示后端文案并引导停用 */
async function handleDeleteConflict(service: Service, error: ApiError): Promise<void> {
  if (service.status === 0) {
    // 已是停用状态：没有可引导的动作，只把后端文案展示清楚
    void ElMessageBox.alert(error.message, '无法删除服务', {
      type: 'warning',
      confirmButtonText: '知道了',
    }).catch(() => undefined)
    return
  }
  try {
    await ElMessageBox.confirm(`${error.message}，是否改为停用该服务？`, '无法删除服务', {
      type: 'warning',
      confirmButtonText: '停用',
      cancelButtonText: '取消',
    })
  } catch {
    return // 用户放弃停用
  }
  try {
    await updateService(service.id, toPayload(service, 0))
    ElMessage.success('服务已停用')
    emit('changed')
  } catch {
    // 拦截器已提示
  }
}
</script>

<style scoped>
/* 手机端卡片列表（plan todo 53/54、07-UI.md:119） */
.service-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.service-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.service-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.service-card__name {
  font-size: 16px;
  font-weight: 600;
  word-break: break-all;
}

.service-card__status {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.service-card__status-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.service-card__stats {
  display: grid;
  grid-template-columns: 1.4fr 1fr 1fr;
  gap: 8px;
  margin: 10px 0 0;
}

.service-card__cell dt {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.service-card__cell dd {
  margin: 2px 0 0;
}

.service-card__value {
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.service-card__actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53/54） */
.service-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

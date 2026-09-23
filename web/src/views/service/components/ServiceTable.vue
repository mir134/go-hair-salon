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
      @create="openCreate"
    />

    <el-table
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
import { formatCents } from '@/utils/format'

import ServiceFormDialog from './ServiceFormDialog.vue'
import ServiceToolbar from './ServiceToolbar.vue'

// 服务表格：前端筛选（服务数量为单店量级）+ 启停开关 + 编辑/删除。
// 数据由父级加载，任何变更后 emit('changed') 触发刷新，避免页面残留旧数据。
const props = defineProps<{
  services: Service[]
  categories: ServiceCategory[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 服务新增/编辑/启停/删除成功，父级需刷新列表 */
  changed: []
}>()

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

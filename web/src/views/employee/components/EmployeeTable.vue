<template>
  <el-card
    shadow="never"
    class="employee-table"
  >
    <!-- 工具栏：手机端纵向排列 + 大控件（plan todo 53/54、07-UI.md:87,114） -->
    <div
      class="employee-table__toolbar"
      :class="{ 'employee-table__toolbar--mobile': isMobile }"
    >
      <el-select
        v-model="statusFilter"
        class="employee-table__filter"
        :size="isMobile ? 'large' : 'default'"
      >
        <el-option
          label="全部状态"
          value="all"
        />
        <el-option
          label="启用"
          value="enabled"
        />
        <el-option
          label="停用"
          value="disabled"
        />
      </el-select>
      <el-button
        type="primary"
        :size="isMobile ? 'large' : 'default'"
        @click="openCreate"
      >
        新增员工
      </el-button>
    </div>

    <!-- 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119、02-AGENTS.md:92） -->
    <div
      v-if="isMobile"
      v-loading="loading"
      class="employee-cards"
    >
      <article
        v-for="employee in filteredEmployees"
        :key="employee.id"
        class="employee-card"
      >
        <header class="employee-card__head">
          <span class="employee-card__name">{{ employee.name }}</span>
          <el-tag
            :type="employee.status === EMPLOYEE_STATUS_ENABLED ? 'success' : 'info'"
            size="small"
          >
            {{ employee.status === EMPLOYEE_STATUS_ENABLED ? '启用' : '停用' }}
          </el-tag>
        </header>

        <dl class="employee-card__grid">
          <div class="employee-card__cell">
            <dt>手机号</dt>
            <dd>{{ employee.phone !== '' ? employee.phone : '—' }}</dd>
          </div>
          <div class="employee-card__cell">
            <dt>职位</dt>
            <dd>{{ employee.position !== '' ? employee.position : '—' }}</dd>
          </div>
          <div class="employee-card__cell">
            <dt>入职日期</dt>
            <dd>{{ formatJoinedAt(employee.joined_at) }}</dd>
          </div>
        </dl>

        <footer class="employee-card__actions">
          <el-button @click="openEdit(employee.id)">
            编辑
          </el-button>
          <el-button
            v-if="employee.status === EMPLOYEE_STATUS_ENABLED"
            type="danger"
            plain
            :loading="togglingId === employee.id"
            @click="handleDisable(employee.id)"
          >
            停用
          </el-button>
          <el-button
            v-else
            type="primary"
            :loading="togglingId === employee.id"
            @click="handleEnable(employee.id)"
          >
            启用
          </el-button>
        </footer>
      </article>

      <el-empty
        v-if="!loading && filteredEmployees.length === 0"
        description="暂无员工"
      />
    </div>

    <el-table
      v-else
      v-loading="loading"
      :data="filteredEmployees"
      row-key="id"
    >
      <el-table-column
        prop="name"
        label="姓名"
        min-width="120"
      />
      <el-table-column
        label="手机号"
        width="150"
      >
        <template #default="{ row }">
          {{ row.phone !== '' ? row.phone : '—' }}
        </template>
      </el-table-column>
      <el-table-column
        label="职位"
        width="140"
      >
        <template #default="{ row }">
          {{ row.position !== '' ? row.position : '—' }}
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="100"
      >
        <template #default="{ row }">
          <el-tag
            :type="row.status === EMPLOYEE_STATUS_ENABLED ? 'success' : 'info'"
            size="small"
          >
            {{ row.status === EMPLOYEE_STATUS_ENABLED ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="入职日期"
        width="130"
      >
        <template #default="{ row }">
          {{ formatJoinedAt(row.joined_at) }}
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="160"
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
            v-if="row.status === EMPLOYEE_STATUS_ENABLED"
            link
            type="danger"
            :loading="togglingId === row.id"
            @click="handleDisable(row.id)"
          >
            停用
          </el-button>
          <el-button
            v-else
            link
            type="primary"
            :loading="togglingId === row.id"
            @click="handleEnable(row.id)"
          >
            启用
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无员工" />
      </template>
    </el-table>
  </el-card>

  <EmployeeFormDialog
    v-model="dialogVisible"
    :employee="editing"
    @saved="emit('changed')"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import {
  EMPLOYEE_STATUS_DISABLED,
  EMPLOYEE_STATUS_ENABLED,
  disableEmployee,
  updateEmployee,
} from '@/api'
import type { Employee, EmployeePayload } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatDay } from '@/utils/format'

import EmployeeFormDialog from './EmployeeFormDialog.vue'

// 员工表格（07-UI.md:13、plan todo 40）：列表 + 新增/编辑 + 停用/启用。
// DELETE /employees/:id 的语义是停用（行保留、历史关联不失效，06 §11:120-125）；
// 已停用员工不再出现在新订单的员工选择中（见 OrderEmployeeCard），但历史记录仍可读。
// 手机端（<768px）：卡片列表替代表格、工具栏纵向大控件（plan todo 53/54、07-UI.md:119）。
const props = defineProps<{
  employees: Employee[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 员工新增/编辑/停用/启用成功，父级需刷新列表 */
  changed: []
}>()

const isMobile = useIsMobile()
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const dialogVisible = ref(false)
const editing = ref<Employee | null>(null)
/** 正在切换状态的员工 id：期间禁用按钮，防止并发重复提交 */
const togglingId = ref<number | null>(null)

const filteredEmployees = computed(() =>
  props.employees.filter((employee) => {
    if (statusFilter.value === 'enabled') {
      return employee.status === EMPLOYEE_STATUS_ENABLED
    }
    if (statusFilter.value === 'disabled') {
      return employee.status === EMPLOYEE_STATUS_DISABLED
    }
    return true
  }),
)

/** el-table 插槽的 row 是 DefaultRow（无法参数化），因此只传 id、在 props 中解析实体 */
function findEmployee(id: number): Employee | null {
  return props.employees.find((employee) => employee.id === id) ?? null
}

/** joined_at 是日期（后端按 UTC 零点存储），展示为本地 YYYY-MM-DD */
function formatJoinedAt(value: string | null): string {
  if (value === null || value === '') {
    return '—'
  }
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : formatDay(date)
}

function openCreate(): void {
  editing.value = null
  dialogVisible.value = true
}

function openEdit(id: number): void {
  editing.value = findEmployee(id)
  dialogVisible.value = true
}

/** 以行数据构造完整请求体：后端 PUT 全量更新档案字段，未提供的字段会被清空 */
function toPayload(employee: Employee, status: number): EmployeePayload {
  return {
    name: employee.name,
    phone: employee.phone,
    avatar: employee.avatar,
    position: employee.position,
    status,
    joined_at: employee.joined_at,
    remark: employee.remark,
  }
}

/** 停用：危险操作，二次确认后调 DELETE（后端语义=停用 status=0，行保留） */
async function handleDisable(id: number): Promise<void> {
  const employee = findEmployee(id)
  if (employee === null) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `停用员工「${employee.name}」？停用后该员工不会出现在新订单的选择列表中，历史订单与账号关联保留。`,
      '停用员工',
      { type: 'warning', confirmButtonText: '停用', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  togglingId.value = employee.id
  try {
    await disableEmployee(employee.id)
    ElMessage.success('员工已停用')
    emit('changed')
  } catch {
    // 拦截器已提示（403/404/网络）
  } finally {
    togglingId.value = null
  }
}

/** 启用：恢复停用员工（PUT status=1），可再次被新订单选择 */
async function handleEnable(id: number): Promise<void> {
  const employee = findEmployee(id)
  if (employee === null) {
    return
  }
  togglingId.value = employee.id
  try {
    await updateEmployee(employee.id, toPayload(employee, EMPLOYEE_STATUS_ENABLED))
    ElMessage.success('员工已启用')
    emit('changed')
  } catch {
    // 拦截器已提示（403/404/网络）
  } finally {
    togglingId.value = null
  }
}
</script>

<style scoped>
.employee-table__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.employee-table__filter {
  width: 160px;
}

/* 手机端：整行布局，触控目标 ≥44px（plan todo 53/54） */
.employee-table__toolbar--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.employee-table__toolbar--mobile .employee-table__filter {
  width: 100%;
}

.employee-table__toolbar--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}

/* 手机端卡片列表（plan todo 53/54） */
.employee-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.employee-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.employee-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.employee-card__name {
  font-size: 17px;
  font-weight: 600;
}

.employee-card__grid {
  display: grid;
  grid-template-columns: 1fr 1fr 1.4fr;
  gap: 8px;
  margin: 10px 0 0;
}

.employee-card__cell dt {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.employee-card__cell dd {
  margin: 2px 0 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.employee-card__actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53/54） */
.employee-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

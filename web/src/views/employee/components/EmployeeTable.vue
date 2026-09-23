<template>
  <el-card
    shadow="never"
    class="employee-table"
  >
    <div class="employee-table__toolbar">
      <el-select
        v-model="statusFilter"
        class="employee-table__filter"
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
        @click="openCreate"
      >
        新增员工
      </el-button>
    </div>

    <el-table
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
import { formatDay } from '@/utils/format'

import EmployeeFormDialog from './EmployeeFormDialog.vue'

// 员工表格（07-UI.md:13、plan todo 40）：列表 + 新增/编辑 + 停用/启用。
// DELETE /employees/:id 的语义是停用（行保留、历史关联不失效，06 §11:120-125）；
// 已停用员工不再出现在新订单的员工选择中（见 OrderEmployeeCard），但历史记录仍可读。
const props = defineProps<{
  employees: Employee[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 员工新增/编辑/停用/启用成功，父级需刷新列表 */
  changed: []
}>()

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
</style>

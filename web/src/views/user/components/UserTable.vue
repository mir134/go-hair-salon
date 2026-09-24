<template>
  <el-card
    shadow="never"
    class="user-table"
  >
    <!-- 工具栏：手机端纵向排列 + 大控件（plan todo 53/54、07-UI.md:87,114） -->
    <div
      class="user-table__toolbar"
      :class="{ 'user-table__toolbar--mobile': isMobile }"
    >
      <el-select
        v-model="statusFilter"
        class="user-table__filter"
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
        @click="createVisible = true"
      >
        新增用户
      </el-button>
    </div>

    <!-- 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119、02-AGENTS.md:92） -->
    <div
      v-if="isMobile"
      v-loading="loading"
      class="user-cards"
    >
      <article
        v-for="user in filteredUsers"
        :key="user.id"
        class="user-card"
      >
        <header class="user-card__head">
          <span class="user-card__name">{{ user.username }}</span>
          <el-tag
            :type="user.role === 'admin' ? 'warning' : 'info'"
            size="small"
          >
            {{ roleLabel(user.role) }}
          </el-tag>
        </header>

        <div class="user-card__status">
          <span class="user-card__status-label">状态</span>
          <el-tag
            :type="user.status === USER_STATUS_ENABLED ? 'success' : 'info'"
            size="small"
          >
            {{ user.status === USER_STATUS_ENABLED ? '启用' : '停用' }}
          </el-tag>
        </div>

        <footer class="user-card__actions">
          <el-button @click="openPasswordDialog(user.id)">
            重置密码
          </el-button>
          <el-button
            v-if="user.status === USER_STATUS_ENABLED"
            type="danger"
            plain
            :loading="togglingId === user.id"
            @click="handleStatus(user.id, USER_STATUS_DISABLED)"
          >
            停用
          </el-button>
          <el-button
            v-else
            type="primary"
            :loading="togglingId === user.id"
            @click="handleStatus(user.id, USER_STATUS_ENABLED)"
          >
            启用
          </el-button>
        </footer>
      </article>

      <el-empty
        v-if="!loading && filteredUsers.length === 0"
        description="暂无用户"
      />
    </div>

    <el-table
      v-else
      v-loading="loading"
      :data="filteredUsers"
      row-key="id"
    >
      <el-table-column
        prop="username"
        label="用户名"
        min-width="140"
      />
      <el-table-column
        label="角色"
        width="110"
      >
        <template #default="{ row }">
          <el-tag
            :type="row.role === 'admin' ? 'warning' : 'info'"
            size="small"
          >
            {{ roleLabel(row.role) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="状态"
        width="100"
      >
        <template #default="{ row }">
          <el-tag
            :type="row.status === USER_STATUS_ENABLED ? 'success' : 'info'"
            size="small"
          >
            {{ row.status === USER_STATUS_ENABLED ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column
        label="关联员工"
        min-width="140"
      >
        <template #default="{ row }">
          {{ employeeName(row.employee_id) }}
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="180"
        align="right"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="openPasswordDialog(row.id)"
          >
            重置密码
          </el-button>
          <el-button
            v-if="row.status === USER_STATUS_ENABLED"
            link
            type="danger"
            :loading="togglingId === row.id"
            @click="handleStatus(row.id, USER_STATUS_DISABLED)"
          >
            停用
          </el-button>
          <el-button
            v-else
            link
            type="primary"
            :loading="togglingId === row.id"
            @click="handleStatus(row.id, USER_STATUS_ENABLED)"
          >
            启用
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无用户" />
      </template>
    </el-table>
  </el-card>

  <UserCreateDialog
    v-model="createVisible"
    :employees="employees"
    @saved="emit('changed')"
  />
  <UserPasswordDialog
    v-model="passwordVisible"
    :user="passwordTarget"
    @saved="emit('changed')"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { USER_STATUS_DISABLED, USER_STATUS_ENABLED, updateUser } from '@/api'
import type { Employee, User, UserRole } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import UserCreateDialog from './UserCreateDialog.vue'
import UserPasswordDialog from './UserPasswordDialog.vue'

// 用户表格（plan todo 40，D6 决议）：列表 + 创建用户 + 重置密码 + 停用/启用。
// 后端没有用户删除接口，因此不提供删除；停用走 PUT status=0（行保留），
// 停用后旧 token 立即失效（后端 JWT 中间件每次请求实时查库）。
// 手机端（<768px）：卡片列表替代表格、工具栏纵向大控件（plan todo 53/54、07-UI.md:119）。
const props = defineProps<{
  users: User[]
  employees: Employee[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 用户创建/重置密码/停用/启用成功，父级需刷新列表 */
  changed: []
}>()

const ROLE_LABELS: Readonly<Record<UserRole, string>> = { admin: '管理员', staff: '店员' }

const isMobile = useIsMobile()
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const createVisible = ref(false)
const passwordVisible = ref(false)
const passwordTarget = ref<User | null>(null)
/** 正在切换状态的用户 id：期间禁用按钮，防止并发重复提交 */
const togglingId = ref<number | null>(null)

const filteredUsers = computed(() =>
  props.users.filter((user) => {
    if (statusFilter.value === 'enabled') {
      return user.status === USER_STATUS_ENABLED
    }
    if (statusFilter.value === 'disabled') {
      return user.status === USER_STATUS_DISABLED
    }
    return true
  }),
)

const employeeNames = computed(
  () => new Map(props.employees.map((employee) => [employee.id, employee.name])),
)

/** el-table 插槽的 row 是 DefaultRow（无法参数化），因此只传 id、在 props 中解析实体 */
function findUser(id: number): User | null {
  return props.users.find((user) => user.id === id) ?? null
}

/** 角色文案：admin/staff 之外的未知值原样展示（防御后端未来扩展） */
function roleLabel(role: string): string {
  return ROLE_LABELS[role as UserRole] ?? role
}

/** 关联员工名：员工被停用后仍展示历史关联名；未关联/已不存在显示「—」 */
function employeeName(employeeId: number | null): string {
  if (employeeId === null) {
    return '—'
  }
  return employeeNames.value.get(employeeId) ?? '—'
}

function openPasswordDialog(id: number): void {
  passwordTarget.value = findUser(id)
  passwordVisible.value = true
}

/** 停用/启用：二次确认后再提交；停用会使该用户所有登录态立即失效 */
async function handleStatus(id: number, status: number): Promise<void> {
  const user = findUser(id)
  if (user === null) {
    return
  }
  const disabling = status === USER_STATUS_DISABLED
  try {
    await ElMessageBox.confirm(
      disabling
        ? `停用用户「${user.username}」？停用后该账号立即无法登录（已登录的会话也会失效），历史操作记录保留。`
        : `启用用户「${user.username}」？启用后该账号可重新登录。`,
      disabling ? '停用用户' : '启用用户',
      {
        type: 'warning',
        confirmButtonText: disabling ? '停用' : '启用',
        cancelButtonText: '取消',
      },
    )
  } catch {
    return // 用户取消
  }
  togglingId.value = user.id
  try {
    await updateUser(user.id, { status })
    ElMessage.success(disabling ? '用户已停用' : '用户已启用')
    emit('changed')
  } catch {
    // 拦截器已提示（403/404/网络）
  } finally {
    togglingId.value = null
  }
}
</script>

<style scoped>
.user-table__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.user-table__filter {
  width: 160px;
}

/* 手机端：整行布局，触控目标 ≥44px（plan todo 53/54） */
.user-table__toolbar--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.user-table__toolbar--mobile .user-table__filter {
  width: 100%;
}

.user-table__toolbar--mobile .el-button {
  min-height: 44px;
  margin-left: 0;
}

/* 手机端卡片列表（plan todo 53/54） */
.user-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.user-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.user-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.user-card__name {
  font-size: 17px;
  font-weight: 600;
  word-break: break-all;
}

.user-card__status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.user-card__status-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.user-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53/54） */
.user-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

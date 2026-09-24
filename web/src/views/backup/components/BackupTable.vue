<template>
  <!-- 手机端（<768px）：卡片列表替代表格，恢复入口保留在卡片内（plan todo 53/54、07-UI.md:119） -->
  <div
    v-if="isMobile"
    v-loading="loading"
    class="backup-cards"
  >
    <article
      v-for="backup in items"
      :key="backup.name"
      class="backup-card"
    >
      <header class="backup-card__head">
        <span class="backup-card__time">{{ formatDateTime(backup.created_at) }}</span>
        <span class="backup-card__size">{{ formatBytes(backup.size_bytes) }}</span>
      </header>
      <p class="backup-card__name">
        {{ backup.name }}
      </p>

      <!-- 恢复是破坏性操作：二次确认由父级 BackupRestoreDialog 负责（不可省略） -->
      <footer class="backup-card__actions">
        <el-button
          type="danger"
          plain
          @click="emit('restore', backup)"
        >
          恢复
        </el-button>
      </footer>
    </article>

    <el-empty
      v-if="!loading && items.length === 0"
      description="暂无备份，点击「手动备份」立即创建"
    />
  </div>

  <el-table
    v-else
    v-loading="loading"
    :data="items"
    row-key="name"
  >
    <el-table-column
      prop="name"
      label="文件名"
      min-width="240"
      show-overflow-tooltip
    />
    <el-table-column
      label="大小"
      width="110"
    >
      <template #default="{ row }">
        {{ formatBytes(row.size_bytes) }}
      </template>
    </el-table-column>
    <el-table-column
      label="创建时间"
      width="170"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      label="操作"
      width="110"
      fixed="right"
    >
      <template #default="{ row }">
        <el-button
          link
          type="danger"
          @click="handleRestore(row)"
        >
          恢复
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无备份，点击「手动备份」立即创建" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { Backup } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatBytes } from '@/utils/backup'
import { formatDateTime } from '@/utils/format'

// 备份列表表格（plan todo 52）：文件名 / 大小 / 创建时间 + 恢复入口。
// 列表不提供删除：保留策略由后端（最近 7 份）统一管理，备份文件是数据安全网。
// 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119），展示 备份时间/大小/文件名
// 与「恢复」入口；PC 端表格列不变。
defineProps<{
  items: Backup[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 请求恢复某个备份（父级打开二次确认弹窗） */
  restore: [backup: Backup]
}>()

const isMobile = useIsMobile()

/** el-table 插槽 row 为库的宽松类型，在此收敛为 Backup（与 LogTable 一致） */
function handleRestore(row: unknown): void {
  emit('restore', row as Backup)
}
</script>

<style scoped>
/* 手机端备份卡片列表（plan todo 53/54） */
.backup-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.backup-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.backup-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.backup-card__time {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.backup-card__size {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.backup-card__name {
  margin: 8px 0 0;
  color: var(--el-text-color-regular);
  font-size: 13px;
  word-break: break-all;
}

.backup-card__actions {
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53/54） */
.backup-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

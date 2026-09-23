<template>
  <section class="backup-list">
    <div class="backup-list__head">
      <div>
        <span class="backup-list__title">备份与恢复</span>
        <span class="backup-list__hint">
          仅管理员；后端自动保留最近 7 份，恢复前会自动生成当前数据的安全备份
        </span>
      </div>
      <el-button
        type="primary"
        :loading="creating"
        @click="handleCreate"
      >
        手动备份
      </el-button>
    </div>

    <el-card shadow="never">
      <BackupTable
        :items="items"
        :loading="loading"
        @restore="openRestore"
      />
    </el-card>

    <BackupRestoreDialog
      v-model="dialogVisible"
      :backup="selected"
      @restored="handleRestored"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { createBackup, listBackups } from '@/api'
import type { Backup, RestoreResult } from '@/api'

import BackupRestoreDialog from './components/BackupRestoreDialog.vue'
import BackupTable from './components/BackupTable.vue'

// 备份与恢复页（plan todo 52、04-API.md:245-256）：仅 admin 菜单/路由（守卫）；
// 列表（文件名/大小/创建时间）+ 手动备份 + 恢复（二次确认弹窗）。
// 备份接口不提供分页（GET /backups 返回全部，含 total），因此使用简单加载器而非 usePagedList。
const items = ref<Backup[]>([])
const loading = ref(false)
const creating = ref(false)
const selected = ref<Backup | null>(null)
const dialogVisible = ref(false)

onMounted(() => {
  void load()
})

async function load(): Promise<void> {
  loading.value = true
  try {
    items.value = (await listBackups()).items
  } catch {
    // 拦截器已提示（401/403/网络）；保留当前列表
  } finally {
    loading.value = false
  }
}

/** 手动备份：成功后刷新列表（新备份出现在最前） */
async function handleCreate(): Promise<void> {
  creating.value = true
  try {
    const created = await createBackup()
    ElMessage.success(`备份完成：${created.name}`)
    await load()
  } catch {
    // 拦截器已提示（备份目录不可写等由后端返回明确错误）
  } finally {
    creating.value = false
  }
}

function openRestore(backup: Backup): void {
  selected.value = backup
  dialogVisible.value = true
}

/** 恢复成功：刷新列表（恢复前安全备份会作为新条目出现） */
function handleRestored(result: RestoreResult): void {
  ElMessage.success(`已恢复到 ${result.restored}`)
  void load()
}
</script>

<style scoped>
.backup-list__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.backup-list__title {
  font-size: 16px;
  font-weight: 600;
}

.backup-list__hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

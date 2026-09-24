<template>
  <section class="tag-list">
    <el-card
      shadow="never"
      class="tag-panel"
    >
      <template #header>
        <div
          class="tag-panel__header"
          :class="{ 'tag-panel__header--mobile': isMobile }"
        >
          <span class="tag-panel__title">标签管理</span>
          <el-button
            type="primary"
            :size="isMobile ? 'large' : 'small'"
            @click="openCreate"
          >
            新增标签
          </el-button>
        </div>
      </template>

      <!-- 手机端（<768px）：卡片列表替代表格（plan todo 53/54、07-UI.md:119） -->
      <div
        v-if="isMobile"
        v-loading="loading"
        class="tag-cards"
      >
        <article
          v-for="tag in tags"
          :key="tag.id"
          class="tag-card"
        >
          <span class="tag-card__name">{{ tag.name }}</span>
          <footer class="tag-card__actions">
            <el-button @click="openEdit(tag.id)">
              编辑
            </el-button>
            <el-button
              type="danger"
              plain
              @click="handleDelete(tag.id)"
            >
              删除
            </el-button>
          </footer>
        </article>

        <el-empty
          v-if="!loading && tags.length === 0"
          description="暂无标签"
        />
      </div>

      <el-table
        v-else
        v-loading="loading"
        :data="tags"
        row-key="id"
        size="small"
      >
        <el-table-column
          prop="name"
          label="名称"
          min-width="160"
        />
        <el-table-column
          label="操作"
          width="140"
          align="right"
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
          <el-empty description="暂无标签" />
        </template>
      </el-table>
    </el-card>

    <TagFormDialog
      v-model="dialogVisible"
      :tag="editing"
      @saved="loadTags"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { deleteTag, listTags } from '@/api'
import type { Tag } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import TagFormDialog from './components/TagFormDialog.vue'

// 标签管理页（07-UI.md:11-14）：仅 admin 可访问（router meta.roles + 导航过滤），
// 后端 POST/PUT/DELETE /tags 仍是最终权限边界。
// 标签是「新增客户 → 标签」下拉的数据来源；此前无维护入口，导致全新系统标签为空。
// 手机端（<768px）：卡片列表替代表格 + 底部抽屉表单（plan todo 53/54）。
const isMobile = useIsMobile()
const tags = ref<Tag[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref<Tag | null>(null)

onMounted(() => {
  void loadTags()
})

async function loadTags(): Promise<void> {
  loading.value = true
  try {
    tags.value = await listTags()
  } catch {
    // 拦截器已提示；保留原列表
  } finally {
    loading.value = false
  }
}

function openCreate(): void {
  editing.value = null
  dialogVisible.value = true
}

/** el-table 插槽的 row 是 DefaultRow（无法参数化），因此只传 id、在 props 中解析实体 */
function findTag(id: number): Tag | null {
  return tags.value.find((tag) => tag.id === id) ?? null
}

function openEdit(id: number): void {
  editing.value = findTag(id)
  dialogVisible.value = true
}

async function handleDelete(id: number): Promise<void> {
  const tag = findTag(id)
  if (tag === null) {
    return
  }
  try {
    await ElMessageBox.confirm(
      `删除标签「${tag.name}」？删除后不再出现在标签选择中，历史客户标签记录仍会保留。`,
      '删除标签',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
      },
    )
  } catch {
    return // 用户取消
  }
  try {
    await deleteTag(tag.id)
    ElMessage.success('标签已删除')
    await loadTags()
  } catch {
    // 403/404/网络等错误拦截器已提示，不静默失败
  }
}
</script>

<style scoped>
.tag-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.tag-panel__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.tag-panel__title {
  font-size: 15px;
  font-weight: 600;
}

/* 手机端卡片列表（plan todo 53/54）：与 OrderCardList/CustomerListView 卡片结构一致 */
.tag-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.tag-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.tag-card__name {
  font-size: 15px;
  font-weight: 600;
  word-break: break-all;
}

.tag-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53、07-UI.md:87） */
.tag-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}

/* 手机端：头部「新增标签」同步加大到 ≥44px（07-UI.md:87） */
.tag-panel__header--mobile .el-button {
  min-height: 44px;
}
</style>

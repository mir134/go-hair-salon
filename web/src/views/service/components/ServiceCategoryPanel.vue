<template>
  <el-card
    shadow="never"
    class="category-panel"
  >
    <template #header>
      <div class="category-panel__header">
        <span class="category-panel__title">服务分类</span>
        <el-button
          type="primary"
          size="small"
          @click="openCreate"
        >
          新增分类
        </el-button>
      </div>
    </template>

    <el-table
      v-loading="loading"
      :data="sortedCategories"
      row-key="id"
      size="small"
    >
      <el-table-column
        prop="name"
        label="名称"
        min-width="140"
      />
      <el-table-column
        prop="sort"
        label="排序"
        width="80"
      />
      <el-table-column
        label="状态"
        width="90"
      >
        <template #default="{ row }">
          <el-tag
            :type="row.status === 1 ? 'success' : 'info'"
            size="small"
          >
            {{ row.status === 1 ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
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
        <el-empty description="暂无分类" />
      </template>
    </el-table>
  </el-card>

  <ServiceCategoryDialog
    v-model="dialogVisible"
    :category="editing"
    @saved="emit('changed')"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { ApiError, CODE_VALIDATION_FAILED, deleteServiceCategory } from '@/api'
import type { ServiceCategory } from '@/api'

import ServiceCategoryDialog from './ServiceCategoryDialog.vue'

// 分类面板：列表 + 新增/编辑/删除。数据由父级加载，变更后 emit('changed') 触发刷新。
const props = defineProps<{
  categories: ServiceCategory[]
  loading: boolean
}>()

const emit = defineEmits<{
  /** 分类新增/编辑/删除成功，父级需刷新分类与服务列表 */
  changed: []
}>()

const dialogVisible = ref(false)
const editing = ref<ServiceCategory | null>(null)

/** sort 升序；同 sort 按 id 稳定排序（后端 GET 已排序，这里兜底展示顺序） */
const sortedCategories = computed(() =>
  [...props.categories].sort((a, b) => a.sort - b.sort || a.id - b.id),
)

function openCreate(): void {
  editing.value = null
  dialogVisible.value = true
}

/** el-table 插槽的 row 是 DefaultRow（无法参数化），因此只传 id、在 props 中解析实体 */
function findCategory(id: number): ServiceCategory | null {
  return props.categories.find((category) => category.id === id) ?? null
}

function openEdit(id: number): void {
  editing.value = findCategory(id)
  dialogVisible.value = true
}

async function handleDelete(id: number): Promise<void> {
  const category = findCategory(id)
  if (category === null) {
    return
  }
  try {
    await ElMessageBox.confirm(`删除分类「${category.name}」？`, '删除分类', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return // 用户取消
  }
  try {
    await deleteServiceCategory(category.id)
    ElMessage.success('分类已删除')
    emit('changed')
  } catch (error) {
    if (error instanceof ApiError && error.code === CODE_VALIDATION_FAILED) {
      // 分类下仍有服务（server/internal/service/servicecategory.go 返回 422）：
      // 后端文案已含"先转移或删除服务"的指引，这里原样展示，不静默失败
      void ElMessageBox.alert(error.message, '无法删除分类', {
        type: 'warning',
        confirmButtonText: '知道了',
      }).catch(() => undefined)
    }
    // 其余错误（403/404/网络）拦截器已提示，不静默：用户可见
  }
}
</script>

<style scoped>
.category-panel__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.category-panel__title {
  font-size: 15px;
  font-weight: 600;
}
</style>

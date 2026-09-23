<template>
  <div class="profile">
    <div class="profile__head">
      <div class="profile__identity">
        <span class="profile__name">{{ customer.name }}</span>
        <template v-if="customer.tags.length > 0">
          <el-tag
            v-for="tag in customer.tags"
            :key="tag.id"
            :type="tag.deleted ? 'info' : 'primary'"
            size="small"
          >
            {{ tag.deleted ? `${tag.name}（已删除）` : tag.name }}
          </el-tag>
        </template>
        <el-tag
          v-else
          type="info"
          size="small"
        >
          无标签
        </el-tag>
      </div>
      <el-button
        :size="isMobile ? 'large' : 'default'"
        @click="openTagEditor"
      >
        编辑标签
      </el-button>
    </div>

    <!-- 手机端：余额/积分大字展示（核心核对信息，plan todo 54、07-UI.md:44-46） -->
    <div
      v-if="isMobile"
      class="profile__hero"
    >
      <div class="profile__hero-cell">
        <span class="profile__hero-label">余额（元）</span>
        <span class="profile__hero-value">{{ formatCents(customer.balance_cents) }}</span>
      </div>
      <div class="profile__hero-cell">
        <span class="profile__hero-label">积分</span>
        <span class="profile__hero-value">{{ customer.points }}</span>
      </div>
    </div>

    <el-descriptions
      :column="isMobile ? 1 : 4"
      border
    >
      <el-descriptions-item label="手机号">
        {{ customer.phone !== '' ? customer.phone : '—' }}
      </el-descriptions-item>
      <el-descriptions-item label="性别">
        {{ GENDER_LABELS[customer.gender] ?? '—' }}
      </el-descriptions-item>
      <el-descriptions-item label="生日">
        {{ customer.birthday ?? '—' }}
      </el-descriptions-item>
      <el-descriptions-item label="微信号">
        {{ customer.wechat !== '' ? customer.wechat : '—' }}
      </el-descriptions-item>
      <el-descriptions-item
        v-if="!isMobile"
        label="余额（元）"
      >
        {{ formatCents(customer.balance_cents) }}
      </el-descriptions-item>
      <el-descriptions-item
        v-if="!isMobile"
        label="积分"
      >
        {{ customer.points }}
      </el-descriptions-item>
      <el-descriptions-item label="累计消费（元）">
        {{ formatCents(customer.total_spent_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="最近到店">
        {{ formatDateTime(customer.last_visit_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="最近消费">
        {{ formatDateTime(latestOrderAt) }}
      </el-descriptions-item>
      <el-descriptions-item label="来源">
        {{ customer.source !== '' ? customer.source : '—' }}
      </el-descriptions-item>
      <el-descriptions-item
        label="备注"
        :span="2"
      >
        {{ customer.remark !== '' ? customer.remark : '—' }}
      </el-descriptions-item>
    </el-descriptions>

    <el-dialog
      v-model="tagDialogVisible"
      title="编辑标签"
      :width="isMobile ? '92%' : '420px'"
    >
      <el-select
        v-model="selectedTagIds"
        class="profile__tag-select"
        multiple
        clearable
        placeholder="选择标签"
      >
        <el-option
          v-for="tag in allTags"
          :key="tag.id"
          :label="tag.name"
          :value="tag.id"
        />
      </el-select>
      <template #footer>
        <el-button @click="tagDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="savingTags"
          @click="handleSaveTags"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'

import { listTags } from '@/api'
import type { Customer, Tag } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { GENDER_LABELS } from '@/constants'
import { editableTagIds, syncCustomerTags } from '@/utils/customerProfile'
import { formatCents, formatDateTime } from '@/utils/format'

// 客户档案头部（07-UI.md:44-46）+ 标签编辑入口（挂/摘接口差集同步）。
// 手机端（<768px）：余额/积分大字摘要 + 单列描述列表（plan todo 54）。
const props = defineProps<{
  customer: Customer
  latestOrderAt: string | null
}>()

const emit = defineEmits<{
  /** 标签保存成功：父组件重新拉取客户 */
  updated: []
}>()

const isMobile = useIsMobile()

const tagDialogVisible = ref(false)
const allTags = ref<Tag[]>([])
const selectedTagIds = ref<number[]>([])
/** 打开编辑器时的未删除标签集合（历史软删除标签不参与差集） */
const editableBefore = ref<number[]>([])
const savingTags = ref(false)

async function openTagEditor(): Promise<void> {
  const editable = editableTagIds(props.customer.tags)
  editableBefore.value = [...editable]
  selectedTagIds.value = [...editable]
  tagDialogVisible.value = true
  if (allTags.value.length === 0) {
    try {
      allTags.value = await listTags()
    } catch {
      // 拦截器已提示；标签选项降级为空
      allTags.value = []
    }
  }
}

async function handleSaveTags(): Promise<void> {
  savingTags.value = true
  try {
    await syncCustomerTags(props.customer.id, editableBefore.value, selectedTagIds.value)
    tagDialogVisible.value = false
    ElMessage.success('标签已更新')
    emit('updated')
  } catch {
    // 拦截器已提示；保持弹窗打开便于重试
  } finally {
    savingTags.value = false
  }
}
</script>

<style scoped>
.profile__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.profile__identity {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.profile__name {
  font-size: 18px;
  font-weight: 600;
}

/* 手机端：余额/积分大字摘要（plan todo 54） */
.profile__hero {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-bottom: 12px;
}

.profile__hero-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  background: #f5f7fa;
  border-radius: 10px;
}

.profile__hero-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.profile__hero-value {
  color: var(--el-text-color-primary);
  font-size: 26px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
}

.profile__tag-select {
  width: 100%;
}
</style>

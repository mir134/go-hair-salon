<template>
  <!-- 手机端（<768px）：底部抽屉表单（plan todo 54、02-AGENTS.md:92） -->
  <el-drawer
    v-if="isMobile"
    v-model="visible"
    direction="btt"
    size="86%"
    :title="dialogTitle"
    :close-on-click-modal="false"
  >
    <TagFormPanel
      v-if="visible"
      :tag="tag"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-drawer>

  <!-- PC：保持原有弹窗形态 -->
  <el-dialog
    v-else
    v-model="visible"
    :title="dialogTitle"
    width="420px"
    :close-on-click-modal="false"
  >
    <TagFormPanel
      v-if="visible"
      :tag="tag"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Tag } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import TagFormPanel from './TagFormPanel.vue'

// 标签新增/编辑外壳（plan todo 54）：表单与提交逻辑在 TagFormPanel 内（两种外壳共用）。
// 手机端用底部抽屉（大触控区域），PC 端保持弹窗；打开时由 Panel 负责回填。
const props = defineProps<{
  modelValue: boolean
  tag: Tag | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: []
}>()

const isMobile = useIsMobile()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.tag === null ? '新增标签' : '编辑标签'))
</script>

<template>
  <!-- 手机端（<768px）：底部抽屉表单（plan todo 54、07-UI.md:119） -->
  <el-drawer
    v-if="isMobile"
    v-model="visible"
    direction="btt"
    size="86%"
    :title="dialogTitle"
    :close-on-click-modal="false"
  >
    <ServiceCategoryFormPanel
      v-if="visible"
      :category="category"
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
    <ServiceCategoryFormPanel
      v-if="visible"
      :category="category"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { ServiceCategory } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import ServiceCategoryFormPanel from './ServiceCategoryFormPanel.vue'

// 新增/编辑分类外壳（plan todo 54）：表单与提交逻辑在 ServiceCategoryFormPanel 内（两种外壳共用）。
// 手机端用底部抽屉（大触控区域），PC 端保持弹窗；打开时由 Panel 负责回填。
const props = defineProps<{
  modelValue: boolean
  category: ServiceCategory | null
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

const dialogTitle = computed(() => (props.category === null ? '新增分类' : '编辑分类'))
</script>

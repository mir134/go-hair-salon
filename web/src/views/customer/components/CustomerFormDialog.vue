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
    <CustomerFormPanel
      v-if="visible"
      :customer-id="customerId"
      :tags="tags"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-drawer>

  <!-- PC：保持原有弹窗形态 -->
  <el-dialog
    v-else
    v-model="visible"
    :title="dialogTitle"
    width="560px"
    :close-on-click-modal="false"
  >
    <CustomerFormPanel
      v-if="visible"
      :customer-id="customerId"
      :tags="tags"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Tag } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import CustomerFormPanel from './CustomerFormPanel.vue'

// 新增/编辑客户外壳（plan todo 16/54）：表单与提交逻辑在 CustomerFormPanel 内（两种外壳共用）。
// 手机端用底部抽屉（大触控区域），PC 端保持弹窗；打开时由 Panel 负责初始化/回填。
const props = defineProps<{
  modelValue: boolean
  customerId: number | null
  tags: Tag[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 资料保存成功（标签可能部分失败）：父组件据此刷新列表 */
  saved: []
}>()

const isMobile = useIsMobile()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.customerId === null ? '新增客户' : '编辑客户'))
</script>

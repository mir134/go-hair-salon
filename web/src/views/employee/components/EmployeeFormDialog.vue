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
    <EmployeeFormPanel
      v-if="visible"
      :employee="employee"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-drawer>

  <!-- PC：保持原有弹窗形态 -->
  <el-dialog
    v-else
    v-model="visible"
    :title="dialogTitle"
    width="520px"
    :close-on-click-modal="false"
  >
    <EmployeeFormPanel
      v-if="visible"
      :employee="employee"
      @saved="emit('saved')"
      @close="visible = false"
    />
  </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Employee } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'

import EmployeeFormPanel from './EmployeeFormPanel.vue'

// 新增/编辑员工外壳（plan todo 40/54）：表单与提交逻辑在 EmployeeFormPanel 内（两种外壳共用）。
// 手机端用底部抽屉（大触控区域），PC 端保持弹窗；打开时由 Panel 负责初始化/回填。
const props = defineProps<{
  modelValue: boolean
  employee: Employee | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 员工保存成功，父级需刷新列表 */
  saved: []
}>()

const isMobile = useIsMobile()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.employee === null ? '新增员工' : '编辑员工'))
</script>

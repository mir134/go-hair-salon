<template>
  <!-- 手机端（<768px）：系统原生日期选择（OS 滚轮/日历），触控友好且不受 el-date-picker
       区间面板固定宽度限制（plan todo 55、07-UI.md:119） -->
  <div
    v-if="isMobile"
    v-bind="attrs"
    class="native-date"
  >
    <template v-if="mode === 'range'">
      <input
        v-model="localStart"
        class="native-date__input"
        type="date"
        aria-label="开始日期"
        @change="commitRange"
      >
      <span class="native-date__sep">至</span>
      <input
        v-model="localEnd"
        class="native-date__input"
        type="date"
        aria-label="结束日期"
        @change="commitRange"
      >
    </template>
    <input
      v-else
      v-model="localStart"
      class="native-date__input"
      type="date"
      aria-label="选择日期"
      @change="commitSingle"
    >
    <el-button
      v-if="hasValue"
      class="native-date__clear"
      link
      type="primary"
      @click="clear"
    >
      清除
    </el-button>
  </div>

  <!-- PC：保持原有 el-date-picker 形态；class/size 等透传，保持各页原有宽度约定 -->
  <el-date-picker
    v-else
    v-bind="attrs"
    :model-value="modelValue"
    :type="mode === 'range' ? 'daterange' : 'date'"
    :size="size"
    :clearable="clearable"
    :placeholder="placeholder"
    :start-placeholder="startPlaceholder"
    :end-placeholder="endPlaceholder"
    :range-separator="mode === 'range' ? '至' : undefined"
    :unlink-panels="mode === 'range'"
    value-format="YYYY-MM-DD"
    @update:model-value="onPickerUpdate"
    @change="onPickerChange"
  />
</template>

<script setup lang="ts">
import { computed, ref, useAttrs, watch } from 'vue'

import { useIsMobile } from '@/composables/useIsMobile'

// 响应式日期选择（plan todo 55）：手机端用系统原生 <input type="date">（OS 滚轮/日历，
// 触控友好，规避 el-date-picker 区间面板在 375px 屏溢出）；PC 端保持 el-date-picker 不变。
// class/size 同时透传（PC 给 el-date-picker、手机给原生容器），保持各页原有宽度约定。
defineOptions({ inheritAttrs: false })

type DateValue = string | [string, string] | null

const props = withDefaults(
  defineProps<{
    modelValue: DateValue
    /** date=单日；range=起止区间 */
    mode?: 'date' | 'range'
    /** 单日占位（PC el-date-picker） */
    placeholder?: string
    startPlaceholder?: string
    endPlaceholder?: string
    size?: 'large' | 'default' | 'small'
    clearable?: boolean
  }>(),
  {
    mode: 'date',
    placeholder: '选择日期',
    startPlaceholder: '开始日期',
    endPlaceholder: '结束日期',
    size: 'default',
    clearable: true,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: DateValue]
  change: [value: DateValue]
}>()

const attrs = useAttrs()
const isMobile = useIsMobile()

/** 原生输入的本地草稿：区间可先选一半，选全后才写回父级（避免半区间被误解） */
const localStart = ref('')
const localEnd = ref('')

watch(
  () => props.modelValue,
  (value) => {
    if (Array.isArray(value)) {
      localStart.value = value[0] ?? ''
      localEnd.value = value[1] ?? ''
      return
    }
    localStart.value = typeof value === 'string' ? value : ''
    localEnd.value = ''
  },
  { immediate: true },
)

/** 是否已有有效值（手机端据此显示「清除」） */
const hasValue = computed(() => {
  const value = props.modelValue
  if (Array.isArray(value)) {
    return value[0] !== '' || value[1] !== ''
  }
  return value !== null && value !== ''
})

function emitValue(value: DateValue): void {
  emit('update:modelValue', value)
  emit('change', value)
}

/** 单日：空值置 null（与 el-date-picker 清空语义一致） */
function commitSingle(): void {
  emitValue(localStart.value === '' ? null : localStart.value)
}

/** 区间：两端齐全才写回；整体清空时置 null，仅有半区间时暂不提交 */
function commitRange(): void {
  if (localStart.value === '' || localEnd.value === '') {
    if (localStart.value === '' && localEnd.value === '') {
      emitValue(null)
    }
    return
  }
  emitValue([localStart.value, localEnd.value])
}

function clear(): void {
  emitValue(null)
}

function onPickerUpdate(value: unknown): void {
  emit('update:modelValue', toDateValue(value))
}

function onPickerChange(value: unknown): void {
  emit('change', toDateValue(value))
}

/** 把 el-date-picker 的回传值收敛为 DateValue（value-format 下为 string / string[] / null） */
function toDateValue(value: unknown): DateValue {
  if (typeof value === 'string') {
    return value
  }
  if (Array.isArray(value)) {
    const first: unknown = value[0]
    const second: unknown = value[1]
    if (typeof first === 'string' && typeof second === 'string') {
      const pair: [string, string] = [first, second]
      return pair
    }
  }
  return null
}
</script>

<style scoped>
.native-date {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.native-date__input {
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: 0 8px;
  font: inherit;
  color: var(--el-text-color-primary);
  background: #fff;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
}

.native-date__input:focus {
  border-color: var(--el-color-primary);
  outline: none;
}

.native-date__sep {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.native-date__clear {
  margin-left: 0;
}
</style>

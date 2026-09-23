<template>
  <div class="pending-add">
    <div class="pending-add__bar">
      <el-select
        :model-value="selectedId"
        class="pending-add__select"
        filterable
        clearable
        placeholder="选择要追加的服务项目"
        :disabled="disabled || services.length === 0"
        @update:model-value="handleSelect"
      >
        <el-option
          v-for="service in services"
          :key="service.id"
          :label="`${service.name}（¥${formatCents(service.price_cents)}）`"
          :value="service.id"
        />
      </el-select>
      <el-button
        type="primary"
        :disabled="selectedId === null"
        :loading="adding"
        @click="handleAdd"
      >
        追加
      </el-button>
    </div>
    <p
      v-if="services.length === 0"
      class="pending-add__hint"
    >
      没有可追加的服务项目（已全部在单内或服务已停用）。
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

import type { Service } from '@/api'
import { formatCents } from '@/utils/format'

// 挂单明细追加栏（04-API.md:136）：从可追加服务中选择 → 追加（默认标准价、数量 1）。
// 本组件只负责选择与触发，请求由父级执行；提交后清空选择。
const props = defineProps<{
  services: Service[]
  disabled: boolean
  adding: boolean
}>()

const emit = defineEmits<{
  add: [service: Service]
}>()

const selectedId = ref<number | null>(null)

/** el-select 的 update:model-value 为宽类型；只接受数字 id，其余（清空）归 null */
function handleSelect(value: unknown): void {
  selectedId.value = typeof value === 'number' ? value : null
}

function handleAdd(): void {
  const service = props.services.find((item) => item.id === selectedId.value)
  if (service === undefined) {
    return
  }
  emit('add', service)
  selectedId.value = null
}
</script>

<style scoped>
.pending-add__bar {
  display: flex;
  gap: 8px;
}

.pending-add__select {
  flex: 1;
}

.pending-add__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

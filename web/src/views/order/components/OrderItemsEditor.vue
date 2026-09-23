<template>
  <el-card
    v-loading="loading"
    shadow="never"
    class="items-editor"
  >
    <template #header>
      <span>2. 选择服务</span>
    </template>
    <el-select
      :model-value="selectedServiceIds"
      class="items-editor__select"
      :size="isMobile ? 'large' : 'default'"
      multiple
      filterable
      clearable
      :disabled="services.length === 0"
      placeholder="选择服务项目（可多选）"
      @change="handleSelectionChange"
    >
      <el-option
        v-for="service in services"
        :key="service.id"
        :label="optionLabel(service)"
        :value="service.id"
      />
    </el-select>
    <p
      v-if="!loading && services.length === 0"
      class="items-editor__hint"
    >
      暂无可用的服务项目（服务列表加载失败或全部停用）。
    </p>

    <ul
      v-if="items.length > 0"
      class="items-editor__list"
    >
      <li
        v-for="item in items"
        :key="item.service.id"
        class="items-editor__row"
      >
        <div class="items-editor__row-head">
          <span class="items-editor__name">{{ item.service.name }}</span>
          <el-button
            link
            type="danger"
            @click="removeItem(item.service.id)"
          >
            移除
          </el-button>
        </div>
        <div class="items-editor__row-body">
          <label class="items-editor__field">
            <span class="items-editor__label">数量</span>
            <el-input-number
              :model-value="item.quantity"
              :min="1"
              :max="99"
              :size="isMobile ? 'large' : 'small'"
              @change="(value) => changeQuantity(item.service.id, value)"
            />
          </label>
          <label class="items-editor__field">
            <span class="items-editor__label">成交单价（元）</span>
            <el-input
              v-if="isAdmin"
              :model-value="item.unitPriceText"
              class="items-editor__price"
              :size="isMobile ? 'large' : 'small'"
              @input="(value) => changePrice(item.service.id, value)"
            />
            <span
              v-else
              class="items-editor__readonly"
            >¥{{ formatCents(item.service.price_cents) }}</span>
          </label>
          <span class="items-editor__amount">¥{{ formatCents(itemUnitPriceCents(item) * item.quantity) }}</span>
        </div>
      </li>
    </ul>
    <el-empty
      v-else-if="!loading"
      description="尚未选择服务项目"
      :image-size="60"
    />

    <p
      v-if="!isAdmin"
      class="items-editor__hint"
    >
      价格输入框仅管理员可用，当前按标准价成交（06-BUSINESS-RULES.md §3.1）。
    </p>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'

import type { Service } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { formatCents } from '@/utils/format'
import { consumeItemFromService, itemUnitPriceCents, type ConsumeItem } from '@/utils/orderForm'

// 消费第 2 步：多选服务 + 数量/成交单价（06 §3.1：改价仅 admin，staff 价格输入框禁用）。
// 手机端控件加大到 large 且每行占满宽度（plan todo 55：快速消费 3~5 步完成）。
const props = defineProps<{
  items: ConsumeItem[]
  services: Service[]
  isAdmin: boolean
  loading: boolean
}>()

const isMobile = useIsMobile()

const emit = defineEmits<{
  'update:items': [value: ConsumeItem[]]
}>()

const selectedServiceIds = computed(() => props.items.map((item) => item.service.id))

function optionLabel(service: Service): string {
  return `${service.name}（¥${formatCents(service.price_cents)}）`
}

/**
 * 多选变化：已选行保留（数量与 admin 的改价不丢失），新选项按标准价建行。
 * 只遍历当前选项，避免重复选择时行重复。
 */
function handleSelectionChange(value: unknown): void {
  const ids = Array.isArray(value) ? value.filter((item): item is number => typeof item === 'number') : []
  const next: ConsumeItem[] = []
  for (const id of ids) {
    const existing = props.items.find((item) => item.service.id === id)
    if (existing !== undefined) {
      next.push(existing)
      continue
    }
    const service = props.services.find((item) => item.id === id)
    if (service !== undefined) {
      next.push(consumeItemFromService(service))
    }
  }
  emit('update:items', next)
}

function removeItem(serviceId: number): void {
  emit(
    'update:items',
    props.items.filter((item) => item.service.id !== serviceId),
  )
}

function changeQuantity(serviceId: number, value: number | undefined): void {
  const quantity = value ?? 1
  emit(
    'update:items',
    props.items.map((item) => (item.service.id === serviceId ? { ...item, quantity } : item)),
  )
}

function changePrice(serviceId: number, value: string): void {
  emit(
    'update:items',
    props.items.map((item) => (item.service.id === serviceId ? { ...item, unitPriceText: value } : item)),
  )
}
</script>

<style scoped>
.items-editor__select {
  width: 100%;
  max-width: 480px;
}

.items-editor__list {
  margin: 12px 0 0;
  padding: 0;
  list-style: none;
}

.items-editor__row {
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.items-editor__row-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.items-editor__name {
  font-weight: 500;
}

.items-editor__row-body {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 8px;
}

.items-editor__field {
  display: flex;
  align-items: center;
  gap: 8px;
}

.items-editor__label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.items-editor__price {
  width: 120px;
}

.items-editor__readonly {
  font-variant-numeric: tabular-nums;
}

.items-editor__amount {
  margin-left: auto;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.items-editor__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

/* 手机端（<768px，与 useIsMobile 断点一致）：控件占满整行，便于手指操作（plan todo 55） */
@media (max-width: 767px) {
  .items-editor__select {
    max-width: none;
  }

  .items-editor__field {
    flex: 1 1 100%;
    justify-content: space-between;
  }

  .items-editor__price {
    flex: 1;
    width: auto;
  }

  .items-editor__amount {
    margin-left: 0;
    font-size: 16px;
  }
}
</style>

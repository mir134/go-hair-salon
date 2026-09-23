<template>
  <el-card shadow="never">
    <template #header>
      <span>1. 选择客户</span>
    </template>
    <el-select
      :model-value="customer?.id ?? null"
      class="customer-card__select"
      filterable
      remote
      clearable
      :remote-method="handleSearch"
      :loading="searching"
      placeholder="输入手机号 / 姓名搜索客户"
      @change="handleChange"
    >
      <el-option
        v-for="option in options"
        :key="option.id"
        :label="optionLabel(option)"
        :value="option.id"
      >
        <span>{{ option.name }}</span>
        <span class="customer-card__meta">
          {{ option.phone !== '' ? option.phone : '无手机号' }} · 余额 ¥{{ formatCents(option.balance_cents) }}
        </span>
      </el-option>
    </el-select>
    <div
      v-if="customer !== null"
      class="customer-card__tags"
    >
      <el-tag type="success">
        余额 ¥{{ formatCents(customer.balance_cents) }}
      </el-tag>
      <el-tag type="info">
        积分 {{ customer.points }}
      </el-tag>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { listCustomers } from '@/api'
import type { Customer, CustomerListQuery } from '@/api'
import { formatCents } from '@/utils/format'

// 消费第 1 步：搜索并选择客户（07-UI.md:114 客户搜索优先手机号；复用 /customers 搜索）。
const customer = defineModel<Customer | null>('customer', { required: true })

const results = ref<Customer[]>([])
const searching = ref(false)

/** 已选客户始终在选项里（客户详情「快速消费」预选的对象可能不在搜索结果中） */
const options = computed<Customer[]>(() => {
  const selected = customer.value
  if (selected === null || results.value.some((item) => item.id === selected.id)) {
    return results.value
  }
  return [selected, ...results.value]
})

onMounted(() => {
  void search('')
})

async function search(keyword: string): Promise<void> {
  searching.value = true
  try {
    const query: CustomerListQuery = { page_size: 20 }
    const trimmed = keyword.trim()
    if (trimmed !== '') {
      query.keyword = trimmed
    }
    const data = await listCustomers(query)
    results.value = data.items
  } catch {
    // 拦截器已提示；保留原结果，可重试搜索
  } finally {
    searching.value = false
  }
}

function handleSearch(keyword: string): void {
  void search(keyword)
}

function optionLabel(item: Customer): string {
  return item.phone !== '' ? `${item.name}（${item.phone}）` : item.name
}

/** 选择/清空（清空时 Element Plus 传出非数字值）→ 统一收敛为 Customer | null */
function handleChange(value: unknown): void {
  if (typeof value !== 'number') {
    customer.value = null
    return
  }
  customer.value = options.value.find((item) => item.id === value) ?? null
}
</script>

<style scoped>
.customer-card__select {
  width: 100%;
  max-width: 360px;
}

.customer-card__meta {
  float: right;
  margin-left: 16px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.customer-card__tags {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
</style>

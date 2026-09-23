<template>
  <div class="recharge-filters">
    <el-select
      :model-value="customer?.id ?? null"
      class="recharge-filters__customer"
      filterable
      remote
      clearable
      :remote-method="handleSearch"
      :loading="searching"
      placeholder="按客户筛选：输入手机号 / 姓名"
      @change="handleCustomerChange"
    >
      <el-option
        v-for="option in options"
        :key="option.id"
        :label="optionLabel(option)"
        :value="option.id"
      />
    </el-select>
    <el-date-picker
      v-model="range"
      type="daterange"
      unlink-panels
      range-separator="至"
      start-placeholder="开始日期"
      end-placeholder="结束日期"
      value-format="YYYY-MM-DD"
    />
    <el-button @click="reset">
      重置
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { listCustomers } from '@/api'
import type { Customer, CustomerListQuery } from '@/api'
import type { RechargeListFilters } from '@/utils/rechargeForm'

// 充值记录筛选（plan todo 33：分页/日期/客户筛选）：
// 客户经 /customers 远程搜索（复用现有接口），日期范围按 YYYY-MM-DD 透传。
// 任一条件变化即 emit change，由列表页重置到第 1 页后加载。
const emit = defineEmits<{
  change: [filters: RechargeListFilters]
}>()

const customer = ref<Customer | null>(null)
const range = ref<[string, string] | null>(null)
const results = ref<Customer[]>([])
const searching = ref(false)

/** 已选客户始终在选项里（远程搜索结果可能不含当前选中项） */
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

watch([customer, range], () => {
  const [start = '', end = ''] = range.value ?? []
  emit('change', { customerId: customer.value?.id ?? null, startDate: start, endDate: end })
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
function handleCustomerChange(value: unknown): void {
  if (typeof value !== 'number') {
    customer.value = null
    return
  }
  customer.value = options.value.find((item) => item.id === value) ?? null
}

function reset(): void {
  customer.value = null
  range.value = null
  void search('')
}
</script>

<style scoped>
.recharge-filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.recharge-filters__customer {
  width: 280px;
}
</style>

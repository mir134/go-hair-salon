<template>
  <el-table
    v-loading="loading"
    :data="items"
    row-key="id"
  >
    <el-table-column
      label="时间"
      width="150"
    >
      <template #default="{ row }">
        {{ formatDateTime(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column
      v-if="showCustomer"
      prop="customer_name"
      label="客户"
      min-width="120"
    />
    <el-table-column
      label="充值本金（元）"
      width="130"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.recharge_amount_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="赠送（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.gift_amount_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="实付（元）"
      width="110"
      align="right"
    >
      <template #default="{ row }">
        {{ formatCents(row.actual_amount_cents) }}
      </template>
    </el-table-column>
    <el-table-column
      label="支付方式"
      width="100"
    >
      <template #default="{ row }">
        {{ PAYMENT_METHOD_LABELS[row.payment_method] ?? row.payment_method }}
      </template>
    </el-table-column>
    <el-table-column
      label="状态"
      width="100"
    >
      <template #default="{ row }">
        <el-tag
          :type="RECHARGE_STATUS_TAG_TYPES[row.status] ?? 'info'"
          size="small"
        >
          {{ RECHARGE_STATUS_LABELS[row.status] ?? row.status }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column
      prop="remark"
      label="备注"
      min-width="120"
    />
    <template #empty>
      <el-empty description="暂无充值记录" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { Recharge } from '@/api'
import {
  PAYMENT_METHOD_LABELS,
  RECHARGE_STATUS_LABELS,
  RECHARGE_STATUS_TAG_TYPES,
} from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 充值记录表格：充值列表页（含客户列）与客户详情「充值记录」tab（不含客户列）共用。
// 金额列全部取服务端整数分：本金 / 赠送 / 实付分列展示，赠送不并入实付（07-UI.md:74）。
withDefaults(
  defineProps<{
    items: Recharge[]
    loading: boolean
    showCustomer?: boolean
  }>(),
  { showCustomer: false },
)
</script>

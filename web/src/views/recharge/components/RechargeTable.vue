<template>
  <!-- 手机端（<768px）：卡片列表替代表格（02-AGENTS.md:92、07-UI.md:119） -->
  <RechargeCardList
    v-if="isMobile"
    :items="items"
    :loading="loading"
    :show-customer="showCustomer"
    :is-admin="isAdmin"
    :refunding-id="refundingId"
    @refund="(id, refundCents, customerName) => emit('refund', id, refundCents, customerName)"
  />
  <el-table
    v-else
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
    <!-- 冲正入口仅 admin（04-API.md:159；plan todo 37）；确认与调用由父级 useRechargeRefund 处理 -->
    <el-table-column
      v-if="isAdmin"
      label="操作"
      width="90"
      fixed="right"
    >
      <template #default="{ row }">
        <el-button
          v-if="row.status === 'active'"
          link
          type="danger"
          :loading="refundingId === row.id"
          @click="emit('refund', row.id, row.recharge_amount_cents + row.gift_amount_cents, row.customer_name)"
        >
          冲正
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="暂无充值记录" />
    </template>
  </el-table>
</template>

<script setup lang="ts">
import type { Recharge } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import {
  PAYMENT_METHOD_LABELS,
  RECHARGE_STATUS_LABELS,
  RECHARGE_STATUS_TAG_TYPES,
} from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

import RechargeCardList from './RechargeCardList.vue'

// 充值记录表格：充值列表页（含客户列）与客户详情「充值记录」tab（不含客户列）共用。
// 金额列全部取服务端整数分：本金 / 赠送 / 实付分列展示，赠送不并入实付（07-UI.md:74）。
// 冲正操作列仅 admin 渲染（隐藏按钮只是 UI 简化，最终边界在后端 RBAC）；
// 二次确认与 API 调用由父级经 useRechargeRefund 完成，本组件只 emit（与 OrderTable 同构）。
withDefaults(
  defineProps<{
    items: Recharge[]
    loading: boolean
    showCustomer?: boolean
    /** 是否渲染「冲正」操作列（仅 admin，plan todo 37） */
    isAdmin?: boolean
    /** 正在冲正的记录 id（父级 useRechargeRefund 提供），按钮据此 loading */
    refundingId?: number | null
  }>(),
  { showCustomer: false, isAdmin: false, refundingId: null },
)

const emit = defineEmits<{
  /** 请求冲正某条充值记录（id、应扣回余额=本金+赠送、客户名；父级二次确认后调用 API） */
  refund: [id: number, refundCents: number, customerName: string]
}>()

/** 手机端渲染 RechargeCardList，PC 端保持表格（plan todo 53） */
const isMobile = useIsMobile()
</script>

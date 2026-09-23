<template>
  <el-card shadow="never">
    <el-result
      icon="success"
      title="充值成功"
      :sub-title="`充值记录 #${recharge.id}`"
    >
      <template #extra>
        <el-button
          type="primary"
          @click="emit('restart')"
        >
          再充一笔
        </el-button>
        <el-button @click="emit('viewCustomer')">
          查看客户
        </el-button>
      </template>
    </el-result>

    <el-descriptions
      :column="2"
      border
    >
      <el-descriptions-item label="客户">
        {{ customerName !== '' ? customerName : recharge.customer_name }}
      </el-descriptions-item>
      <el-descriptions-item label="支付方式">
        {{ PAYMENT_METHOD_LABELS[recharge.payment_method] ?? recharge.payment_method }}
      </el-descriptions-item>
      <el-descriptions-item label="实付金额（元）">
        {{ formatCents(recharge.actual_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="赠送金额（元）">
        {{ formatCents(recharge.gift_amount_cents) }}
      </el-descriptions-item>
      <el-descriptions-item label="增加余额（元）">
        {{ formatCents(recharge.recharge_amount_cents + recharge.gift_amount_cents) }}
      </el-descriptions-item>
      <!-- 最新余额为提交成功后回读的服务端真值（不做本地加减） -->
      <el-descriptions-item
        v-if="balanceAfter !== null"
        label="最新余额（元）"
      >
        {{ formatCents(balanceAfter) }}
      </el-descriptions-item>
    </el-descriptions>

    <p class="recharge-result__note">
      余额增加 = 实付 + 赠送（06-BUSINESS-RULES.md:50）；余额与流水已由后端在同一事务内入账。
    </p>
  </el-card>
</template>

<script setup lang="ts">
import type { Recharge } from '@/api'
import { PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents } from '@/utils/format'

// 充值结果（07-UI.md:72-74）：金额明细 + 客户最新余额（服务端真值）。
// 实付取 actual_amount_cents、增加余额取本金+赠送，两者均来自服务端返回，避免把赠送误标为实付。
defineProps<{
  recharge: Recharge
  customerName: string
  balanceAfter: number | null
}>()

const emit = defineEmits<{
  restart: []
  viewCustomer: []
}>()
</script>

<style scoped>
.recharge-result__note {
  margin: 12px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

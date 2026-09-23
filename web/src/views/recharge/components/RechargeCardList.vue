<template>
  <div
    v-loading="loading"
    class="recharge-cards"
  >
    <article
      v-for="item in items"
      :key="item.id"
      class="recharge-card"
    >
      <header class="recharge-card__head">
        <span class="recharge-card__time">{{ formatDateTime(item.created_at) }}</span>
        <el-tag
          :type="RECHARGE_STATUS_TAG_TYPES[item.status] ?? 'info'"
          size="small"
        >
          {{ RECHARGE_STATUS_LABELS[item.status] ?? item.status }}
        </el-tag>
      </header>

      <p
        v-if="showCustomer"
        class="recharge-card__customer"
      >
        {{ item.customer_name }}
      </p>

      <dl class="recharge-card__grid">
        <div class="recharge-card__cell">
          <dt>本金（元）</dt>
          <dd>{{ formatCents(item.recharge_amount_cents) }}</dd>
        </div>
        <div class="recharge-card__cell">
          <dt>赠送（元）</dt>
          <dd>{{ formatCents(item.gift_amount_cents) }}</dd>
        </div>
        <div class="recharge-card__cell">
          <dt>实付（元）</dt>
          <dd>{{ formatCents(item.actual_amount_cents) }}</dd>
        </div>
      </dl>

      <div class="recharge-card__meta">
        <span>{{ PAYMENT_METHOD_LABELS[item.payment_method] ?? item.payment_method }}</span>
        <span v-if="item.remark !== ''">{{ item.remark }}</span>
      </div>

      <!-- 冲正入口仅 admin（plan todo 37）；确认与调用由父级 useRechargeRefund 处理 -->
      <footer
        v-if="isAdmin && item.status === 'active'"
        class="recharge-card__actions"
      >
        <el-button
          type="danger"
          plain
          :loading="refundingId === item.id"
          @click="emit('refund', item.id, item.recharge_amount_cents + item.gift_amount_cents, item.customer_name)"
        >
          冲正
        </el-button>
      </footer>
    </article>

    <el-empty
      v-if="!loading && items.length === 0"
      description="暂无充值记录"
    />
  </div>
</template>

<script setup lang="ts">
import {
  PAYMENT_METHOD_LABELS,
  RECHARGE_STATUS_LABELS,
  RECHARGE_STATUS_TAG_TYPES,
} from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 充值记录卡片列表（手机端替代表格，plan todo 53、07-UI.md:119、02-AGENTS.md:92）。
// 金额分列展示本金/赠送/实付：赠送不并入实付（07-UI.md:74）；
// 入参只要求用到的字段，充值列表页与客户详情「充值记录」tab 共用同一 DTO。
withDefaults(
  defineProps<{
    items: readonly {
      id: number
      customer_name: string
      recharge_amount_cents: number
      gift_amount_cents: number
      actual_amount_cents: number
      payment_method: string
      status: string
      remark: string
      created_at: string
    }[]
    loading: boolean
    /** 是否展示客户名（列表页 true；客户详情 tab false） */
    showCustomer?: boolean
    /** 是否渲染「冲正」按钮（仅 admin，plan todo 37） */
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
</script>

<style scoped>
.recharge-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.recharge-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.recharge-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.recharge-card__time {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.recharge-card__customer {
  margin: 8px 0 0;
  font-size: 16px;
  font-weight: 600;
}

.recharge-card__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  margin: 10px 0 0;
}

.recharge-card__cell dt {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.recharge-card__cell dd {
  margin: 2px 0 0;
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.recharge-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.recharge-card__actions {
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53） */
.recharge-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

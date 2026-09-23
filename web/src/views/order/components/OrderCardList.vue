<template>
  <div
    v-loading="loading"
    class="order-cards"
  >
    <article
      v-for="order in items"
      :key="order.id"
      class="order-card"
      @click="emit('detail', order.id)"
    >
      <header class="order-card__head">
        <span class="order-card__no">{{ order.order_no }}</span>
        <el-tag
          :type="ORDER_STATUS_TAG_TYPES[order.status] ?? 'info'"
          size="small"
        >
          {{ ORDER_STATUS_LABELS[order.status] ?? order.status }}
        </el-tag>
      </header>

      <div class="order-card__meta">
        <span v-if="order.customer_name !== undefined && order.customer_name !== ''">
          {{ order.customer_name }}
        </span>
        <span>{{ formatDateTime(order.created_at) }}</span>
        <span>
          {{ order.payment_method === '' ? '—' : (PAYMENT_METHOD_LABELS[order.payment_method] ?? order.payment_method) }}
        </span>
      </div>

      <div class="order-card__amount">
        <span class="order-card__amount-label">实付（元）</span>
        <span class="order-card__amount-value">¥{{ formatCents(order.paid_amount_cents) }}</span>
      </div>

      <!-- 卡片内的操作按钮不触发整卡跳转（@click.stop） -->
      <footer
        v-if="actions"
        class="order-card__actions"
        @click.stop
      >
        <template v-if="order.status === 'pending'">
          <el-button
            type="primary"
            @click="emit('checkout', order.id)"
          >
            结账
          </el-button>
          <el-button @click="emit('edit-items', order.id)">
            编辑明细
          </el-button>
          <el-button
            v-if="isAdmin"
            type="danger"
            plain
            @click="emit('cancel', order.id, order.order_no)"
          >
            取消
          </el-button>
        </template>
        <el-button
          v-else
          @click="emit('detail', order.id)"
        >
          查看详情
        </el-button>
      </footer>
    </article>

    <el-empty
      v-if="!loading && items.length === 0"
      description="暂无消费记录"
    />
  </div>
</template>

<script setup lang="ts">
import { ORDER_STATUS_LABELS, ORDER_STATUS_TAG_TYPES, PAYMENT_METHOD_LABELS } from '@/constants'
import { formatCents, formatDateTime } from '@/utils/format'

// 消费记录卡片列表（手机端替代表格，plan todo 53、07-UI.md:119、02-AGENTS.md:92）。
// 字段是 OrderTable 表格列的卡片化投影：订单号/状态/客户/时间/支付方式/实付；
// 待结账卡片提供 结账/编辑明细（取消仅 admin）。入参只要求用到的字段，
// 订单列表（Order）与客户详情消费记录（CustomerOrder）两个 DTO 都能直接传入。
withDefaults(
  defineProps<{
    items: readonly {
      id: number
      order_no: string
      customer_name?: string
      paid_amount_cents: number
      payment_method: string
      status: string
      created_at: string
    }[]
    loading: boolean
    isAdmin: boolean
    /** 是否渲染底部操作区（客户详情「消费记录」tab 只读展示时关闭） */
    actions?: boolean
  }>(),
  { actions: true },
)

const emit = defineEmits<{
  detail: [id: number]
  checkout: [id: number]
  'edit-items': [id: number]
  cancel: [id: number, orderNo: string]
}>()
</script>

<style scoped>
.order-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.order-card {
  padding: 12px 14px;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
}

.order-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.order-card__no {
  font-size: 14px;
  font-weight: 600;
  word-break: break-all;
}

.order-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.order-card__amount {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-top: 10px;
}

.order-card__amount-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.order-card__amount-value {
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.order-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

/* 触控目标 ≥44px（plan todo 53） */
.order-card__actions .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

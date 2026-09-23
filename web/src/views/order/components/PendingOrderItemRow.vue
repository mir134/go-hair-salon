<template>
  <li class="pending-item">
    <div class="pending-item__head">
      <span class="pending-item__name">{{ item.service_name_snapshot }}</span>
      <el-button
        link
        type="danger"
        :disabled="busy || !canRemove"
        @click="emit('remove')"
      >
        删除
      </el-button>
    </div>
    <div class="pending-item__body">
      <label class="pending-item__field">
        <span class="pending-item__label">数量</span>
        <el-input-number
          :model-value="item.quantity"
          :min="1"
          :max="99"
          size="small"
          :disabled="busy"
          @change="handleQuantity"
        />
      </label>
      <label class="pending-item__field">
        <span class="pending-item__label">成交单价（元）</span>
        <!-- 改价仅 admin（06-BUSINESS-RULES.md §3.1）：staff 只读标准价，不渲染输入框 -->
        <el-input
          v-if="isAdmin"
          :model-value="priceText"
          class="pending-item__price"
          size="small"
          :disabled="busy"
          @input="handlePriceInput"
          @change="commitPrice"
        />
        <span
          v-else
          class="pending-item__readonly"
        >¥{{ formatCents(item.unit_price_cents) }}</span>
      </label>
      <span class="pending-item__amount">¥{{ formatCents(item.amount_cents) }}</span>
    </div>
    <p
      v-if="isAdmin && item.discount_amount_cents !== 0"
      class="pending-item__hint"
    >
      已改价：本行优惠 ¥{{ formatCents(item.discount_amount_cents) }}（06 §3.1）。
    </p>
  </li>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import type { OrderItem } from '@/api'
import { formatCents } from '@/utils/format'
import { parsePriceToCents } from '@/utils/serviceForm'

// 挂单明细行（07-UI.md:59）：数量可改；单价仅 admin 可改且必须填写原因（06 §3.1）。
// 本组件只负责交互与本地校验，API 调用与订单重载由 OrderItemsDrawer 统一执行。
const props = defineProps<{
  item: OrderItem
  isAdmin: boolean
  busy: boolean
  canRemove: boolean
}>()

const emit = defineEmits<{
  quantityChange: [quantity: number]
  /** 改价已确认（admin 且已填原因），单位：分 */
  priceChange: [cents: number, reason: string]
  remove: []
}>()

const priceText = ref(formatCents(props.item.unit_price_cents))

// 服务端重算/刷新后同步显示（请求失败时回退到服务端值）
watch(
  () => props.item.unit_price_cents,
  (cents) => {
    priceText.value = formatCents(cents)
  },
)

function syncFromItem(): void {
  priceText.value = formatCents(props.item.unit_price_cents)
}

/** 数量：钳制为 ≥1 的整数，未变化不发请求 */
function handleQuantity(value: number | undefined): void {
  const quantity = value ?? props.item.quantity
  if (!Number.isInteger(quantity) || quantity < 1 || quantity === props.item.quantity) {
    return
  }
  emit('quantityChange', quantity)
}

function handlePriceInput(value: string): void {
  priceText.value = value
}

/**
 * 失焦/回车提交改价：格式非法或未变化则回退；有变化时弹出「改价原因」，
 * 取消或原因留空均不提交（06 §3.1 改价必填原因）。
 */
async function commitPrice(): Promise<void> {
  const cents = parsePriceToCents(priceText.value)
  if (cents === null) {
    ElMessage.error('成交单价格式不正确（最多两位小数的元金额）')
    syncFromItem()
    return
  }
  if (cents === props.item.unit_price_cents) {
    syncFromItem()
    return
  }
  try {
    const { value } = await ElMessageBox.prompt(
      '修改成交单价必须填写原因（写入操作日志，06-BUSINESS-RULES.md §3.1）。',
      '改价原因',
      {
        confirmButtonText: '确认改价',
        cancelButtonText: '取消',
        inputPlaceholder: '例如：老客户折扣',
        inputValidator: (input) => (input.trim() !== '' ? true : '改价原因不能为空'),
      },
    )
    emit('priceChange', cents, value.trim())
  } catch {
    // 用户取消改价：回退显示，不提交
    syncFromItem()
  }
}
</script>

<style scoped>
.pending-item {
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  list-style: none;
}

.pending-item__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.pending-item__name {
  font-weight: 500;
}

.pending-item__body {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 8px;
}

.pending-item__field {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pending-item__label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.pending-item__price {
  width: 120px;
}

.pending-item__readonly {
  font-variant-numeric: tabular-nums;
}

.pending-item__amount {
  margin-left: auto;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.pending-item__hint {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

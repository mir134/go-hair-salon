<template>
  <el-drawer
    v-model="visible"
    title="编辑明细（挂单）"
    size="600px"
    :close-on-click-modal="false"
  >
    <div
      v-loading="loading"
      class="items-drawer"
    >
      <!-- 非 pending（已被结账/取消，或并发操作导致状态变化）：只读提示，避免误编辑 -->
      <el-alert
        v-if="order !== null && !isPending"
        type="warning"
        :closable="false"
        show-icon
        title="订单已非待结账状态，明细不可编辑（06-BUSINESS-RULES.md:32）。"
      />

      <template v-else>
        <PendingOrderAddBar
          :services="addableServices"
          :disabled="mutating"
          :adding="mutating"
          @add="handleAdd"
        />

        <ul
          v-if="items.length > 0"
          class="items-drawer__list"
        >
          <PendingOrderItemRow
            v-for="item in items"
            :key="`${item.id}-${orderRevision}`"
            :item="item"
            :is-admin="isAdmin"
            :busy="mutating"
            :can-remove="items.length > 1"
            @quantity-change="(quantity) => handleQuantityChange(item, quantity)"
            @price-change="(cents, reason) => handlePriceChange(item, cents, reason)"
            @remove="handleRemove(item)"
          />
        </ul>
        <el-empty
          v-else-if="order !== null"
          description="订单已无明细：请追加服务项目，或取消该订单"
          :image-size="60"
        />

        <PendingOrderTotals
          v-if="order !== null"
          :order="order"
        />
      </template>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import {
  addOrderItem,
  deleteOrderItem,
  getOrder,
  listServices,
  updateOrderItem,
} from '@/api'
import type { Order, OrderItem, Service } from '@/api'

import PendingOrderAddBar from './PendingOrderAddBar.vue'
import PendingOrderItemRow from './PendingOrderItemRow.vue'
import PendingOrderTotals from './PendingOrderTotals.vue'

// 挂单明细编辑抽屉（07-UI.md:59、04-API.md:136-138）：
// 增删服务项目、修改数量（金额由后端随明细重算）；修改单价仅 admin 且必填原因。
// 每次变更后重新拉取订单，界面金额一律以服务端重算结果为准。
const props = defineProps<{
  modelValue: boolean
  orderId: number | null
  isAdmin: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 明细变更成功（金额已重算）：父级据此刷新列表/详情 */
  updated: []
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const order = ref<Order | null>(null)
const services = ref<Service[]>([])
const loading = ref(false)
const mutating = ref(false)
/** 每次重载自增：作为行 key 的一部分强制重挂载，确保编辑失败后输入框回到服务端真值 */
const orderRevision = ref(0)

const items = computed(() => order.value?.items ?? [])
const isPending = computed(() => order.value?.status === 'pending')
/** 可追加：启用中且不在单内的服务（同一服务重复追加由后端决定，这里先避免） */
const addableServices = computed(() =>
  services.value.filter(
    (service) =>
      service.status === 1 && !items.value.some((item) => item.service_id === service.id),
  ),
)

watch(visible, (open) => {
  if (open) {
    void prepare()
  }
})

async function prepare(): Promise<void> {
  order.value = null
  services.value = []
  const id = props.orderId
  if (id === null) {
    return
  }
  loading.value = true
  try {
    const [detail, serviceList] = await Promise.all([getOrder(id), listServices()])
    order.value = detail
    services.value = serviceList
  } catch {
    // 拦截器已提示（404/网络）；保持空态，可关闭重开重试
  } finally {
    loading.value = false
  }
}

async function loadOrder(): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  try {
    order.value = await getOrder(id)
    orderRevision.value += 1
  } catch {
    // 拦截器已提示；保留旧数据
  }
}

/**
 * 明细变更统一入口：请求 → 重新拉取订单 → 通知父级刷新。
 * 失败（409 已结账/403 无权限/422 校验失败）也重载一次，把服务端真实状态同步到界面。
 */
async function runMutation(action: () => Promise<unknown>): Promise<void> {
  mutating.value = true
  try {
    await action()
    await loadOrder()
    emit('updated')
  } catch {
    await loadOrder()
  } finally {
    mutating.value = false
  }
}

async function handleAdd(service: Service): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  await runMutation(() =>
    addOrderItem(id, {
      service_id: service.id,
      quantity: 1,
      unit_price_cents: service.price_cents,
    }),
  )
}

async function handleQuantityChange(item: OrderItem, quantity: number): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  await runMutation(() =>
    updateOrderItem(id, item.id, {
      quantity,
      unit_price_cents: item.unit_price_cents,
    }),
  )
}

async function handlePriceChange(item: OrderItem, cents: number, reason: string): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  await runMutation(() =>
    updateOrderItem(id, item.id, {
      quantity: item.quantity,
      unit_price_cents: cents,
      discount_reason: reason,
    }),
  )
}

async function handleRemove(item: OrderItem): Promise<void> {
  const id = props.orderId
  if (id === null) {
    return
  }
  // 订单必须至少保留一条明细（后端创建同样要求 ≥1 项）；清空请走「取消订单」
  if (items.value.length <= 1) {
    ElMessage.warning('订单至少保留一个服务项目；如需作废请使用「取消订单」')
    return
  }
  try {
    await ElMessageBox.confirm(
      `删除明细「${item.service_name_snapshot} ×${item.quantity}」？订单金额将重新计算。`,
      '删除明细',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return // 用户取消
  }
  await runMutation(() => deleteOrderItem(id, item.id))
}
</script>

<style scoped>
.items-drawer__list {
  margin: 12px 0 0;
  padding: 0;
}
</style>

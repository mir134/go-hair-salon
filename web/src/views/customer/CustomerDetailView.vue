<template>
  <section class="customer-detail">
    <div class="customer-detail__nav">
      <el-button
        link
        type="primary"
        @click="goBack"
      >
        ← 返回客户列表
      </el-button>
      <el-button
        type="primary"
        :disabled="customer === null"
        @click="goConsume"
      >
        快速消费
      </el-button>
    </div>

    <el-card
      v-loading="loading"
      shadow="never"
    >
      <CustomerProfileCard
        v-if="customer !== null"
        :customer="customer"
        :latest-order-at="latestOrderAt"
        @updated="refresh"
      />
      <el-empty
        v-else-if="!loading"
        description="客户不存在或已删除"
      />
    </el-card>

    <el-card
      v-if="customer !== null"
      shadow="never"
      class="customer-detail__ledger"
    >
      <el-tabs
        :key="customerId"
        v-model="activeTab"
      >
        <el-tab-pane
          label="消费记录"
          name="orders"
          lazy
        >
          <CustomerOrdersTable :customer-id="customerId" />
        </el-tab-pane>
        <el-tab-pane
          label="充值记录"
          name="recharges"
          lazy
        >
          <!-- GET /recharges（04-API.md:162）随充值模块提供（plan todo 30-31），后端尚未注册该路由 -->
          <el-empty description="接口待接入（充值记录接口将随充值模块提供）" />
        </el-tab-pane>
        <el-tab-pane
          label="余额流水"
          name="balance"
          lazy
        >
          <CustomerBalanceTable :customer-id="customerId" />
        </el-tab-pane>
        <el-tab-pane
          label="积分流水"
          name="points"
          lazy
        >
          <CustomerPointsTable :customer-id="customerId" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getCustomer, listCustomerOrders } from '@/api'
import type { Customer } from '@/api'

import CustomerBalanceTable from './components/CustomerBalanceTable.vue'
import CustomerOrdersTable from './components/CustomerOrdersTable.vue'
import CustomerPointsTable from './components/CustomerPointsTable.vue'
import CustomerProfileCard from './components/CustomerProfileCard.vue'

// 客户详情页编排（07-UI.md:44-46）：档案头部 + 消费/充值/余额/积分 4 个 tab。
const route = useRoute()
const router = useRouter()

const customerId = computed(() => Number(String(route.params.id)))
const customer = ref<Customer | null>(null)
const loading = ref(false)
/** 最近消费：取消费记录第一条（07-UI.md:46） */
const latestOrderAt = ref<string | null>(null)
const activeTab = ref('orders')

// 路由参数变化（从列表进入其它客户）时整体重载；:key 让各 tab 子表随客户重建
watch(
  customerId,
  () => {
    refresh()
  },
  { immediate: true },
)

function refresh(): void {
  void loadCustomer()
  void loadLatestOrder()
}

async function loadCustomer(): Promise<void> {
  loading.value = true
  try {
    customer.value = await getCustomer(customerId.value)
  } catch {
    // 拦截器已提示（404 客户不存在等）
    customer.value = null
  } finally {
    loading.value = false
  }
}

async function loadLatestOrder(): Promise<void> {
  try {
    const data = await listCustomerOrders(customerId.value, { page: 1, page_size: 1 })
    latestOrderAt.value = data.items[0]?.created_at ?? null
  } catch {
    latestOrderAt.value = null
  }
}

function goBack(): void {
  void router.push('/customers')
}

/** 快速消费入口（07-UI.md:36、plan todo 23）：带客户 id 进入，消费页预选该客户 */
function goConsume(): void {
  void router.push({ path: '/orders/new', query: { customer_id: String(customerId.value) } })
}
</script>

<style scoped>
.customer-detail__nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.customer-detail__ledger {
  margin-top: 16px;
}
</style>

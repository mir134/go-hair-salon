<template>
  <section class="customer-detail">
    <div
      class="customer-detail__nav"
      :class="{ 'customer-detail__nav--mobile': isMobile }"
    >
      <el-button
        link
        type="primary"
        @click="goBack"
      >
        ← 返回客户列表
      </el-button>
      <div class="customer-detail__actions">
        <el-button
          :size="actionSize"
          :disabled="customer === null"
          @click="goRecharge"
        >
          充值
        </el-button>
        <el-button
          type="primary"
          :size="actionSize"
          :disabled="customer === null"
          @click="goConsume"
        >
          快速消费
        </el-button>
        <!-- 余额调整仅 admin（06-BUSINESS-RULES.md:62）；隐藏按钮是 UI 简化，后端 RBAC 是最终边界 -->
        <el-button
          v-if="isAdmin"
          :size="actionSize"
          :disabled="customer === null"
          @click="adjustVisible = true"
        >
          余额调整
        </el-button>
      </div>
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
        :key="`${customerId}-${ledgerVersion}`"
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
          <CustomerRechargesTable
            :customer-id="customerId"
            @refunded="handleRechargeRefunded"
          />
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

    <!-- 余额调整仅 admin 渲染（入口隐藏只是 UI 简化，后端 RBAC 是最终边界） -->
    <BalanceAdjustDialog
      v-if="isAdmin"
      v-model="adjustVisible"
      :customer-id="customerId"
      :customer-name="customer?.name ?? ''"
      :balance-cents="customer?.balance_cents ?? 0"
      @adjusted="handleAdjusted"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { getCustomer, listCustomerOrders } from '@/api'
import type { Customer } from '@/api'
import { useIsMobile } from '@/composables/useIsMobile'
import { useAuthStore } from '@/stores/auth'

import BalanceAdjustDialog from './components/BalanceAdjustDialog.vue'
import CustomerBalanceTable from './components/CustomerBalanceTable.vue'
import CustomerOrdersTable from './components/CustomerOrdersTable.vue'
import CustomerPointsTable from './components/CustomerPointsTable.vue'
import CustomerProfileCard from './components/CustomerProfileCard.vue'
import CustomerRechargesTable from './components/CustomerRechargesTable.vue'

// 客户详情页编排（07-UI.md:44-46）：档案头部 + 消费/充值/余额/积分 4 个 tab；
// 头部提供 充值 / 快速消费 / 余额调整（仅 admin）入口。
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isAdmin = computed(() => auth.role === 'admin')
const isMobile = useIsMobile()
/** 手机端操作按钮加大到 large（触控目标 ≥44px，plan todo 53/54） */
const actionSize = computed(() => (isMobile.value ? 'large' : 'default'))

const customerId = computed(() => Number(String(route.params.id)))
const customer = ref<Customer | null>(null)
const loading = ref(false)
/** 最近消费：取消费记录第一条（07-UI.md:46） */
const latestOrderAt = ref<string | null>(null)
const activeTab = ref('orders')
const adjustVisible = ref(false)
/** 余额流水/充值记录 tab 的重载版本：余额调整成功后 +1，强制各 tab 重新挂载并拉取 */
const ledgerVersion = ref(0)

// 路由参数变化（从列表进入其它客户）时整体重载；:key 让各 tab 子表随客户重建
watch(
  customerId,
  () => {
    adjustVisible.value = false
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

/** 充值入口（07-UI.md:68、plan todo 33）：带客户 id 进入充值页并预选该客户 */
function goRecharge(): void {
  void router.push({ path: '/recharges/new', query: { customer_id: String(customerId.value) } })
}

/** 余额调整成功：重载客户（余额）并强制各流水 tab 重新挂载拉取 */
function handleAdjusted(): void {
  ledgerVersion.value += 1
  refresh()
}

/** 充值冲正成功（plan todo 37）：余额/积分与各流水均已变化，同余额调整一并刷新（服务端真值） */
function handleRechargeRefunded(): void {
  ledgerVersion.value += 1
  refresh()
}
</script>

<style scoped>
.customer-detail__nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.customer-detail__actions {
  display: flex;
  gap: 8px;
}

/* 手机端：操作按钮换行铺满（plan todo 54） */
.customer-detail__nav--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.customer-detail__nav--mobile .customer-detail__actions {
  width: 100%;
}

.customer-detail__nav--mobile .customer-detail__actions .el-button {
  flex: 1;
  min-height: 44px;
  margin-left: 0;
}

.customer-detail__ledger {
  margin-top: 16px;
}
</style>

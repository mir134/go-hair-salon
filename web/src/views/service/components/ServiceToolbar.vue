<template>
  <!-- 手机端（<768px）：新增入口置顶 + 大控件，触控目标 ≥44px（plan todo 53/54、07-UI.md:87,119） -->
  <div
    v-if="mobile"
    class="service-toolbar service-toolbar--mobile"
  >
    <el-button
      type="primary"
      size="large"
      class="service-toolbar__create"
      @click="emit('create')"
    >
      新增服务
    </el-button>
    <el-input
      v-model="keyword"
      class="service-toolbar__search"
      size="large"
      placeholder="搜索服务名称"
      clearable
    />
    <div class="service-toolbar__row">
      <el-select
        v-model="categoryFilter"
        class="service-toolbar__filter"
        size="large"
        placeholder="全部分类"
        clearable
      >
        <el-option
          v-for="category in categories"
          :key="category.id"
          :label="category.name"
          :value="category.id"
        />
      </el-select>
      <el-select
        v-model="statusFilter"
        class="service-toolbar__status"
        size="large"
        placeholder="全部状态"
        clearable
      >
        <el-option
          label="启用"
          :value="1"
        />
        <el-option
          label="停用"
          :value="0"
        />
      </el-select>
    </div>
  </div>

  <!-- PC：新增入口排最前（左起第一），便于管理 -->
  <div
    v-else
    class="service-toolbar"
  >
    <el-button
      type="primary"
      @click="emit('create')"
    >
      新增服务
    </el-button>
    <el-input
      v-model="keyword"
      class="service-toolbar__search"
      placeholder="搜索服务名称"
      clearable
    />
    <el-select
      v-model="categoryFilter"
      class="service-toolbar__filter"
      placeholder="全部分类"
      clearable
    >
      <el-option
        v-for="category in categories"
        :key="category.id"
        :label="category.name"
        :value="category.id"
      />
    </el-select>
    <el-select
      v-model="statusFilter"
      class="service-toolbar__status"
      placeholder="全部状态"
      clearable
    >
      <el-option
        label="启用"
        :value="1"
      />
      <el-option
        label="停用"
        :value="0"
      />
    </el-select>
  </div>
</template>

<script setup lang="ts">
import type { ServiceCategory } from '@/api'

// 服务表格工具栏：筛选状态由父级持有（用于过滤），本组件只做 v-model 透传与新增入口。
// 新增入口排在工具栏最前（PC 左起第一、手机置顶），便于管理（07-UI.md:12）。
// 手机端（mobile=true）：整列布局，控件 size="large"，新增按钮全宽 ≥44px（plan todo 53/54）。
withDefaults(
  defineProps<{
    categories: ServiceCategory[]
    /** 手机布局（<768px）：渲染大号控件与全宽新增按钮变体 */
    mobile?: boolean
  }>(),
  { mobile: false },
)

const keyword = defineModel<string>('keyword', { required: true })
const categoryFilter = defineModel<number | null>('categoryFilter', { required: true })
const statusFilter = defineModel<number | null>('statusFilter', { required: true })

const emit = defineEmits<{
  create: []
}>()
</script>

<style scoped>
.service-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.service-toolbar__search {
  width: 220px;
}

.service-toolbar__filter {
  width: 160px;
}

.service-toolbar__status {
  width: 130px;
}

/* 手机端：整列布局（plan todo 53/54、07-UI.md:119） */
.service-toolbar--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.service-toolbar--mobile .service-toolbar__search {
  width: 100%;
}

.service-toolbar__row {
  display: flex;
  align-items: center;
  gap: 10px;
}

/* 手机端两枚筛选下拉等宽铺满一行（选择器覆盖 PC 固定宽度） */
.service-toolbar__row .service-toolbar__filter,
.service-toolbar__row .service-toolbar__status {
  width: auto;
  flex: 1;
}

/* 手机端新增按钮全宽 + 触控目标 ≥44px（plan todo 53） */
.service-toolbar--mobile .service-toolbar__create {
  width: 100%;
  min-height: 44px;
  margin-left: 0;
}
</style>

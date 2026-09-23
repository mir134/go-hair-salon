<template>
  <!-- 手机端（<768px）：大搜索框优先手机号 + 大按钮（plan todo 54、07-UI.md:87,114） -->
  <div
    v-if="mobile"
    class="customer-toolbar customer-toolbar--mobile"
  >
    <el-input
      ref="searchRef"
      v-model="keyword"
      class="customer-toolbar__search"
      size="large"
      inputmode="tel"
      placeholder="搜索手机号 / 姓名 / 微信号"
      clearable
      @keyup.enter="emit('search')"
    >
      <template #append>
        <el-button
          type="primary"
          @click="emit('search')"
        >
          搜索
        </el-button>
      </template>
    </el-input>
    <div class="customer-toolbar__row">
      <el-select
        v-model="tagFilter"
        class="customer-toolbar__filter"
        size="large"
        placeholder="全部标签"
        clearable
        @change="emit('search')"
      >
        <el-option
          v-for="tag in tags"
          :key="tag.id"
          :label="tag.name"
          :value="tag.id"
        />
      </el-select>
      <el-button
        size="large"
        @click="emit('reset')"
      >
        重置
      </el-button>
    </div>
    <div class="customer-toolbar__row">
      <el-tag
        v-if="recentSort"
        closable
        type="primary"
        @close="emit('clear-sort')"
      >
        按最近到访排序
      </el-tag>
      <span class="customer-toolbar__spacer" />
      <el-button
        type="primary"
        size="large"
        @click="emit('create')"
      >
        新增客户
      </el-button>
    </div>
  </div>

  <div
    v-else
    class="customer-toolbar"
  >
    <el-input
      v-model="keyword"
      class="customer-toolbar__search"
      placeholder="搜索手机号 / 姓名 / 微信号"
      clearable
      @keyup.enter="emit('search')"
    />
    <el-select
      v-model="tagFilter"
      class="customer-toolbar__filter"
      placeholder="全部标签"
      clearable
      @change="emit('search')"
    >
      <el-option
        v-for="tag in tags"
        :key="tag.id"
        :label="tag.name"
        :value="tag.id"
      />
    </el-select>
    <el-button
      type="primary"
      @click="emit('search')"
    >
      搜索
    </el-button>
    <el-button @click="emit('reset')">
      重置
    </el-button>
    <span class="customer-toolbar__spacer" />
    <el-button
      type="primary"
      @click="emit('create')"
    >
      新增客户
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { InputInstance } from 'element-plus'

import type { Tag } from '@/api'

// 客户列表工具栏：关键字（优先手机号）/标签筛选 + 新增入口。
// 筛选状态由父级持有，本组件只负责交互与 v-model 透传。
// 手机端（mobile=true）：大搜索框（自动聚焦可选）+ 大按钮（plan todo 54）。
const props = withDefaults(
  defineProps<{
    tags: Tag[]
    /** 手机布局（<768px）：渲染大号搜索/按钮变体 */
    mobile?: boolean
    /** 当前是否「按最近到访排序」（首页快捷入口 ?sort=recent 进入时展示可关闭标记） */
    recentSort?: boolean
    /** 手机端进入页面时是否自动聚焦搜索框（首页「搜索客户」快捷入口） */
    autoFocus?: boolean
  }>(),
  { mobile: false, recentSort: false, autoFocus: false },
)

const keyword = defineModel<string>('keyword', { required: true })
const tagFilter = defineModel<number | null>('tagFilter', { required: true })

const emit = defineEmits<{
  /** 关键字/标签变化或点击搜索 */
  search: []
  reset: []
  create: []
  /** 关闭「最近到访排序」标记（由父级清空 sort 条件并重新加载） */
  'clear-sort': []
}>()

const searchRef = ref<InputInstance>()

onMounted(() => {
  if (props.mobile && props.autoFocus) {
    searchRef.value?.focus()
  }
})
</script>

<style scoped>
.customer-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.customer-toolbar__search {
  width: 260px;
}

.customer-toolbar__filter {
  width: 180px;
}

.customer-toolbar__spacer {
  flex: 1;
}

/* 手机端：整行布局，触控目标 ≥44px（plan todo 53/54） */
.customer-toolbar--mobile {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.customer-toolbar--mobile .customer-toolbar__search {
  width: 100%;
}

.customer-toolbar--mobile .customer-toolbar__filter {
  width: auto;
  flex: 1;
}

.customer-toolbar__row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.customer-toolbar__row .el-button {
  min-height: 44px;
  margin-left: 0;
}
</style>

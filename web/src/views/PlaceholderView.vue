<template>
  <section class="placeholder">
    <el-card shadow="never">
      <h2 class="placeholder__title">
        {{ title }}
      </h2>
      <el-empty description="建设中" />
    </el-card>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

// 各导航项的占位页：标题取自路由 meta（后续 todo 用真实页面替换 component）
const route = useRoute()
const title = computed(() => route.meta.title ?? '')
</script>

<style scoped>
.placeholder__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
</style>

<template>
  <el-card shadow="never">
    <template #header>
      <span>3. 选择员工（可选）</span>
    </template>
    <el-select
      class="employee-card__select"
      disabled
      placeholder="暂不可选"
    />
    <p class="employee-card__hint">
      员工接口尚未落地（plan todo 34+），本单将不指定员工（employee_id 为空），可留空直接提交。
    </p>
  </el-card>
</template>

<script setup lang="ts">
// 消费第 3 步：员工（07-UI.md:50、05-TASKS.md:67）。
// 员工 CRUD API（04-API.md:194-204）尚未实现，因此这里不请求 /employees（否则页面加载会弹 404 提示），
// 选择器保持禁用并明确说明：本单 employee_id 留空，不阻塞消费。
</script>

<style scoped>
.employee-card__select {
  width: 100%;
  max-width: 320px;
}

.employee-card__hint {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>

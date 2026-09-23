<template>
  <el-card shadow="never">
    <template #header>
      <span>3. 选择员工（可选）</span>
    </template>
    <el-select
      :model-value="modelValue"
      class="employee-card__select"
      :loading="loading"
      clearable
      filterable
      placeholder="选择员工（可留空）"
      @update:model-value="handleChange"
    >
      <el-option
        v-for="employee in employees"
        :key="employee.id"
        :label="employeeLabel(employee)"
        :value="employee.id"
      />
    </el-select>
    <p
      v-if="loadError"
      class="employee-card__hint"
    >
      员工列表加载失败，可留空提交；
      <el-button
        link
        type="primary"
        @click="emit('retry')"
      >
        重试
      </el-button>
    </p>
    <p
      v-else
      class="employee-card__hint"
    >
      仅显示启用中的员工：已停用员工不可被新订单选择（plan todo 40）；留空表示不指定员工。
    </p>
  </el-card>
</template>

<script setup lang="ts">
import type { Employee } from '@/api'

// 消费第 3 步：员工（07-UI.md:50、05-TASKS.md:67）。
// 员工列表由父级（QuickConsumeView）加载：仅启用中的员工可被选择，
// 已停用员工不得出现在新订单的选择池中（plan todo 40 风险项）。
defineProps<{
  /** 选中的员工 id；null = 不指定 */
  modelValue: number | null
  employees: Employee[]
  loading: boolean
  loadError: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  retry: []
}>()

/** el-select 清空时可能给出 ''/undefined；统一归一化为 number | null */
function handleChange(value: unknown): void {
  emit('update:modelValue', typeof value === 'number' ? value : null)
}

/** 下拉展示：姓名（职位），职位为空时只显示姓名 */
function employeeLabel(employee: Employee): string {
  return employee.position === '' ? employee.name : `${employee.name}（${employee.position}）`
}
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

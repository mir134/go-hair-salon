<template>
  <el-form-item
    label="名称"
    prop="name"
  >
    <el-input
      v-model="form.name"
      maxlength="128"
      show-word-limit
      placeholder="如：男士剪发"
    />
  </el-form-item>
  <el-form-item
    label="分类"
    prop="categoryId"
  >
    <el-select
      v-model="form.categoryId"
      class="service-fields__control"
      placeholder="选择分类"
    >
      <el-option
        v-for="category in categories"
        :key="category.id"
        :label="category.name"
        :value="category.id"
      />
    </el-select>
  </el-form-item>
  <el-form-item
    label="价格（元）"
    prop="priceText"
  >
    <el-input
      v-model="form.priceText"
      class="service-fields__control"
      placeholder="如：38.00（必须大于 0，最多两位小数）"
    />
  </el-form-item>
  <el-form-item
    label="时长（分钟）"
    prop="durationText"
  >
    <el-input
      v-model="form.durationText"
      class="service-fields__duration"
      placeholder="如：30"
    />
  </el-form-item>
  <el-form-item label="状态">
    <el-radio-group v-model="form.status">
      <el-radio :value="1">
        启用
      </el-radio>
      <el-radio :value="0">
        停用
      </el-radio>
    </el-radio-group>
  </el-form-item>
  <el-form-item
    label="备注"
    prop="remark"
  >
    <el-input
      v-model="form.remark"
      type="textarea"
      :rows="2"
      maxlength="500"
      placeholder="选填"
    />
  </el-form-item>
</template>

<script setup lang="ts">
import type { ServiceCategory } from '@/api'
import type { ServiceFormModel } from '@/utils/serviceForm'

// 服务表单字段：校验上下文由父级 el-form 提供（provide/inject 跨组件生效）。
const form = defineModel<ServiceFormModel>({ required: true })

defineProps<{ categories: ServiceCategory[] }>()
</script>

<style scoped>
.service-fields__control {
  width: 100%;
}

.service-fields__duration {
  width: 160px;
}
</style>

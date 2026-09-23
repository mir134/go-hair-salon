<template>
  <el-form-item
    label="姓名"
    prop="name"
  >
    <el-input
      v-model="form.name"
      maxlength="32"
      placeholder="必填"
    />
  </el-form-item>
  <el-form-item
    label="手机号"
    prop="phone"
  >
    <el-input
      v-model="form.phone"
      maxlength="20"
      placeholder="选填；非空手机号对应唯一客户"
    />
  </el-form-item>
  <el-form-item
    label="性别"
    prop="gender"
  >
    <el-select
      v-model="form.gender"
      class="customer-fields__control"
      placeholder="未填写"
      clearable
    >
      <el-option
        v-for="(label, value) in GENDER_LABELS"
        :key="value"
        :label="label"
        :value="value"
      />
    </el-select>
  </el-form-item>
  <el-form-item
    label="生日"
    prop="birthday"
  >
    <el-date-picker
      v-model="form.birthday"
      class="customer-fields__control"
      type="date"
      value-format="YYYY-MM-DD"
      placeholder="选择日期"
    />
  </el-form-item>
  <el-form-item
    label="微信号"
    prop="wechat"
  >
    <el-input
      v-model="form.wechat"
      maxlength="64"
      placeholder="选填"
    />
  </el-form-item>
  <el-form-item
    label="来源"
    prop="source"
  >
    <el-input
      v-model="form.source"
      maxlength="32"
      placeholder="选填，如：朋友介绍"
    />
  </el-form-item>
  <el-form-item
    label="标签"
    prop="tagIds"
  >
    <el-select
      v-model="form.tagIds"
      class="customer-fields__control"
      multiple
      clearable
      placeholder="选择标签"
    >
      <el-option
        v-for="tag in tags"
        :key="tag.id"
        :label="tag.name"
        :value="tag.id"
      />
    </el-select>
  </el-form-item>
  <el-form-item
    label="备注"
    prop="remark"
  >
    <el-input
      v-model="form.remark"
      type="textarea"
      :rows="3"
      maxlength="500"
      placeholder="选填"
    />
  </el-form-item>
</template>

<script setup lang="ts">
import type { Tag } from '@/api'
import { GENDER_LABELS } from '@/constants'
import type { CustomerProfileForm } from '@/utils/customerProfile'

// 客户表单字段：校验上下文由父级 el-form 提供（provide/inject 跨组件生效）。
const form = defineModel<CustomerProfileForm>({ required: true })

defineProps<{ tags: Tag[] }>()
</script>

<style scoped>
.customer-fields__control {
  width: 100%;
}
</style>

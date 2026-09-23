<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="560px"
    :close-on-click-modal="false"
    @closed="handleClosed"
  >
    <el-alert
      v-if="submitError !== ''"
      :title="submitError"
      type="error"
      :closable="false"
      show-icon
      class="customer-form__alert"
    />
    <el-form
      ref="formRef"
      v-loading="loadingDetail"
      :model="form"
      :rules="rules"
      label-width="80px"
      @submit.prevent
    >
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
          class="customer-form__control"
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
          class="customer-form__control"
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
          class="customer-form__control"
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
    </el-form>
    <template #footer>
      <el-button @click="visible = false">
        取消
      </el-button>
      <el-button
        type="primary"
        :loading="submitting"
        :disabled="loadFailed"
        @click="handleSubmit"
      >
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'

import {
  ApiError,
  attachCustomerTag,
  createCustomer,
  detachCustomerTag,
  getCustomer,
  updateCustomer,
} from '@/api'
import type { CustomerPayload, Tag } from '@/api'
import { GENDER_LABELS } from '@/constants'

// 新增 = customerId 为 null；编辑 = 传入客户 id（打开弹窗后拉取详情，含标签）。
const props = defineProps<{
  modelValue: boolean
  customerId: number | null
  tags: Tag[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  /** 资料保存成功（标签可能部分失败）：父组件据此刷新列表 */
  saved: []
}>()

interface CustomerForm {
  name: string
  phone: string
  gender: string
  birthday: string | null
  wechat: string
  source: string
  remark: string
  tagIds: number[]
}

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const dialogTitle = computed(() => (props.customerId === null ? '新增客户' : '编辑客户'))

const formRef = ref<FormInstance>()
const form = reactive<CustomerForm>(emptyForm())
const submitting = ref(false)
const loadingDetail = ref(false)
/** 编辑时详情加载失败：禁止保存，避免把空表单覆盖到已有客户 */
const loadFailed = ref(false)
const submitError = ref('')
/** 编辑前的标签集合（仅未删除标签可编辑；历史软删除标签保持原样） */
const originalTagIds = ref<number[]>([])

const rules: FormRules<CustomerForm> = {
  name: [{ required: true, message: '请输入客户姓名', trigger: 'blur' }],
}

watch(visible, (open) => {
  if (open) {
    void prepare()
  }
})

function emptyForm(): CustomerForm {
  return {
    name: '',
    phone: '',
    gender: '',
    birthday: null,
    wechat: '',
    source: '',
    remark: '',
    tagIds: [],
  }
}

/** 打开弹窗时初始化：新增清空表单；编辑拉取详情（含标签）回填 */
async function prepare(): Promise<void> {
  submitError.value = ''
  loadFailed.value = false
  Object.assign(form, emptyForm())
  originalTagIds.value = []
  const customerId = props.customerId
  if (customerId === null) {
    return
  }
  loadingDetail.value = true
  try {
    const detail = await getCustomer(customerId)
    const editableTagIds = detail.tags.filter((tag) => !tag.deleted).map((tag) => tag.id)
    originalTagIds.value = editableTagIds
    Object.assign(form, {
      name: detail.name,
      phone: detail.phone,
      gender: detail.gender,
      birthday: detail.birthday,
      wechat: detail.wechat,
      source: detail.source,
      remark: detail.remark,
      tagIds: [...editableTagIds],
    })
  } catch (error) {
    // 拦截器已提示；同时禁止保存，避免空表单覆盖原数据
    loadFailed.value = true
    submitError.value = errorMessage(error)
  } finally {
    loadingDetail.value = false
  }
}

function handleClosed(): void {
  formRef.value?.clearValidate()
  submitError.value = ''
}

async function handleSubmit(): Promise<void> {
  submitError.value = ''
  if (formRef.value === undefined) {
    return
  }
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) {
    return
  }
  const payload: CustomerPayload = {
    name: form.name.trim(),
    phone: form.phone.trim(),
    gender: form.gender,
    birthday: form.birthday,
    wechat: form.wechat.trim(),
    source: form.source.trim(),
    remark: form.remark,
  }
  submitting.value = true
  try {
    let customerId: number
    if (props.customerId === null) {
      customerId = (await createCustomer(payload)).id
    } else {
      customerId = props.customerId
      await updateCustomer(customerId, payload)
    }
    emit('saved')
    visible.value = false
    try {
      await syncTags(customerId, [...originalTagIds.value], [...form.tagIds])
    } catch (error) {
      ElMessage.warning(`客户资料已保存，但标签未全部保存：${errorMessage(error)}`)
    }
  } catch (error) {
    // 重复手机号（409）等业务错误：就地展示后端文案（07-UI.md:88）
    submitError.value = errorMessage(error)
  } finally {
    submitting.value = false
  }
}

/** 差异同步标签：新增的挂载、取消的摘除；历史软删除标签不在编辑集合内，保持原样 */
async function syncTags(customerId: number, before: number[], after: number[]): Promise<void> {
  const toAttach = after.filter((tagId) => !before.includes(tagId))
  const toDetach = before.filter((tagId) => !after.includes(tagId))
  for (const tagId of toAttach) {
    await attachCustomerTag(customerId, tagId)
  }
  for (const tagId of toDetach) {
    await detachCustomerTag(customerId, tagId)
  }
}

function errorMessage(error: unknown): string {
  return error instanceof ApiError ? error.message : '保存失败，请稍后重试'
}
</script>

<style scoped>
.customer-form__alert {
  margin-bottom: 16px;
}

.customer-form__control {
  width: 100%;
}
</style>

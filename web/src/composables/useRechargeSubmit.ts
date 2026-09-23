import { ref, type Ref } from 'vue'

import { ApiError, createRecharge, getCustomer } from '@/api'
import type { Recharge, RechargeCreatePayload } from '@/api'

/** useRechargeSubmit 返回句柄：提交状态 + 结果记录 + 客户最新余额（plan todo 33） */
export interface RechargeSubmit {
  submitting: Ref<boolean>
  /** 后端业务错误文案（400 金额非法 / 404 客户不存在等），就地展示；空串=无错误 */
  errorMessage: Ref<string>
  /** 成功后的充值记录（服务端返回）；null=尚未成功 */
  result: Ref<Recharge | null>
  /** 成功后的客户最新余额（getCustomer 回读的服务端真值）；失败为 null */
  balanceAfter: Ref<number | null>
  /** 提交充值；返回是否成功（失败时 errorMessage 已填充，表单与幂等键由调用方保留） */
  submit: (payload: RechargeCreatePayload) => Promise<boolean>
  /** 清空结果与错误（再充一笔） */
  reset: () => void
}

/**
 * 充值提交与结果状态（07-UI.md:68-74、04-API.md:162-174）：
 * - 成功（201 新建 / 200 幂等命中）：回读客户最新余额供核对，不在本地做金额加减；
 * - 失败：保留表单与幂等键，后端文案就地展示，重试复用同一 request_id 不会重复入账。
 */
export function useRechargeSubmit(): RechargeSubmit {
  const submitting = ref(false)
  const errorMessage = ref('')
  const result = ref<Recharge | null>(null)
  const balanceAfter = ref<number | null>(null)

  async function submit(payload: RechargeCreatePayload): Promise<boolean> {
    submitting.value = true
    errorMessage.value = ''
    try {
      const recharge = await createRecharge(payload)
      result.value = recharge
      balanceAfter.value = await fetchBalance(recharge.customer_id)
      return true
    } catch (error) {
      // 失败不丢表单、不重置幂等键：重试仍复用同一 request_id（04-API.md:283-286）
      errorMessage.value = error instanceof ApiError ? error.message : '请求失败，请稍后重试'
      return false
    } finally {
      submitting.value = false
    }
  }

  async function fetchBalance(customerId: number): Promise<number | null> {
    try {
      return (await getCustomer(customerId)).balance_cents
    } catch {
      // 拦截器已提示；余额显示降级为 null，充值结果仍然有效
      return null
    }
  }

  function reset(): void {
    result.value = null
    balanceAfter.value = null
    errorMessage.value = ''
  }

  return { submitting, errorMessage, result, balanceAfter, submit, reset }
}

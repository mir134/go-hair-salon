import { ref, type Ref } from 'vue'

import { ApiError, createOrder, getCustomer } from '@/api'
import type { Order, OrderCreatePayload, OrderSubmitStatus } from '@/api'

/** useConsumeSubmit 返回句柄：提交状态 + 结果订单 + 最新余额（plan todo 23/29） */
export interface ConsumeSubmit {
  submitting: Ref<boolean>
  /** 后端业务错误文案（如 422 余额不足），就地展示；空串=无错误 */
  errorMessage: Ref<string>
  /** 提交成功后的订单（直接完成或挂单）；null=尚未成功 */
  result: Ref<Order | null>
  /** 直接完成后的客户最新余额（服务端真值）；挂单/失败为 null */
  balanceAfter: Ref<number | null>
  /** 提交订单；返回是否成功（失败时 errorMessage 已填充，表单与幂等键由调用方保留） */
  submit: (payload: OrderCreatePayload, mode: OrderSubmitStatus) => Promise<boolean>
  /** 清空结果与错误（再开一单） */
  reset: () => void
}

/**
 * 快速消费的提交与结果状态（07-UI.md:52-67）：
 * - 直接完成（completed）：同一事务入账，成功后拉取客户最新余额供核对；
 * - 挂单（pending）：不产生任何资金/余额/积分变动，不拉取余额；
 * - 失败（如 422 余额不足、409 冲突）：保留表单与幂等键，错误文案就地展示。
 */
export function useConsumeSubmit(): ConsumeSubmit {
  const submitting = ref(false)
  const errorMessage = ref('')
  const result = ref<Order | null>(null)
  const balanceAfter = ref<number | null>(null)

  async function submit(payload: OrderCreatePayload, mode: OrderSubmitStatus): Promise<boolean> {
    submitting.value = true
    errorMessage.value = ''
    try {
      const order = await createOrder(payload)
      result.value = order
      // 挂单不收款：无余额变动可核对（06-BUSINESS-RULES.md:30）
      if (mode === 'completed') {
        balanceAfter.value = await fetchBalance(order.customer_id)
      }
      return true
    } catch (error) {
      // 失败不丢表单、不重置幂等键：后端文案就地展示，重试仍复用同一 request_id
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
      // 拦截器已提示；余额显示降级为 null，订单结果仍然有效
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

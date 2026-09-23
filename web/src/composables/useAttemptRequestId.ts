import { ref, watch } from 'vue'

import { generateRequestId } from '@/utils/requestId'

/** useAttemptRequestId 返回句柄：只在一次「提交尝试」内保证幂等键稳定 */
export interface AttemptRequestId {
  /** 返回当前幂等键；没有则生成一个并用本次提交复用 */
  ensure: () => string
  /** 提交成功后清除；下一次提交会重新生成 */
  clear: () => void
}

/**
 * 幂等键（request_id）生命周期（04-API.md:283-286、06-BUSINESS-RULES.md:29）。
 *
 * 同一「提交尝试」只生成一次：`ensure()` 返回现有值或新建。
 * 表单签名（signature）变化 = 这是另一次提交 → 作废旧键；
 * 因此失败重试（表单未变）复用原 request_id，后端据此判定重复提交、不重复入账。
 */
export function useAttemptRequestId(signature: () => string): AttemptRequestId {
  const requestId = ref<string | null>(null)

  watch(signature, () => {
    requestId.value = null
  })

  function ensure(): string {
    if (requestId.value === null) {
      requestId.value = generateRequestId()
    }
    return requestId.value
  }

  function clear(): void {
    requestId.value = null
  }

  return { ensure, clear }
}

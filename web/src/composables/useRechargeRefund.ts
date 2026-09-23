import { ref, type Ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

import { refundRecharge } from '@/api'
import { formatCents } from '@/utils/format'

/**
 * 充值冲正（04-API.md:163-164、plan todo 36-37）：仅 admin 的危险账务操作。
 *
 * 二次确认（07-UI.md:89）后调用 POST /recharges/:id/refund；充值列表页与客户详情
 * 「充值记录」tab 共用本 composable，避免两处重复实现确认与调用逻辑（AGENTS.md §7）。
 */
/**
 * 充值冲正结果：调用方据此决定是否刷新（409 重复冲正等失败也要同步服务端状态）。
 * - cancelled：用户取消二次确认，未发请求；
 * - failed：后端拒绝（403/404/409/422 等），文案由 axios 拦截器弹出；
 * - refunded：冲正成功，余额/流水/记录状态均已变化。
 */
export type RechargeRefundOutcome = 'cancelled' | 'failed' | 'refunded'

export interface RechargeRefundHandle {
  /** 正在冲正的记录 id：按钮据此 loading，防重复点击 */
  refundingId: Ref<number | null>
  /** 二次确认并冲正；按 outcome 决定刷新（成功与冲突均需刷新，取消不需要） */
  refund: (
    id: number,
    refundCents: number,
    customerName: string,
  ) => Promise<RechargeRefundOutcome>
}

export function useRechargeRefund(): RechargeRefundHandle {
  const refundingId = ref<number | null>(null)

  async function refund(
    id: number,
    refundCents: number,
    customerName: string,
  ): Promise<RechargeRefundOutcome> {
    const customer = customerName !== '' ? `「${customerName}」的` : ''
    try {
      await ElMessageBox.confirm(
        `确认冲正${customer}充值记录（扣回余额 ¥${formatCents(refundCents)}）？` +
          '冲正后记录状态变为「已冲正」并写入反向余额流水；客户余额不足以扣回时将被拒绝（422）。本操作不可撤销。',
        '充值冲正二次确认',
        { type: 'warning', confirmButtonText: '确认冲正', cancelButtonText: '返回' },
      )
    } catch {
      return 'cancelled' // 用户取消
    }

    refundingId.value = id
    try {
      await refundRecharge(id)
      ElMessage.success('充值已冲正')
      return 'refunded'
    } catch {
      // 拦截器已提示后端文案（403 无权限 / 409 重复冲正 / 422 余额不足等）
      return 'failed'
    } finally {
      refundingId.value = null
    }
  }

  return { refundingId, refund }
}

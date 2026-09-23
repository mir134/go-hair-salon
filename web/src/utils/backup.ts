/**
 * 备份页展示与二次确认工具（plan todo 52）。
 *
 * 这里只做展示格式化与输入校验，不参与任何账务计算。
 */

/** 字节数 → 可读大小：1024 → "1.0 KB"、0 → "0 B"（整数运算，避免展示层浮点误差） */
export function formatBytes(bytes: number): string {
  const value = Math.trunc(bytes)
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB'] as const
  let divisor = 1
  let unitIndex = 0
  while (unitIndex < units.length - 1 && value >= divisor * 1024) {
    divisor *= 1024
    unitIndex += 1
  }
  if (unitIndex === 0) {
    return `${value} B`
  }
  const whole = Math.floor(value / divisor)
  const tenths = Math.floor(((value % divisor) * 10) / divisor)
  return `${whole}.${tenths} ${units[unitIndex]}`
}

/** 恢复二次确认要求用户原样输入的文本（type-to-confirm） */
export const RESTORE_CONFIRM_TEXT = '恢复'

/** 类型确认输入是否匹配（去首尾空格后精确匹配） */
export function isRestoreConfirmMatched(input: string): boolean {
  return input.trim() === RESTORE_CONFIRM_TEXT
}

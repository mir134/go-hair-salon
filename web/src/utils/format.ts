/**
 * 展示层格式化工具（07-UI.md:90 金额统一显示两位小数）。
 *
 * 金额在后端一律是整数分；本文件只做「分 → 元」的整数运算显示，
 * 不参与任何计算或提交，避免浮点误差进入账务链路。
 */

/** 整数分 → 两位小数字符串：1230 → "12.30"、-5 → "-0.05"、0 → "0.00" */
export function formatCents(cents: number): string {
  const total = Math.trunc(cents)
  const sign = total < 0 ? '-' : ''
  const abs = Math.abs(total)
  const yuan = Math.floor(abs / 100)
  const fen = abs % 100
  return `${sign}${yuan}.${String(fen).padStart(2, '0')}`
}

/** ISO 时间（UTC）→ 本地 "YYYY-MM-DD HH:mm"；空值/非法值显示 "—" */
export function formatDateTime(value: string | null | undefined): string {
  if (value === null || value === undefined || value === '') {
    return '—'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }
  return `${formatDay(date)} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** 日期 → "YYYY-MM-DD"（本地时区） */
export function formatDay(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

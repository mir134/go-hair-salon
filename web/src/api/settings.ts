import { get, put } from './http'

/**
 * 系统设置 DTO，对齐 server/internal/model/system.go 的 Setting
 * （03-DATABASE.md:242-263）：value 一律字符串（points_per_yuan 也是十进制串）。
 */
export interface Setting {
  id: number
  key: string
  value: string
  description: string
  updated_by: number | null
  updated_at: string
}

/** MVP 公开设置键（04-API.md:206-215、03-DATABASE.md:256-261）。 */
export const SETTING_KEY_SHOP_NAME = 'shop_name'
export const SETTING_KEY_POINTS_PER_YUAN = 'points_per_yuan'

/**
 * GET /api/v1/settings（both）：仅返回公开键 shop_name / points_per_yuan，
 * 值为服务端现值（不写死默认）。
 */
export function listSettings(): Promise<Setting[]> {
  return get<Setting[]>('/settings')
}

/**
 * PUT /api/v1/settings/:key（admin）：value 一律字符串。
 * 未知键后端返回 404；非法值（比例非正整数、店名空白/超长）返回 400，由拦截器提示。
 */
export function updateSetting(key: string, value: string): Promise<Setting> {
  return put<Setting>(`/settings/${encodeURIComponent(key)}`, { value })
}

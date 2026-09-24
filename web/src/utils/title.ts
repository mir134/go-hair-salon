import { DEFAULT_SHOP_NAME } from '@/constants'

/**
 * 网页标题统一格式：「{店名} - 客户管理系统」。
 *
 * - 登录页从 GET /shop 读取店名后设置；
 * - 已登录态由 AppShell 监听 settings.shopName 变化同步更新；
 * - 管理员保存店名后 settings store 即时更新，标题随之变化；
 * - SSR/非浏览器环境直接忽略（本项目是纯 SPA，保留兜底）。
 *
 * 空值或纯空白时回退到 DEFAULT_SHOP_NAME，保证标题永远有意义。
 */
export function setPageTitle(shopName: string | null | undefined): void {
  if (typeof document === 'undefined') {
    return
  }
  const name = (shopName ?? '').trim() || DEFAULT_SHOP_NAME
  document.title = `${name} - 客户管理系统`
}

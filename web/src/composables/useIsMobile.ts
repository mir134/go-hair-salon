import { onBeforeUnmount, onMounted, readonly, ref, type Ref } from 'vue'

/**
 * 手机布局断点（plan todo 53、07-UI.md:87、02-AGENTS.md:88-92）：
 * 视口 <768px 使用「底部 Tab + 卡片/列表 + 大触控区域」的手机布局。
 */
export const MOBILE_BREAKPOINT_PX = 768

/** matchMedia 查询串：与 MOBILE_BREAKPOINT_PX 必须保持对应（767 = 768 - 1） */
export const MOBILE_MEDIA_QUERY = `(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`

/**
 * 是否为手机视口（<768px）。
 *
 * 用 matchMedia 而不是 resize 事件：断点跨越时自动回调，且初始值在 setup 阶段
 * 即同步可得，避免手机首屏先渲染 PC 布局再跳变。
 * 不支持 matchMedia 的环境（SSR/单测）退化为 false，即沿用 PC 布局。
 */
export function useIsMobile(): Readonly<Ref<boolean>> {
  const query = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(MOBILE_MEDIA_QUERY)
    : null
  const isMobile = ref(query?.matches ?? false)

  function handleChange(event: MediaQueryListEvent): void {
    isMobile.value = event.matches
  }

  onMounted(() => {
    query?.addEventListener('change', handleChange)
  })
  onBeforeUnmount(() => {
    query?.removeEventListener('change', handleChange)
  })

  return readonly(isMobile)
}

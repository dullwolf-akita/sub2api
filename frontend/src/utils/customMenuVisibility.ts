import type { CustomMenuItem } from '@/types'

/** 菜单是否仅渠道代理人可见（显式配置或 URL 含 channel-agent） */
export function customMenuRequiresChannelAgent(item: CustomMenuItem): boolean {
  if (item.visibility === 'channel_agent') return true
  return /channel-agent/i.test(item.url || '')
}

/** 用户侧是否展示该自定义菜单项 */
export function isCustomMenuVisibleToUser(
  item: CustomMenuItem,
  isChannelAgent: boolean | null,
): boolean {
  if (item.visibility === 'admin') return false
  if (customMenuRequiresChannelAgent(item)) {
    return isChannelAgent === true
  }
  return item.visibility === 'user'
}

/** 从自定义菜单 URL 提取可用于 channel-agent API 的 origin 列表 */
export function collectChannelAgentApiOrigins(items: CustomMenuItem[]): string[] {
  const origins = new Set<string>()
  for (const item of items) {
    if (!customMenuRequiresChannelAgent(item) || !item.url?.startsWith('http')) continue
    try {
      origins.add(new URL(item.url).origin)
    } catch {
      // ignore invalid URL
    }
  }
  return [...origins]
}

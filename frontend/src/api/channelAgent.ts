import { apiClient } from './client'

/**
 * 检查当前用户是否为渠道代理人（8080 后端）。
 * 403 表示已认证但非代理人；404 表示当前 origin 无此 API，由调用方尝试其它 origin。
 */
export async function checkChannelAgentOnOrigin(origin: string, token: string): Promise<'agent' | 'not_agent' | 'unavailable'> {
  try {
    const res = await fetch(`${origin}/api/v1/channel-agent/dashboard`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    if (res.ok) return 'agent'
    if (res.status === 403) return 'not_agent'
    return 'unavailable'
  } catch {
    return 'unavailable'
  }
}

/** 同源优先，失败时尝试自定义菜单里配置的外链 origin */
export async function checkChannelAgentAccess(fallbackOrigins: string[] = []): Promise<boolean> {
  const token = localStorage.getItem('auth_token')
  if (!token) return false

  try {
    await apiClient.get('/channel-agent/dashboard')
    return true
  } catch (err: unknown) {
    const status = (err as { response?: { status?: number } })?.response?.status
    if (status === 403) return false
  }

  const origins = [
    typeof window !== 'undefined' ? window.location.origin : '',
    ...fallbackOrigins,
  ].filter(Boolean)

  let sawForbidden = false
  for (const origin of [...new Set(origins)]) {
    const result = await checkChannelAgentOnOrigin(origin, token)
    if (result === 'agent') return true
    if (result === 'not_agent') sawForbidden = true
  }

  return sawForbidden ? false : false
}

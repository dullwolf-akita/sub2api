import { onMounted, ref, watch } from 'vue'
import { checkChannelAgentAccess } from '@/api/channelAgent'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { collectChannelAgentApiOrigins, customMenuRequiresChannelAgent } from '@/utils/customMenuVisibility'

/**
 * 渠道代理人身份（用于隐藏非代理人不可见的自定义菜单）。
 * null = 尚未请求完成。
 */
export function useChannelAgentAccess() {
  const authStore = useAuthStore()
  const appStore = useAppStore()
  const isChannelAgent = ref<boolean | null>(null)
  const loading = ref(false)

  function menuNeedsAgentCheck(): boolean {
    const items = appStore.cachedPublicSettings?.custom_menu_items ?? []
    return items.some((item) => customMenuRequiresChannelAgent(item))
  }

  async function refresh(): Promise<boolean> {
    if (!authStore.isAuthenticated || !authStore.token) {
      isChannelAgent.value = false
      return false
    }
    if (!menuNeedsAgentCheck()) {
      isChannelAgent.value = false
      return false
    }

    loading.value = true
    try {
      const items = appStore.cachedPublicSettings?.custom_menu_items ?? []
      const ok = await checkChannelAgentAccess(collectChannelAgentApiOrigins(items))
      isChannelAgent.value = ok
      return ok
    } catch {
      isChannelAgent.value = false
      return false
    } finally {
      loading.value = false
    }
  }

  onMounted(() => {
    if (authStore.isAuthenticated && menuNeedsAgentCheck()) {
      void refresh()
    } else {
      isChannelAgent.value = false
    }
  })

  watch(
    () => authStore.isAuthenticated,
    (authed) => {
      if (authed && menuNeedsAgentCheck()) {
        void refresh()
      } else {
        isChannelAgent.value = false
      }
    },
  )

  watch(
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => {
      if (authStore.isAuthenticated && menuNeedsAgentCheck()) {
        void refresh()
      }
    },
    { deep: true },
  )

  return { isChannelAgent, loading, refresh }
}

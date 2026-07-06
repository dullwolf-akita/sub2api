/** 整页跳转到独立登录页（5173），保留 redirect 回跳参数 */
export function redirectToLoginPage(redirectPath?: string): void {
  const params = new URLSearchParams()
  if (redirectPath && redirectPath !== '/login') {
    params.set('redirect', redirectPath)
  }
  const query = params.toString()
  window.location.href = query ? `/login?${query}` : '/login'
}

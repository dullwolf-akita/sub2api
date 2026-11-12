/**
 * 部分生产构建会去掉 vue-i18n 的消息编译器，此时 t() 会把 "{count}" 原样返回。
 * 先走正常插值，再按占位符补一层替换，两种构建都能显示数字。
 * 项目 tsconfig 的 lib 是 ES2020，不能用 replaceAll（ES2021）。
 * split/join 语义相同，也避免占位符里的特殊字符被当成正则。
 */
export function fillI18nParams(
  message: string,
  params: Record<string, string | number>
): string {
  let s = String(message)
  for (const [k, v] of Object.entries(params)) {
    s = s.split(`{${k}}`).join(String(v))
  }
  return s
}

// vue-i18n 的 t 重载很多；参数用宽松类型，避免 ComposerTranslation 赋不进去。
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Translate = (key: string, params?: any) => unknown

export function makeTFill(t: Translate) {
  return function tFill(key: string, params: Record<string, string | number>): string {
    return fillI18nParams(String(t(key, params)), params)
  }
}

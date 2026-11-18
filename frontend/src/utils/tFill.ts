/**
 * 部分生产构建会去掉 vue-i18n 的消息编译器，此时 t() 会把 "{count}" 原样返回。
 * 先走正常插值，再按占位符补一层替换，两种构建都能显示数字。
 * 项目 tsconfig 的 lib 是 ES2020，不能用 replaceAll（ES2021）。
 * split/join 语义相同，也避免占位符里的特殊字符被当成正则。
 */

const wrappedComposers = new WeakSet<object>()

const TRANSLATE_OPTION_KEYS = new Set([
  'locale',
  'missingWarn',
  'fallbackWarn',
  'fallbackLocale',
  'fallbackFormat',
  'escapeParameter',
  'default',
  'resolvedMessage',
  'plural',
  'list',
  'named'
])

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function isTranslateOptions(obj: Record<string, unknown>): boolean {
  const keys = Object.keys(obj)
  return keys.length > 0 && keys.every((key) => TRANSLATE_OPTION_KEYS.has(key))
}

function scalarToText(value: unknown): string | null {
  if (typeof value === 'string' || typeof value === 'number') {
    return String(value)
  }
  if (typeof value === 'boolean') {
    return String(value)
  }
  return null
}

export function fillI18nParams(
  message: string,
  params: Record<string, string | number> | ReadonlyArray<unknown>
): string {
  let s = String(message)
  if (Array.isArray(params)) {
    for (let i = 0; i < params.length; i++) {
      const text = scalarToText(params[i])
      if (text == null) {
        continue
      }
      s = s.split(`{${i}}`).join(text)
    }
    return s
  }
  for (const [k, v] of Object.entries(params)) {
    s = s.split(`{${k}}`).join(String(v))
  }
  return s
}

/**
 * 从 t() 在 key 之后的参数里取出可用于补替换的命名对象或列表。
 * 字符串兜底文案、locale 字符串、TranslateOptions 都不会当成插值参数。
 */
export function paramsFromTranslateArgs(
  args: unknown[]
): Record<string, string | number> | unknown[] | null {
  for (const arg of args) {
    if (Array.isArray(arg)) {
      return arg
    }
    if (isPlainObject(arg) && !isTranslateOptions(arg)) {
      const out: Record<string, string | number> = {}
      for (const [k, v] of Object.entries(arg)) {
        const text = scalarToText(v)
        if (text != null) {
          out[k] = typeof v === 'number' ? v : text
        }
      }
      return Object.keys(out).length > 0 ? out : null
    }
  }
  return null
}

// vue-i18n 的 t 重载很多；参数用宽松类型，避免 ComposerTranslation 赋不进去。
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Translate = (key: string, ...args: any[]) => unknown

export function wrapTranslate(t: Translate): Translate {
  return function wrappedT(key: string, ...args: unknown[]) {
    const result = t(key, ...args)
    const params = paramsFromTranslateArgs(args)
    if (params == null || (typeof result !== 'string' && typeof result !== 'number')) {
      return result
    }
    return fillI18nParams(String(result), params)
  }
}

export function makeTFill(t: Translate) {
  return function tFill(key: string, params: Record<string, string | number>): string {
    return fillI18nParams(String(t(key, params)), params)
  }
}

// composer 用宽松类型，避免 vue-i18n Composer.t 的重载/只读签名在赋值时报错。
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function installTFillOnComposer(composer: { t: Translate } | any): void {
  if (!composer || typeof composer.t !== 'function' || wrappedComposers.has(composer)) {
    return
  }
  composer.t = wrapTranslate(composer.t.bind(composer))
  wrappedComposers.add(composer)
}

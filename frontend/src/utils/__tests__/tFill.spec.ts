import { describe, expect, it } from 'vitest'
import {
  fillI18nParams,
  installTFillOnComposer,
  makeTFill,
  paramsFromTranslateArgs,
  wrapTranslate
} from '@/utils/tFill'

describe('fillI18nParams', () => {
  it('replaces named placeholders when the compiler left them raw', () => {
    expect(fillI18nParams('{count} 个 IP 待获取地区', { count: 3 })).toBe('3 个 IP 待获取地区')
  })

  it('is a no-op when the value was already interpolated', () => {
    expect(fillI18nParams('3 个 IP 待获取地区', { count: 3 })).toBe('3 个 IP 待获取地区')
  })

  it('does not treat replacement text as a regular expression', () => {
    expect(fillI18nParams('reset at {time}', { time: '12:00 $1' })).toBe('reset at 12:00 $1')
  })

  it('replaces list placeholders used by some admin messages', () => {
    expect(fillI18nParams('模型「{0}」在多个定价条目中重复', ['gpt-4'])).toBe('模型「gpt-4」在多个定价条目中重复')
  })
})

describe('paramsFromTranslateArgs', () => {
  it('picks a named interpolation object', () => {
    expect(paramsFromTranslateArgs([{ count: 4 }])).toEqual({ count: 4 })
  })

  it('ignores fallback strings and translate options', () => {
    expect(paramsFromTranslateArgs(['Search channels...'])).toBeNull()
    expect(paramsFromTranslateArgs([{ locale: 'zh', missingWarn: false }])).toBeNull()
  })

  it('returns list interpolation arguments', () => {
    expect(paramsFromTranslateArgs([['gpt-4']])).toEqual(['gpt-4'])
  })
})

describe('makeTFill', () => {
  it('fills leftovers after t() returns a raw message', () => {
    const t = (key: string) => (key === 'usage.ipGeo.pending' ? '{count} 个 IP 待获取地区' : key)
    const tFill = makeTFill(t)
    expect(tFill('usage.ipGeo.pending', { count: 2 })).toBe('2 个 IP 待获取地区')
  })

  it('still fills if t() already interpolated (idempotent)', () => {
    const t = (_key: string, params?: Record<string, unknown>) => `${params?.count} IPs pending`
    const tFill = makeTFill(t)
    expect(tFill('usage.ipGeo.pending', { count: 2 })).toBe('2 IPs pending')
  })
})

describe('wrapTranslate', () => {
  it('fills leftovers for ordinary t() calls', () => {
    const t = (key: string) =>
      key === 'admin.riskControl.apiKeyRows' ? '{count} 个密钥' : key
    const wrapped = wrapTranslate(t)
    expect(wrapped('admin.riskControl.apiKeyRows', { count: 6 })).toBe('6 个密钥')
  })

  it('does not treat a fallback string argument as named params', () => {
    const t = (_key: string, fallback?: unknown) =>
      typeof fallback === 'string' ? fallback : String(_key)
    const wrapped = wrapTranslate(t)
    expect(wrapped('admin.channels.searchChannels', 'Search channels...')).toBe(
      'Search channels...'
    )
  })

  it('fills list placeholders after t() returns a raw message', () => {
    const t = () => 'Model "{0}" appears in multiple pricing entries'
    const wrapped = wrapTranslate(t)
    expect(wrapped('admin.channels.duplicateModels', ['gpt-4'])).toBe(
      'Model "gpt-4" appears in multiple pricing entries'
    )
  })
})

describe('installTFillOnComposer', () => {
  it('wraps composer.t once and keeps later calls filled', () => {
    const composer = {
      t: (key: string) => (key === 'admin.audit.clearConfirm.success' ? '已删除 {count} 条' : key)
    }
    installTFillOnComposer(composer)
    const first = composer.t
    installTFillOnComposer(composer)
    expect(composer.t).toBe(first)
    expect(composer.t('admin.audit.clearConfirm.success', { count: 8 })).toBe('已删除 8 条')
  })
})

import { describe, expect, it } from 'vitest'
import { fillI18nParams, makeTFill } from '@/utils/tFill'

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

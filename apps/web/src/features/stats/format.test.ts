import { describe, expect, it } from 'vitest'
import { formatDuration } from './format'

/** 口径：秒（<1 分钟）→ 分钟（<1 小时，四舍五入）→ 小时（1 位小数） */
describe('formatDuration 累计时长人话格式', () => {
  it('0 / 负数 → 0 秒', () => {
    expect(formatDuration(0)).toBe('0 秒')
    expect(formatDuration(-5)).toBe('0 秒')
  })

  it('不足 1 分钟 → 秒', () => {
    expect(formatDuration(1_000)).toBe('1 秒')
    expect(formatDuration(59_400)).toBe('59 秒')
  })

  it('1 分钟..1 小时 → 分钟（四舍五入）', () => {
    expect(formatDuration(60_000)).toBe('1 分钟')
    expect(formatDuration(90_000)).toBe('2 分钟')
    expect(formatDuration(3_540_000)).toBe('59 分钟')
  })

  it('≥ 1 小时 → 小时（1 位小数）', () => {
    expect(formatDuration(3_600_000)).toBe('1.0 小时')
    expect(formatDuration(5_400_000)).toBe('1.5 小时')
    expect(formatDuration(45_340_000)).toBe('12.6 小时')
  })
})

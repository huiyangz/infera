import { describe, expect, it } from 'vitest'
import { axisTime, formatDuration } from './format'

describe('formatDuration（执行耗时中文紧凑口径）', () => {
  it('秒级：四舍五入到整秒', () => {
    expect(formatDuration(0)).toBe('0 秒')
    expect(formatDuration(42_000)).toBe('42 秒')
    expect(formatDuration(1_500)).toBe('2 秒')
  })

  it('分级：不足 1 小时，余秒非零时带秒', () => {
    expect(formatDuration(60_000)).toBe('1 分')
    expect(formatDuration(252_375)).toBe('4 分 12 秒')
    expect(formatDuration(3_540_000)).toBe('59 分')
  })

  it('时级：不足 1 天，余分非零时分', () => {
    expect(formatDuration(3_600_000)).toBe('1 时')
    expect(formatDuration(13_320_000)).toBe('3 时 42 分')
  })

  it('天级：跨天带余时', () => {
    expect(formatDuration(86_400_000)).toBe('1 天')
    expect(formatDuration(97_200_000)).toBe('1 天 3 时')
  })

  it('空值（running 未收尾 / 聚合无已收尾运行）与非有限值显示占位符', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
    expect(formatDuration(Number.NaN)).toBe('—')
    expect(formatDuration(-1)).toBe('—')
  })
})

describe('axisTime（时序横轴刻度标签）', () => {
  const ts = new Date(2026, 7, 22, 9, 5, 0).getTime()
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = (t: number) => {
    const d = new Date(t)
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  const mdhm = (t: number) => {
    const d = new Date(t)
    return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm(t)}`
  }

  it('跨度 ≤ 24 小时只显示 HH:mm', () => {
    const span = 6 * 3600_000
    expect(axisTime(ts, span)).toBe(hm(ts))
    expect(axisTime(ts, 24 * 3600_000)).toBe(hm(ts))
  })

  it('跨度 > 24 小时显示 MM-DD HH:mm', () => {
    expect(axisTime(ts, 24 * 3600_000 + 1)).toBe(mdhm(ts))
    expect(axisTime(ts, 3 * 24 * 3600_000)).toBe(mdhm(ts))
  })
})

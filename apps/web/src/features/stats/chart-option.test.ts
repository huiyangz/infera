import { describe, expect, it } from 'vitest'
import {
  NIGHT_END_HOUR,
  NIGHT_START_HOUR,
  buildHourlyOption,
  isNightHour,
  type StatsChartOption,
} from './chart-option'
import type { WorkspaceStatsResponse } from './types'

/** 组合 option 的 tooltip/legend/xAxis 类型为 Arrayable（对象或数组），
 *  装配只产出单对象形态——测试里窄化为断言用的最小结构（与
 *  agent-activity/chart-option.test 同构） */
type Narrow<T> = Extract<NonNullable<T>, object> & Record<string, unknown>
const tooltipOf = (o: StatsChartOption) =>
  o.tooltip as Narrow<StatsChartOption['tooltip']> & {
    trigger?: string
    formatter?: (p: unknown) => string
  }
const axisListOf = (o: StatsChartOption, k: 'xAxis' | 'yAxis') =>
  (Array.isArray(o[k]) ? o[k] : [o[k]]) as Array<
    Narrow<StatsChartOption['yAxis']> & {
      name?: string
      data?: string[]
      minInterval?: number
      position?: string
      splitLine?: { show?: boolean }
    }
  >
interface BarPoint {
  value: number
  itemStyle?: { opacity?: number }
}
interface BarSeries {
  name?: string
  type?: string
  yAxisIndex?: number
  data?: BarPoint[]
}
const seriesOf = (o: StatsChartOption) => o.series as unknown as BarSeries[]

/** 测试调色板（页面运行时从 --chart-1..5 解析；单测显式注入保持确定性） */
const PALETTE = ['#030303', '#404040', '#676f7b', '#939393', '#e7eaf0']

/** 冻结契约载荷：24 桶升序补零，夜间 2 时有量、白天 14 时有量 */
function payload(over: Partial<WorkspaceStatsResponse>): WorkspaceStatsResponse {
  const hourly = Array.from({ length: 24 }, (_, hour) => ({
    hour,
    runs: hour === 2 ? 3 : hour === 14 ? 2 : 0,
    duration_ms: hour === 2 ? 5_400_000 : hour === 14 ? 1_800_000 : 0,
  }))
  return {
    window: { from: '2026-08-18T12:00:00Z', to: '2026-08-25T12:00:00Z' },
    timezone: 'Asia/Shanghai',
    task_status: {
      total: 12,
      done: 5,
      in_progress: 3,
      todo: 2,
      cancelled: 2,
      by_status: { active: 3, queued: 1, blocked: 1, completed: 5, cancelled: 2 },
    },
    execution: {
      runs_total: 5,
      running: 0,
      done: 5,
      failed: 0,
      duration_ms_total: 7_200_000,
    },
    hourly,
    ...over,
  }
}

describe('isNightHour 夜间时段边界（约 22:00–06:00）', () => {
  it('边界常量：22 时起、6 时止（前闭后开）', () => {
    expect(NIGHT_START_HOUR).toBe(22)
    expect(NIGHT_END_HOUR).toBe(6)
  })

  it('22/23/0..5 为夜间，6..21 为白天', () => {
    for (const h of [22, 23, 0, 1, 2, 3, 4, 5]) expect(isNightHour(h)).toBe(true)
    for (const h of [6, 12, 14, 21]) expect(isNightHour(h)).toBe(false)
  })
})

describe('buildHourlyOption 执行时段分布直方图装配', () => {
  it('X 轴 24 桶：00时..23时 升序（契约恒 24 桶）', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    const labels = axisListOf(o, 'xAxis')[0].data ?? []
    expect(labels).toHaveLength(24)
    expect(labels[0]).toBe('00时')
    expect(labels[2]).toBe('02时')
    expect(labels[23]).toBe('23时')
  })

  it('两条 bar 序列：执行次数（左轴）+ 累计时长（右轴，ms→分钟）', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    const series = seriesOf(o)
    expect(series).toHaveLength(2)
    expect(series[0]).toMatchObject({ name: '执行次数', type: 'bar', yAxisIndex: 0 })
    expect(series[1]).toMatchObject({ name: '累计时长', type: 'bar', yAxisIndex: 1 })
    // 值对齐载荷：2 时 3 次 / 90 分钟，14 时 2 次 / 30 分钟
    expect(seriesOf(o)[0].data?.[2]?.value).toBe(3)
    expect(seriesOf(o)[1].data?.[2]?.value).toBe(90)
    expect(seriesOf(o)[0].data?.[14]?.value).toBe(2)
    expect(seriesOf(o)[1].data?.[14]?.value).toBe(30)
  })

  it('双 Y 轴：左次数整数刻度，右分钟；splitLine 只挂左轴', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    const ys = axisListOf(o, 'yAxis')
    expect(ys).toHaveLength(2)
    expect(ys[0].name).toBe('执行次数')
    expect(ys[0].minInterval).toBe(1)
    expect(ys[1].name).toBe('累计时长（分钟）')
    expect(ys[1].position).toBe('right')
    expect(ys[1].splitLine?.show).toBe(false)
  })

  it('夜间柱高亮：22–06 时柱体全不透明，白天柱降透明度（两序列同规则）', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    for (const s of seriesOf(o)) {
      // 夜间桶（2 时、22 时、0 时）不带降透明 itemStyle
      expect(s.data?.[2]?.itemStyle).toBeUndefined()
      expect(s.data?.[22]?.itemStyle).toBeUndefined()
      expect(s.data?.[0]?.itemStyle).toBeUndefined()
      // 白天桶（6 时边界、14 时）降透明
      expect(s.data?.[6]?.itemStyle?.opacity).toBeLessThan(1)
      expect(s.data?.[14]?.itemStyle?.opacity).toBeLessThan(1)
    }
  })

  it('序列用色来自注入调色板（第 1/4 色），轴/legend 文字用 labelColor', () => {
    const o = buildHourlyOption(payload({}), {
      palette: PALETTE,
      labelColor: '#676f7b',
    })
    expect(o.color).toEqual(PALETTE)
    const xs = axisListOf(o, 'xAxis')[0] as { axisLabel?: { color?: string } }
    expect(xs.axisLabel?.color).toBe('#676f7b')
    const legend = o.legend as Narrow<StatsChartOption['legend']> & {
      data?: string[]
    }
    expect(legend.data).toEqual(['执行次数', '累计时长'])
  })

  it('tooltip：夜间桶标注「夜间」，含次数与时长（人话格式）', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    const fmt = tooltipOf(o).formatter!
    const mk = (dataIndex: number) => [
      {
        name: `${String(dataIndex).padStart(2, '0')}时`,
        seriesName: '执行次数',
        value: 3,
        marker: '',
        dataIndex,
      },
      {
        name: `${String(dataIndex).padStart(2, '0')}时`,
        seriesName: '累计时长',
        value: 90,
        marker: '',
        dataIndex,
      },
    ]
    const night = fmt(mk(2))
    expect(night).toContain('02时（夜间）')
    expect(night).toContain('执行次数：3 次')
    expect(night).toContain('累计时长：1.5 小时')
    const day = fmt(mk(14))
    expect(day).toContain('14时')
    expect(day).not.toContain('夜间')
  })

  it('tooltip：非数组入参（单 series 点）不崩，按单行解析', () => {
    const o = buildHourlyOption(payload({}), { palette: PALETTE })
    const fmt = tooltipOf(o).formatter!
    const out = fmt({
      name: '23时',
      seriesName: '执行次数',
      value: 1,
      marker: '',
      dataIndex: 23,
    })
    expect(out).toContain('23时（夜间）')
    expect(out).toContain('1 次')
  })
})

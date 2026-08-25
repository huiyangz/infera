import { describe, expect, it } from 'vitest'
import {
  buildAgentActivityOption,
  type ActivityChartOption,
} from './chart-option'
import type { AgentActivityResponse } from './types'

/** 组合 option 的 tooltip/legend/xAxis 类型为 Arrayable（对象或数组），
 *  装配只产出单对象形态——测试里窄化为断言用的最小结构 */
type Narrow<T> = Extract<NonNullable<T>, object> &
  Record<string, unknown>
const tooltipOf = (o: ActivityChartOption) =>
  o.tooltip as Narrow<ActivityChartOption['tooltip']> & {
    trigger?: string
    formatter?: (p: unknown) => string
  }
const seriesOf = (o: ActivityChartOption) =>
  o.series as unknown as Array<{ name?: string; type?: string; data?: number[] }>
const legendOf = (o: ActivityChartOption) =>
  o.legend as Narrow<ActivityChartOption['legend']> & { data?: string[] }
const axisOf = <K extends 'xAxis' | 'yAxis'>(o: ActivityChartOption, k: K) =>
  o[k] as Narrow<ActivityChartOption[K]> & {
    type?: string
    data?: string[]
    minInterval?: number
  }

/** 测试调色板（页面运行时从 --chart-1..5 解析；单测显式注入保持确定性） */
const PALETTE = ['#030303', '#404040', '#676f7b', '#939393', '#e7eaf0']

function resp(over: Partial<AgentActivityResponse>): AgentActivityResponse {
  return {
    window: { from: '2026-08-25T04:00:00Z', to: '2026-08-25T08:00:00Z' },
    bucket_minutes: 30,
    series: [
      {
        agent_id: '7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
        agent_name: 'SDD',
        points: [
          { t: '2026-08-25T04:00:00Z', count: 1 },
          { t: '2026-08-25T04:30:00Z', count: 0 },
          { t: '2026-08-25T05:00:00Z', count: 3 },
        ],
      },
      {
        agent_id: '',
        agent_name: 'unbound',
        points: [
          { t: '2026-08-25T04:00:00Z', count: 2 },
          { t: '2026-08-25T04:30:00Z', count: 0 },
          { t: '2026-08-25T05:00:00Z', count: 5 },
        ],
      },
    ],
    ...over,
  }
}

/** 与实现独立的本地时间 HH:mm（窗口 ≤24h 口径） */
function localHM(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

describe('buildAgentActivityOption（INFERA-254：折线图 option 装配）', () => {
  it('每个 agent 一条 line 曲线：曲线数 = series 数，name = agent_name，data = 各桶 count', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(seriesOf(option)).toHaveLength(2)
    const sdd = seriesOf(option).find((s) => s.name === 'SDD')!
    const unbound = seriesOf(option).find((s) => s.name === 'unbound')!
    expect(sdd.type).toBe('line')
    expect(sdd.data).toEqual([1, 0, 3])
    expect(unbound.data).toEqual([2, 0, 5])
  })

  it('unbound 分组按普通一条曲线渲染（不特殊折叠、不丢名）', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    const unbound = seriesOf(option).find((s) => s.name === 'unbound')
    expect(unbound).toBeDefined()
    expect(unbound!.type).toBe('line')
  })

  it('X 轴为 category 时间轴：RFC3339 桶起点 → 本地 HH:mm 标签，覆盖全部桶（含零桶）', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(axisOf(option, 'xAxis').type).toBe('category')
    expect(axisOf(option, 'xAxis').data).toEqual(
      resp({}).series[0].points.map((p) => localHM(p.t))
    )
  })

  it('Y 轴为执行次数：minInterval=1（整数刻度，不给 0.5 桶）', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(axisOf(option, 'yAxis').type).toBe('value')
    expect(axisOf(option, 'yAxis').minInterval).toBe(1)
  })

  it('legend 携带全部 agent 名（显隐切换的载体），顺序与 series 一致', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(legendOf(option).data).toEqual(['SDD', 'unbound'])
  })

  it('调色板进入 option.color（单色系 chart token，暗色主题由 CSS 变量侧适配）', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(option.color).toBe(PALETTE)
  })

  it('tooltip 走 axis 触发，formatter 输出时间点 + 各 agent 次数', () => {
    const option = buildAgentActivityOption(resp({}), PALETTE)

    expect(tooltipOf(option).trigger).toBe('axis')
    const fmt = tooltipOf(option).formatter!
    const out = fmt([
      { name: localHM('2026-08-25T05:00:00Z'), seriesName: 'SDD', value: 3 },
      { name: localHM('2026-08-25T05:00:00Z'), seriesName: 'unbound', value: 5 },
    ])
    expect(out).toContain(localHM('2026-08-25T05:00:00Z'))
    expect(out).toContain('SDD')
    expect(out).toContain('3 次')
    expect(out).toContain('unbound')
    expect(out).toContain('5 次')
  })

  it('单 agent 也成立：一条曲线、legend 仍可切换', () => {
    const option = buildAgentActivityOption(
      resp({
        series: [
          {
            agent_id: 'a1',
            agent_name: 'Reviewer',
            points: [{ t: '2026-08-25T04:00:00Z', count: 4 }],
          },
        ],
      }),
      PALETTE
    )

    expect(seriesOf(option)).toHaveLength(1)
    expect(legendOf(option).data).toEqual(['Reviewer'])
    expect(axisOf(option, 'xAxis').data).toEqual([localHM('2026-08-25T04:00:00Z')])
  })
})

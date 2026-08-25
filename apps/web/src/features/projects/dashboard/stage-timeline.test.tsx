import { render, cleanup } from 'vitest-browser-react'
import { afterEach, describe, expect, it } from 'vitest'
import type { StageRunDetail } from '@/lib/infera-types'
import { AgentStageTimeline } from './stage-timeline'

/** 固定时基：2026-08-22 10:00 起的整分时间戳（本地时区，构造即断言口径） */
const T = (h: number, m: number) =>
  new Date(2026, 7, 22, h, m, 0).getTime()
const NOW = T(10, 30)
const iso = (ts: number) => new Date(ts).toISOString()

function makeRun(overrides: Partial<StageRunDetail> = {}): StageRunDetail {
  return {
    id: 'sr-1',
    delivery_id: 'd-1',
    title: '补一个设置页',
    external_issue_key: 'INFERA-79',
    stage: 'spec',
    attempt: 1,
    status: 'done',
    agent_name: 'spec-agent',
    started_at: iso(T(10, 0)),
    finished_at: iso(T(10, 4)),
    duration_ms: 4 * 60_000,
    ...overrides,
  }
}

/** 两条泳道的标准夹具：d1（agent 执行 + 失败重试 + running）、d2（门禁空跑） */
function standardRuns(): StageRunDetail[] {
  return [
    makeRun({
      id: 'sr-c',
      stage: 'unit_test',
      status: 'running',
      agent_name: 'tester',
      started_at: iso(T(10, 15)),
      finished_at: null,
      duration_ms: null,
    }),
    makeRun({
      id: 'sr-b',
      stage: 'code_gen',
      attempt: 2,
      status: 'failed',
      agent_name: 'coder',
      started_at: iso(T(10, 10)),
      finished_at: iso(T(10, 12)),
      duration_ms: 2 * 60_000,
    }),
    makeRun({
      id: 'sr-a',
      stage: 'spec',
      agent_name: 'spec-agent',
      started_at: iso(T(10, 0)),
      finished_at: iso(T(10, 4)),
      duration_ms: 4 * 60_000,
    }),
    makeRun({
      id: 'sr-d',
      delivery_id: 'd-2',
      title: '优化首页',
      external_issue_key: '',
      stage: 'spec_approval',
      status: 'done',
      agent_name: null,
      started_at: iso(T(10, 0)),
      finished_at: iso(T(10, 5)),
      duration_ms: 5 * 60_000,
    }),
  ]
}

/** 取某条 run 条的 left/width 百分比数值（style 序列化按 CSSOM 规范化，取数比较） */
function geom(selector: string): { left: number; width: number } {
  const el = document.querySelector(selector)
  const style = el?.getAttribute('style') ?? ''
  const left = Number(/left:\s*([\d.]+)%/.exec(style)?.[1] ?? NaN)
  const width = Number(/width:\s*([\d.]+)%/.exec(style)?.[1] ?? NaN)
  return { left, width }
}

function runEl(id: string) {
  return document.querySelector(`[data-run-id='${id}']`)
}

afterEach(async () => {
  await cleanup()
})

describe('AgentStageTimeline（agent 执行时序甘特区）', () => {
  it('按 delivery 分泳道：标题与 issue key 可见，最近活动者在前', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)

    const lanes = document.querySelectorAll('[data-lane]')
    expect(lanes.length).toBe(2)
    expect(lanes[0]?.getAttribute('data-lane')).toBe('d-1')
    expect(lanes[0]?.textContent).toContain('补一个设置页')
    expect(lanes[0]?.textContent).toContain('INFERA-79')
    expect(lanes[1]?.getAttribute('data-lane')).toBe('d-2')
    expect(lanes[1]?.textContent).toContain('优化首页')
  })

  it('横轴时间比例定位：起止映射为百分比 left/width（域 = 全部起止与 now 的包络）', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)

    // 域 10:00 → 10:30（30min）。sr-b 10:10–10:12 → left 33.3%、width 6.7%
    expect(geom("[data-run-id='sr-b']").left).toBeCloseTo(33.333, 1)
    expect(geom("[data-run-id='sr-b']").width).toBeCloseTo(6.667, 1)
    // sr-a 10:00–10:04 → left 0、width 13.3%
    expect(geom("[data-run-id='sr-a']").left).toBeCloseTo(0, 5)
    expect(geom("[data-run-id='sr-a']").width).toBeCloseTo(13.333, 1)
    // sr-d（d2 门禁）10:00–10:05 → left 0、width 16.7%
    expect(geom("[data-run-id='sr-d']").width).toBeCloseTo(16.667, 1)
  })

  it('running 条延伸到 now：sr-c 10:15 起 → left 50%、width 50%', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)
    expect(geom("[data-run-id='sr-c']").left).toBeCloseTo(50, 5)
    expect(geom("[data-run-id='sr-c']").width).toBeCloseTo(50, 5)
  })

  it('成败与耗时语义：failed 墨底条纹醒目、running 脉冲、done 浅墨；attempt 与时长进 tooltip', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)

    const failed = runEl('sr-b')
    expect(failed?.className).toContain('bg-foreground')
    expect(failed?.getAttribute('style')).toContain('repeating-linear-gradient')
    expect(failed?.getAttribute('data-status')).toBe('failed')
    // 可视 attempt 标注
    expect(failed?.textContent).toContain('×2')
    // tooltip：阶段中文名 + 第几次 + 成败 + agent + 耗时
    const title = failed?.getAttribute('title') ?? ''
    expect(title).toContain('实现')
    expect(title).toContain('第 2 次')
    expect(title).toContain('失败')
    expect(title).toContain('coder')
    expect(title).toContain('2 分')

    const running = runEl('sr-c')
    expect(running?.className).toContain('animate-pulse')
    expect(running?.getAttribute('title')).toContain('进行中')
    expect(running?.getAttribute('title')).toContain('tester')

    const done = runEl('sr-a')
    expect(done?.className).toContain('bg-foreground/30')
  })

  it('门禁/系统节点（agent_name 空）描边空心，与 agent 执行实心区分', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)

    const gate = runEl('sr-d')
    expect(gate?.className).toContain('border')
    expect(gate?.className).not.toContain('bg-foreground')
    expect(gate?.getAttribute('title')).toContain('门禁')
  })

  it('横轴刻度标签按域起止渲染（跨天以内 HH:mm）', async () => {
    await render(<AgentStageTimeline runs={standardRuns()} now={NOW} />)

    const ticks = Array.from(document.querySelectorAll('[data-tick]'))
    expect(ticks.length).toBe(4)
    expect(ticks[0]?.textContent?.trim()).toBe('10:00')
    expect(ticks[ticks.length - 1]?.textContent?.trim()).toBe('10:30')
  })

  it('瞬时任务（started = finished）不产生 NaN 宽度', async () => {
    await render(
      <AgentStageTimeline
        runs={[
          makeRun({
            id: 'sr-x',
            started_at: iso(T(10, 10)),
            finished_at: iso(T(10, 10)),
            duration_ms: 0,
          }),
        ]}
        now={NOW}
      />
    )
    const el = runEl('sr-x')
    expect(el?.getAttribute('style') ?? '').not.toContain('NaN')
  })

  it('有界展示：默认 6 条泳道，「查看更多」逐步展开到全部后收起按钮', async () => {
    const runs = Array.from({ length: 8 }, (_, i) =>
      makeRun({
        id: `sr-${i}`,
        delivery_id: `d-${i}`,
        title: `任务 ${i}`,
        started_at: iso(T(9, i)),
        finished_at: iso(T(9, i)),
        duration_ms: 0,
      })
    )
    const screen = await render(
      <AgentStageTimeline runs={runs} now={NOW} initialVisible={6} />
    )

    expect(document.querySelectorAll('[data-lane]').length).toBe(6)
    // 泳道按最近活动倒序：默认展示最近 6 条（任务 7…2），最旧的任务 0/1 被截断
    expect(await screen.getByText('任务 7').element()).toBeInTheDocument()
    expect(screen.getByText('任务 0').query()).toBeNull()

    await screen.getByRole('button', { name: /查看更多/ }).click()
    expect(document.querySelectorAll('[data-lane]').length).toBe(8)
    expect(await screen.getByText('任务 0').element()).toBeInTheDocument()
    expect(
      await screen.getByRole('button', { name: /查看更多/ }).query()
    ).toBeNull()
  })

  it('空态有设计：图标 + 主文案 + 引导文案，而非空白', async () => {
    const screen = await render(<AgentStageTimeline runs={[]} now={NOW} />)

    await expect
      .element(screen.getByText('暂无执行记录', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/任务开始流转后/))
      .toBeInTheDocument()
  })
})

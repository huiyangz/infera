import { render, cleanup, type RenderResult } from 'vitest-browser-react'
import { afterEach, describe, expect, it } from 'vitest'
import type { RequirementStats } from '@/lib/infera-types'
import { KpiHeader } from './kpi-header'

/** 一致夹具：桶计数之和 = requirement_total（8） */
function makeStats(
  overrides: Partial<RequirementStats> = {}
): RequirementStats {
  return {
    project_id: 'p1',
    requirement_total: 8,
    by_status: { active: 2, queued: 1, completed: 3, blocked: 1, cancelled: 1 },
    pending_decisions: 2,
    delivered: 3,
    last_synced_at: '2026-08-22T03:00:05Z',
    ...overrides,
  }
}

/** 取指定 KPI 瓦片的值区文本（label 元素的同级值节点） */
async function kpiValue(screen: RenderResult, label: string) {
  const el = await screen.getByText(label, { exact: true }).element()
  return el?.parentElement?.textContent?.replace(label, '').trim() ?? ''
}

/** 取状态分布条某分段的行内宽度样式（如 'width: 25.000%'） */
function segWidth(bucket: string) {
  return (
    document.querySelector(`[data-seg='${bucket}']`)?.getAttribute('style') ??
    ''
  )
}

afterEach(async () => {
  await cleanup()
})

describe('KpiHeader（dashboard 头部统计区）', () => {
  it('四张 KPI 瓦片：任务总数 / 待决策 / 已交付 / 最近活动（时间经 dateTime 绝对展示）', async () => {
    const screen = await render(<KpiHeader stats={makeStats()} />)

    expect(await kpiValue(screen, '任务总数')).toBe('8')
    expect(await kpiValue(screen, '待决策')).toBe('2')
    expect(await kpiValue(screen, '已交付')).toBe('3')
    // dateTime 输出含绝对年份；「暂无活动」不出现
    expect(await kpiValue(screen, '最近活动')).toMatch(/2026/)
    expect(
      await screen.getByText('暂无活动', { exact: true }).query()
    ).toBeNull()
  })

  it('last_synced_at 为 null 时最近活动显示「暂无活动」', async () => {
    const screen = await render(
      <KpiHeader stats={makeStats({ last_synced_at: null })} />
    )
    expect(await kpiValue(screen, '最近活动')).toBe('暂无活动')
  })

  it('状态分布条按桶占比定宽（分母 = 五桶计数之和），零计数桶不渲染分段', async () => {
    await render(<KpiHeader stats={makeStats()} />)

    // active 2/8 = 25%，completed 3/8 = 37.5%（style 序列化按 CSSOM 规范化）
    expect(segWidth('active')).toContain('width: 25%')
    expect(segWidth('completed')).toContain('width: 37.5%')
    expect(segWidth('queued')).toContain('width: 12.5%')
    expect(segWidth('blocked')).toContain('width: 12.5%')
    expect(segWidth('cancelled')).toContain('width: 12.5%')
  })

  it('图例五个状态桶齐全，带计数与整数百分比（StatusBadge 中文口径）', async () => {
    await render(<KpiHeader stats={makeStats()} />)

    for (const [bucket, label, pct] of [
      ['active', '进行中', '25%'],
      ['queued', '未启动', '13%'],
      ['completed', '已完成', '38%'],
      ['blocked', '已阻塞', '13%'],
      ['cancelled', '已取消', '13%'],
    ] as const) {
      const el = document.querySelector(`[data-legend='${bucket}']`)
      expect(el?.textContent).toContain(label)
      expect(el?.textContent).toContain(pct)
    }
  })

  it('空项目（全桶为 0）不渲染分段、不出现 NaN，图例计数为 0', async () => {
    const screen = await render(
      <KpiHeader
        stats={makeStats({
          requirement_total: 0,
          by_status: {
            active: 0,
            queued: 0,
            completed: 0,
            blocked: 0,
            cancelled: 0,
          },
        })}
      />
    )
    expect(await kpiValue(screen, '任务总数')).toBe('0')

    const bar = document.querySelector('[data-statusbar]')
    expect(bar?.querySelectorAll('[data-seg]').length).toBe(0)
    expect(bar?.textContent).not.toContain('NaN')
    const el = document.querySelector("[data-legend='active']")
    expect(el?.textContent).toContain('0')
    expect(el?.textContent).toContain('0%')
  })

  it('stats 未就绪时渲染骨架占位（无数字文本）', async () => {
    const screen = await render(<KpiHeader stats={undefined} />)

    const grid = document.querySelector('[data-kpi-grid]')
    expect(
      grid?.querySelectorAll('[data-slot="skeleton"]').length
    ).toBeGreaterThan(0)
    expect(
      await screen.getByText('任务总数', { exact: true }).query()
    ).toBeNull()
  })

  it('stats 未就绪时骨架不触发 <p> 嵌套 <div> 的 DOM 嵌套报错（KpiTile 路径）', async () => {
    // Skeleton 渲染为 <div>：KpiTile 里包它的外层若还是 <p>，React 会在
    // 控制台报 "`<p>` cannot contain a nested `<div>`"（INFERA-301）。该
    // 告警被 React 按组件去重，断言 console 会偶发漏报；这里直接锁定其
    // 结构成因——骨架已渲染的前提下，KPI 网格内任何 <p> 都不得再包 <div>。
    await render(<KpiHeader stats={undefined} />)

    const grid = document.querySelector('[data-kpi-grid]')
    expect(
      grid?.querySelectorAll('[data-slot="skeleton"]').length
    ).toBeGreaterThan(0)
    grid?.querySelectorAll('p').forEach((p) => {
      expect(p.querySelector('div')).toBeNull()
    })
  })
})

import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import { NODE_SEQUENCE, nodeLabel } from './types'
import { NodeTimeline } from './node-timeline'

/** 取某大节点标记的节点行 data-state（Locator → 真实 DOM 元素再取属性） */
async function stateOf(screen: RenderResult, label: string) {
  const el = await screen.getByText(label).element()
  return el.closest('[data-node]')?.getAttribute('data-state')
}

describe('NodeTimeline 大节点时间线（4+1）', () => {
  afterEach(async () => {
    await cleanup()
  })

  it('intake：首个节点 active，其余 upcoming，无异常标记', async () => {
    const screen = await render(<NodeTimeline node='intake' />)
    await expect
      .element(screen.getByText(nodeLabel('intake')))
      .toBeInTheDocument()
    for (const n of NODE_SEQUENCE) {
      expect(await stateOf(screen, nodeLabel(n))).toBe(
        n === 'intake' ? 'active' : 'upcoming'
      )
    }
    expect(screen.container.querySelector('[data-anomaly]')).toBeNull()
  })

  it('in_progress：此前节点 done、当前 active、其后 upcoming（节点推进）', async () => {
    const screen = await render(<NodeTimeline node='in_progress' />)
    await expect.element(screen.getByText('执行中')).toBeInTheDocument()
    expect(await stateOf(screen, '需求受理')).toBe('done')
    expect(await stateOf(screen, '已派发')).toBe('done')
    expect(await stateOf(screen, '执行中')).toBe('active')
    expect(await stateOf(screen, '待验收')).toBe('upcoming')
    expect(await stateOf(screen, '已交付')).toBe('upcoming')
  })

  it('delivered：五个节点全部 done', async () => {
    const screen = await render(<NodeTimeline node='delivered' />)
    await expect.element(screen.getByText('已交付')).toBeInTheDocument()
    for (const label of ['需求受理', '已派发', '执行中', '待验收', '已交付']) {
      expect(await stateOf(screen, label)).toBe('done')
    }
  })

  it('needs_decision：异常节点单独呈现，主线节点不标 active/done', async () => {
    const screen = await render(<NodeTimeline node='needs_decision' />)
    const anomaly = screen.container.querySelector('[data-anomaly]')
    expect(anomaly).not.toBeNull()
    expect(anomaly?.textContent).toContain('需决策')
    for (const n of NODE_SEQUENCE) {
      expect(await stateOf(screen, nodeLabel(n))).toBe('upcoming')
    }
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { StatsChart } from './stats-chart'

/**
 * 受控 ECharts 封装的生命周期单测（与 agent-activity/echarts-line-chart.test
 * 同构）：mock echarts/core 的 init，断言 挂载 init → setOption(option)、
 * option 变更 → 再 setOption、容器尺寸变化 → resize、卸载 → dispose。
 * 直方图真渲染由 stats-page.test 用真 echarts 覆盖。
 */
vi.mock('echarts/core', () => {
  const chart = {
    setOption: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn(),
  }
  return { init: vi.fn(() => chart), use: vi.fn(), chart }
})

import { init } from 'echarts/core'

const option = { series: [] } as never

function chartStub() {
  return vi.mocked(init).mock.results[0]?.value as {
    setOption: ReturnType<typeof vi.fn>
    resize: ReturnType<typeof vi.fn>
    dispose: ReturnType<typeof vi.fn>
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('StatsChart 受控 ECharts 封装（bar 直方图）', () => {
  it('挂载：init 容器元素 + 初始 setOption', async () => {
    const screen = await render(<StatsChart option={option} />)

    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))
    const el = vi.mocked(init).mock.calls[0][0]
    expect(el).toBe(screen.container.firstElementChild)
    expect(chartStub().setOption).toHaveBeenCalledWith(option)
  })

  it('option 变更：增量 setOption（受控更新，不重建实例）', async () => {
    const screen = await render(<StatsChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))

    const option2 = { series: [1] } as never
    screen.rerender(<StatsChart option={option2} />)

    await vi.waitFor(() =>
      expect(chartStub().setOption).toHaveBeenLastCalledWith(option2)
    )
    expect(init).toHaveBeenCalledTimes(1)
  })

  it('容器尺寸变化 → resize（ResizeObserver 接线）', async () => {
    const screen = await render(<StatsChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))
    const before = chartStub().resize.mock.calls.length

    const el = screen.container.firstElementChild as HTMLElement
    el.style.width = '300px'
    await vi.waitFor(() =>
      expect(chartStub().resize.mock.calls.length).toBeGreaterThan(before)
    )
  })

  it('卸载：dispose（不留僵尸实例）', async () => {
    await render(<StatsChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))

    await cleanup()

    expect(chartStub().dispose).toHaveBeenCalledTimes(1)
  })

  it('无障碍：role=img + aria-label 透传，容器可加 className', async () => {
    const screen = await render(
      <StatsChart option={option} aria-label='执行时段分布图表' className='h-72' />
    )
    const el = screen.container.firstElementChild as HTMLElement
    expect(el.getAttribute('role')).toBe('img')
    expect(el.getAttribute('aria-label')).toBe('执行时段分布图表')
    expect(el.className).toContain('h-72')
  })
})

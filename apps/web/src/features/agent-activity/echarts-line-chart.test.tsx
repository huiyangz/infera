import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { ActivityChart } from './echarts-line-chart'

/**
 * 受控 ECharts 封装的生命周期单测：mock echarts/core 的 init，
 * 断言 挂载 init → setOption(option)、option 变更 → 再 setOption、
 * 容器尺寸变化 → resize、卸载 → dispose。真渲染交互（legend/tooltip）
 * 由 agent-activity-page.test.tsx 用真 echarts 覆盖。
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

describe('ActivityChart 受控 ECharts 封装', () => {
  it('挂载：init 容器元素 + 初始 setOption', async () => {
    const screen = await render(<ActivityChart option={option} />)

    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))
    const el = vi.mocked(init).mock.calls[0][0]
    expect(el).toBe(screen.container.firstElementChild)
    expect(chartStub().setOption).toHaveBeenCalledWith(option)
  })

  it('option 变更：增量 setOption（受控更新，不重建实例）', async () => {
    const screen = await render(<ActivityChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))

    const option2 = { series: [1] } as never
    screen.rerender(<ActivityChart option={option2} />)

    await vi.waitFor(() =>
      expect(chartStub().setOption).toHaveBeenLastCalledWith(option2)
    )
    expect(init).toHaveBeenCalledTimes(1)
  })

  it('容器尺寸变化 → resize（ResizeObserver 接线）', async () => {
    const screen = await render(<ActivityChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))
    // RO observe 首次通知即触发一次 resize；再变宽应再次触发
    const before = chartStub().resize.mock.calls.length

    const el = screen.container.firstElementChild as HTMLElement
    el.style.width = '300px'
    await vi.waitFor(() =>
      expect(chartStub().resize.mock.calls.length).toBeGreaterThan(before)
    )
  })

  it('卸载：dispose（不留僵尸实例）', async () => {
    await render(<ActivityChart option={option} />)
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1))

    await cleanup()

    expect(chartStub().dispose).toHaveBeenCalledTimes(1)
  })
})

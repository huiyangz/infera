import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import '@/styles/index.css'
import { getAgentActivity } from './api'
import type { AgentActivityResponse } from './types'
import { AgentActivityPage } from './agent-activity-page'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    getAgentActivity: vi.fn(),
  }
})

/** 三条曲线（含 unbound），窗口对齐等长 points */
function payload(over: Partial<AgentActivityResponse>): AgentActivityResponse {
  const points = [
    { t: '2026-08-25T04:00:00Z', count: 1 },
    { t: '2026-08-25T04:30:00Z', count: 2 },
    { t: '2026-08-25T05:00:00Z', count: 0 },
  ]
  return {
    window: { from: '2026-08-25T04:00:00Z', to: '2026-08-25T08:00:00Z' },
    bucket_minutes: 30,
    series: [
      {
        agent_id: '7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
        agent_name: 'SDD',
        points,
      },
      {
        agent_id: '029ecb6a-c6f6-4885-ac75-01bd24e766c0',
        agent_name: 'Reviewer',
        points,
      },
      { agent_id: '', agent_name: 'unbound', points },
    ],
    ...over,
  }
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：Header 依赖 SidebarProvider 上下文（与 decisions-page 测试同构）
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <AgentActivityPage />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/** 图表容器内的 tooltip DOM（echarts axis tooltip 挂在容器内 div；
 *  formatter 行含全角冒号「：」，用它区别于含轴文字的 svg 包装 div） */
async function tooltipText(screen: { container: HTMLElement }) {
  const chart = screen.container.querySelector('[role="img"]')
  const tip = Array.from(chart?.querySelectorAll('div') ?? []).find((d) =>
    d.textContent?.includes('：')
  )
  return tip?.textContent ?? ''
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getAgentActivity).mockResolvedValue(payload({}))
})

afterEach(async () => {
  await cleanup()
})

describe('AgentActivityPage Agent 执行时序页', () => {
  it('默认加载最近 24h / 30 分钟桶，页头标题「Agent 执行时序」', async () => {
    const screen = await mount()

    await expect
      .element(screen.getByText('Agent 执行时序', { exact: true }))
      .toBeInTheDocument()
    expect(getAgentActivity).toHaveBeenCalledWith({ hours: 24 })
  })

  it('加载中：骨架占位，不出图表', async () => {
    vi.mocked(getAgentActivity).mockReturnValue(new Promise(() => {}))
    const screen = await mount()

    await expect
      .element(
        screen.container.querySelector('[data-slot="skeleton"]') as HTMLElement
      )
      .toBeInTheDocument()
    expect(screen.container.querySelector('[role="img"]')).toBeNull()
  })

  it('数据态：曲线数 = 接口返回 series 数（legend 各 agent 一项，含 unbound）', async () => {
    const screen = await mount()

    await expect.element(screen.getByText('SDD', { exact: true })).toBeInTheDocument()
    await expect
      .element(screen.getByText('Reviewer', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('unbound', { exact: true }))
      .toBeInTheDocument()
    // 真 echarts 渲染出 svg 图面
    expect(screen.container.querySelector('[role="img"] svg')).not.toBeNull()
  })

  it('tooltip：悬停图面显示时间点 + agent + 次数', async () => {
    const screen = await mount()
    const chart = screen.getByRole('img', { name: 'Agent 执行时序' })

    await chart.hover()

    await vi.waitFor(async () => {
      const text = await tooltipText(screen)
      expect(text).toContain('SDD')
      expect(text).toContain('unbound')
      expect(text).toContain('次')
      expect(text).toMatch(/\d{2}:\d{2}/)
    })
  })

  it('legend 切换：点掉 unbound 后 tooltip 不再含该曲线', async () => {
    const screen = await mount()
    const chart = screen.getByRole('img', { name: 'Agent 执行时序' })

    await chart.hover()
    await vi.waitFor(async () => {
      expect(await tooltipText(screen)).toContain('unbound')
    })

    await screen.getByText('unbound', { exact: true }).click()
    // 移出再移回图面，强制 tooltip 按当前显隐重算
    await screen.getByText('Agent 执行时序', { exact: true }).hover()
    await chart.hover()

    await vi.waitFor(async () => {
      const text = await tooltipText(screen)
      expect(text).toContain('SDD')
      expect(text).not.toContain('unbound')
    })
  })

  it('空数据：series=[] 给空态提示，不出图表', async () => {
    vi.mocked(getAgentActivity).mockResolvedValue(payload({ series: [] }))
    const screen = await mount()

    await expect
      .element(screen.getByText('窗口内没有 agent 执行记录'))
      .toBeInTheDocument()
    expect(screen.container.querySelector('[role="img"]')).toBeNull()
  })

  it('请求失败：错误态 + 重试按钮，点击后重新拉取', async () => {
    vi.mocked(getAgentActivity).mockRejectedValue(new Error('HTTP 500'))
    const screen = await mount()

    await expect
      .element(screen.getByText('时序数据加载失败'))
      .toBeInTheDocument()
    expect(getAgentActivity).toHaveBeenCalledTimes(1)

    await screen.getByRole('button', { name: '重试' }).click()

    expect(getAgentActivity).toHaveBeenCalledTimes(2)
  })

  it('窗口切换 24h / 12h / 6h：切换即按新窗口取数', async () => {
    const screen = await mount()
    await expect.element(screen.getByText('SDD', { exact: true })).toBeInTheDocument()

    await screen.getByRole('button', { name: '12 小时' }).click()
    expect(getAgentActivity).toHaveBeenLastCalledWith({ hours: 12 })

    await screen.getByRole('button', { name: '6 小时' }).click()
    expect(getAgentActivity).toHaveBeenLastCalledWith({ hours: 6 })
  })
})

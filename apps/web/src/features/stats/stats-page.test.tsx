import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import '@/styles/index.css'
import { SidebarProvider } from '@/components/ui/sidebar'
import { getWorkspaceStats } from './api'
import type { WorkspaceStatsResponse } from './types'
import { StatsPage } from './stats-page'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    getWorkspaceStats: vi.fn(),
  }
})

/** 冻结契约载荷：夜间 2/23 时与白天 14 时有量，其余补零 */
function payload(over: Partial<WorkspaceStatsResponse>): WorkspaceStatsResponse {
  const hourly = Array.from({ length: 24 }, (_, hour) => ({
    hour,
    runs: hour === 2 ? 3 : hour === 14 ? 2 : hour === 23 ? 1 : 0,
    duration_ms:
      hour === 2 ? 5_400_000 : hour === 14 ? 1_800_000 : hour === 23 ? 600_000 : 0,
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
      runs_total: 6,
      running: 1,
      done: 4,
      failed: 1,
      duration_ms_total: 45_340_000,
    },
    hourly,
    ...over,
  }
}

/** 页面含 Header（SidebarTrigger），需 SidebarProvider 上下文——真实路由
 *  由 AuthenticatedLayout 提供，单测就地包一层 */
async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <StatsPage />
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/** 数字卡片的整卡文本（label 与数值同卡，避免裸数字多卡歧义） */
async function cardText(screen: { container: HTMLElement }, label: string) {
  const el = screen.container.querySelector(
    `[data-card-label="${label}"]`
  ) as HTMLElement | null
  return el?.closest('[data-slot="card"]')?.textContent ?? ''
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getWorkspaceStats).mockResolvedValue(payload({}))
})

afterEach(async () => {
  await cleanup()
})

describe('StatsPage 统计页', () => {
  it('默认窗口 7 天（hours=168，后端缺省口径）取数', async () => {
    await mount()
    expect(getWorkspaceStats).toHaveBeenCalledWith({ hours: 168 })
  })

  it('数字卡片：任务状态分布五项 + 执行次数 + 累计时长（口径与后端一致）', async () => {
    const screen = await mount()

    await vi.waitFor(async () => {
      expect(await cardText(screen, '任务总数')).toContain('12')
    })
    expect(await cardText(screen, '已完成')).toContain('5')
    expect(await cardText(screen, '进行中')).toContain('3')
    expect(await cardText(screen, '待办')).toContain('2')
    expect(await cardText(screen, '已取消')).toContain('2')
    expect(await cardText(screen, '执行次数')).toContain('6')
    // 累计时长 45_340_000ms → 12.6 小时（只计已收尾执行）
    expect(await cardText(screen, '累计时长')).toContain('12.6 小时')
  })

  it('口径说明：窗口范围、分桶时区（回显响应 timezone）、夜间边界可见', async () => {
    const screen = await mount()

    await expect.element(screen.getByText('最近 7 天')).toBeInTheDocument()
    await expect.element(screen.getByText('Asia/Shanghai')).toBeInTheDocument()
    await expect.element(screen.getByText(/22:00–06:00/)).toBeInTheDocument()
    // 状态分布为全量快照（不受窗口选择影响）的口径注记
    await expect.element(screen.getByText(/全量快照/)).toBeInTheDocument()
  })

  it('时段分布图：真 echarts 渲染出 svg 图面 + 夜间图例说明', async () => {
    const screen = await mount()

    await vi.waitFor(() => {
      expect(
        screen.container.querySelector('[role="img"] svg')
      ).not.toBeNull()
    })
    await expect
      .element(screen.getByText('夜间（22:00–06:00）时段柱体高亮'))
      .toBeInTheDocument()
  })

  it('窗口切换 24 小时 / 30 天：按新 hours 取数，口径文案随之更新', async () => {
    const screen = await mount()
    await vi.waitFor(() =>
      expect(screen.container.querySelector('[role="img"] svg')).not.toBeNull()
    )

    await screen.getByRole('button', { name: '24 小时' }).click()
    expect(getWorkspaceStats).toHaveBeenLastCalledWith({ hours: 24 })
    await expect.element(screen.getByText('最近 24 小时')).toBeInTheDocument()

    await screen.getByRole('button', { name: '30 天' }).click()
    expect(getWorkspaceStats).toHaveBeenLastCalledWith({ hours: 720 })
    await expect.element(screen.getByText('最近 30 天')).toBeInTheDocument()
  })

  it('加载中：骨架占位，不出图表', async () => {
    vi.mocked(getWorkspaceStats).mockReturnValue(new Promise(() => {}))
    const screen = await mount()

    await expect
      .element(
        screen.container.querySelector('[data-slot="skeleton"]') as HTMLElement
      )
      .toBeInTheDocument()
    expect(screen.container.querySelector('[role="img"]')).toBeNull()
  })

  it('请求失败：错误态 + 重试按钮，点击后重新拉取', async () => {
    vi.mocked(getWorkspaceStats).mockRejectedValue(new Error('HTTP 500'))
    const screen = await mount()

    await expect
      .element(screen.getByText('统计数据加载失败'))
      .toBeInTheDocument()
    expect(getWorkspaceStats).toHaveBeenCalledTimes(1)

    await screen.getByRole('button', { name: '重试' }).click()
    expect(getWorkspaceStats).toHaveBeenCalledTimes(2)
  })

  it('窗口内无执行：空态提示，不出图表', async () => {
    vi.mocked(getWorkspaceStats).mockResolvedValue(
      payload({
        execution: {
          runs_total: 0,
          running: 0,
          done: 0,
          failed: 0,
          duration_ms_total: 0,
        },
        hourly: Array.from({ length: 24 }, (_, hour) => ({
          hour,
          runs: 0,
          duration_ms: 0,
        })),
      })
    )
    const screen = await mount()

    await expect.element(screen.getByText('窗口内没有执行记录')).toBeInTheDocument()
    expect(screen.container.querySelector('[role="img"]')).toBeNull()
  })
})

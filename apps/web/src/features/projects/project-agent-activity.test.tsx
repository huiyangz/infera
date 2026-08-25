import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// 布局断言依赖 Tailwind 工具类真实生效，需引入应用样式入口
import '@/styles/index.css'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { getProject } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { getAgentActivity } from '@/features/agent-activity/api'
import type { AgentActivityResponse } from '@/features/agent-activity/types'
import { ProjectAgentActivity } from './project-agent-activity'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
  }
})

vi.mock('@/features/agent-activity/api', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/agent-activity/api')>()
  return {
    ...actual,
    getAgentActivity: vi.fn(),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  // Link 脱离 Router 上下文无法渲染，用 <a> 替身（带 $id 参数替换）；
  // to/params/activeOptions 为 Link 自有 props，真实实现不透传 DOM，替身同样剥掉
  const MockLink = ({
    children,
    to,
    params,
    activeOptions: _activeOptions,
    ...props
  }: React.ComponentProps<'a'> & {
    to?: string
    params?: Record<string, string>
    activeOptions?: unknown
  }) => (
    <a href={(to ?? '#').replace('$id', params?.id ?? '')} {...props}>
      {children}
    </a>
  )
  return {
    ...actual,
    Link: MockLink,
  }
})

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: '演示项目',
    repo_url: 'git@github.com:acme/repo.git',
    default_branch: 'main',
    pinned: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    external_project_id: '',
    external_synced_at: null,
    ...overrides,
  }
}

/** 两条曲线的最小时序载荷（窗口对齐等长 points） */
function makeActivity(): AgentActivityResponse {
  const points = [
    { t: '2026-08-25T04:00:00Z', count: 1 },
    { t: '2026-08-25T04:30:00Z', count: 2 },
  ]
  return {
    window: { from: '2026-08-25T04:00:00Z', to: '2026-08-25T05:00:00Z' },
    bucket_minutes: 30,
    series: [
      { agent_id: 'a1', agent_name: 'SDD', points },
      { agent_id: '', agent_name: 'unbound', points },
    ],
  }
}

/** 时序数据形态：'reject' = 查询失败；缺省 = 正常返回两条曲线 */
async function renderPage(activity: AgentActivityResponse | 'reject' = makeActivity()) {
  vi.mocked(getProject).mockResolvedValue(makeProject())
  if (activity === 'reject') {
    vi.mocked(getAgentActivity).mockRejectedValue(new Error('HTTP 500'))
  } else {
    vi.mocked(getAgentActivity).mockResolvedValue(activity)
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：SidebarProvider > SidebarInset(flex 纵向列) 承载页面内容
  return await render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <SidebarInset>
          <ProjectAgentActivity projectId='p1' />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

// vitest-browser-react 不自动卸载，逐测试清理避免跨测试定位串扰
afterEach(async () => {
  await cleanup()
})

describe('ProjectAgentActivity 项目详情 Agent 执行时序 tab（INFERA-259）', () => {
  it('AC1: 页内一级导航出现第三个页签「Agent 执行时序」，当前页为该页签', async () => {
    const screen = await renderPage()

    const nav = await screen.getByRole('navigation', { name: '项目导航' }).element()
    const links = nav?.querySelectorAll('a') ?? []
    expect(links.length).toBe(3)
    expect(links[2]?.getAttribute('href')).toBe('/projects/p1/agent-activity')
    expect(links[2]?.getAttribute('aria-current')).toBe('page')
    expect(links[0]?.getAttribute('aria-current')).toBeNull()
    expect(links[1]?.getAttribute('aria-current')).toBeNull()
  })

  it('AC2: 页头面包屑定位到项目下的 Agent 执行时序，口径说明保持工作区全局', async () => {
    const screen = await renderPage()

    // 面包屑：项目 / 演示项目 / Agent 执行时序（页头不重复独立页标题的 h1）
    await expect
      .element(screen.getByText('演示项目', { exact: true }))
      .toBeInTheDocument()
    const crumb = await screen.getByText(/跨项目/).element()
    expect(crumb?.textContent).toContain('30 分钟桶')
    expect(await screen.getByRole('heading').query()).toBeNull()
  })

  it('AC3: 嵌入原可视化主体——默认 24h 取数、窗口控件与真图面齐备', async () => {
    const screen = await renderPage()

    await expect.element(screen.getByText('SDD', { exact: true })).toBeInTheDocument()
    expect(getAgentActivity).toHaveBeenCalledWith({ hours: 24 })
    // 窗口切换控件随主体迁入（原独立页页头右侧控件）
    await expect
      .element(screen.getByRole('button', { name: '12 小时' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '6 小时' }))
      .toBeInTheDocument()
    // 真 echarts 渲染出 svg 图面
    expect(screen.container.querySelector('[role="img"] svg')).not.toBeNull()
  })

  it('AC3: 主体错误态可重试——迁入后行为不回退', async () => {
    const screen = await renderPage('reject')

    await expect
      .element(screen.getByText('时序数据加载失败'))
      .toBeInTheDocument()
    await screen.getByRole('button', { name: '重试' }).click()
    expect(getAgentActivity).toHaveBeenCalledTimes(2)
  })
})

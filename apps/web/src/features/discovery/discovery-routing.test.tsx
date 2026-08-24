import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import { listProjects, me } from '@/lib/infera-api'
import { routeTree } from '@/routeTree.gen'
import { listDiscoveryTasks } from './api'
import type { DiscoveryTaskRow } from './types'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  // me：路由守卫；listProjects：起始路由 / 渲染项目列表（与
  // project-routing.test 同构，避免真实 fetch 打到未起的后端）
  return { ...actual, me: vi.fn(), listProjects: vi.fn() }
})

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, listDiscoveryTasks: vi.fn() }
})

/** 路由接线集成测试（与 projects 域 project-routing.test 同构）：
 * 挂真实 routeTree.gen 生成的 router，验证侧边栏「需求发现」入口可达
 * /discovery 独立路由并渲染集中列表页。
 */
async function renderApp(initialPath: string) {
  vi.mocked(me).mockResolvedValue({ logged_in: true })
  vi.mocked(listProjects).mockResolvedValue([])
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const router = createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  })
  const screen = await render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { screen, router }
}

function row(over: Partial<DiscoveryTaskRow>): DiscoveryTaskRow {
  return {
    id: 'd-000000000001',
    project_id: 'p1',
    title: '情报：支付渠道调研',
    description: '',
    status: 'active',
    current_stage: 'intake',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-24T05:00:00Z',
    updated_at: '2026-08-24T05:10:00Z',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    external_synced_at: null,
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    agent_types: ['mining'],
    project_name: '自动闭环',
    labels: [],
    ...over,
  }
}

beforeAll(async () => {
  await page.viewport(1280, 720)
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listDiscoveryTasks).mockResolvedValue([row({})])
})

afterEach(async () => {
  await cleanup()
})

describe('需求发现域路由接线（INFERA-226）', () => {
  it('侧边栏含「需求发现」入口，指向 /discovery，点击进入独立路由页', async () => {
    const { screen, router } = await renderApp('/')
    const entry = screen.getByRole('link', { name: '需求发现' })
    await expect.element(entry).toBeInTheDocument()
    expect((await entry.element()).getAttribute('href')).toBe('/discovery')

    await router.navigate({ to: '/discovery' })
    // 页面标志性内容出现：页头文案 + 集中列表行
    await expect
      .element(screen.getByText('需求分析与需求挖掘两类 agent 任务集中视图'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/discovery')
  })

  it('直达 /discovery 同样渲染需求发现页（非项目总览兜底）', async () => {
    const { screen } = await renderApp('/discovery')
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()
    // 主看板（项目列表）独有内容不应出现
    expect(await screen.getByText('项目统计').query()).toBeNull()
  })
})

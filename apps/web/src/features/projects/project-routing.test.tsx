import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import {
  getProject,
  getProjectStageRuns,
  getProjectStats,
  listProjectTaskGroups,
  listProjects,
  me,
} from '@/lib/infera-api'
import { getTaskSyncStatus } from '@/features/task-sync/api'
import { getAgentActivity } from '@/features/agent-activity/api'
import type {
  Delivery,
  Project,
  TaskChild,
  TaskGroupRow,
} from '@/lib/infera-types'
import {
  getRequirement,
  listRequirementAudit,
  listRequirements,
} from '@/features/requirements/api'
import { routeTree } from '@/routeTree.gen'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    me: vi.fn(),
    listProjects: vi.fn(),
    getProject: vi.fn(),
    getProjectStats: vi.fn(),
    listProjectTaskGroups: vi.fn(),
    getProjectStageRuns: vi.fn(),
  }
})

vi.mock('@/features/task-sync/api', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/task-sync/api')>()
  return {
    ...actual,
    // 侧栏自动同步状态轮询与本文件断言无关——mock 掉，避免真实 fetch 打到 vite 代理报错
    getTaskSyncStatus: vi.fn(),
  }
})

vi.mock('@/features/requirements/api', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/requirements/api')>()
  return {
    ...actual,
    listRequirements: vi.fn(),
    getRequirement: vi.fn(),
    listRequirementAudit: vi.fn(),
  }
})

vi.mock('@/features/agent-activity/api', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/agent-activity/api')>()
  return {
    ...actual,
    // 时序接口与本文件多数断言无关——mock 掉，避免真实 fetch 打到 vite 代理报错
    getAgentActivity: vi.fn(),
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

function makeDelivery(overrides: Partial<Delivery> = {}): Delivery {
  return {
    id: 'd1',
    project_id: 'p1',
    title: '父需求甲',
    description: '',
    status: 'active',
    current_stage: 'spec',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    external_synced_at: null,
    ...overrides,
  }
}

/** 任务页数据源 task-groups 的最小夹具：一个父任务 + 阶段 1 的一条子任务 */
function makeTaskGroup(): TaskGroupRow {
  const child: TaskChild = {
    id: 'd2',
    title: '子任务一',
    stage: 1,
    status: 'active',
    current_stage: 'code',
    pending_gate: '',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    labels: [],
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  }
  return {
    ...makeDelivery(),
    child_total: 1,
    child_completed: 0,
    stages: [{ stage: 1, tasks: [child] }],
  }
}

/**
 * 路由接线集成测试（INFERA-119）：挂真实 routeTree.gen 生成的 router，
 * 验证 /projects/{id}/tasks 渲染任务列表页、详情入口可达、
 * /requirements/* 直达不受导航隐藏影响。
 */
async function renderApp(initialPath: string) {
  vi.mocked(me).mockResolvedValue({ logged_in: true })
  vi.mocked(listProjects).mockResolvedValue([])
  vi.mocked(getProject).mockResolvedValue(makeProject())
  vi.mocked(getProjectStats).mockResolvedValue({
    project_id: 'p1',
    requirement_total: 3,
    pending_decisions: 1,
    delivered: 1,
    by_status: { active: 1, queued: 1, completed: 1, blocked: 0, cancelled: 0 },
    last_synced_at: null,
  })
  vi.mocked(listProjectTaskGroups).mockResolvedValue([makeTaskGroup()])
  // 时序接口与本文件断言无关——mock 掉，避免真实 fetch 打到 vite 代理报错
  vi.mocked(getProjectStageRuns).mockResolvedValue({
    project_id: 'p1',
    runs: [],
    by_stage: [],
  })
  vi.mocked(getTaskSyncStatus).mockResolvedValue({
    lastSyncAt: null,
    status: 'idle',
    error: '',
  })
  vi.mocked(listRequirements).mockResolvedValue([])
  vi.mocked(getRequirement).mockResolvedValue({
    id: 'r1',
    title: '需求甲',
  } as never)
  vi.mocked(listRequirementAudit).mockResolvedValue([])
  // Agent 执行时序 tab 页数据源：窗口对齐的两条曲线（含 unbound）
  vi.mocked(getAgentActivity).mockResolvedValue({
    window: { from: '2026-08-25T04:00:00Z', to: '2026-08-25T05:00:00Z' },
    bucket_minutes: 30,
    series: [
      {
        agent_id: 'a1',
        agent_name: 'SDD',
        points: [{ t: '2026-08-25T04:00:00Z', count: 1 }],
      },
      {
        agent_id: '',
        agent_name: 'unbound',
        points: [{ t: '2026-08-25T04:00:00Z', count: 2 }],
      },
    ],
  })

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

beforeAll(async () => {
  await page.viewport(1280, 720)
})

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('projects 域路由接线（INFERA-119）', () => {
  it('AC1-1: 直达 /projects/{id}/tasks 渲染任务列表页而非项目总览，URL 停留项目域任务路径', async () => {
    const { screen, router } = await renderApp('/projects/p1/tasks')
    await router.navigate({ to: '/projects/$id/tasks', params: { id: 'p1' } })

    // 任务列表页标志性内容出现（页头文案以 project-tasks.tsx 已交付口径为准）
    await expect
      .element(
        screen.getByText('本项目的父任务与子任务，子任务按阶段分组（只读）')
      )
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: /父需求甲/ }))
      .toBeInTheDocument()
    // 不是项目总览（总览独有内容不应出现）
    expect(await screen.getByText('项目统计').query()).toBeNull()
    // URL 停留在项目域任务路径
    expect(router.state.location.pathname).toBe('/projects/p1/tasks')
  })

  it('AC1-2: 项目详情页「项目任务」入口点击后进入项目域任务页', async () => {
    const { screen, router } = await renderApp('/projects/p1')
    await router.navigate({ to: '/projects/$id', params: { id: 'p1' } })
    await expect
      .element(screen.getByText('项目统计'))
      .toBeInTheDocument()

    await screen.getByRole('link', { name: '项目任务' }).first().click()

    await expect
      .element(
        screen.getByText('本项目的父任务与子任务，子任务按阶段分组（只读）')
      )
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1/tasks')
  })

  it('AC1-3: /projects/{id} 仍渲染项目详情总览（含「项目任务」入口）', async () => {
    const { screen, router } = await renderApp('/projects/p1')
    await router.navigate({ to: '/projects/$id', params: { id: 'p1' } })

    await expect
      .element(screen.getByText('项目统计'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: '项目任务' }).first())
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1')
  })
})

describe('项目详情 tab 双向切换（INFERA-248：总览 ⇄ 项目任务）', () => {
  it('AC1-回归: 任务页渲染 tab 条，点击「总览」切回项目总览（URL 与内容一致）', async () => {
    const { screen, router } = await renderApp('/projects/p1/tasks')
    await expect
      .element(
        screen.getByText('本项目的父任务与子任务，子任务按阶段分组（只读）')
      )
      .toBeInTheDocument()

    // 回归点：任务页此前没有 tab 条（INFERA-234 只给总览页加了导航）
    await expect
      .element(screen.getByRole('navigation', { name: '项目导航' }))
      .toBeInTheDocument()

    await screen.getByRole('link', { name: /总览/ }).click()

    await expect
      .element(screen.getByText('项目统计'))
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1')
  })

  it('AC1-正向: 总览页点击「项目任务」tab 切到任务页（双向另一半）', async () => {
    const { screen, router } = await renderApp('/projects/p1')
    await expect
      .element(screen.getByText('项目统计'))
      .toBeInTheDocument()

    await screen.getByRole('link', { name: /项目任务/ }).first().click()

    await expect
      .element(
        screen.getByText('本项目的父任务与子任务，子任务按阶段分组（只读）')
      )
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1/tasks')
  })

  it('AC2: 当前路由与 tab 激活态一致——任务 URL 高亮「项目任务」，总览 URL 高亮「总览」', async () => {
    const { screen: tasksPage } = await renderApp('/projects/p1/tasks')
    await expect
      .element(
        tasksPage.getByText('本项目的父任务与子任务，子任务按阶段分组（只读）')
      )
      .toBeInTheDocument()
    const tasksNav = await tasksPage.getByRole('navigation', {
      name: '项目导航',
    }).element()
    const tasksLinks = tasksNav?.querySelectorAll('a') ?? []
    expect(tasksLinks[1]?.getAttribute('aria-current')).toBe('page')
    expect(tasksLinks[0]?.getAttribute('aria-current')).toBeNull()
    await tasksPage.unmount()

    const { screen: overviewPage } = await renderApp('/projects/p1')
    await expect
      .element(overviewPage.getByText('项目统计'))
      .toBeInTheDocument()
    const overviewNav = await overviewPage.getByRole('navigation', {
      name: '项目导航',
    }).element()
    const overviewLinks = overviewNav?.querySelectorAll('a') ?? []
    expect(overviewLinks[0]?.getAttribute('aria-current')).toBe('page')
    expect(overviewLinks[1]?.getAttribute('aria-current')).toBeNull()
  })
})

describe('项目详情 Agent 执行时序 tab（INFERA-259：原独立路由迁入项目域）', () => {
  it('AC1-1: 直达 /projects/{id}/agent-activity 渲染原可视化主体，URL 停留项目域', async () => {
    const { screen, router } = await renderApp('/projects/p1/agent-activity')
    await router.navigate({
      to: '/projects/$id/agent-activity',
      params: { id: 'p1' },
    })

    // 原可视化标志性内容：窗口切换控件 + 真 echarts 图面（legend 出现即图已装配）
    await expect
      .element(screen.getByRole('button', { name: '12 小时' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('SDD', { exact: true }))
      .toBeInTheDocument()
    expect(screen.container.querySelector('[role="img"] svg')).not.toBeNull()
    expect(getAgentActivity).toHaveBeenCalledWith({ hours: 24 })
    // 页内一级导航在位，当前页签为第三个「Agent 执行时序」
    const nav = await screen.getByRole('navigation', { name: '项目导航' }).element()
    const links = nav?.querySelectorAll('a') ?? []
    expect(links[2]?.getAttribute('aria-current')).toBe('page')
    // 不是项目总览（总览独有内容不应出现）
    expect(await screen.getByText('项目统计', { exact: true }).query()).toBeNull()
    expect(router.state.location.pathname).toBe('/projects/p1/agent-activity')
  })

  it('AC1-2: 总览页点击「Agent 执行时序」页签进入该页（tab 可达另一半）', async () => {
    const { screen, router } = await renderApp('/projects/p1')
    await router.navigate({ to: '/projects/$id', params: { id: 'p1' } })
    await expect
      .element(screen.getByText('项目统计', { exact: true }))
      .toBeInTheDocument()

    // 页签以「项目导航」作用域定位（总览页同名 section 标题非链接，不算重名）
    await screen
      .getByRole('navigation', { name: '项目导航' })
      .getByRole('link', { name: 'Agent 执行时序' })
      .click()

    await expect
      .element(screen.getByRole('button', { name: '12 小时' }))
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1/agent-activity')
  })

  it('AC1-3: 该页点击「总览」页签可切回项目总览（第三个页签不破坏既有双向切换）', async () => {
    const { screen, router } = await renderApp('/projects/p1/agent-activity')
    await expect
      .element(screen.getByRole('button', { name: '12 小时' }))
      .toBeInTheDocument()

    await screen.getByRole('link', { name: /总览/ }).click()

    await expect
      .element(screen.getByText('项目统计', { exact: true }))
      .toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/projects/p1')
  })

  it('AC2: 独立路由 /agent-activity 已移除——直达落 404（侧栏入口移除见 sidebar-data.test）', async () => {
    const { screen } = await renderApp('/agent-activity')

    await expect
      .element(screen.getByText(/Page Not Found/))
      .toBeInTheDocument()
  })
})

describe('/requirements/* 直达保留（INFERA-119 AC3）', () => {
  it('AC3-1: /requirements 直达可访问——不 404、不跳转', async () => {
    const { screen, router } = await renderApp('/requirements')
    await router.navigate({ to: '/requirements' })

    expect(await screen.getByText('Page Not Found').query()).toBeNull()
    expect(router.state.location.pathname).toBe('/requirements')
    const matches = router.state.matches
    expect(matches[matches.length - 1]?.routeId).toBe(
      '/_authenticated/requirements/'
    )
  })

  it('AC3-2: /requirements/{id} 直达可访问——不 404、不跳转', async () => {
    const { screen, router } = await renderApp('/requirements/r1')
    await router.navigate({ to: '/requirements/$id', params: { id: 'r1' } })

    expect(await screen.getByText('Page Not Found').query()).toBeNull()
    expect(router.state.location.pathname).toBe('/requirements/r1')
    const matches = router.state.matches
    expect(matches[matches.length - 1]?.routeId).toBe(
      '/_authenticated/requirements/$id'
    )
  })
})

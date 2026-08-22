import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
// 布局断言依赖 Tailwind 工具类真实生效，需引入应用样式入口
import '@/styles/index.css'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import {
  getProject,
  getProjectStats,
  listProjectDeliveries,
} from '@/lib/infera-api'
import type { Project, RequirementStats } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectDetail } from './project-detail'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
    getProjectStats: vi.fn(),
    listProjectDeliveries: vi.fn(),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  // Link 脱离 Router 上下文无法渲染，用 <a> 替身（带 $id 参数替换）
  const MockLink = ({
    children,
    to,
    params,
    ...props
  }: React.ComponentProps<'a'> & {
    to?: string
    params?: Record<string, string>
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

const SHORT_REPO = 'git@github.com:acme/repo.git'
// 400+ 字符、无空格不可折行的 SSH 地址
const LONG_REPO = `git@github.com:acme-corp/${'verylongrepopathsegment'.repeat(18)}.git`

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: '演示项目',
    repo_url: SHORT_REPO,
    default_branch: 'main',
    pinned: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    multica_project_id: '',
    multica_synced_at: null,
    ...overrides,
  }
}

function makeStats(
  overrides: Partial<RequirementStats> = {}
): RequirementStats {
  return {
    project_id: 'p1',
    requirement_total: 7,
    by_status: { active: 2, queued: 1, completed: 3, blocked: 1 },
    pending_decisions: 2,
    delivered: 3,
    last_synced_at: '2026-08-22T03:00:05Z',
    ...overrides,
  }
}

async function renderProjectDetail(
  project: Project,
  stats: RequirementStats | null = makeStats()
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  if (stats) vi.mocked(getProjectStats).mockResolvedValue(stats)
  else vi.mocked(getProjectStats).mockRejectedValue(new Error('x'))
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：SidebarProvider > SidebarInset(flex 纵向列) 承载页面内容
  return await render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <SidebarInset>
          <ProjectDetail projectId={project.id} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/** 等项目数据加载完成（项目名出现在面包屑即 proj query 已 resolve；repo_url 在顶栏与配置区出现两次，不可作信号） */
async function waitForProject(screen: RenderResult, projectName = '演示项目') {
  await expect
    .element(screen.getByText(projectName, { exact: true }))
    .toBeInTheDocument()
}

beforeAll(async () => {
  // 布局断言需要桌面视口（vitest browser 模式默认 414px 窄屏，窄屏响应式在本任务范围外）
  await page.viewport(1280, 720)
})

beforeEach(() => {
  vi.clearAllMocks()
})

// vitest-browser-react 不自动卸载，逐测试清理避免跨测试定位串扰
afterEach(async () => {
  await cleanup()
})

describe('ProjectDetail 项目域重构（AC：统计 + 必需配置 + 任务列表入口）', () => {
  it('AC1-1: 不再拉取也不展示需求列表（listProjectDeliveries 不被调用）', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    expect(listProjectDeliveries).not.toHaveBeenCalled()
    // 需求列表栏（旧左栏标题「需求」）不复存在
    expect(
      await screen.getByText('需求', { exact: true }).query()
    ).toBeNull()
  })

  it('AC1-2: 项目统计展示 T01 冻结契约各字段（总数/四状态桶/待决策/已交付）', async () => {
    const screen = await renderProjectDetail(makeProject(), makeStats())
    await waitForProject(screen)

    // 头部数字：需求总数 / 待决策 / 已交付
    await expect
      .element(screen.getByText('需求总数', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('待决策', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('已交付', { exact: true }))
      .toBeInTheDocument()
    // 数字与标签同现（delivered 与 by_status.completed 同源同值，取 .first() 避免歧义）
    await expect.element(screen.getByText('7')).toBeInTheDocument()
    await expect.element(screen.getByText('3').first()).toBeInTheDocument()

    // 四个状态桶按 StatusBadge 口径命名并带计数
    await expect
      .element(screen.getByText('进行中', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('未启动', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('已完成', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('已阻塞', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC1-3: last_synced_at null 显示「从未同步」，非 null 显示同步时间', async () => {
    const never = await renderProjectDetail(
      makeProject(),
      makeStats({ last_synced_at: null })
    )
    await waitForProject(never)
    await expect
      .element(never.getByText('从未同步', { exact: true }))
      .toBeInTheDocument()
    await never.unmount()

    const synced = await renderProjectDetail(
      makeProject(),
      makeStats({ last_synced_at: '2026-08-22T03:00:05Z' })
    )
    await waitForProject(synced)
    await expect
      .element(synced.getByText('从未同步', { exact: true }).query())
      .toBeNull()
    // dateTime 输出含绝对年份
    await expect.element(synced.getByText(/2026/)).toBeInTheDocument()
  })

  it('AC1-4: 必需配置只呈现项目已有配置字段（仓库地址/默认分支），未绑仓库给占位', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    await expect
      .element(screen.getByText('必需配置', { exact: true }))
      .toBeInTheDocument()
    // 仓库地址与默认分支原样呈现（顶栏与配置区各一次，取 .first()）
    await expect
      .element(screen.getByText(SHORT_REPO).first())
      .toBeInTheDocument()
    await expect.element(screen.getByText('main').first()).toBeInTheDocument()
    await screen.unmount()

    // 未绑仓库：占位文案，不渲染空值
    const unbound = await renderProjectDetail(
      makeProject({ repo_url: '', default_branch: '' })
    )
    await expect
      .element(unbound.getByText('（未绑仓库）', { exact: true }).first())
      .toBeInTheDocument()
  })

  it('AC1-5: 任务列表入口链到 /projects/{id}/tasks', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    // 标题链与图标链都指向任务页（取 .first() 消歧），至少一个可达即入口成立
    const entry = screen.getByRole('link', { name: /项目任务/ }).first()
    await expect.element(entry).toBeInTheDocument()
    const el = await entry.element()
    expect(el?.getAttribute('href')).toBe('/projects/p1/tasks')
  })

  it('AC1-回归: 顶栏超长 repo_url 截断省略号、悬停可见完整地址、页面无横向滚动', async () => {
    const screen = await renderProjectDetail(
      makeProject({ repo_url: LONG_REPO })
    )
    await waitForProject(screen)

    // 原生 title 携带完整地址（截断后悬停可读）
    const urlEl = await screen.getByTitle(LONG_REPO).element()
    expect(urlEl?.getAttribute('title')).toBe(LONG_REPO)
    // 截断生效：内容宽度超出可视盒（overflow hidden + ellipsis）
    expect(urlEl!.scrollWidth).toBeGreaterThan(urlEl!.clientWidth)
    // 默认分支完整可见（顶栏与配置区各一次，取首个即顶栏）
    const branch = await screen.getByText('main').first().element()
    expect(branch?.scrollWidth).toBeLessThanOrEqual(branch!.clientWidth)
    // 无横向滚动
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      document.documentElement.clientWidth
    )
  })

  it('AC1-回归: 编排入口保留可用', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)
    await expect
      .element(screen.getByRole('button', { name: '编排' }))
      .toBeInTheDocument()
  })
})

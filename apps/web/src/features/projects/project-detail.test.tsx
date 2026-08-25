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
  getProjectPipeline,
  getProjectStageRuns,
  getProjectStats,
  listAgents,
  listProjects,
  putProjectPipeline,
} from '@/lib/infera-api'
import type {
  Agent,
  Project,
  ProjectPipeline,
  ProjectStageRuns,
  RequirementStats,
} from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectDetail } from './project-detail'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
    getProjectStats: vi.fn(),
    getProjectStageRuns: vi.fn(),
    listProjects: vi.fn(),
    getProjectPipeline: vi.fn(),
    putProjectPipeline: vi.fn(),
    listAgents: vi.fn(),
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
    external_project_id: '',
    external_synced_at: null,
    ...overrides,
  }
}

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

/** 时序夹具：一个 delivery 的 spec(done) + code_gen 失败重试，by_stage 字典序 */
function makeStageRuns(): ProjectStageRuns {
  const t = (h: number, m: number) =>
    new Date(2026, 7, 22, h, m, 0).toISOString()
  return {
    project_id: 'p1',
    runs: [
      {
        id: 'sr-2',
        delivery_id: 'd-1',
        title: '补一个设置页',
        external_issue_key: 'INFERA-79',
        stage: 'code_gen',
        attempt: 2,
        status: 'failed',
        agent_name: 'coder',
        started_at: t(3, 10),
        finished_at: t(3, 12),
        duration_ms: 2 * 60_000,
      },
      {
        id: 'sr-1',
        delivery_id: 'd-1',
        title: '补一个设置页',
        external_issue_key: 'INFERA-79',
        stage: 'spec',
        attempt: 1,
        status: 'done',
        agent_name: 'spec-agent',
        started_at: t(3, 0),
        finished_at: t(3, 4),
        duration_ms: 4 * 60_000,
      },
    ],
    by_stage: [
      {
        stage: 'code_gen',
        total: 1,
        done: 0,
        failed: 1,
        running: 0,
        avg_ms: 2 * 60_000,
        p95_ms: 2 * 60_000,
      },
      {
        stage: 'spec',
        total: 1,
        done: 1,
        failed: 0,
        running: 0,
        avg_ms: 4 * 60_000,
        p95_ms: 4 * 60_000,
      },
    ],
  }
}

/** 时序数据形态：'reject' = 查询失败；'pending' = 永不 resolve（加载态） */
type StageRunsMode = ProjectStageRuns | 'reject' | 'pending'

async function renderProjectDetail(
  project: Project,
  stats: RequirementStats | null = makeStats(),
  stageRuns: StageRunsMode = makeStageRuns()
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  if (stats) vi.mocked(getProjectStats).mockResolvedValue(stats)
  else vi.mocked(getProjectStats).mockRejectedValue(new Error('x'))
  if (stageRuns === 'reject') {
    vi.mocked(getProjectStageRuns).mockRejectedValue(new Error('后端不可用'))
  } else if (stageRuns === 'pending') {
    vi.mocked(getProjectStageRuns).mockReturnValue(
      new Promise(() => {}) as never
    )
  } else {
    vi.mocked(getProjectStageRuns).mockResolvedValue(stageRuns)
  }
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

/** 等项目数据加载完成（项目名出现在面包屑即 proj query 已 resolve；repo_url 在顶栏出现，不可作信号） */
async function waitForProject(screen: RenderResult, projectName = '演示项目') {
  await expect
    .element(screen.getByText(projectName, { exact: true }))
    .toBeInTheDocument()
}

/** 定位「项目统计」区（section）的 DOM 节点，重复计数断言都以它为作用域 */
async function statsSection(screen: RenderResult): Promise<HTMLElement> {
  const heading = await screen.getByText('项目统计', { exact: true }).element()
  return heading?.closest('section') as HTMLElement
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

describe('ProjectDetail dashboard 化（INFERA-243）', () => {
  it('AC1: 顶部统计与列表各自承担不同信息——统计区内每个状态计数只出现一次（图例承载），旧的逐行重复计数 dl 结构不复存在', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    const section = await statsSection(screen)
    // 五个状态桶各出现且仅出现一次（KPI 图例）；旧的「三张数字卡 + 状态逐行 dl」不复存在
    for (const label of ['进行中', '未启动', '已完成', '已阻塞', '已取消']) {
      const hits = Array.from(
        section.querySelectorAll('*')
      ).filter((el) => el.textContent === label && el.children.length === 0)
      expect(hits.length, label).toBe(1)
    }
    expect(section.querySelectorAll('dl')).toHaveLength(0)
    expect(section.querySelectorAll('dt, dd')).toHaveLength(0)
    // KPI 瓦片承担总量与可行动量
    expect(section.textContent).toContain('任务总数')
    expect(section.textContent).toContain('待决策')
    expect(section.textContent).toContain('已交付')
    expect(section.textContent).toContain('8')
    expect(section.textContent).toContain('2')
    expect(section.textContent).toContain('3')
  })

  it('AC2-1: Agent 执行时序区用真实数据渲染——泳道、成败条与阶段耗时聚合齐备', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    await expect
      .element(screen.getByText('Agent 执行时序', { exact: true }))
      .toBeInTheDocument()
    // 泳道与条：d-1 两条 run，failed / done 语义可见
    expect(document.querySelectorAll('[data-lane]').length).toBe(1)
    expect(document.querySelectorAll('[data-run-id]').length).toBe(2)
    expect(
      document.querySelectorAll("[data-run-id][data-status='failed']").length
    ).toBe(1)
    // by_stage 聚合经阶段序重排后呈现中文阶段名与失败计数
    expect(document.querySelector('[data-stage-stats]')).not.toBeNull()
    const codeGenRow = document.querySelector(
      "[data-stage-stats] [data-stage='code_gen']"
    )
    expect(codeGenRow?.textContent).toContain('实现')
    expect(codeGenRow?.textContent).toContain('2 分')
    // spec 行排在 code_gen 前（阶段序，非后端字典序）
    expect(codeGenRow?.previousElementSibling?.getAttribute('data-stage')).toBe('spec')
  })

  it('AC2-2: 时序空态有设计（暂无执行记录），不渲染甘特与聚合表', async () => {
    const screen = await renderProjectDetail(
      makeProject(),
      makeStats(),
      { project_id: 'p1', runs: [], by_stage: [] }
    )
    await waitForProject(screen)

    await expect
      .element(screen.getByText('暂无执行记录', { exact: true }))
      .toBeInTheDocument()
    expect(document.querySelector('[data-lane]')).toBeNull()
    expect(document.querySelector('[data-stage-stats]')).toBeNull()
  })

  it('AC2-3: 时序错误态给重试入口，点击后重新请求', async () => {
    const screen = await renderProjectDetail(makeProject(), makeStats(), 'reject')
    await waitForProject(screen)

    await expect
      .element(screen.getByText('时序数据加载失败', { exact: true }))
      .toBeInTheDocument()
    expect(getProjectStageRuns).toHaveBeenCalledTimes(1)

    await screen.getByRole('button', { name: '重试' }).click()
    await vi.waitFor(() => {
      expect(getProjectStageRuns).toHaveBeenCalledTimes(2)
    })
  })

  it('AC2-4: 时序加载态渲染骨架而非空白', async () => {
    const screen = await renderProjectDetail(
      makeProject(),
      makeStats(),
      'pending'
    )
    await waitForProject(screen)

    expect(document.querySelector('[data-timeline-skeleton]')).not.toBeNull()
    expect(document.querySelector('[data-lane]')).toBeNull()
  })

  it('AC3: 任务列表入口是一等页内导航——「项目导航」内可达 /projects/{id}/tasks，当前页为总览', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    const nav = await screen.getByRole('navigation', { name: '项目导航' }).element()
    expect(nav).toBeInTheDocument()
    const links = nav?.querySelectorAll('a') ?? []
    expect(links.length).toBe(2)
    expect(links[0]?.getAttribute('href')).toBe('/projects/p1')
    expect(links[0]?.getAttribute('aria-current')).toBe('page')
    expect(links[1]?.getAttribute('href')).toBe('/projects/p1/tasks')
    // 旧版「任务列表」卡片入口（角落小链接）不复存在
    expect(await screen.getByText('任务列表', { exact: true }).query()).toBeNull()
  })

  it('AC4-1: 时间信息中性展示为「最近活动」——null 显示「暂无活动」，非 null 显示时间且无「同步」字样', async () => {
    const never = await renderProjectDetail(
      makeProject(),
      makeStats({ last_synced_at: null })
    )
    await waitForProject(never)
    await expect
      .element(never.getByText('最近活动', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(never.getByText('暂无活动', { exact: true }))
      .toBeInTheDocument()
    await never.unmount()

    const synced = await renderProjectDetail(
      makeProject(),
      makeStats({ last_synced_at: '2026-08-22T03:00:05Z' })
    )
    await waitForProject(synced)
    await expect
      .element(synced.getByText('最近活动', { exact: true }))
      .toBeInTheDocument()
    expect(
      await synced.getByText('暂无活动', { exact: true }).query()
    ).toBeNull()
    // dateTime 输出含绝对年份
    await expect.element(synced.getByText(/2026/)).toBeInTheDocument()
    // 页面不出现「同步」字样（INFERA-194 中性展示）
    expect(await synced.getByText(/同步/).query()).toBeNull()
  })

  it('AC4-2: 必需配置呈现项目已有配置字段（Git 仓库/默认分支）', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    await expect
      .element(screen.getByText('必需配置', { exact: true }))
      .toBeInTheDocument()
    // git 地址（SHORT_REPO）与默认分支原样呈现（顶栏与配置区各一次，取 .first()）
    await expect
      .element(screen.getByText(SHORT_REPO).first())
      .toBeInTheDocument()
    await expect.element(screen.getByText('main').first()).toBeInTheDocument()
  })

  it('AC4-回归: 顶栏超长 repo_url 截断省略号、悬停可见完整地址、页面无横向滚动', async () => {
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

  it('AC4-回归: 编排入口保留可用', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)
    await expect
      .element(screen.getByRole('button', { name: '编排' }))
      .toBeInTheDocument()
  })
})

describe('ProjectDetail 新建需求入口（INFERA-178：与任务列表页共享对话框）', () => {
  it('页头「新建需求」按钮打开共享创建对话框，项目默认当前项目', async () => {
    vi.mocked(listProjects).mockResolvedValue([makeProject()])
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    await screen.getByRole('button', { name: '新建需求' }).click()
    await expect.element(screen.getByRole('dialog')).toBeInTheDocument()
    await expect.element(screen.getByLabelText('标题')).toBeInTheDocument()

    // 共享对话框默认值：项目 = 当前项目
    const project = (await screen
      .getByRole('combobox', { name: '项目', exact: true })
      .element())!
    expect(project.textContent).toContain('演示项目')
  })
})

// —— 编排对话框：项目级唯一定义（INFERA-181，契约 = {nodes, bindings}）——

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'a1',
    name: 'agent',
    runner: 'cli',
    config: {},
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

const DIALOG_AGENTS = [
  makeAgent({ id: 'a1', name: '规格机' }),
  makeAgent({ id: 'a2', name: '实现机', runner: 'local' }),
]

async function renderWithDialogMocks(pipeline: ProjectPipeline) {
  vi.mocked(getProjectPipeline).mockResolvedValue(pipeline)
  vi.mocked(putProjectPipeline).mockResolvedValue({
    nodes: pipeline.nodes,
    bindings: {},
  })
  vi.mocked(listAgents).mockResolvedValue(DIALOG_AGENTS)
  const screen = await renderProjectDetail(makeProject())
  await waitForProject(screen)
  await screen.getByRole('button', { name: '编排' }).click()
  await expect.element(screen.getByRole('dialog')).toBeInTheDocument()
  return screen
}

describe('ProjectDetail 编排对话框项目级唯一定义（INFERA-181）', () => {
  it('按 {nodes, bindings} 契约渲染当前项目绑定', async () => {
    const screen = await renderWithDialogMocks({
      nodes: ['spec', 'code_gen'],
      bindings: { spec: 'a1', code_gen: 'a2' },
    })
    // 节点行来自响应的 nodes（阶段耗时表也可能出现同名阶段，取 .first() 消歧）
    await expect
      .element(screen.getByText('规格生成', { exact: true }).first())
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('实现', { exact: true }).first())
      .toBeInTheDocument()
    // 注册 Agent 抵达编辑器下拉（选中态经 bindings 驱动，保存断言另测）
    await screen.getByRole('combobox').first().click()
    await expect
      .element(screen.getByRole('option', { name: /规格机/ }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('option', { name: /实现机/ }))
      .toBeInTheDocument()
  })

  it('无「沿用全局默认」语义：文案与来源列都不存在，清空按钮为「清空项目编排」', async () => {
    const screen = await renderWithDialogMocks({
      nodes: ['spec'],
      bindings: { spec: 'a1' },
    })
    expect(await screen.getByText(/全局默认/).query()).toBeNull()
    expect(
      await screen.getByText('恢复默认', { exact: true }).query()
    ).toBeNull()
    await expect
      .element(screen.getByRole('button', { name: '清空项目编排' }))
      .toBeInTheDocument()
    // 来源列（默认/项目覆盖徽标）不复存在
    expect(await screen.getByText('来源', { exact: true }).query()).toBeNull()
    expect(
      await screen.getByText('项目覆盖', { exact: true }).query()
    ).toBeNull()
  })

  it('保存提交全量绑定（项目级唯一定义，非增量覆盖）', async () => {
    const screen = await renderWithDialogMocks({
      nodes: ['spec', 'code_gen'],
      bindings: { spec: 'a1', code_gen: 'a2' },
    })
    await screen.getByRole('button', { name: '保存项目编排' }).click()
    await vi.waitFor(() => {
      expect(putProjectPipeline).toHaveBeenCalledWith('p1', {
        spec: 'a1',
        code_gen: 'a2',
      })
    })
  })

  it('「清空项目编排」提交空绑定（PUT {}）', async () => {
    const screen = await renderWithDialogMocks({
      nodes: ['spec', 'code_gen'],
      bindings: { spec: 'a1', code_gen: 'a2' },
    })
    await screen.getByRole('button', { name: '清空项目编排' }).click()
    await vi.waitFor(() => {
      expect(putProjectPipeline).toHaveBeenCalledWith('p1', {})
    })
  })
})

// —— INFERA-209：必需配置卡片移除「本地路径」行 ——
// repo_url 单字段仍承载本地路径（/ 开头）或 git 地址（https/ssh/git@），
// 但本地路径不再入卡：配置卡只保留「Git 仓库」「默认分支」两行，
// 本地形态 repo_url 时 Git 仓库行给「未绑定」占位。

/** 定位「必需配置」区（section）的 DOM 节点，行内断言都以它为作用域 */
async function configSection(screen: RenderResult): Promise<HTMLElement> {
  const heading = await screen.getByText('必需配置', { exact: true }).element()
  return heading?.closest('section') as HTMLElement
}

/** 取配置区中指定标签行的值单元格（dd）文本 */
async function configRowValue(
  screen: RenderResult,
  label: string
): Promise<string> {
  const section = await configSection(screen)
  const dts = Array.from(section.querySelectorAll('dt'))
  const row = dts.find((dt) => dt.textContent?.trim() === label)?.parentElement
  return row?.querySelector('dd')?.textContent?.trim() ?? ''
}

/** 配置区全部行标签（dt 文本，按 DOM 顺序） */
async function configLabels(screen: RenderResult): Promise<string[]> {
  const section = await configSection(screen)
  return Array.from(section.querySelectorAll('dt')).map((dt) =>
    dt.textContent?.trim()
  )
}

describe('ProjectDetail 必需配置卡片移除「本地路径」行（INFERA-209）', () => {
  it('AC1: 配置卡只保留「Git 仓库」「默认分支」两行，整页不再出现「本地路径」文案', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    expect(await configLabels(screen)).toEqual(['Git 仓库', '默认分支'])
    // 整页（含顶栏）不再出现「本地路径」文案
    expect(
      await screen.getByText('本地路径', { exact: true }).query()
    ).toBeNull()
  })

  it('AC1: repo_url 为本地路径时不再入卡——Git 仓库行给「未绑定」，路径值与文案都不出现在配置卡', async () => {
    const LOCAL = '/Users/dev/demo-project'
    const screen = await renderProjectDetail(makeProject({ repo_url: LOCAL }))
    await waitForProject(screen)

    expect(await configLabels(screen)).toEqual(['Git 仓库', '默认分支'])
    expect(await configRowValue(screen, 'Git 仓库')).toBe('未绑定')
    // 路径值只留在顶栏 repo_url 展示，配置卡不呈现
    const section = await configSection(screen)
    expect(section.textContent).not.toContain(LOCAL)
    expect(
      await screen.getByText('本地路径', { exact: true }).query()
    ).toBeNull()
  })

  it('AC1: repo_url 为 git 地址时归「Git 仓库」行（git@ 与 https 形态）', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)
    expect(await configRowValue(screen, 'Git 仓库')).toBe(SHORT_REPO)
    await screen.unmount()

    const HTTPS = 'https://github.com/acme/repo2.git'
    const httpsScreen = await renderProjectDetail(
      makeProject({ repo_url: HTTPS })
    )
    await waitForProject(httpsScreen)
    expect(await configRowValue(httpsScreen, 'Git 仓库')).toBe(HTTPS)
  })

  it('AC2: repo_url 为空时「Git 仓库」行仍可见、显示「未绑定」占位（不隐藏整行）', async () => {
    const screen = await renderProjectDetail(
      makeProject({ repo_url: '', default_branch: '' })
    )
    await waitForProject(screen)

    expect(await configLabels(screen)).toEqual(['Git 仓库', '默认分支'])
    expect(await configRowValue(screen, 'Git 仓库')).toBe('未绑定')
  })

  it('AC3: 必需配置区只读——无编辑入口（不含按钮/输入框/链接/可编辑节点）', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    const section = await configSection(screen)
    expect(section.querySelectorAll('button')).toHaveLength(0)
    expect(section.querySelectorAll('input, textarea, select')).toHaveLength(0)
    expect(section.querySelectorAll('a')).toHaveLength(0)
    expect(section.querySelectorAll('[contenteditable="true"]')).toHaveLength(0)
  })
})

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
  getProjectStats,
  listAgents,
  listProjects,
  putProjectPipeline,
} from '@/lib/infera-api'
import type {
  Agent,
  Project,
  ProjectPipeline,
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
    listProjects: vi.fn(),
    getProjectPipeline: vi.fn(),
    putProjectPipeline: vi.fn(),
    listAgents: vi.fn(),
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
    requirement_total: 7,
    by_status: { active: 2, queued: 1, completed: 3, blocked: 1, cancelled: 1 },
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
  it('AC1-1: 不再拉取也不展示需求列表（listProjectDeliveries 已随死代码清理移除）', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen)

    // 需求列表栏（旧左栏标题「需求」）不复存在
    expect(await screen.getByText('需求', { exact: true }).query()).toBeNull()
  })

  it('AC1-2: 项目统计展示 T01 冻结契约各字段（总数/五状态桶/待决策/已交付）', async () => {
    const screen = await renderProjectDetail(makeProject(), makeStats())
    await waitForProject(screen)

    // 头部数字：任务总数 / 待决策 / 已交付
    await expect
      .element(screen.getByText('任务总数', { exact: true }))
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
    // INFERA-233：第五状态桶「已取消」（cancelled 终态接入）
    await expect
      .element(screen.getByText('已取消', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC1-3: 时间信息中性展示为「最近活动」——null 显示「暂无活动」，非 null 显示时间且无「同步」字样', async () => {
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

  it('AC1-4: 必需配置呈现项目已有配置字段（Git 仓库/默认分支）', async () => {
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
    // 节点行来自响应的 nodes
    await expect
      .element(screen.getByText('规格生成', { exact: true }))
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

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
import { page, type Locator } from 'vitest/browser'
import { getDelivery, getProject, listProjectDeliveries } from '@/lib/infera-api'
import type { Delivery, Project } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectDetail } from './project-detail'

const navigate = vi.fn()

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
    listProjectDeliveries: vi.fn(),
    getDelivery: vi.fn(),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  // Link 脱离 Router 上下文无法渲染，用 <a> 替身
  const MockLink = ({
    children,
    to,
    ...props
  }: React.ComponentProps<'a'> & { to?: string }) => (
    <a href={to ?? '#'} {...props}>
      {children}
    </a>
  )
  return {
    ...actual,
    useNavigate: () => navigate,
    Link: MockLink,
  }
})

const SHORT_REPO = 'git@github.com:acme/repo.git'
// 400+ 字符、无空格不可折行的 SSH 地址
const LONG_REPO = `git@github.com:acme-corp/${'verylongrepopathsegment'.repeat(18)}.git`
// 300+ 字符、无断点的拉丁项目名
const LONG_NAME = 'verylongprojectnamewithoutbreaks'.repeat(9)

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

function makeDelivery(overrides: Partial<Delivery> = {}): Delivery {
  return {
    id: 'd1',
    project_id: 'p1',
    title: '本地需求',
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
    multica_issue_id: '',
    multica_issue_key: '',
    assignee: '',
    priority: '',
    multica_synced_at: null,
    ...overrides,
  }
}

async function renderProjectDetail(
  project: Project,
  deliveries: Delivery[] = []
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  vi.mocked(listProjectDeliveries).mockResolvedValue(deliveries)
  // 右侧详情面板按选中行拉取；给出最小形状避免真实 fetch
  if (deliveries[0]) {
    vi.mocked(getDelivery).mockResolvedValue({
      delivery: deliveries[0],
      timeline: [],
      artifacts: [],
    })
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

type Rect = { x: number; y: number; width: number; height: number }

async function rectOf(locator: Locator): Promise<Rect> {
  const el = await locator.element()
  if (!el) throw new Error(`element not found: ${locator.selector}`)
  const r = el.getBoundingClientRect()
  return { x: r.x, y: r.y, width: r.width, height: r.height }
}

/** 等项目数据加载完成（repo_url 出现即 proj query 已 resolve） */
async function waitForProject(screen: RenderResult, repoUrl: string) {
  await expect.element(screen.getByText(repoUrl)).toBeInTheDocument()
}

describe('ProjectDetail 顶栏长文本布局', () => {
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

  it('AC-1: 200+ 字符 repo_url 下顶栏无横向滚动，行动区位置尺寸与短地址完全一致', async () => {
    const short = await renderProjectDetail(makeProject())
    await waitForProject(short, SHORT_REPO)

    const inputShort = await rectOf(
      short.getByPlaceholder('一句话需求，回车提交…')
    )
    const createBtnShort = await rectOf(
      short.getByRole('button', { name: '新建交付' })
    )
    const orchestrateBtnShort = await rectOf(
      short.getByRole('button', { name: '编排' })
    )
    await short.unmount()

    const long = await renderProjectDetail(makeProject({ repo_url: LONG_REPO }))
    await waitForProject(long, LONG_REPO)

    const inputLong = await rectOf(
      long.getByPlaceholder('一句话需求，回车提交…')
    )
    const createBtnLong = await rectOf(
      long.getByRole('button', { name: '新建交付' })
    )
    const orchestrateBtnLong = await rectOf(
      long.getByRole('button', { name: '编排' })
    )

    // 无横向滚动
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      document.documentElement.clientWidth
    )

    // 行动区位置尺寸与短地址渲染逐项一致
    expect(inputLong).toEqual(inputShort)
    expect(createBtnLong).toEqual(createBtnShort)
    expect(orchestrateBtnLong).toEqual(orchestrateBtnShort)
  })

  it('AC-2: 超长 repo_url 截断省略号，title 悬停可见完整地址，图标与默认分支完整', async () => {
    const screen = await renderProjectDetail(
      makeProject({ repo_url: LONG_REPO })
    )
    await waitForProject(screen, LONG_REPO)

    // 原生 title 携带完整地址（截断后悬停可读）
    const urlEl = await screen.getByTitle(LONG_REPO).element()
    expect(urlEl?.getAttribute('title')).toBe(LONG_REPO)

    // 截断生效：内容宽度超出可视盒（overflow hidden + ellipsis）
    expect(urlEl!.scrollWidth).toBeGreaterThan(urlEl!.clientWidth)

    // GitBranch 图标在 git 行中
    expect(document.querySelector('.lucide-git-branch')).not.toBeNull()

    // 默认分支完整可见：文本完整且未发生截断
    const branch = await screen.getByText('main').element()
    expect(branch?.textContent).toBe('main')
    expect(branch!.scrollWidth).toBeLessThanOrEqual(branch!.clientWidth)
  })

  it('AC-3: 超长项目名截断省略号，git 行不受影响', async () => {
    const screen = await renderProjectDetail(makeProject({ name: LONG_NAME }))
    await expect.element(screen.getByText(LONG_NAME)).toBeInTheDocument()

    // 项目名截断生效：内容宽度超出可视盒
    const nameEl = await screen.getByText(LONG_NAME).element()
    expect(nameEl!.scrollWidth).toBeGreaterThan(nameEl!.clientWidth)

    // git 行不受影响：默认分支完整可见
    const branch = await screen.getByText('main').element()
    expect(branch?.textContent).toBe('main')
    expect(branch!.scrollWidth).toBeLessThanOrEqual(branch!.clientWidth)

    // 顶栏无横向滚动
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      document.documentElement.clientWidth
    )
  })

  it('AC-4(回归): 常规长度下行动区可见且同一行，URL 完整显示，无横向溢出', async () => {
    const screen = await renderProjectDetail(makeProject())
    await waitForProject(screen, SHORT_REPO)

    const input = await rectOf(screen.getByPlaceholder('一句话需求，回车提交…'))
    const createBtn = await rectOf(
      screen.getByRole('button', { name: '新建交付' })
    )
    const orchestrateBtn = await rectOf(
      screen.getByRole('button', { name: '编排' })
    )

    // 输入框与两个按钮均可见且同行对齐（items-center 下垂直中心重合；
    // input h-9 与 size=lg 按钮高度不同，顶部 y 天然相差 2px）
    expect(input.width).toBeGreaterThan(0)
    const centerY = (r: Rect) => r.y + r.height / 2
    expect(Math.abs(centerY(createBtn) - centerY(input))).toBeLessThan(1)
    expect(Math.abs(centerY(orchestrateBtn) - centerY(input))).toBeLessThan(1)
    expect(orchestrateBtn.x).toBeGreaterThan(createBtn.x)

    // URL 完整显示（无截断）
    const urlEl = await screen.getByTitle(SHORT_REPO).element()
    expect(urlEl!.scrollWidth).toBeLessThanOrEqual(urlEl!.clientWidth)

    // 无横向溢出
    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      document.documentElement.clientWidth
    )
  })
})

describe('ProjectDetail 需求列表 multica 来源标识', () => {
  beforeAll(async () => {
    await page.viewport(1280, 720)
  })

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(async () => {
    await cleanup()
  })

  it('同步进来的父/子需求行带 Multica 标识与 issue key，父行显示负责人短标；本地需求行不带', async () => {
    const screen = await renderProjectDetail(
      makeProject(),
      [
        makeDelivery({ id: 'd1', title: '本地需求' }),
        makeDelivery({
          id: 'd2',
          title: '同步需求',
          status: 'queued',
          current_stage: '',
          multica_issue_id: 'mi-2',
          multica_issue_key: 'INFERA-77',
          assignee: 'agent:7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
          multica_synced_at: '2026-08-22T03:00:05Z',
        }),
        makeDelivery({
          id: 'd3',
          title: '同步子需求',
          status: 'queued',
          current_stage: '',
          parent_id: 'd2',
          wave: 1,
          multica_issue_id: 'mi-3',
          multica_issue_key: 'INFERA-78',
          assignee: 'member:9b45e9f4-a3f2-4c1e-92f4-1cbd88238da3',
          multica_synced_at: '2026-08-22T03:00:05Z',
        }),
      ]
    )
    await waitForProject(screen, SHORT_REPO)

    // 行级断言走 button 角色：选中行的标题会同时出现在右侧详情面板，裸文本定位有歧义
    await expect.element(screen.getByRole('button', { name: /本地需求/ })).toBeInTheDocument()
    await expect.element(screen.getByRole('button', { name: /同步需求/ })).toBeInTheDocument()
    await expect.element(screen.getByRole('button', { name: /同步子需求/ })).toBeInTheDocument()
    // 父/子两条同步行各带一枚来源标识；本地行不带
    const chips = await screen.getByText('Multica', { exact: true }).all()
    expect(chips).toHaveLength(2)
    // issue key 代替空阶段位展示（同步镜像无 current_stage）
    await expect
      .element(screen.getByText('INFERA-77', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('INFERA-78', { exact: true }))
      .toBeInTheDocument()
    // 负责人展示串 type:id 的前端短标（姓名解析归展示层）
    await expect.element(screen.getByText('Agent 7bc775bc')).toBeInTheDocument()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
import { getProject, listProjectTaskGroups } from '@/lib/infera-api'
import type { Project, TaskChild, TaskGroupRow } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectTasks } from './project-tasks'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
    listProjectTaskGroups: vi.fn(),
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

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: '演示项目',
    repo_url: 'git@github.com:acme/repo.git',
    default_branch: 'main',
    pinned: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    multica_project_id: '',
    multica_synced_at: null,
    ...overrides,
  }
}

function makeChild(overrides: Partial<TaskChild> = {}): TaskChild {
  return {
    id: 'c1',
    title: '子任务',
    stage: 1,
    status: 'active',
    current_stage: 'code_gen',
    pending_gate: '',
    multica_issue_id: '',
    multica_issue_key: '',
    assignee: '',
    priority: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

function makeGroup(overrides: Partial<TaskGroupRow> = {}): TaskGroupRow {
  return {
    id: 'g1',
    project_id: 'p1',
    title: '本地任务',
    description: '',
    status: 'active',
    current_stage: 'spec',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    multica_issue_id: '',
    multica_issue_key: '',
    assignee: '',
    priority: '',
    multica_synced_at: null,
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    child_total: 0,
    child_completed: 0,
    stages: [],
    ...overrides,
  }
}

async function renderProjectTasks(
  project: Project,
  groups: TaskGroupRow[]
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  vi.mocked(listProjectTaskGroups).mockResolvedValue(groups)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <SidebarInset>
          <ProjectTasks projectId={project.id} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/**
 * 分组 fixture：本地父（无子任务）+ 同步父（两个阶段共三个子任务，
 * 一完成两未完成），覆盖「父卡片 + 子任务按阶段分组」主路径。
 */
function groupsFixture(): TaskGroupRow[] {
  return [
    makeGroup({ id: 'g1', title: '本地任务', status: 'active' }),
    makeGroup({
      id: 'g2',
      title: '同步父任务',
      status: 'queued',
      current_stage: '',
      multica_issue_id: 'mi-2',
      multica_issue_key: 'INFERA-77',
      assignee: 'agent:7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
      multica_synced_at: '2026-08-22T03:00:05Z',
      child_total: 3,
      child_completed: 1,
      stages: [
        {
          stage: 1,
          tasks: [
            makeChild({
              id: 'c3',
              title: '同步子任务甲',
              status: 'completed',
              current_stage: '',
              multica_issue_id: 'mi-3',
              multica_issue_key: 'INFERA-78',
              assignee: 'member:9b45e9f4-a3f2-4c1e-92f4-1cbd88238da3',
            }),
            makeChild({
              id: 'c4',
              title: '同步子任务乙',
              status: 'queued',
              current_stage: '',
              multica_issue_id: 'mi-4',
              multica_issue_key: 'INFERA-79',
            }),
          ],
        },
        {
          stage: 2,
          tasks: [
            makeChild({ id: 'c5', title: '第二批子任务', stage: 2 }),
          ],
        },
      ],
    }),
  ]
}

/** 等任务卡片渲染完成（第一行标题出现即 task-groups query 已 resolve） */
async function waitForTasks(screen: RenderResult, title: string) {
  await expect
    .element(screen.getByRole('link', { name: new RegExp(title) }))
    .toBeInTheDocument()
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

describe('ProjectTasks 项目任务页（L202608221704-2-T02：父任务卡片 + 子任务按阶段分组）', () => {
  it('AC1-1: 父任务渲染为卡片，子任务按「阶段 N」分组列于对应父任务下', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 两个父任务卡片 + 三个子任务行都以链接渲染
    for (const t of [
      '本地任务',
      '同步父任务',
      '同步子任务甲',
      '同步子任务乙',
      '第二批子任务',
    ]) {
      await expect
        .element(screen.getByRole('link', { name: new RegExp(t) }))
        .toBeInTheDocument()
    }
    // 阶段分组标题（每个阶段组一枚，含该组任务计数）
    await expect
      .element(screen.getByText('阶段 1 · 2 个子任务', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 2 · 1 个子任务', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC1-2: 阶段组按阶段号升序排列（阶段 1 在阶段 2 之前）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '第二批子任务')

    const s1 = await screen
      .getByText('阶段 1 · 2 个子任务', { exact: true })
      .element()
    const s2 = await screen
      .getByText('阶段 2 · 1 个子任务', { exact: true })
      .element()
    expect(s1).toBeTruthy()
    expect(s2).toBeTruthy()
    // s1 需在文档流中先于 s2
    expect(
      s1!.compareDocumentPosition(s2!) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('AC2-1: 父任务展示状态、阶段与子任务进度（x/y 完成）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '同步父任务')

    // 状态徽标：本地任务=进行中，同步父任务=未启动（父/子徽标文案相同，
    // 多元素命中用 .all() 断言存在性）
    expect(
      await screen.getByText('进行中', { exact: true }).all()
    ).not.toHaveLength(0)
    expect(
      await screen.getByText('未启动', { exact: true }).all()
    ).not.toHaveLength(0)
    // 阶段位：本地任务在规格生成；同步父无 current_stage 时以 issue key 顶替
    await expect
      .element(screen.getByText('规格生成', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('INFERA-77', { exact: true }))
      .toBeInTheDocument()
    // 子任务进度 1/3
    await expect
      .element(screen.getByText(/1\/3/))
      .toBeInTheDocument()
  })

  it('AC2-2: 子任务行展示状态与阶段信息（状态徽标 + 阶段位）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '第二批子任务')

    // 子任务状态：已完成子任务的徽标在页面上出现
    expect(
      await screen.getByText('已完成', { exact: true }).all()
    ).not.toHaveLength(0)
    // 阶段位：有 current_stage 的子任务展示阶段 label；同步镜像以 issue key 顶替
    await expect
      .element(screen.getByText('INFERA-78', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('INFERA-79', { exact: true }))
      .toBeInTheDocument()
    // 第二批子任务 current_stage=code_gen → 实现阶段 label
    await expect
      .element(screen.getByText('实现', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC3-1: 界面文案不出现「需求」', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '同步父任务')

    expect(await screen.getByText(/需求/).query()).toBeNull()
  })

  it('AC3-2: 页头口径为「父任务/子任务」', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    await expect
      .element(screen.getByText(/父任务与子任务/))
      .toBeInTheDocument()
  })

  it('导航: 父与子任务均可点击进入任务详情 /deliveries/{id}', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const cases: Array<[string, string]> = [
      ['本地任务', '/deliveries/g1'],
      ['同步父任务', '/deliveries/g2'],
      ['同步子任务甲', '/deliveries/c3'],
      ['同步子任务乙', '/deliveries/c4'],
      ['第二批子任务', '/deliveries/c5'],
    ]
    for (const [title, href] of cases) {
      const el = await screen
        .getByRole('link', { name: new RegExp(title) })
        .element()
      expect(el?.getAttribute('href')).toBe(href)
    }
  })

  it('multica 来源: 同步行带 Multica 标识与 issue key，父卡片显示负责人短标', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 三条同步行（父 g2 + 子 c3/c4）各带一枚来源标识；本地行不带
    const chips = await screen.getByText('Multica', { exact: true }).all()
    expect(chips).toHaveLength(3)
    // 负责人展示串 type:id 的前端短标
    await expect
      .element(screen.getByText('Agent 7bc775bc'))
      .toBeInTheDocument()
  })

  it('父任务可展开/收起子任务（纯视图切换，进度恒可见）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '同步父任务')

    // 默认展开：子任务与阶段组可见
    await expect
      .element(screen.getByRole('link', { name: /同步子任务甲/ }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 1 · 2 个子任务', { exact: true }))
      .toBeInTheDocument()

    // 收起：子任务与阶段组隐藏，父卡片与进度仍在
    await screen.getByRole('button', { name: '收起子任务' }).click()
    await expect
      .element(screen.getByRole('link', { name: /同步子任务甲/ }).query())
      .toBeNull()
    await expect
      .element(screen.getByText('阶段 1 · 2 个子任务', { exact: true }).query())
      .toBeNull()
    await expect
      .element(screen.getByRole('link', { name: /同步父任务/ }))
      .toBeInTheDocument()
    await expect.element(screen.getByText(/1\/3/)).toBeInTheDocument()

    // 再展开：子任务恢复
    await screen.getByRole('button', { name: '展开子任务' }).click()
    await expect
      .element(screen.getByRole('link', { name: /同步子任务甲/ }))
      .toBeInTheDocument()
  })

  it('门禁与冲突徽标: 待审批父任务展示徽标（只读，无动作按钮）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      [
        ...groupsFixture(),
        makeGroup({
          id: 'g6',
          title: '卡门任务',
          pending_gate: 'spec_approval',
        }),
      ]
    )
    await waitForTasks(screen, '卡门任务')

    // 待审批只作状态展示（徽标），不提供动作按钮
    await expect
      .element(screen.getByText('待审批', { exact: true }))
      .toBeInTheDocument()
    expect(
      await screen.getByRole('button', { name: /批准|通过|审批/ }).query()
    ).toBeNull()
  })

  it('空项目给空态提示', async () => {
    const screen = await renderProjectTasks(makeProject(), [])
    await expect
      .element(screen.getByText('还没有任务'))
      .toBeInTheDocument()
  })

  it('面包屑回链项目详情', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const back = await screen.getByRole('link', { name: '演示项目' }).element()
    expect(back?.getAttribute('href')).toBe('/projects/p1')
  })
})

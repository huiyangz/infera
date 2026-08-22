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
import { getProject, listProjectDeliveries } from '@/lib/infera-api'
import type { Delivery, Project } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectTasks } from './project-tasks'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
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

async function renderProjectTasks(
  project: Project,
  deliveries: Delivery[]
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  vi.mocked(listProjectDeliveries).mockResolvedValue(deliveries)
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

/** 三层结构 fixture：本地父 + 同步父（两个子，一个完成一个排队）+ 孤儿排序稳定 */
function familyFixture(): Delivery[] {
  return [
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
      title: '同步子需求甲',
      status: 'completed',
      current_stage: '',
      parent_id: 'd2',
      wave: 1,
      multica_issue_id: 'mi-3',
      multica_issue_key: 'INFERA-78',
      assignee: 'member:9b45e9f4-a3f2-4c1e-92f4-1cbd88238da3',
      multica_synced_at: '2026-08-22T03:00:05Z',
    }),
    makeDelivery({
      id: 'd4',
      title: '同步子需求乙',
      status: 'queued',
      current_stage: '',
      parent_id: 'd2',
      wave: 1,
      multica_issue_id: 'mi-4',
      multica_issue_key: 'INFERA-79',
      multica_synced_at: '2026-08-22T03:00:05Z',
    }),
  ]
}

/** 等任务行渲染完成（第一行标题出现即 deliveries query 已 resolve） */
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

describe('ProjectTasks 项目任务列表页（AC：父子结构只读 + 可进需求详情）', () => {
  it('AC2-1: 父子结构展示——父行在顶、子行缩进带「子」chip、父行有完成进度', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      familyFixture()
    )
    await waitForTasks(screen, '本地需求')

    // 两个父行 + 两个子行都以链接渲染
    for (const t of ['本地需求', '同步需求', '同步子需求甲', '同步子需求乙']) {
      await expect
        .element(screen.getByRole('link', { name: new RegExp(t) }))
        .toBeInTheDocument()
    }
    // 子行带「子」chip，父行不带
    const childChips = await screen.getByText('子', { exact: true }).all()
    expect(childChips).toHaveLength(2)
    // 父行完成进度：1/2
    await expect
      .element(screen.getByText(/1\/2/))
      .toBeInTheDocument()
  })

  it('AC2-2: 只读——无创建表单、无审批/操作按钮（展示性徽标除外）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      [
        ...familyFixture(),
        makeDelivery({
          id: 'd5',
          title: '卡门需求',
          pending_gate: 'spec_approval',
        }),
      ]
    )
    await waitForTasks(screen, '卡门需求')

    // 无创建入口（旧项目页的「新建交付」表单不在此页）
    expect(
      await screen.getByPlaceholder('一句话需求，回车提交…').query()
    ).toBeNull()
    expect(
      await screen.getByRole('button', { name: '新建交付' }).query()
    ).toBeNull()
    // 待审批只作状态展示（徽标），不提供动作按钮
    await expect
      .element(screen.getByText('待审批', { exact: true }))
      .toBeInTheDocument()
    expect(
      await screen.getByRole('button', { name: /批准|通过|审批/ }).query()
    ).toBeNull()
  })

  it('AC2-3: 每条（父与子）可点击进入需求详情 /deliveries/{id}', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      familyFixture()
    )
    await waitForTasks(screen, '本地需求')

    const cases: Array<[string, string]> = [
      ['本地需求', '/deliveries/d1'],
      ['同步需求', '/deliveries/d2'],
      ['同步子需求甲', '/deliveries/d3'],
      ['同步子需求乙', '/deliveries/d4'],
    ]
    for (const [title, href] of cases) {
      const el = await screen
        .getByRole('link', { name: new RegExp(title) })
        .element()
      expect(el?.getAttribute('href')).toBe(href)
    }
  })

  it('AC2-4: multica 来源标识——同步行带 Multica chip 与 issue key，父行显示负责人短标', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      familyFixture()
    )
    await waitForTasks(screen, '本地需求')

    // 三条同步行各带一枚来源标识；本地行不带
    const chips = await screen.getByText('Multica', { exact: true }).all()
    expect(chips).toHaveLength(3)
    // issue key 代替空阶段位展示（同步镜像无 current_stage）
    await expect
      .element(screen.getByText('INFERA-77', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('INFERA-78', { exact: true }))
      .toBeInTheDocument()
    // 负责人展示串 type:id 的前端短标（姓名解析归展示层）
    await expect
      .element(screen.getByText('Agent 7bc775bc'))
      .toBeInTheDocument()
  })

  it('AC2-5: 空项目给空态提示', async () => {
    const screen = await renderProjectTasks(makeProject(), [])
    await expect
      .element(screen.getByText('还没有任务'))
      .toBeInTheDocument()
  })

  it('AC2-6: 父行可展开/收起子任务（纯视图切换）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      familyFixture()
    )
    await waitForTasks(screen, '同步需求')

    // 默认展开：子行可见
    await expect
      .element(screen.getByRole('link', { name: /同步子需求甲/ }))
      .toBeInTheDocument()

    // 收起：子行隐藏，父行仍在
    await screen.getByRole('button', { name: '收起子任务' }).click()
    await expect
      .element(screen.getByRole('link', { name: /同步子需求甲/ }).query())
      .toBeNull()
    await expect
      .element(screen.getByRole('link', { name: /同步需求/ }))
      .toBeInTheDocument()

    // 再展开：子行恢复
    await screen.getByRole('button', { name: '展开子任务' }).click()
    await expect
      .element(screen.getByRole('link', { name: /同步子需求甲/ }))
      .toBeInTheDocument()
  })

  it('AC2-7: 面包屑回链项目详情', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      familyFixture()
    )
    await waitForTasks(screen, '本地需求')

    const back = await screen.getByRole('link', { name: '演示项目' }).element()
    expect(back?.getAttribute('href')).toBe('/projects/p1')
  })
})

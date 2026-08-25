// 分栏几何断言依赖真实 Tailwind 布局（lg:flex-row 等），对齐 delivery-detail
// / project-detail 测试的做法引入全局样式
import '@/styles/index.css'
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
import { getProject, listProjectTaskGroups, listProjects } from '@/lib/infera-api'
import type {
  Delivery,
  Project,
  TaskChild,
  TaskGroupRow,
} from '@/lib/infera-types'
import { Sidebar, SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'
import { createProjectRequirement } from './api'
import { ProjectTasks } from './project-tasks'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getProject: vi.fn(),
    listProjectTaskGroups: vi.fn(),
    listProjects: vi.fn(),
  }
})

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, createProjectRequirement: vi.fn() }
})

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

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

function makeChild(overrides: Partial<TaskChild> = {}): TaskChild {
  return {
    id: 'c1',
    title: '子任务',
    stage: 1,
    status: 'active',
    current_stage: 'code_gen',
    pending_gate: '',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    labels: [],
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
 * 真实应用布局渲染（INFERA-272 返工）：对齐 authenticated-layout 的骨架 ——
 * 渲染真实 Sidebar peer（inset 变体下 SidebarInset 命中 m-2 边距）并带上
 * 布局锁定高度链（has-data-[layout=fixed]）。原 renderProjectTasks 没有
 * Sidebar peer，inset 边距永不生效，「整页锁一屏」类断言验证的并非真实
 * 页面布局（测试绿但真实页面仍滚）。默认 inset 即应用默认变体
 * （layout-provider 的 DEFAULT_VARIANT）。
 */
async function renderProjectTasksInLayout(
  project: Project,
  groups: TaskGroupRow[],
  variant: 'inset' | 'sidebar' | 'floating' = 'inset'
): Promise<RenderResult> {
  vi.mocked(getProject).mockResolvedValue(project)
  vi.mocked(listProjectTaskGroups).mockResolvedValue(groups)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider defaultOpen>
        <Sidebar variant={variant} collapsible='icon' />
        <SidebarInset
          className={cn(
            '@container/content',
            'has-data-[layout=fixed]:h-svh',
            'peer-data-[variant=inset]:has-data-[layout=fixed]:h-[calc(100svh-(var(--spacing)*4))]'
          )}
        >
          <ProjectTasks projectId={project.id} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/**
 * 分组 fixture（L202608222116-1-T02 多阶段覆盖）：本地父（无子任务）+
 * 同步父（两个阶段共五个子任务，五种状态各一：一完成、一未启动、
 * 一进行中、一阻塞、一已取消），进度计数 1/5（cancelled 不计入完成）。
 */
function groupsFixture(): TaskGroupRow[] {
  return [
    makeGroup({ id: 'g1', title: '本地任务', status: 'active' }),
    makeGroup({
      id: 'g2',
      title: '同步父任务',
      status: 'queued',
      current_stage: '',
      external_issue_id: 'mi-2',
      external_issue_key: 'INFERA-77',
      assignee: 'agent:7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
      external_synced_at: '2026-08-22T03:00:05Z',
      child_total: 5,
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
              external_issue_id: 'mi-3',
              external_issue_key: 'INFERA-78',
              assignee: 'member:9b45e9f4-a3f2-4c1e-92f4-1cbd88238da3',
            }),
            makeChild({
              id: 'c4',
              title: '同步子任务乙',
              status: 'queued',
              current_stage: '',
              external_issue_id: 'mi-4',
              external_issue_key: 'INFERA-79',
            }),
          ],
        },
        {
          stage: 2,
          tasks: [
            makeChild({ id: 'c5', title: '第二批子任务', stage: 2 }),
            makeChild({
              id: 'c6',
              title: '第二批阻塞任务',
              status: 'blocked',
              stage: 2,
            }),
            makeChild({
              id: 'c7',
              title: '第二批放弃任务',
              status: 'cancelled',
              stage: 2,
            }),
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

/** 左/右两栏的 data-slot（INFERA-229 起左栏也渲染子任务标题与状态图标，
 *  断言某一栏的结构时按面板收窄查询，避免跨栏同名元素互相干扰） */
const MASTER_SLOT = 'task-master-list'
const DETAIL_SLOT = 'task-detail-pane'

/** 面板内按 role 命中的元素列表：.all() 给全部命中的 Locator，逐个取
 *  element() 后按「是否落在目标面板内」过滤 */
async function paneRoleEls(
  screen: RenderResult,
  slot: string,
  role: string,
  opts: Parameters<RenderResult['getByRole']>[1] = {}
) {
  return (await screen.getByRole(role, opts).all())
    .map((l) => l.element())
    .filter((el) => el.closest(`[data-slot="${slot}"]`) !== null)
}

/** 面板内按文本（全匹配）命中的元素列表 */
async function paneTextEls(
  screen: RenderResult,
  slot: string,
  text: Parameters<RenderResult['getByText']>[0]
) {
  return (await screen.getByText(text, { exact: true }).all())
    .map((l) => l.element())
    .filter((el) => el.closest(`[data-slot="${slot}"]`) !== null)
}

/** 文档流顺序断言（右栏 detail 面板内）：a 元素需在 b 之前 */
async function expectBefore(
  screen: RenderResult,
  a: Parameters<RenderResult['getByText']>[0],
  b: Parameters<RenderResult['getByText']>[0]
) {
  const ea = (await paneTextEls(screen, DETAIL_SLOT, a))[0]
  const eb = (await paneTextEls(screen, DETAIL_SLOT, b))[0]
  expect(ea).toBeTruthy()
  expect(eb).toBeTruthy()
  expect(
    ea!.compareDocumentPosition(eb!) & Node.DOCUMENT_POSITION_FOLLOWING
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
}

/** 左栏父任务列表选择：等列表项按钮出现后点击（INFERA-173 master-detail） */
async function selectParent(screen: RenderResult, title: string) {
  await expect
    .element(screen.getByRole('button', { name: new RegExp(title) }))
    .toBeInTheDocument()
  await screen.getByRole('button', { name: new RegExp(title) }).click()
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

describe('ProjectTasks 项目任务页（L202608222116-1-T02 阶段分组语义，INFERA-173 起收窄到选中父任务）', () => {
  it('AC1-a: 子任务区顶部为「子任务 n/n」进度头（标签 + 完成计数）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步父任务')

    // 进度头：左侧「子任务」标签，右侧完成计数（来自 child_completed/child_total）
    await expect
      .element(screen.getByText('子任务', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('1/5', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC1-b: 子任务按「阶段 N」分组标题纵向排列（标题即阶段号）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批子任务')

    await expect
      .element(screen.getByText('阶段 1', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 2', { exact: true }))
      .toBeInTheDocument()
    // 阶段组按阶段号升序（阶段 1 在阶段 2 之前）
    await expectBefore(screen, '阶段 1', '阶段 2')
  })

  it('AC1-c: 子任务行缩进于所属阶段标题之下（行内容起点在标题右侧）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    const stage = (await screen.getByText('阶段 1', { exact: true }).element())!
    const icon = (
      await paneRoleEls(screen, DETAIL_SLOT, 'img', { name: '已完成' })
    )[0]!
    // 行内状态图标（行内容最左元素）明显右于阶段标题文本
    const dx = icon.getBoundingClientRect().x - stage.getBoundingClientRect().x
    expect(dx).toBeGreaterThan(8)
  })

  it('AC1-d: 每个子任务行带状态图标（已完成/未启动/进行中/已阻塞/已取消）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批阻塞任务')

    for (const label of ['已完成', '未启动', '进行中', '已阻塞', '已取消']) {
      // 左栏（INFERA-229）也渲染同名状态图标——收窄到右栏断言
      expect(
        (await paneRoleEls(screen, DETAIL_SLOT, 'img', {
          name: label,
          exact: true,
        })).length
      ).toBeGreaterThan(0)
    }
  })

  it('INFERA-145 返工: 五态图标 class 逐属性钉死（尺寸/单色/进行中 spin），重构前后渲染一致', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批阻塞任务')

    // role='img' + aria-label 已由 AC1-d 覆盖；这里钉 class 全串
    // （含 lucide 自身前缀——连图标组件名一并钉死）
    const cases: Array<[string, string]> = [
      ['已完成', 'lucide lucide-circle-check size-3.5 shrink-0 text-foreground'],
      [
        '进行中',
        'lucide lucide-loader-circle size-3.5 shrink-0 animate-spin text-foreground',
      ],
      ['已阻塞', 'lucide lucide-circle-alert size-3.5 shrink-0 text-foreground'],
      [
        '未启动',
        'lucide lucide-circle-dashed size-3.5 shrink-0 text-muted-foreground',
      ],
      // INFERA-233：已取消 = 灰色禁用圈（中性弱化，与 StatusBadge 口径一致）
      [
        '已取消',
        'lucide lucide-circle-off size-3.5 shrink-0 text-muted-foreground',
      ],
    ]
    for (const [label, cls] of cases) {
      const el = (
        await paneRoleEls(screen, DETAIL_SLOT, 'img', {
          name: label,
          exact: true,
        })
      )[0]!
      expect(el.getAttribute('class')).toBe(cls)
    }
  })

  it('AC1-e: 子任务行单行展示：粗体 issue key + 标题（对齐参考图行式）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    await expect
      .element(screen.getByText('INFERA-78', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('INFERA-79', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC2-a: 多阶段分组 fixture 覆盖：组内任务紧跟所属阶段标题，不跨组', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批阻塞任务')

    // 阶段 1 → 甲、乙 → 阶段 2 → 第二批、阻塞（文档流顺序即分组归属）
    await expectBefore(screen, '阶段 1', '同步子任务甲')
    await expectBefore(screen, '同步子任务甲', '同步子任务乙')
    await expectBefore(screen, '同步子任务乙', '阶段 2')
    await expectBefore(screen, '阶段 2', '第二批子任务')
    await expectBefore(screen, '第二批子任务', '第二批阻塞任务')
  })

  it('无阶段分组: stage 0 渲染「无阶段」标题且位于编号阶段之后（INFERA-146）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      [
        makeGroup({
          id: 'g3',
          title: '混合父任务',
          child_total: 3,
          child_completed: 1,
          // 后端契约：编号阶段升序在前，wave 0「无阶段」分组垫底
          stages: [
            {
              stage: 1,
              tasks: [
                makeChild({ id: 'c11', title: '第一批任务', status: 'completed' }),
              ],
            },
            {
              stage: 2,
              tasks: [makeChild({ id: 'c12', title: '第二批任务', stage: 2 })],
            },
            {
              stage: 0,
              tasks: [
                makeChild({
                  id: 'c13',
                  title: '无阶段同步子任务',
                  stage: 0,
                  status: 'queued',
                  current_stage: '',
                  external_issue_id: 'mi-13',
                  external_issue_key: 'INFERA-13',
                }),
              ],
            },
          ],
        }),
      ]
    )
    await waitForTasks(screen, '无阶段同步子任务')

    // stage 0 组标题为「无阶段」（不渲染「阶段 0」）；编号阶段标题不变
    await expect
      .element(screen.getByText('无阶段', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 1', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 2', { exact: true }))
      .toBeInTheDocument()

    // 文档流顺序：阶段 1 → 阶段 2 → 无阶段 → 组内子任务
    await expectBefore(screen, '阶段 1', '阶段 2')
    await expectBefore(screen, '阶段 2', '无阶段')
    await expectBefore(screen, '无阶段', '无阶段同步子任务')
  })

  it('AC2-b: 进度头计数随 fixture 数据驱动（另一组数据给 2/3）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      [
        makeGroup({
          id: 'g9',
          title: '另一父任务',
          child_total: 3,
          child_completed: 2,
          stages: [
            {
              stage: 1,
              tasks: [
                makeChild({ id: 'c9', title: '子九', status: 'completed' }),
                makeChild({ id: 'c8', title: '子八', status: 'completed' }),
                makeChild({ id: 'c7', title: '子七', status: 'active' }),
              ],
            },
          ],
        }),
      ]
    )
    await waitForTasks(screen, '子七')

    await expect
      .element(screen.getByText('2/3', { exact: true }))
      .toBeInTheDocument()
  })

  it('父任务卡片: 状态徽标、阶段位与负责人（父行信息保留）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // g1（默认选中）：状态徽标=进行中、阶段位=规格生成
    expect(
      await screen.getByText('进行中', { exact: true }).all()
    ).not.toHaveLength(0)
    await expect
      .element(screen.getByText('规格生成', { exact: true }))
      .toBeInTheDocument()

    // g2（切换选中）：状态徽标=未启动、issue key 顶替阶段位、负责人前端短标
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')
    expect(
      await screen.getByText('未启动', { exact: true }).all()
    ).not.toHaveLength(0)
    await expect
      .element(screen.getByText('INFERA-77', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('Agent 7bc775bc'))
      .toBeInTheDocument()
  })

  it('来源标识不可见化: 父卡片不带「已同步」徽标（INFERA-194）；子任务行保留粗体 issue key', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 默认选中的本地父：无来源徽
    expect(await screen.getByText('已同步', { exact: true }).query()).toBeNull()

    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')
    // 带 external_issue_id 的父卡片也不渲染来源徽标（同步信息不可见）
    expect(
      await screen.getByText('已同步', { exact: true }).query(),
    ).toBeNull()
    await expect
      .element(screen.getByText('INFERA-77', { exact: true }))
      .toBeInTheDocument()
  })

  it('导航: 选中父与子任务均可点击进入任务详情 /deliveries/{id}', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 默认选中 g1：其卡片可进详情
    const local = await screen
      .getByRole('link', { name: /本地任务/ })
      .element()
    expect(local?.getAttribute('href')).toBe('/deliveries/g1')

    // 切到 g2：父与四个子任务链接齐备
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批阻塞任务')

    const cases: Array<[string, string]> = [
      ['同步父任务', '/deliveries/g2'],
      ['同步子任务甲', '/deliveries/c3'],
      ['同步子任务乙', '/deliveries/c4'],
      ['第二批子任务', '/deliveries/c5'],
      ['第二批阻塞任务', '/deliveries/c6'],
    ]
    for (const [title, href] of cases) {
      const el = await screen
        .getByRole('link', { name: new RegExp(title) })
        .element()
      expect(el?.getAttribute('href')).toBe(href)
    }
  })

  it('父任务可展开/收起子任务（进度头恒可见，分组与行随收起隐藏）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步父任务')

    // 默认展开：子任务行与阶段分组标题可见
    await expect
      .element(screen.getByRole('link', { name: /同步子任务甲/ }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('阶段 1', { exact: true }))
      .toBeInTheDocument()

    // 收起：分组与行隐藏，父卡片与进度头仍在
    await screen.getByRole('button', { name: '收起子任务' }).click()
    await expect
      .element(screen.getByRole('link', { name: /同步子任务甲/ }).query())
      .toBeNull()
    await expect
      .element(screen.getByText('阶段 1', { exact: true }).query())
      .toBeNull()
    await expect
      .element(screen.getByRole('link', { name: /同步父任务/ }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('1/5', { exact: true }))
      .toBeInTheDocument()

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
    await selectParent(screen, '卡门任务')
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

  it('任务内容文案不出现「需求」（唯一例外：新建需求入口按钮）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 任务行/阶段/状态文案仍统一为「任务」口径；「需求」只允许出现在
    // 创建入口按钮（INFERA-178 新增）上——全页唯一命中即该按钮
    const hits = await screen.getByText(/需求/).all()
    expect(hits).toHaveLength(1)
    await expect
      .element(screen.getByRole('button', { name: '新建需求' }))
      .toBeInTheDocument()
  })

  it('面包屑回链项目详情', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const back = await screen.getByRole('link', { name: '演示项目' }).element()
    expect(back?.getAttribute('href')).toBe('/projects/p1')
  })
})

describe('ProjectTasks 页内一级导航（INFERA-248：任务页渲染 tab 条，可切回总览）', () => {
  it('AC1: 任务页渲染「项目导航」tab 条——总览 / 项目任务 / Agent 执行时序三入口，总览链回 /projects/{id}', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const nav = await screen.getByRole('navigation', { name: '项目导航' }).element()
    const links = nav?.querySelectorAll('a') ?? []
    // INFERA-259：第三个页签「Agent 执行时序」挂在项目域导航上
    expect(links.length).toBe(3)
    expect(links[0]?.getAttribute('href')).toBe('/projects/p1')
    expect(links[1]?.getAttribute('href')).toBe('/projects/p1/tasks')
    expect(links[2]?.getAttribute('href')).toBe('/projects/p1/agent-activity')
  })

  it('AC2: 当前页为项目任务——「项目任务」tab 激活、「总览」待切换（与路由一致）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const tasks = await screen.getByRole('link', { name: /项目任务/ }).element()
    expect(tasks?.getAttribute('aria-current')).toBe('page')
    const overview = await screen.getByRole('link', { name: /总览/ }).element()
    expect(overview?.getAttribute('aria-current')).toBeNull()
  })
})

/** 标签 fixture（INFERA-220）：Multica 标签库的真实色值。auto/文档 为本视图
 *  可见标签；候选/情报 是需求挖掘域分类，本视图不渲染（INFERA-261，由
 *  「不渲染情报候选」describe 单独覆盖） */
const LABELS = {
  auto: { name: 'auto', color: '#22c55e' },
  docs: { name: '文档', color: '#eab308' },
  candidate: { name: '候选', color: '#a855f7' },
  intel: { name: '情报', color: '#3b82f6' },
}

/** 带（可选）标签的分组 fixture：本地父 + 同步镜像父 + 其同步子任务 */
function labeledGroupsFixture(
  parentLabels: TaskGroupRow['labels'],
  childLabels: TaskChild['labels']
): TaskGroupRow[] {
  return [
    makeGroup({ id: 'g1', title: '本地任务', labels: parentLabels }),
    makeGroup({
      id: 'g2',
      title: '同步父任务',
      status: 'queued',
      current_stage: '',
      external_issue_id: 'mi-2',
      external_issue_key: 'INFERA-77',
      labels: parentLabels,
      child_total: 1,
      child_completed: 0,
      stages: [
        {
          stage: 1,
          tasks: [
            makeChild({
              id: 'c3',
              title: '同步子任务甲',
              current_stage: '',
              external_issue_id: 'mi-3',
              external_issue_key: 'INFERA-78',
              labels: childLabels,
            }),
          ],
        },
      ],
    }),
  ]
}

describe('ProjectTasks 标签展示（INFERA-220：交付列表渲染 Multica 标签 chip）', () => {
  it('AC1-a: 父任务卡片显示标签 chip，名称与底色（hex 原值）与后端一致', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([LABELS.auto, LABELS.docs], [])
    )
    await waitForTasks(screen, '本地任务')

    await expect
      .element(screen.getByText('auto', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('文档', { exact: true }))
      .toBeInTheDocument()
    const chip = (await screen.getByText('auto', { exact: true }).element())!
    expect(getComputedStyle(chip).backgroundColor).toBe('rgb(34, 197, 94)')
  })

  it('AC1-b: 子任务行同样显示标签 chip', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([], [LABELS.docs])
    )
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    await expect
      .element(screen.getByText('文档', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC2: 同步来源的交付（external 标记）同样显示标签', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([LABELS.auto], [LABELS.docs])
    )
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    // 镜像父卡片与其镜像子任务行都带标签（同步链路已把标签落库并随行返回）
    await expect
      .element(screen.getByText('auto', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('文档', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC3-a: 无标签的交付不渲染 chip 行（不占位、不留空壳 UI）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([], [])
    )
    await waitForTasks(screen, '本地任务')

    expect(document.querySelector('[data-slot="label-chip"]')).toBeNull()
    expect(document.querySelector('[data-slot="label-chip-row"]')).toBeNull()
  })

  it('AC3-b: 超长标签名在行内截断（完整名保留在 title），不撑破卡片', async () => {
    const long = '一个特别长的标签名称用来验证列表行内截断'.repeat(3)
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([{ name: long, color: '#22c55e' }], [])
    )
    await waitForTasks(screen, '本地任务')

    const chip = document.querySelector('[data-slot="label-chip"]')!
    expect(chip.getAttribute('title')).toBe(long)
    expect(chip.scrollWidth).toBeGreaterThan(chip.clientWidth)
  })
})

/** 足够长的父任务列表：左栏内容高过自身可视高度，才会触发栏内滚动 */
function longListFixture(count = 40): TaskGroupRow[] {
  return Array.from({ length: count }, (_, i) =>
    makeGroup({ id: `g-long-${i}`, title: `长列表任务 ${String(i + 1).padStart(2, '0')}` })
  )
}

/** 右栏内容超高：单个父任务带大量子任务，详情卡高过右栏可视高度 */
function tallDetailFixture(count = 40): TaskGroupRow[] {
  return [
    makeGroup({
      id: 'g-tall',
      title: '多子任务父任务',
      child_total: count,
      child_completed: 0,
      stages: [
        {
          stage: 1,
          tasks: Array.from({ length: count }, (_, i) =>
            makeChild({
              id: `c-tall-${i}`,
              title: `右栏子任务 ${String(i + 1).padStart(2, '0')}`,
              current_stage: '',
              external_issue_key: `INFERA-${100 + i}`,
            })
          ),
        },
      ],
    }),
  ]
}

/** 滚动收敛断言用的三个锚点元素（渲染完成后才可取） */
const masterPane = () =>
  document.querySelector("[data-slot='task-master-list']") as HTMLElement
const detailPane = () =>
  document.querySelector("[data-slot='task-detail-pane']") as HTMLElement
const tabsNav = () =>
  document.querySelector("nav[aria-label='项目导航']") as HTMLElement

describe('ProjectTasks 滚动区域收敛（INFERA-261：仅左栏滚动，tab 头与右栏不动）', () => {
  it('AC1-a: 整页锁定一屏，不产生文档级滚动（真实默认 inset 布局）', async () => {
    // INFERA-272：渲染路径带真实 variant='inset' Sidebar peer，SidebarInset
    // 命中 m-2 边距（上下各 8px）——高度锁定必须计入该边距，否则文档被撑到
    // 100svh + 16px 出现页面级滚动（1280×720 下 736 > 720）
    const screen = await renderProjectTasksInLayout(
      makeProject(),
      longListFixture()
    )
    await waitForTasks(screen, '长列表任务 01')

    const de = document.documentElement
    expect(de.scrollHeight).toBeLessThanOrEqual(de.clientHeight + 1)
    expect(de.scrollTop).toBe(0)

    // tab 头完整落在视口内（不因列表过长被顶出屏幕）
    const navRect = tabsNav().getBoundingClientRect()
    expect(navRect.bottom).toBeGreaterThan(0)
    expect(navRect.bottom).toBeLessThanOrEqual(window.innerHeight)
  })

  it('AC1-a2: 锁屏高度计入 inset 边距——内容链填满 inset 而非固定 100svh', async () => {
    const screen = await renderProjectTasksInLayout(
      makeProject(),
      longListFixture()
    )
    await waitForTasks(screen, '长列表任务 01')

    // inset 边距上下各 8px：内容链高度应为视口 - 16px，底缘与视口对齐
    // （既不外溢也不欠填）。h-svh 硬锁 100svh 会把 provider 撑到 736px
    const root = document.querySelector('[data-layout="fixed"]') as HTMLElement
    expect(root).toBeTruthy()
    const rootRect = root.getBoundingClientRect()
    expect(Math.abs(rootRect.height - (window.innerHeight - 16))).toBeLessThanOrEqual(1)
    expect(Math.abs(rootRect.bottom - (window.innerHeight - 8))).toBeLessThanOrEqual(1)
  })

  it('AC1-b: 左栏自身是滚动容器，长列表在栏内溢出而非撑高页面', async () => {
    const screen = await renderProjectTasksInLayout(
      makeProject(),
      longListFixture()
    )
    await waitForTasks(screen, '长列表任务 01')

    const master = masterPane()
    expect(getComputedStyle(master).overflowY).toBe('auto')
    expect(master.scrollHeight).toBeGreaterThan(master.clientHeight)
  })

  it('AC1-c: 滚动左栏时顶部 tab 头与右栏面板位置不动', async () => {
    const screen = await renderProjectTasksInLayout(
      makeProject(),
      longListFixture()
    )
    await waitForTasks(screen, '长列表任务 01')

    const master = masterPane()
    const nav = tabsNav()
    const detail = detailPane()
    const navBefore = nav.getBoundingClientRect()
    const detailBefore = detail.getBoundingClientRect()

    master.scrollTop = 120
    expect(master.scrollTop).toBeGreaterThan(0)

    // 左栏滚动不外溢：文档不动，锚点元素 rect 逐轴不变
    expect(document.documentElement.scrollTop).toBe(0)
    const navAfter = nav.getBoundingClientRect()
    expect(navAfter.top).toBe(navBefore.top)
    expect(navAfter.bottom).toBe(navBefore.bottom)
    const detailAfter = detail.getBoundingClientRect()
    expect(detailAfter.top).toBe(detailBefore.top)
    expect(detailAfter.bottom).toBe(detailBefore.bottom)
  })

  it('AC2: 右栏自身是滚动容器，内容超高时栏内滚动、tab 头不动', async () => {
    const screen = await renderProjectTasksInLayout(
      makeProject(),
      tallDetailFixture()
    )
    await waitForTasks(screen, '右栏子任务 01')

    const detail = detailPane()
    expect(getComputedStyle(detail).overflowY).toBe('auto')
    expect(detail.scrollHeight).toBeGreaterThan(detail.clientHeight)

    const nav = tabsNav()
    const navBefore = nav.getBoundingClientRect()
    detail.scrollTop = 80
    expect(detail.scrollTop).toBeGreaterThan(0)
    expect(document.documentElement.scrollTop).toBe(0)
    expect(nav.getBoundingClientRect().top).toBe(navBefore.top)
  })

  // INFERA-272 AC：审查已确认 sidebar / floating 变体当前无溢出，修复不得
  // 引入新问题（既不外溢也不欠填——防止用「无差别扣 16px」的方式修 inset）
  it.each(['sidebar', 'floating'] as const)(
    'AC-无回归: %s 变体下仍无文档级滚动且内容填满视口',
    async (variant) => {
      const screen = await renderProjectTasksInLayout(
        makeProject(),
        longListFixture(),
        variant
      )
      await waitForTasks(screen, '长列表任务 01')

      const de = document.documentElement
      expect(de.scrollHeight).toBeLessThanOrEqual(de.clientHeight + 1)
      expect(de.scrollTop).toBe(0)

      // 无 inset 边距：内容链高度应恰为视口高（欠填说明高度被错误扣减）
      const root = document.querySelector('[data-layout="fixed"]') as HTMLElement
      expect(root).toBeTruthy()
      expect(
        Math.abs(root.getBoundingClientRect().height - window.innerHeight)
      ).toBeLessThanOrEqual(1)
    }
  )
})

describe('ProjectTasks 不渲染「情报」「候选」（INFERA-261：需求挖掘域分类不进项目任务页）', () => {
  it('AC3-a: 父任务卡片不渲染「情报」「候选」chip，其余标签照常显示', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([LABELS.auto, LABELS.candidate, LABELS.intel], [])
    )
    await waitForTasks(screen, '本地任务')

    await expect
      .element(screen.getByText('auto', { exact: true }))
      .toBeInTheDocument()
    expect(await screen.getByText('候选', { exact: true }).query()).toBeNull()
    expect(await screen.getByText('情报', { exact: true }).query()).toBeNull()
  })

  it('AC3-b: 子任务行同样不渲染「情报」「候选」chip', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture(
        [LABELS.auto],
        [LABELS.candidate, LABELS.intel, LABELS.auto]
      )
    )
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    // 父卡与子行都只剩 auto；两枚挖掘域标签在整页任何位置都不出现
    const chips = Array.from(
      document.querySelectorAll("[data-slot='label-chip']")
    )
    expect(chips.length).toBeGreaterThan(0)
    expect(chips.map((c) => c.textContent)).toEqual(['auto', 'auto'])
    expect(await screen.getByText('候选', { exact: true }).query()).toBeNull()
    expect(await screen.getByText('情报', { exact: true }).query()).toBeNull()
  })

  it('AC3-c: 标签全为「情报」「候选」时不留空壳 chip 行（不占位）', async () => {
    const screen = await renderProjectTasks(
      makeProject(),
      labeledGroupsFixture([LABELS.intel, LABELS.candidate], [])
    )
    await waitForTasks(screen, '本地任务')

    expect(document.querySelector('[data-slot="label-chip"]')).toBeNull()
    expect(document.querySelector('[data-slot="label-chip-row"]')).toBeNull()
  })
})

describe('ProjectTasks 左右分栏 master-detail（INFERA-173：左父任务列表，右选中父任务父子树）', () => {
  it('AC1: 左栏列出全部父任务（含无子任务的独立任务），整体位于右栏卡片左侧', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 左栏列表项（按钮选择器）：两个父任务各一条——无子任务的本地任务也是独立条目
    for (const title of ['本地任务', '同步父任务']) {
      await expect
        .element(screen.getByRole('button', { name: new RegExp(title) }))
        .toBeInTheDocument()
    }
    // 几何分栏：左栏列表项右缘不越过右栏卡片左缘（两栏并列不重叠）
    const listBtn = (await screen
      .getByRole('button', { name: /本地任务/ })
      .element())!
    const card = (await screen
      .getByRole('link', { name: /本地任务/ })
      .element())!
    expect(listBtn.getBoundingClientRect().right).toBeLessThanOrEqual(
      card.getBoundingClientRect().left
    )
  })

  it('AC2: 默认选中第一个父任务，右栏只渲染它——其他父任务的子树不出现', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 默认第一项选中（aria-current 高亮）
    const first = (await screen
      .getByRole('button', { name: /本地任务/ })
      .element())!
    expect(first.getAttribute('aria-current')).toBe('true')

    // 右栏只有 g1：g2 的卡片、子任务与阶段分组均不渲染
    expect(
      await screen.getByRole('link', { name: /同步父任务/ }).query()
    ).toBeNull()
    expect(
      await screen.getByRole('link', { name: /同步子任务甲/ }).query()
    ).toBeNull()
    expect(await screen.getByText('阶段 1', { exact: true }).query()).toBeNull()
  })

  it('AC3: 切换左栏选中项，右栏随之切换且高亮跟随', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    // 右栏换成 g2 子树：g1 卡片消失，g2 卡片与其子任务出现
    expect(
      await screen.getByRole('link', { name: /本地任务/ }).query()
    ).toBeNull()
    await expect
      .element(screen.getByRole('link', { name: /同步父任务/ }))
      .toBeInTheDocument()
    const second = (await screen
      .getByRole('button', { name: /同步父任务/ })
      .element())!
    expect(second.getAttribute('aria-current')).toBe('true')
    const first = (await screen
      .getByRole('button', { name: /本地任务/ })
      .element())!
    expect(first.getAttribute('aria-current')).toBeNull()

    // 切回 g1：g2 子树消失
    await selectParent(screen, '本地任务')
    await waitForTasks(screen, '本地任务')
    expect(
      await screen.getByRole('link', { name: /同步子任务甲/ }).query()
    ).toBeNull()
  })

  it('AC4: 无任务时右栏空态、左栏无列表项', async () => {
    const screen = await renderProjectTasks(makeProject(), [])
    await expect
      .element(screen.getByText('还没有任务'))
      .toBeInTheDocument()
    expect(
      await screen.getByRole('button', { name: /本地任务|同步父任务/ }).query()
    ).toBeNull()
  })
})

describe('ProjectTasks 左栏主/子层级列表（INFERA-229：图标区分 + 缩进连线 + 状态可视化）', () => {
  /** 左栏某父/子行按钮（按可访问名收窄到 master 面板） */
  async function masterRow(
    screen: RenderResult,
    name: RegExp
  ): Promise<HTMLElement> {
    const el = (await paneRoleEls(screen, MASTER_SLOT, 'button', { name }))[0]
    expect(el).toBeTruthy()
    return el as HTMLElement
  }

  /** 行内标题 span：状态图标是 svg（非 span），行内第一个 span 即标题 */
  const rowTitle = (row: HTMLElement) =>
    row.querySelector('span') as HTMLElement

  it('AC1-a: 左栏在父任务行之下渲染其子任务行（父行在前，子行按 stages 展平跟随）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // g2 的四个子任务（跨两个阶段）都进了左栏
    for (const title of [
      '同步子任务甲',
      '同步子任务乙',
      '第二批子任务',
      '第二批阻塞任务',
    ]) {
      expect(
        (
          await paneRoleEls(screen, MASTER_SLOT, 'button', {
            name: new RegExp(title),
          })
        ).length
      ).toBeGreaterThan(0)
    }

    // 文档流顺序：父行 → 首个子行 → 末个子行（阶段分组留给右栏承载）
    const parent = await masterRow(screen, /同步父任务/)
    const first = await masterRow(screen, /同步子任务甲/)
    const last = await masterRow(screen, /第二批阻塞任务/)
    expect(
      parent.compareDocumentPosition(first) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(
      first.compareDocumentPosition(last) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('AC1-b: 子任务行整体缩进于父行之下（子行左缘明显右于父行左缘）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const parent = await masterRow(screen, /同步父任务/)
    const child = await masterRow(screen, /同步子任务甲/)
    const dx =
      child.getBoundingClientRect().x - parent.getBoundingClientRect().x
    expect(dx).toBeGreaterThan(8)
  })

  it('AC1-c: 子任务组带竖向连线（组容器实线左边框）——缩进 + 连线组合表达层级', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const group = document.querySelector(
      "[data-slot='task-master-list'] [data-slot='task-child-group']"
    )
    expect(group).not.toBeNull()
    const cs = getComputedStyle(group!)
    expect(cs.borderLeftStyle).toBe('solid')
    expect(parseFloat(cs.borderLeftWidth)).toBeGreaterThan(0)
  })

  it('AC1-d: 主/子图标不同——父行状态图标嵌于带边框的方形标识位，子行为裸图标', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const parent = await masterRow(screen, /同步父任务/)
    const child = await masterRow(screen, /同步子任务甲/)
    const parentIcon = parent.querySelector("[role='img']")!
    const childIcon = child.querySelector("[role='img']")!
    expect(parentIcon).not.toBeNull()
    expect(childIcon).not.toBeNull()

    // 父行图标外层是带实线边框的方形 tile（主任务标识位）；子行图标外层无边框
    const tile = parentIcon.parentElement!
    expect(getComputedStyle(tile).borderStyle).toBe('solid')
    expect(parseFloat(getComputedStyle(tile).borderWidth)).toBeGreaterThan(0)
    expect(getComputedStyle(childIcon.parentElement!).borderWidth).toBe('0px')
  })

  it('AC2-a: 左栏父行与子行均带状态图标（四态各一）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // fixture：g1 父=进行中；g2 父=未启动；子=已完成/未启动/进行中/已阻塞
    for (const label of ['已完成', '未启动', '进行中', '已阻塞']) {
      expect(
        (
          await paneRoleEls(screen, MASTER_SLOT, 'img', {
            name: label,
            exact: true,
          })
        ).length
      ).toBeGreaterThan(0)
    }
  })

  it('AC2-b: 选中态明确——选中父任务的子行组文字深于未选中组（aria-current 由既有用例覆盖）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 默认选中 g1：g2 未选中，其子行标题为次级灰
    const before = getComputedStyle(
      rowTitle(await masterRow(screen, /同步子任务甲/))
    ).color

    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')

    // 选中 g2 后：其子行标题转为墨色（同组激活）
    const after = getComputedStyle(
      rowTitle(await masterRow(screen, /同步子任务甲/))
    ).color
    expect(after).not.toBe(before)
  })

  it('AC2-c: 悬停态——hover 子任务行后背景变化', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const row = screen.getByRole('button', { name: /同步子任务甲/ })
    const before = getComputedStyle((await row.element())!).backgroundColor
    await row.hover()
    await expect
      .poll(
        async () => getComputedStyle((await row.element())!).backgroundColor,
        { timeout: 4000 }
      )
      .not.toBe(before)
  })

  it('AC2-d: 点击左栏子任务行选中其父任务（右栏切到该父任务的父子树）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 初始右栏是 g1
    expect(
      (await paneRoleEls(screen, DETAIL_SLOT, 'link', { name: /本地任务/ }))
        .length
    ).toBeGreaterThan(0)

    await (await masterRow(screen, /第二批阻塞任务/)).click()
    await waitForTasks(screen, '同步子任务甲')

    // 右栏换成 g2 子树，左栏父行高亮跟随
    expect(
      (await paneRoleEls(screen, DETAIL_SLOT, 'link', { name: /本地任务/ }))
        .length
    ).toBe(0)
    const parent = await masterRow(screen, /同步父任务/)
    expect(parent.getAttribute('aria-current')).toBe('true')
  })
})

describe('ProjectTasks 新建需求入口（INFERA-178：与项目详情页共享对话框）', () => {
  /** 创建成功响应（同步侧 Delivery 形状，契约 201） */
  function makeCreatedDelivery(): Delivery {
    return {
      id: 'd-new',
      project_id: 'p1',
      title: '登录页改版',
      description: '',
      status: 'queued',
      current_stage: '',
      pending_gate: null,
      fail_count: 0,
      created_at: '2026-08-23T00:00:00Z',
      updated_at: '2026-08-23T00:00:00Z',
      external_issue_id: 'mi-9',
      external_issue_key: 'INFERA-99',
      assignee: 'agent:tech-lead',
      priority: '',
      external_synced_at: '2026-08-23T00:00:00Z',
      parent_id: '',
      wave: 0,
      split_mode: false,
      merge_state: '',
      complexity: '',
    }
  }

  it('页头「新建需求」按钮打开共享创建对话框（默认项目=当前项目）', async () => {
    vi.mocked(listProjects).mockResolvedValue([makeProject()])
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    await screen.getByRole('button', { name: '新建需求' }).click()
    await expect.element(screen.getByRole('dialog')).toBeInTheDocument()
    await expect
      .element(screen.getByLabelText('标题'))
      .toBeInTheDocument()
    const project = (await screen
      .getByRole('combobox', { name: '项目', exact: true })
      .element())!
    expect(project.textContent).toContain('演示项目')
  })

  it('创建成功后对话框关闭、task-groups 刷新出新任务卡', async () => {
    vi.mocked(listProjects).mockResolvedValue([makeProject()])
    vi.mocked(createProjectRequirement).mockResolvedValue(makeCreatedDelivery())
    // 首拉为空（空态）；创建成功失效缓存后的重拉给默认值（新任务）
    vi.mocked(listProjectTaskGroups).mockResolvedValueOnce([])
    const screen = await renderProjectTasks(
      makeProject(),
      [
        makeGroup({
          id: 'g-new',
          title: '登录页改版',
          status: 'queued',
          current_stage: '',
          external_issue_id: 'mi-9',
          external_issue_key: 'INFERA-99',
        }),
      ]
    )
    await expect
      .element(screen.getByText('还没有任务'))
      .toBeInTheDocument()

    await screen.getByRole('button', { name: '新建需求' }).click()
    await screen.getByLabelText('标题').fill('登录页改版')
    await screen.getByRole('button', { name: '创建需求' }).click()

    // 缓存失效 → 重拉：空态消失，新任务出现在左栏列表（父任务条目为按钮）
    await expect
      .element(screen.getByRole('button', { name: /登录页改版/ }))
      .toBeInTheDocument()
    expect(
      await screen.getByText('还没有任务', { exact: true }).query()
    ).toBeNull()
    expect(vi.mocked(createProjectRequirement)).toHaveBeenCalledWith('p1', {
      title: '登录页改版',
      description: '',
      status: 'backlog',
      priority: 'none',
      auto_merge: false,
      agent_id: '',
    })
  })
})

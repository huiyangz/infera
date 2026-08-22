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
 * 分组 fixture（L202608222116-1-T02 多阶段覆盖）：本地父（无子任务）+
 * 同步父（两个阶段共四个子任务，四种状态各一：一完成、一未启动、
 * 一进行中、一阻塞），进度计数 1/4。
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
      child_total: 4,
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
            makeChild({
              id: 'c6',
              title: '第二批阻塞任务',
              status: 'blocked',
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

/** 文档流顺序断言：a 元素需在 b 之前 */
async function expectBefore(
  screen: RenderResult,
  a: Parameters<RenderResult['getByText']>[0],
  b: Parameters<RenderResult['getByText']>[0]
) {
  const ea = (await screen.getByText(a, { exact: true }).element())!
  const eb = (await screen.getByText(b, { exact: true }).element())!
  expect(
    ea.compareDocumentPosition(eb) & Node.DOCUMENT_POSITION_FOLLOWING
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
      .element(screen.getByText('1/4', { exact: true }))
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
    const icon = (await screen.getByRole('img', { name: '已完成' }).element())!
    // 行内状态图标（行内容最左元素）明显右于阶段标题文本
    const dx = icon.getBoundingClientRect().x - stage.getBoundingClientRect().x
    expect(dx).toBeGreaterThan(8)
  })

  it('AC1-d: 每个子任务行带状态图标（已完成/未启动/进行中/已阻塞）', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '第二批阻塞任务')

    for (const label of ['已完成', '未启动', '进行中', '已阻塞']) {
      await expect
        .element(screen.getByRole('img', { name: label, exact: true }))
        .toBeInTheDocument()
    }
  })

  it('INFERA-145 返工: 四态图标 class 逐属性钉死（尺寸/单色/进行中 spin），重构前后渲染一致', async () => {
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
    ]
    for (const [label, cls] of cases) {
      const el = (await screen
        .getByRole('img', { name: label, exact: true })
        .element())!
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
                  multica_issue_id: 'mi-13',
                  multica_issue_key: 'INFERA-13',
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

  it('multica 来源: 选中同步父卡片带 Multica 标识；子任务行以粗体 issue key 标识来源', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    // 默认选中的本地父：无来源徽
    expect(await screen.getByText('Multica', { exact: true }).query()).toBeNull()

    await selectParent(screen, '同步父任务')
    await waitForTasks(screen, '同步子任务甲')
    // 来源徽只出现在选中父卡片（子行信息由粗体 key 承担，对齐参考图行式）
    const chips = await screen.getByText('Multica', { exact: true }).all()
    expect(chips).toHaveLength(1)
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
      .element(screen.getByText('1/4', { exact: true }))
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

  it('界面文案不出现「需求」', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    expect(await screen.getByText(/需求/).query()).toBeNull()
  })

  it('面包屑回链项目详情', async () => {
    const screen = await renderProjectTasks(makeProject(), groupsFixture())
    await waitForTasks(screen, '本地任务')

    const back = await screen.getByRole('link', { name: '演示项目' }).element()
    expect(back?.getAttribute('href')).toBe('/projects/p1')
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

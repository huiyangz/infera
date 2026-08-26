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
import { getDelivery, getChildProgress } from '@/lib/infera-api'
import type { ChildProgress, Delivery } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { DeliveryDetail } from './delivery-detail'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getDelivery: vi.fn(),
    // 进度聚合默认空（无子任务）：既有用例不渲染进度卡、无 query 噪音
    getChildProgress: vi.fn().mockResolvedValue({
      delivery_id: 'd1',
      active_stage: null,
      total: 0,
      done: 0,
      in_progress: 0,
      in_review: 0,
      blocked: 0,
      todo: 0,
      cancelled: 0,
      by_status: {
        active: 0,
        queued: 0,
        completed: 0,
        blocked: 0,
        cancelled: 0,
      },
      stages: [],
    } satisfies ChildProgress),
    // useLocalNodes 依赖的两条查询（空绑定即可，不触发本机停车 UI）
    listAgents: vi.fn().mockResolvedValue([]),
    getProjectPipeline: vi.fn().mockResolvedValue({ nodes: [], bindings: {} }),
  }
})

// WS 实时订阅是纯网络副作用，布局测试里以 no-op 替身（断线重连不在本卡范围）
vi.mock('@/hooks/use-delivery-events', () => ({
  useDeliveryEvents: vi.fn(),
}))

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

// 多行 + 超长不可折行片段：同时压测 pre-wrap 换行保留与横向溢出
const LONG_DESC = [
  '这是一段很长的任务描述，用来验证描述面板归位后的排版。',
  '第二行带超长不可折行片段：' + '特别长的描述片段'.repeat(16),
  '第三行：描述面板归位到正文卡片内。',
].join('\n')

const LONG_TITLE = '一个相当长的任务标题用来验证顶栏截断而正文完整展示'.repeat(3)

function makeDelivery(overrides: Partial<Delivery> = {}): Delivery {
  return {
    id: 'd1',
    project_id: 'p1',
    title: '演示任务',
    description: '',
    status: 'active',
    current_stage: 'code_gen',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-20T10:00:00Z',
    updated_at: '2026-08-22T10:00:00Z',
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
    ...overrides,
  }
}

async function renderDetail(
  delivery: Delivery,
  children: Delivery[] = []
): Promise<RenderResult> {
  vi.mocked(getDelivery).mockResolvedValue({
    delivery,
    timeline: [],
    artifacts: [],
    children,
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：SidebarProvider > SidebarInset(flex 纵向列) 承载页面内容
  return await render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <SidebarInset>
          <DeliveryDetail deliveryId={delivery.id} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

/** 等 delivery 数据加载完成（阶段推进卡出现即 getDelivery 已 resolve） */
async function waitForLoad(screen: RenderResult) {
  await expect
    .element(screen.getByText('阶段推进', { exact: true }))
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

describe('DeliveryDetail 任务详情页布局（INFERA-137：描述面板归位、版式理顺）', () => {
  it('AC1-1: 描述面板在正文卡片内，不在顶栏 header 里，且不与顶栏重叠', async () => {
    const screen = await renderDetail(
      makeDelivery({ description: LONG_DESC })
    )
    await waitForLoad(screen)

    const headerEl = document.querySelector('header')
    expect(headerEl).toBeTruthy()
    const descEl = await screen
      .getByText('描述面板归位到正文卡片内', { exact: false })
      .element()
    // 结构：描述不是 header 的后代（旧版挂在 h-16 固定高 header 里，长描述直接飘出）
    expect(headerEl!.contains(descEl!)).toBe(false)
    // 几何：描述顶部在 header 底边之下（无重叠/飘出）
    const headerRect = headerEl!.getBoundingClientRect()
    const descRect = descEl!.getBoundingClientRect()
    expect(descRect.top).toBeGreaterThanOrEqual(headerRect.bottom - 1)
  })

  it('AC1-2: 顶栏保持单行紧凑——返回链接 + 截断标题 + 状态徽标，内容不溢出 64px 盒', async () => {
    const screen = await renderDetail(
      makeDelivery({ title: LONG_TITLE, description: LONG_DESC })
    )
    await waitForLoad(screen)

    const headerEl = document.querySelector('header')!
    // 内容不溢出固定高 header（旧版把整段描述塞在 header 里，scrollHeight 远超盒高）
    expect(headerEl.scrollHeight).toBeLessThanOrEqual(
      headerEl.clientHeight + 1
    )
    // 返回链接与状态徽标都在 header 内
    const backLink = await screen
      .getByRole('link', { name: '返回项目任务' })
      .element()
    expect(headerEl.contains(backLink!)).toBe(true)
    const badge = await screen
      .getByText('进行中', { exact: true })
      .first()
      .element()
    expect(headerEl.contains(badge!)).toBe(true)
    // 顶栏标题截断（视觉截断，DOM 文本仍完整），正文标题完整展示（可换行）
    const titleEls = await screen
      .getByText(LONG_TITLE, { exact: true })
      .elements()
    expect(titleEls.length).toBeGreaterThanOrEqual(2)
    const [headerTitle, bodyTitle] = titleEls
    expect(headerTitle!.scrollWidth).toBeGreaterThan(headerTitle!.clientWidth)
    expect(bodyTitle!.scrollWidth).toBeLessThanOrEqual(bodyTitle!.clientWidth + 1)
  })

  it('AC1-3: 正文任务信息卡完整展示描述（Markdown 渲染），排在阶段推进之前', async () => {
    const screen = await renderDetail(
      makeDelivery({ description: LONG_DESC })
    )
    await waitForLoad(screen)

    // 描述区块带「描述」小标
    await expect
      .element(screen.getByText('描述', { exact: true }))
      .toBeInTheDocument()
    // INFERA-296 起描述经 MarkdownEditor 渲染：多行内容逐行完整出现在
    // 渲染容器内（不截断、不丢行），并取到 markdown 排版字号
    const descEl = await screen
      .getByText('描述面板归位到正文卡片内', { exact: false })
      .element()
    const mdRender = descEl!.closest('.md-render')
    expect(mdRender).toBeTruthy()
    expect(mdRender!.textContent).toContain('第二行带超长不可折行片段')
    expect(mdRender!.textContent).toContain('第三行：描述面板归位到正文卡片内')
    expect(getComputedStyle(descEl!).fontSize).toBe('14px') // 0.875rem
    // 信息卡在阶段推进卡之前（阅读顺序：先任务信息，再阶段推进）
    const stageCardTitle = await screen
      .getByText('阶段推进', { exact: true })
      .element()
    expect(
      descEl!.compareDocumentPosition(stageCardTitle!) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('AC1-回归: 超长不可折行描述不产生页面横向滚动', async () => {
    const screen = await renderDetail(
      makeDelivery({ description: LONG_DESC })
    )
    await waitForLoad(screen)

    expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
      document.documentElement.clientWidth
    )
  })

  it('AC1-回归: 空描述显示占位文案，不渲染空段', async () => {
    const screen = await renderDetail(makeDelivery({ description: '' }))
    await waitForLoad(screen)

    await expect
      .element(screen.getByText('（无补充描述）', { exact: true }))
      .toBeInTheDocument()
  })

  it('INFERA-145 返工: 镜像任务 current_stage 为空串时阶段位给占位，不留悬空「阶段」标签', async () => {
    // 同步镜像任务无 current_stage：stageLabel('') 为空串，旧版元信息行渲染成「阶段 · 创建 …」
    const screen = await renderDetail(
      makeDelivery({ current_stage: '', external_issue_key: 'INFERA-77' })
    )
    await waitForLoad(screen)

    // 对齐 project-tasks 的 `stageLabel(...) || '—'` 占位写法：阶段位显示占位符
    await expect
      .element(screen.getByText('阶段 — ·', { exact: false }))
      .toBeInTheDocument()
  })
})

describe('DeliveryDetail 描述区 Markdown 接入（INFERA-296：渲染 / 预览源码切换 / 编辑）', () => {
  // 真实任务描述形态：issue 正文同步而来的 Markdown（标题 + 列表）
  const MD_DESC = [
    '## 目标',
    '',
    '统一接入 Markdown 组件：',
    '',
    '- 列表项正常渲染',
    '- 预览与源码一键切换',
  ].join('\n')

  it('AC1: 描述按 Markdown 渲染（标题/列表成节点），不再原样显示源码', async () => {
    const screen = await renderDetail(makeDelivery({ description: MD_DESC }))
    await waitForLoad(screen)

    await expect
      .element(screen.getByRole('heading', { level: 2, name: '目标' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('列表项正常渲染', { exact: true }))
      .toBeInTheDocument()
    // 旧版 <p> 原样显示源码的问题消失：字面「## 目标」不再出现在页面上
    expect(
      await screen.getByText('## 目标', { exact: true }).query()
    ).toBeNull()
  })

  it('AC2: 默认预览（无编辑框）；一键切源码显示原始 Markdown，可切回预览', async () => {
    const screen = await renderDetail(makeDelivery({ description: MD_DESC }))
    await waitForLoad(screen)

    // 默认预览：只有渲染结果，没有编辑框
    expect(document.querySelector('textarea')).toBeNull()
    await expect
      .element(screen.getByRole('heading', { level: 2, name: '目标' }))
      .toBeInTheDocument()

    // 切源码：编辑框出现并携带原始 Markdown，渲染节点退场
    await screen.getByRole('button', { name: '源码' }).click()
    const box = screen.getByRole('textbox')
    await expect.element(box).toBeInTheDocument()
    expect(((await box.element()) as HTMLTextAreaElement).value).toContain(
      '## 目标'
    )
    expect(
      await screen.getByRole('heading', { level: 2, name: '目标' }).query()
    ).toBeNull()

    // 切回预览：恢复渲染
    await screen.getByRole('button', { name: '预览' }).click()
    await expect
      .element(screen.getByRole('heading', { level: 2, name: '目标' }))
      .toBeInTheDocument()
  })

  it('AC3: 源码模式可直接编辑——草稿受控回显，切回预览即见改后渲染', async () => {
    const screen = await renderDetail(makeDelivery({ description: MD_DESC }))
    await waitForLoad(screen)

    await screen.getByRole('button', { name: '源码' }).click()
    const box = screen.getByRole('textbox')
    await box.fill('## 改后的目标\n\n- 新草稿内容')
    expect(((await box.element()) as HTMLTextAreaElement).value).toContain(
      '改后的目标'
    )

    await screen.getByRole('button', { name: '预览' }).click()
    await expect
      .element(screen.getByRole('heading', { level: 2, name: '改后的目标' }))
      .toBeInTheDocument()
  })
})

/** 标签 fixture（INFERA-220）：Multica 标签库的真实三色——auto/候选/情报 */
const LABELS = {
  auto: { name: 'auto', color: '#22c55e' },
  candidate: { name: '候选', color: '#a855f7' },
  intel: { name: '情报', color: '#3b82f6' },
}

describe('DeliveryDetail 已取消终态（INFERA-233：cancelled 全站接入）', () => {
  it('cancelled 任务正常渲染：徽标「已取消」，阶段条不再出现进行中 spinner', async () => {
    const screen = await renderDetail(
      makeDelivery({ status: 'cancelled', current_stage: 'code_gen' })
    )
    await waitForLoad(screen)

    // 顶栏徽标按新词表渲染（不再落入「已完成」兜底）
    await expect
      .element(screen.getByText('已取消', { exact: true }))
      .toBeInTheDocument()
    expect(await screen.getByText('已完成', { exact: true }).query()).toBeNull()

    // 阶段条语义：放弃 = 无进行中阶段（无 spinner、无门禁等待文案）
    const stageCard = [...document.querySelectorAll("[data-slot='card']")].find(
      (c) => c.textContent?.includes('阶段推进')
    )
    expect(stageCard).toBeTruthy()
    expect(stageCard!.querySelector('.animate-spin')).toBeNull()
    expect(stageCard!.textContent).not.toContain('等待你的审批')
  })
})

describe('DeliveryDetail 标签展示（INFERA-220：任务详情渲染 Multica 标签 chip）', () => {
  it('AC1-a: 任务信息卡显示标签 chip，名称与底色（hex 原值）与后端一致', async () => {
    const screen = await renderDetail(
      makeDelivery({ labels: [LABELS.auto, LABELS.candidate] })
    )
    await waitForLoad(screen)

    for (const name of ['auto', '候选']) {
      await expect
        .element(screen.getByText(name, { exact: true }))
        .toBeInTheDocument()
    }
    const chip = (await screen.getByText('auto', { exact: true }).element())!
    expect(getComputedStyle(chip).backgroundColor).toBe('rgb(34, 197, 94)')
  })

  it('AC2: 同步来源的交付（external 标记）同样显示标签', async () => {
    const screen = await renderDetail(
      makeDelivery({
        external_issue_id: 'mi-2',
        external_issue_key: 'INFERA-77',
        current_stage: '',
        labels: [LABELS.intel],
      })
    )
    await waitForLoad(screen)

    await expect
      .element(screen.getByText('情报', { exact: true }))
      .toBeInTheDocument()
    const chip = (await screen.getByText('情报', { exact: true }).element())!
    expect(getComputedStyle(chip).backgroundColor).toBe('rgb(59, 130, 246)')
  })

  it('AC3-a: 无标签的交付不渲染 chip 行（不占位、不留空壳 UI）', async () => {
    const screen = await renderDetail(makeDelivery())
    await waitForLoad(screen)

    expect(document.querySelector('[data-slot="label-chip"]')).toBeNull()
    expect(document.querySelector('[data-slot="label-chip-row"]')).toBeNull()
  })

  it('AC3-b: 超长标签名截断展示（完整名保留在 title），不撑破信息卡', async () => {
    const long = '一个特别长的标签名称用来验证详情卡内截断'.repeat(3)
    const screen = await renderDetail(
      makeDelivery({ labels: [{ name: long, color: '#22c55e' }] })
    )
    await waitForLoad(screen)

    const chip = document.querySelector('[data-slot="label-chip"]')!
    expect(chip.getAttribute('title')).toBe(long)
    expect(chip.scrollWidth).toBeGreaterThan(chip.clientWidth)
  })

  it('AC1-b: 拆分子任务清单的子需求行同样显示各自标签', async () => {
    const screen = await renderDetail(
      makeDelivery({ split_mode: true, current_stage: 'code_gen' }),
      [
        makeDelivery({
          id: 'c1',
          title: '子需求甲',
          wave: 1,
          labels: [LABELS.auto],
        }),
        makeDelivery({ id: 'c2', title: '子需求乙', wave: 2 }),
      ]
    )
    await waitForLoad(screen)
    await expect
      .element(screen.getByText('子任务清单', { exact: true }))
      .toBeInTheDocument()

    // 带标签的子需求显示 chip；无标签的不显示（同一列表两种行都有）
    await expect
      .element(screen.getByText('auto', { exact: true }))
      .toBeInTheDocument()
    const chips = document.querySelectorAll('[data-slot="label-chip"]')
    expect(chips).toHaveLength(1)
  })
})

// —— 子任务进度区接入（INFERA-297：消费 L202608260142-1-T01 只读聚合接口） ——

/** 单维度计数（后端契约：by_status 恒含五键） */
function aggCounts(overrides: Record<string, number> = {}) {
  return {
    total: 0,
    done: 0,
    in_progress: 0,
    in_review: 0,
    blocked: 0,
    todo: 0,
    cancelled: 0,
    by_status: {
      active: 0,
      queued: 0,
      completed: 0,
      blocked: 0,
      cancelled: 0,
    },
    ...overrides,
  }
}

/** 冻结契约形状的聚合 fixture：混合状态、两编号阶段 + 无阶段组、活跃阶段 2 */
function makeAggregation(overrides: Partial<ChildProgress> = {}): ChildProgress {
  return {
    delivery_id: 'd1',
    active_stage: 2,
    ...aggCounts({
      total: 5,
      done: 2,
      in_progress: 1,
      in_review: 1,
      blocked: 1,
      todo: 0,
      cancelled: 0,
    }),
    stages: [
      { stage: 1, ...aggCounts({ total: 2, done: 2 }) },
      {
        stage: 2,
        ...aggCounts({
          total: 3,
          done: 0,
          in_progress: 1,
          in_review: 1,
          blocked: 1,
        }),
      },
      { stage: 0, ...aggCounts({ total: 0 }) },
    ],
    ...overrides,
  }
}

describe('DeliveryDetail 子任务进度区（INFERA-297：真实执行对齐）', () => {
  it('AC1: 拆分父的进度区消费聚合接口——混合状态可见、按阶段分组、活跃阶段可辨', async () => {
    vi.mocked(getChildProgress).mockResolvedValue(makeAggregation())
    const screen = await renderDetail(
      makeDelivery({ split_mode: true, complexity: 'large', current_stage: 'code_gen' }),
      [
        makeDelivery({ id: 'c1', title: '子需求甲', wave: 1, status: 'completed' }),
        makeDelivery({
          id: 'c2',
          title: '子需求乙',
          wave: 2,
          status: 'active',
          pending_gate: 'code_review',
        }),
        makeDelivery({ id: 'c3', title: '子需求丙', wave: 2, status: 'blocked' }),
      ]
    )
    await waitForLoad(screen)

    // 进度卡出现，总体计数逐字来自聚合（可对照核验）
    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()
    expect(vi.mocked(getChildProgress)).toHaveBeenCalledWith('d1')
    await expect
      .element(screen.getByText('2 / 5 完成 · 40%', { exact: true }))
      .toBeInTheDocument()
    // 运行中 / 待审批 / 已阻塞 在进度区可见；按阶段分组、活跃阶段标注
    for (const label of ['运行中 1', '待审批 1', '已阻塞 1']) {
      await expect
        .element(screen.getByText(label, { exact: true }).first())
        .toBeInTheDocument()
    }
    for (const title of ['阶段 1', '阶段 2']) {
      await expect
        .element(screen.getByText(title, { exact: true }))
        .toBeInTheDocument()
    }
    // 子任务清单（导航列表）仍在，不回退
    await expect
      .element(screen.getByText('子任务清单', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC2: 子任务清单的头部计数不再由 children 平表自算（撤掉装饰性数字，进度数字归聚合区）', async () => {
    // children 平表 3 个全 completed（旧口径会显示「3 / 3 完成」）；聚合另有口径 2/3
    vi.mocked(getChildProgress).mockResolvedValue(
      makeAggregation({
        ...aggCounts({ total: 3, done: 2, in_progress: 1 }),
        stages: [
          { stage: 1, ...aggCounts({ total: 3, done: 2, in_progress: 1 }) },
        ],
      })
    )
    const screen = await renderDetail(
      makeDelivery({ split_mode: true, current_stage: 'code_gen' }),
      [
        makeDelivery({ id: 'c1', title: '子需求甲', wave: 1, status: 'completed' }),
        makeDelivery({ id: 'c2', title: '子需求乙', wave: 1, status: 'completed' }),
        makeDelivery({ id: 'c3', title: '子需求丙', wave: 2, status: 'completed' }),
      ]
    )
    await waitForLoad(screen)

    // 聚合区显示后端口径 2/3；旧 children 自算的「3 / 3 完成」不复存在
    await expect
      .element(screen.getByText('2 / 3 完成 · 67%', { exact: true }))
      .toBeInTheDocument()
    expect(await screen.getByText('3 / 3 完成', { exact: true }).query()).toBeNull()
  })

  it('AC3: 任务同步镜像父（非拆分）同样显示子任务进度——聚合不区分父类型', async () => {
    vi.mocked(getChildProgress).mockResolvedValue(
      makeAggregation({
        ...aggCounts({ total: 2, done: 1, in_review: 1 }),
        stages: [
          {
            stage: 1,
            ...aggCounts({ total: 2, done: 1, in_review: 1 }),
          },
        ],
      })
    )
    // 镜像父：split_mode=false、children 不随详情返回（后端仅拆分父返回）
    const screen = await renderDetail(
      makeDelivery({ external_issue_key: 'INFERA-77', current_stage: '' })
    )
    await waitForLoad(screen)

    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('待审批 1', { exact: true }).first())
      .toBeInTheDocument()
    // 非拆分父不渲染子任务清单列表
    expect(await screen.getByText('子任务清单', { exact: true }).query()).toBeNull()
  })

  it('AC4: 无子任务（聚合 total=0）不渲染进度区——空态不占位', async () => {
    // 默认 mock 即空聚合（factory 里的空 fixture），显式声明便于阅读
    vi.mocked(getChildProgress).mockResolvedValue({
      delivery_id: 'd1',
      active_stage: null,
      ...aggCounts(),
      stages: [],
    } satisfies ChildProgress)
    const screen = await renderDetail(makeDelivery())
    await waitForLoad(screen)

    expect(await screen.getByText('子任务进度', { exact: true }).query()).toBeNull()
    expect(document.querySelector('[data-slot="child-progress"]')).toBeNull()
  })
})

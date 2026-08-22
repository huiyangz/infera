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
import { getDelivery } from '@/lib/infera-api'
import type { Delivery } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { DeliveryDetail } from './delivery-detail'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getDelivery: vi.fn(),
    // useLocalNodes 依赖的两条 pipeline 查询（空绑定即可，不触发本机停车 UI）
    getPipeline: vi.fn().mockResolvedValue({ nodes: [], agents: [], bindings: {} }),
    getProjectPipeline: vi.fn().mockResolvedValue({
      nodes: [],
      defaults: {},
      overrides: {},
      effective: {},
    }),
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
    ...overrides,
  }
}

async function renderDetail(
  delivery: Delivery
): Promise<RenderResult> {
  vi.mocked(getDelivery).mockResolvedValue({
    delivery,
    timeline: [],
    artifacts: [],
    children: [],
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

  it('AC1-3: 正文任务信息卡完整展示描述（whitespace-pre-wrap 保留换行），排在阶段推进之前', async () => {
    const screen = await renderDetail(
      makeDelivery({ description: LONG_DESC })
    )
    await waitForLoad(screen)

    // 描述区块带「描述」小标
    await expect
      .element(screen.getByText('描述', { exact: true }))
      .toBeInTheDocument()
    const descEl = await screen
      .getByText('描述面板归位到正文卡片内', { exact: false })
      .element()
    // pre-wrap：多行描述的换行符在渲染中保留（不折叠成一行）
    expect(getComputedStyle(descEl!).whiteSpace).toBe('pre-wrap')
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
})

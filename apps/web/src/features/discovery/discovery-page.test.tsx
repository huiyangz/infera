import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { page } from 'vitest/browser'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
// 布局回归需要真实 Tailwind 样式（与 project-detail / label-chip 测试同构）
import '@/styles/index.css'
import { listDiscoveryTasks } from './api'
import type { DiscoveryTaskRow } from './types'
import { DiscoveryPage } from './discovery-page'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    listDiscoveryTasks: vi.fn(),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@tanstack/react-router')>()
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
  return { ...actual, Link: MockLink }
})

/** 夹具行工厂：默认一张需求挖掘（情报）卡 */
function row(over: Partial<DiscoveryTaskRow>): DiscoveryTaskRow {
  return {
    id: 'd-000000000001',
    project_id: 'p1',
    title: '情报：支付渠道调研',
    description: '',
    status: 'active',
    current_stage: 'intake',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-24T05:00:00Z',
    updated_at: '2026-08-24T05:10:00Z',
    external_issue_id: '',
    external_issue_key: 'INFERA-230',
    assignee: '',
    priority: '',
    external_synced_at: null,
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    agent_types: ['mining'],
    project_name: '自动闭环',
    labels: [{ name: '情报', color: '#22c55e' }],
    ...over,
  }
}

/** 四行典型数据：纯挖掘 / 纯分析（已完成）/ 双标签（阻塞）/ 已放弃 */
function mixedRows(): DiscoveryTaskRow[] {
  return [
    row({}),
    row({
      id: 'd-000000000002',
      title: '候选：一键对账需求',
      status: 'completed',
      agent_types: ['analysis'],
      labels: [{ name: '候选', color: '#3b82f6' }],
      project_name: 'infera',
    }),
    row({
      id: 'd-000000000003',
      title: '情报+候选：多币种结算',
      status: 'blocked',
      agent_types: ['mining', 'analysis'],
      labels: [
        { name: '情报', color: '#22c55e' },
        { name: '候选', color: '#3b82f6' },
      ],
    }),
    row({
      id: 'd-000000000004',
      title: '情报：多语言站点评测',
      status: 'cancelled',
      agent_types: ['mining'],
      labels: [],
    }),
  ]
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：Header 依赖 SidebarProvider 上下文（与 decisions-page 测试同构）
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <DiscoveryPage />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listDiscoveryTasks).mockResolvedValue([])
})

afterEach(async () => {
  await cleanup()
})

describe('DiscoveryPage 需求发现页', () => {
  it('并集拉取一次，完整渲染两类 agent 的任务：标题 / 项目名 / 状态 / 标签，标题链接跳任务详情', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()

    // 一次并集取回（零参调用 = 省略 agent 参数），筛选/分组在客户端完成
    expect(vi.mocked(listDiscoveryTasks)).toHaveBeenCalledWith()

    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()
    await expect.element(screen.getByText('候选：一键对账需求')).toBeInTheDocument()
    await expect
      .element(screen.getByText('情报+候选：多币种结算'))
      .toBeInTheDocument()
    // 跨项目标注项目名（两行同项目 → 取首个匹配即可）
    await expect
      .element(screen.getByText('自动闭环').first())
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('infera', { exact: true }))
      .toBeInTheDocument()
    // 状态徽标（与主看板同款单色徽标）
    await expect
      .element(screen.getByText('进行中', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('已完成', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('已阻塞', { exact: true }))
      .toBeInTheDocument()
    // 标签 chip 展示标签名
    await expect.element(screen.getByText('情报').first()).toBeInTheDocument()
    // 行内 id 即 delivery ID → 跳既有任务详情页
    expect(
      (await screen.getByText('情报：支付渠道调研').element()).closest('a')
        ?.getAttribute('href')
    ).toBe('/deliveries/d-000000000001')
  })

  it('按 agent 筛选：选「需求分析」仅留 analysis 行，双标签卡保留', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '类型', exact: true }).click()
    await screen.getByRole('option', { name: '需求分析', exact: true }).click()

    // 纯挖掘卡被滤掉；分析卡与双标签卡保留
    expect(await screen.getByText('情报：支付渠道调研').query()).toBeNull()
    await expect
      .element(screen.getByText('候选：一键对账需求'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('情报+候选：多币种结算'))
      .toBeInTheDocument()
  })

  it('按状态筛选：选「已完成」仅留已完成行', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '状态', exact: true }).click()
    await screen.getByRole('option', { name: '已完成', exact: true }).click()

    expect(await screen.getByText('情报：支付渠道调研').query()).toBeNull()
    expect(await screen.getByText('情报+候选：多币种结算').query()).toBeNull()
    await expect
      .element(screen.getByText('候选：一键对账需求'))
      .toBeInTheDocument()
  })

  it('按类型分组：组头「需求挖掘 / 需求分析」，双标签卡两组都出现', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '分组', exact: true }).click()
    await screen.getByRole('option', { name: '按类型', exact: true }).click()

    // INFERA-233 后组头分栏渲染：候选栏出现需求挖掘/需求分析两组头，
    // 已放弃栏的 cancelled 行也是挖掘类 → 需求挖掘组头两栏各一
    const miningHeaders = await screen
      .getByRole('heading', { name: '需求挖掘' })
      .elements()
    expect(miningHeaders).toHaveLength(2)
    await expect
      .element(screen.getByRole('heading', { name: '需求分析' }))
      .toBeInTheDocument()
    // 双标签卡在两组都渲染（标题文本出现两次）
    expect(
      (screen.container.textContent?.match(/情报\+候选：多币种结算/g) ?? [])
        .length
    ).toBe(2)
  })

  it('按状态分组：组头带状态中文标签', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '分组', exact: true }).click()
    await screen.getByRole('option', { name: '按状态', exact: true }).click()

    // 组头（h3）：进行中 / 已完成 / 已阻塞 —— 与状态徽标文本同词，
    // 以 heading role 限定断言组头本身存在
    await expect
      .element(screen.getByRole('heading', { name: '进行中' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('heading', { name: '已完成' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('heading', { name: '已阻塞' }))
      .toBeInTheDocument()
  })

  it('INFERA-233 左右双栏：cancelled 行只出现在右栏「已放弃」，候选行留在左栏', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    // 两栏各有栏头（heading）：候选 / 已放弃
    await expect
      .element(screen.getByRole('heading', { name: '候选', exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('heading', { name: '已放弃', exact: true }))
      .toBeInTheDocument()

    // cancelled 卡落在「已放弃」栏内（沿 DOM 向上找到所属 section）
    const cancelledCard = await screen
      .getByText('情报：多语言站点评测')
      .element()
    expect(
      cancelledCard?.closest("section[aria-label='已放弃']")
    ).not.toBeNull()
    expect(cancelledCard?.closest("section[aria-label='候选']")).toBeNull()

    // 候选卡落在「候选」栏内，且不串栏
    const candidateCard = await screen
      .getByText('情报：支付渠道调研')
      .element()
    expect(candidateCard?.closest("section[aria-label='候选']")).not.toBeNull()
    expect(
      candidateCard?.closest("section[aria-label='已放弃']")
    ).toBeNull()

    // cancelled 卡全页只渲染一次（不因双栏重复出现）
    expect(
      (screen.container.textContent?.match(/情报：多语言站点评测/g) ?? [])
        .length
    ).toBe(1)
  })

  it('INFERA-233 右栏空态：无 cancelled 行时右栏给出提示，不渲染放弃卡', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows().slice(0, 3))
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await expect
      .element(screen.getByText('还没有放弃的任务'))
      .toBeInTheDocument()
    expect(await screen.getByText('已取消', { exact: true }).query()).toBeNull()
  })

  it('INFERA-233 筛选语义不变：状态筛选先作用于全量再拆栏（选「已取消」全落右栏）', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '状态', exact: true }).click()
    await screen.getByRole('option', { name: '已取消', exact: true }).click()

    // 候选行被滤掉，cancelled 行保留在右栏
    expect(await screen.getByText('情报：支付渠道调研').query()).toBeNull()
    const cancelledCard = await screen
      .getByText('情报：多语言站点评测')
      .element()
    expect(
      cancelledCard?.closest("section[aria-label='已放弃']")
    ).not.toBeNull()
  })

  it('INFERA-233 分组先作用全量再拆栏：按状态分组时右栏沿用状态组头', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '分组', exact: true }).click()
    await screen.getByRole('option', { name: '按状态', exact: true }).click()

    // 左栏出现候选侧组头；右栏内 cancelled 卡带自己的状态组头「已取消」
    await expect
      .element(screen.getByRole('heading', { name: '进行中' }))
      .toBeInTheDocument()
    const cancelledCard = await screen
      .getByText('情报：多语言站点评测')
      .element()
    const rightCol = cancelledCard?.closest("section[aria-label='已放弃']")
    expect(rightCol?.textContent).toContain('已取消')
    // 左栏不含 cancelled 卡（候选栏文本里没有该标题）
    const candidateCard = await screen
      .getByText('情报：支付渠道调研')
      .element()
    const leftCol = candidateCard?.closest("section[aria-label='候选']")
    expect(leftCol?.textContent).not.toContain('情报：多语言站点评测')
  })

  it('空列表呈现空态', async () => {
    const screen = await mount()
    await expect
      .element(screen.getByText('还没有需求发现任务'))
      .toBeInTheDocument()
  })

  it('筛选无命中时给出可区分提示（数据非空但当前筛选为空）', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(mixedRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('情报：支付渠道调研'))
      .toBeInTheDocument()

    await screen.getByRole('combobox', { name: '状态', exact: true }).click()
    await screen.getByRole('option', { name: '未启动', exact: true }).click()

    expect(await screen.getByText('情报：支付渠道调研').query()).toBeNull()
    await expect
      .element(screen.getByText('没有匹配当前筛选的任务'))
      .toBeInTheDocument()
  })
})

/* ─── INFERA-250 双栏布局回归（几何断言）───────────────────────────────
 *
 * 根因：栏内卡片栈的隐式 auto 轨道按卡片 nowrap min-content 撑宽，
 * 卡片越出栏框——左栏卡压进右栏、右栏卡顶出视口（横向溢出）。
 * 这里用真实浏览器几何（getBoundingClientRect）钉住：两栏各自独立
 * 占位、卡片不越栏、不串栏；窄屏按既有断点退化为上下堆叠。
 */

/** 亚像素容差：布局取整误差留 0.5px */
const EPS = 0.5

/** 超长标题（nowrap min-content 远超栏宽，触发回归的驱动内容） */
const LONG_CANDIDATE_TITLE =
  '超长候选标题压力样本——支付渠道调研对账冲正差错追踪多币种结算与自动核销的完整链路描述'
const LONG_CANCELLED_TITLE =
  '超长已放弃标题压力样本——多语言站点评测本地化翻译质量抽检与发布门禁回归的完整链路描述'

/** 压力夹具：左右两栏各两张卡，含超长标题与超长标签 */
function overlapRows(): DiscoveryTaskRow[] {
  return [
    row({
      title: LONG_CANDIDATE_TITLE,
      labels: [
        { name: '情报', color: '#22c55e' },
        {
          name: '一个非常长的标签名称用于验证窄栏下标签行换行与截断的表现是否稳定',
          color: '#3b82f6',
        },
      ],
    }),
    row({
      id: 'd-000000000002',
      title: '候选：一键对账需求',
      status: 'completed',
      agent_types: ['analysis'],
      labels: [{ name: '候选', color: '#3b82f6' }],
      project_name: 'infera',
    }),
    row({
      id: 'd-000000000004',
      title: LONG_CANCELLED_TITLE,
      status: 'cancelled',
      agent_types: ['mining'],
      labels: [],
    }),
    row({
      id: 'd-000000000005',
      title: '情报：多语言站点评测',
      status: 'cancelled',
      agent_types: ['analysis'],
      labels: [],
    }),
  ]
}

interface Box {
  left: number
  right: number
  top: number
  bottom: number
}

/** 元素视口矩形（.element() 回传真实 DOM 节点，几何 API 可用） */
function boxOf(el: Element | null | undefined, what: string): Box {
  if (!el) throw new Error(`element not found: ${what}`)
  const r = el.getBoundingClientRect()
  return { left: r.left, right: r.right, top: r.top, bottom: r.bottom }
}

const fmt = (b: Box) =>
  `left=${b.left.toFixed(1)} right=${b.right.toFixed(1)} top=${b.top.toFixed(1)} bottom=${b.bottom.toFixed(1)}`

/** 卡片所属栏的 aria-label（沿 DOM 上溯 section[aria-label]） */
function columnOf(card: Element): string {
  return card.closest('section[aria-label]')?.getAttribute('aria-label') ?? ''
}

describe('DiscoveryPage 双栏布局（INFERA-250）', () => {
  afterEach(async () => {
    await cleanup()
    // 还原 vitest browser 默认视口，不污染同文件其他用例
    await page.viewport(414, 896)
  })

  it('宽屏两栏并排不压叠：栏几何互斥，卡片（含超长标题/长标签）不越出自己的栏', async () => {
    await page.viewport(1280, 900)
    vi.mocked(listDiscoveryTasks).mockResolvedValue(overlapRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('候选：一键对账需求'))
      .toBeInTheDocument()

    const candidateCol = boxOf(
      await screen.getByRole('region', { name: '候选' }).element(),
      '候选栏'
    )
    const cancelledCol = boxOf(
      await screen.getByRole('region', { name: '已放弃' }).element(),
      '已放弃栏'
    )

    // 宽屏（md+）两栏并排：左栏整体在右栏左侧，无水平交集
    expect(
      candidateCol.right,
      `候选栏 ${fmt(candidateCol)} 已放弃栏 ${fmt(cancelledCol)}`
    ).toBeLessThanOrEqual(cancelledCol.left + EPS)
    // 并排而非堆叠：右栏起点在左栏起点右侧
    expect(cancelledCol.left).toBeGreaterThan(candidateCol.left)

    // 每张卡都完整落在自己所属的栏框内（不越栏 → 不压叠、不横向溢出）
    const cards = await screen.getByRole('article').elements()
    expect(cards.length).toBe(4)
    for (const card of cards) {
      const label = columnOf(card)
      const col = label === '候选' ? candidateCol : cancelledCol
      const cardBox = boxOf(card, `卡片（${label}）`)
      expect(
        cardBox.left,
        `${label}栏卡片应落在栏内：card ${fmt(cardBox)} vs 栏 ${fmt(col)}`
      ).toBeGreaterThanOrEqual(col.left - EPS)
      expect(
        cardBox.right,
        `${label}栏卡片不应越出右边界：card ${fmt(cardBox)} vs 栏 ${fmt(col)}`
      ).toBeLessThanOrEqual(col.right + EPS)
    }

    // 双栏互斥口径（几何面）：左栏卡的右边界不越过右栏左边界
    const longCandidate = boxOf(
      (await screen.getByText(LONG_CANDIDATE_TITLE).element())?.closest(
        'article'
      ),
      '左栏超长标题卡'
    )
    expect(
      longCandidate.right,
      `左栏卡压进右栏：card ${fmt(longCandidate)} vs 右栏 left=${cancelledCol.left.toFixed(1)}`
    ).toBeLessThanOrEqual(cancelledCol.left + EPS)
  })

  it('窄屏退化为上下堆叠：两栏垂直互斥，卡片仍在各自栏内，无横向溢出', async () => {
    await page.viewport(375, 900)
    vi.mocked(listDiscoveryTasks).mockResolvedValue(overlapRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('候选：一键对账需求'))
      .toBeInTheDocument()

    const candidateCol = boxOf(
      await screen.getByRole('region', { name: '候选' }).element(),
      '候选栏'
    )
    const cancelledCol = boxOf(
      await screen.getByRole('region', { name: '已放弃' }).element(),
      '已放弃栏'
    )

    // 窄屏（md 以下）上下堆叠：候选栏整体在已放弃栏上方
    expect(
      candidateCol.bottom,
      `窄屏应上下堆叠：候选 ${fmt(candidateCol)} 已放弃 ${fmt(cancelledCol)}`
    ).toBeLessThanOrEqual(cancelledCol.top + EPS)

    // 卡片不越栏（含超长标题卡），即不产生横向溢出
    const cards = await screen.getByRole('article').elements()
    expect(cards.length).toBe(4)
    for (const card of cards) {
      const label = columnOf(card)
      const col = label === '候选' ? candidateCol : cancelledCol
      const cardBox = boxOf(card, `卡片（${label}）`)
      expect(
        cardBox.right,
        `${label}栏卡片不应越出栏右边界：card ${fmt(cardBox)} vs 栏 ${fmt(col)}`
      ).toBeLessThanOrEqual(col.right + EPS)
    }
  })

  it('栏头计数与该栏卡片数一致（左候选右已放各自的行数）', async () => {
    vi.mocked(listDiscoveryTasks).mockResolvedValue(overlapRows())
    const screen = await mount()
    await expect
      .element(screen.getByText('候选：一键对账需求'))
      .toBeInTheDocument()

    const cards = await screen.getByRole('article').elements()
    const perColumn = { 候选: 0, 已放弃: 0 }
    for (const card of cards) {
      const label = columnOf(card)
      if (label === '候选' || label === '已放弃') perColumn[label] += 1
    }

    // 栏头旁的计数 span（h2 的下一个兄弟）与几何归属的卡片数一致
    for (const label of ['候选', '已放弃'] as const) {
      const header = await screen
        .getByRole('heading', { name: label, exact: true })
        .element()
      const count = Number(header?.nextElementSibling?.textContent)
      expect(count, `「${label}」栏头计数应为 ${perColumn[label]}`).toBe(
        perColumn[label]
      )
    }
  })
})

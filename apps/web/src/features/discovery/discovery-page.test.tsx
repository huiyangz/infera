import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
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

/** 三行典型数据：纯挖掘 / 纯分析（已完成）/ 双标签（阻塞） */
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

    await expect
      .element(screen.getByText('需求挖掘', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('需求分析', { exact: true }))
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

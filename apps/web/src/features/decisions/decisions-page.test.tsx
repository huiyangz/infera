import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { listPendingDecisions } from './api'
import type { PendingDecisionRow } from './types'
import { DecisionsPage } from './decisions-page'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    listPendingDecisions: vi.fn(),
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

function row(over: Partial<PendingDecisionRow>): PendingDecisionRow {
  return {
    id: '9f2f9f34-1a2b-4c3d-8e9f-000000000001',
    project_id: 'd2836d4e-3b90-4808-adf4-c30be224eb1e',
    project_name: '自动闭环',
    title: '示例：卡在规格审批的需求',
    status: 'active',
    pending_gate: 'spec_approval',
    current_stage: 'spec',
    multica_issue_key: 'INFERA-108',
    assignee: 'agent:7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
    priority: '',
    created_at: '2026-08-22T05:00:00Z',
    updated_at: '2026-08-22T05:10:00Z',
    ...over,
  }
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：Header 依赖 SidebarProvider 上下文（与 requirements-list 测试同构）
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <DecisionsPage />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listPendingDecisions).mockResolvedValue([])
})

afterEach(async () => {
  await cleanup()
})

describe('DecisionsPage 需要决策列表', () => {
  it('拉取并渲染待决策行：标题 / 项目名 / 待决策门，行链接跳既有需求详情', async () => {
    vi.mocked(listPendingDecisions).mockResolvedValue([
      row({}),
      row({
        id: '9f2f9f34-1a2b-4c3d-8e9f-000000000002',
        title: '本地需求等设计审批',
        project_name: 'infera',
        pending_gate: 'design_approval',
        multica_issue_key: '',
      }),
    ])
    const screen = await mount()
    await expect
      .element(screen.getByText('示例：卡在规格审批的需求'))
      .toBeInTheDocument()
    await expect.element(screen.getByText('本地需求等设计审批')).toBeInTheDocument()
    // 项目名标注（跨项目全局列表口径）
    await expect.element(screen.getByText('自动闭环')).toBeInTheDocument()
    await expect
      .element(screen.getByText('infera', { exact: true }))
      .toBeInTheDocument()
    // 待决策门以中文阶段标签展示（exact：标题文本里会含门名子串）
    await expect
      .element(screen.getByText('规格审批', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('设计审批', { exact: true }))
      .toBeInTheDocument()
    // 行内 id 即 delivery ID → 跳既有需求详情页
    expect(
      (await screen.getByText('示例：卡在规格审批的需求').element()).closest('a')
        ?.getAttribute('href')
    ).toBe('/deliveries/9f2f9f34-1a2b-4c3d-8e9f-000000000001')
    expect(
      (await screen.getByText('本地需求等设计审批').element()).closest('a')
        ?.getAttribute('href')
    ).toBe('/deliveries/9f2f9f34-1a2b-4c3d-8e9f-000000000002')
  })

  it('multica 来源键随行展示（本地需求不展示）', async () => {
    vi.mocked(listPendingDecisions).mockResolvedValue([
      row({}),
      row({
        id: '9f2f9f34-1a2b-4c3d-8e9f-000000000002',
        title: '本地需求',
        multica_issue_key: '',
      }),
    ])
    const screen = await mount()
    await expect.element(screen.getByText('INFERA-108')).toBeInTheDocument()
    expect(screen.container.textContent?.match(/INFERA-\d+/g)?.length).toBe(1)
  })

  it('空列表呈现空态', async () => {
    const screen = await mount()
    await expect
      .element(screen.getByText('当前没有等待决策的任务'))
      .toBeInTheDocument()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { getRequirement, listRequirementAudit } from './api'
import type { RequirementDetail } from './types'
import { RequirementDetailPage } from './requirement-detail'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    getRequirement: vi.fn(),
    listRequirementAudit: vi.fn(),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@tanstack/react-router')>()
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

function detail(over: Partial<RequirementDetail> = {}): RequirementDetail {
  return {
    id: 'r1',
    title: '深色模式',
    description: '全站适配深色主题',
    acceptance_criteria: '主要页面无可读性问题',
    source: 'web',
    priority: 'P1',
    acceptors: ['张三', '李四'],
    multica_issue_id: 'm1',
    multica_issue_key: 'INFERA-31',
    multica_issue_url: 'http://multica.local/infera/issues/m1',
    pr_url: 'https://github.com/acme/repo/pull/7',
    node: 'in_review',
    created_at: '2026-08-21T00:00:00Z',
    updated_at: '2026-08-21T01:00:00Z',
    pending_cards: [],
    ...over,
  }
}

async function mount(pollMs = 60_000) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <RequirementDetailPage requirementId='r1' pollMs={pollMs} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listRequirementAudit).mockResolvedValue([])
})

afterEach(async () => {
  await cleanup()
})

describe('RequirementDetailPage 需求详情', () => {
  it('渲染需求信息、时间线、深链（Multica issue / PR 新窗口）与审计记录', async () => {
    vi.mocked(getRequirement).mockResolvedValue(detail())
    vi.mocked(listRequirementAudit).mockResolvedValue([
      {
        id: 'a1',
        requirement_id: 'r1',
        actor: 'user',
        action: 'approve',
        detail: 'approved',
        at: '2026-08-21T03:00:00Z',
      },
    ])
    const screen = await mount()
    // 标题出现在面包屑与信息卡两处，取其一断言存在即可
    await expect
      .element(screen.getByText('深色模式').first())
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('全站适配深色主题'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('主要页面无可读性问题'))
      .toBeInTheDocument()
    // 时间线主线节点（面包屑徽标亦有同文案，断言时间线内该节点存在）
    await expect
      .element(screen.getByLabelText('大节点时间线').getByText('待验收'))
      .toBeInTheDocument()
    // 验收人
    await expect.element(screen.getByText('张三、李四')).toBeInTheDocument()
    // 深链：Multica issue 与 PR，均新窗口
    const issue = screen.getByRole('link', { name: /INFERA-31/ })
    expect((await issue.element()).getAttribute('href')).toBe(
      'http://multica.local/infera/issues/m1'
    )
    expect((await issue.element()).getAttribute('target')).toBe('_blank')
    const pr = screen.getByRole('link', { name: /pull\/7/ })
    expect((await pr.element()).getAttribute('href')).toBe(
      'https://github.com/acme/repo/pull/7'
    )
    // 审计：动作中文化 + detail 原样
    await expect
      .element(screen.getByText(/批准/))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/张三、李四/))
      .toBeInTheDocument()
  })

  it('待处理卡按 kind 分发渲染（approval → 审批卡含批准按钮）', async () => {
    vi.mocked(getRequirement).mockResolvedValue(
      detail({
        pending_cards: [
          {
            id: 'c1',
            requirement_id: 'r1',
            kind: 'approval',
            status: 'pending',
            payload: '待批准：实现计划',
            comment_id: 'cm1',
            created_at: '2026-08-21T02:00:00Z',
            resolved_at: null,
          },
        ],
      })
    )
    const screen = await mount()
    await expect
      .element(screen.getByText('待批准：实现计划'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '批准' }))
      .toBeInTheDocument()
  })

  it('无待处理卡时呈现空态提示', async () => {
    vi.mocked(getRequirement).mockResolvedValue(detail())
    const screen = await mount()
    await expect
      .element(screen.getByText('暂无待处理事项'))
      .toBeInTheDocument()
  })

  it('404 时呈现需求不存在提示（后端 error 文案不吞）', async () => {
    const { ApiError } = await import('@/lib/infera-api')
    vi.mocked(getRequirement).mockRejectedValue(
      new ApiError(404, '资源不存在')
    )
    const screen = await mount()
    await expect
      .element(screen.getByText(/需求不存在或已删除/))
      .toBeInTheDocument()
  })

  it('轮询：按 pollMs 周期重新拉取详情', async () => {
    vi.mocked(getRequirement).mockResolvedValue(detail())
    await mount(100)
    await vi.waitFor(
      () =>
        expect(vi.mocked(getRequirement).mock.calls.length).toBeGreaterThanOrEqual(3),
      { timeout: 5_000 }
    )
  })
})

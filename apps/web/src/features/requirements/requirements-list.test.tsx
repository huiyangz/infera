import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { createRequirement, listRequirements } from './api'
import type { RequirementListItem } from './types'
import { RequirementsList } from './requirements-list'

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    listRequirements: vi.fn(),
    createRequirement: vi.fn(),
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
    <a
      href={(to ?? '#').replace('$id', params?.id ?? '')}
      {...props}
    >
      {children}
    </a>
  )
  return { ...actual, Link: MockLink }
})

function item(over: Partial<RequirementListItem>): RequirementListItem {
  return {
    id: 'r1',
    title: '深色模式',
    description: '',
    acceptance_criteria: '',
    source: 'web',
    priority: 'P1',
    acceptors: [],
    external_issue_id: 'm1',
    external_issue_key: 'INFERA-31',
    external_issue_url: '',
    pr_url: '',
    node: 'dispatched',
    created_at: '2026-08-21T00:00:00Z',
    updated_at: '2026-08-21T01:00:00Z',
    pending_card_count: 0,
    ...over,
  }
}

async function mount(pollMs = 60_000) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 真实布局：Header 依赖 SidebarProvider 上下文（与 project-detail 测试同构）
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <RequirementsList pollMs={pollMs} />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listRequirements).mockResolvedValue([])
})

afterEach(async () => {
  await cleanup()
})

describe('RequirementsList 需求列表', () => {
  it('渲染需求行：标题 / 大节点 / 待处理卡计数，行链接指向详情', async () => {
    vi.mocked(listRequirements).mockResolvedValue([
      item({ id: 'r1', title: '深色模式', node: 'in_review', pending_card_count: 2 }),
      item({ id: 'r2', title: '导出报表', node: 'needs_decision', pending_card_count: 1 }),
    ])
    const screen = await mount()
    await expect.element(screen.getByText('深色模式')).toBeInTheDocument()
    await expect.element(screen.getByText('导出报表')).toBeInTheDocument()
    await expect.element(screen.getByText('待验收')).toBeInTheDocument()
    await expect.element(screen.getByText('需决策')).toBeInTheDocument()
    await expect
      .element(screen.getByText('2 张待处理卡'))
      .toBeInTheDocument()
    expect(
      (await screen.getByText('深色模式').element()).closest('a')?.getAttribute('href')
    ).toBe('/requirements/r1')
  })

  it('空列表呈现引导空态', async () => {
    const screen = await mount()
    await expect
      .element(screen.getByText('还没有任务'))
      .toBeInTheDocument()
  })

  it('轮询：按 pollMs 周期重新拉取列表', async () => {
    vi.mocked(listRequirements).mockResolvedValue([item({})])
    await mount(100)
    await vi.waitFor(
      () => expect(vi.mocked(listRequirements).mock.calls.length).toBeGreaterThanOrEqual(3),
      { timeout: 5_000 }
    )
  })
})

describe('发起任务表单', () => {
  it('标题必填：空时提交禁用', async () => {
    const screen = await mount()
    await screen.getByRole('button', { name: '发起任务' }).click()
    await expect
      .element(screen.getByRole('button', { name: '提交任务' }))
      .toBeDisabled()
  })

  it('提交契约：全字段 + 验收人按分隔符切分为数组，成功后关闭对话框', async () => {
    vi.mocked(createRequirement).mockResolvedValue(item({}))
    const screen = await mount()
    await screen.getByRole('button', { name: '发起任务' }).click()
    await screen.getByLabelText('标题').fill('深色模式')
    await screen.getByLabelText('描述').fill('全站适配深色主题')
    await screen
      .getByLabelText('验收标准')
      .fill('切换主题后主要页面无可读性问题')
    await screen.getByLabelText('来源').fill('web')
    await screen.getByLabelText('优先级').fill('P1')
    await screen.getByLabelText('验收人').fill('张三、李四, 王五')
    await screen.getByRole('button', { name: '提交任务' }).click()
    await vi.waitFor(() =>
      expect(createRequirement).toHaveBeenCalledWith({
        title: '深色模式',
        description: '全站适配深色主题',
        acceptance_criteria: '切换主题后主要页面无可读性问题',
        source: 'web',
        priority: 'P1',
        acceptors: ['张三', '李四', '王五'],
      })
    )
    // 成功后对话框关闭：表单输入随之脱离文档
    await vi.waitFor(() =>
      expect(screen.container.querySelector('#req-title')).toBeNull()
    )
  })
})
